#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
"""
akshare-sidecar — the production akshare data sidecar.

WHY THIS EXISTS
---------------
The Go akshare adapter (internal/datasource/source/akshare) speaks a
small custom HTTP contract:

    GET /kline?code=600519.SH&period=1d&start=YYYY-MM-DD&end=YYYY-MM-DD
        -> {"klines":[{"day","open","high","low","close","volume","amount"}, ...]}
    GET /quote?code=600519.SH
        -> {"code","name","price","open","high","low","prev_close","volume","amount"}
    GET /fundamental?code=600519.SH
        -> {"code","name","pe","pb",...}
    GET /news?code=600519.SH&limit=10
        -> {"news":[{"title","content","url","published_at"}, ...]}
           (published_at is RFC3339 with +08:00 — Go parses time.Time)
    GET /health -> {"status":"ok"}

aktools' own API is generic (/api/public/<akshare_func>) and does NOT
match that contract, so this thin wrapper bridges the two: it calls
akshare directly and reshapes the result into the adapter's format.

IMPORTANT — source choice: this box (a Tencent Cloud IP) is blocked by
eastmoney's push endpoints (RemoteDisconnected), but the SINA backend
(akshare.stock_zh_a_daily) works fine. So klines use stock_zh_a_daily
(sina). If you deploy somewhere eastmoney is reachable, stock_zh_a_hist
is an alternative.

Usage:
    AKSHARE_SIDECAR_PORT=18800 /home/ubuntu/aktools-venv/bin/python scripts/akshare-sidecar.py

Real market data — safe for production. Read-only public quotes.
"""
import json
import os
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

import akshare as ak

PORT = int(os.environ.get("AKSHARE_SIDECAR_PORT", "18800"))

# Cache the whole-market spot snapshot — it's ~5500 rows / ~25s to
# fetch, so a short TTL turns a universe quote run into one cheap call.
_SPOT_CACHE = {"ts": 0.0, "rows": None}
_SPOT_TTL = 300.0  # seconds — name is stable; price lags ~5min, fine for a holdings view


def to_sina_symbol(code: str) -> str:
    """600519.SH -> sh600519 ; 000001.SZ -> sz000001 ; bare -> infer."""
    c = code.strip().upper()
    m = re.match(r"^(\d{6})(?:\.(SH|SZ|BJ))?$", c)
    if not m:
        return ""
    digits, suffix = m.group(1), m.group(2)
    if suffix == "SH":
        return "sh" + digits
    if suffix == "SZ":
        return "sz" + digits
    if suffix == "BJ":
        return "bj" + digits
    # infer from leading digit
    d0 = digits[0]
    if d0 in "69":
        return "sh" + digits
    if d0 in "03":
        return "sz" + digits
    if d0 in "48":
        return "bj" + digits
    return ""


def fetch_klines(code: str, start: str, end: str):
    sym = to_sina_symbol(code)
    if not sym:
        return []
    s = (start or "").replace("-", "") or "20200101"
    e = (end or "").replace("-", "") or "20990101"
    # sina rate-limits bursts (a universe run fetches many stocks back to
    # back). Retry a few times with backoff so transient throttling
    # doesn't blank a stock; small inter-try sleep spreads the load.
    import time as _t
    last = None
    for attempt in range(3):
        try:
            df = ak.stock_zh_a_daily(symbol=sym, start_date=s, end_date=e, adjust="qfq")
            out = []
            for _, r in df.iterrows():
                day = str(r["date"])[:10]
                out.append({
                    "day": day,
                    "open": float(r["open"]),
                    "high": float(r["high"]),
                    "low": float(r["low"]),
                    "close": float(r["close"]),
                    "volume": int(r["volume"]),
                    "amount": float(r["amount"]),
                })
            return out
        except Exception as ex:  # noqa: BLE001 — surface after retries
            last = ex
            _t.sleep(0.6 * (attempt + 1))
    raise last if last else RuntimeError("kline fetch failed")


def _digits(code: str) -> str:
    m = re.match(r"^(\d{6})", code.strip())
    return m.group(1) if m else ""


def to_canonical(code: str) -> str:
    """Normalize a bare 6-digit ('600519') or sina-prefixed
    ('sh600519') code to the canonical 'NNNNNN.SH' form the templates
    use. Returns '' if it can't be parsed."""
    c = str(code).strip().lower()
    m = re.match(r"^(sh|sz|bj)?(\d{6})$", c)
    if not m:
        return ""
    prefix, d = m.group(1), m.group(2)
    if prefix == "sh":
        return d + ".SH"
    if prefix == "sz":
        return d + ".SZ"
    if prefix == "bj":
        return d + ".BJ"
    # bare: infer from leading digit
    if d[0] in "69":
        return d + ".SH"
    if d[0] in "03":
        return d + ".SZ"
    if d[0] in "48":
        return d + ".BJ"
    return ""


# Universe key → akshare index symbol (index_stock_cons, sina-safe).
_INDEX_UNIVERSES = {
    "hs300": "000300",  # 沪深300
    "sse50": "000016",  # 上证50
    "zz500": "000905",  # 中证500
    "zz1000": "000852", # 中证1000
}


def fetch_universe(key: str, limit: int):
    """Resolve a universe key to a list of canonical codes.
    - hs300/sse50/zz500/zz1000 → index_stock_cons (品种代码, bare 6-digit)
    - all                      → stock_zh_a_spot (代码, sina-prefixed)
    limit (>0) caps the result.
    """
    k = (key or "").strip().lower()
    codes = []
    if k in _INDEX_UNIVERSES:
        df = ak.index_stock_cons(symbol=_INDEX_UNIVERSES[k])
        col = "品种代码" if "品种代码" in df.columns else df.columns[0]
        codes = [to_canonical(x) for x in df[col].tolist()]
    elif k == "all":
        # Reuse the cached whole-market spot snapshot (kept warm by
        # _prewarm_spot) instead of re-fetching stock_zh_a_spot (~25s),
        # which exceeded the datasource timeout and failed universe=all
        # (data_source news node → "one or more nodes failed").
        codes = list(_spot_rows().keys())
    else:
        return None  # unknown key
    codes = [c for c in codes if c]
    if limit and limit > 0:
        codes = codes[:limit]
    return codes


def _spot_rows():
    """Whole-market spot snapshot as {canonical_code: quote_dict},
    cached for _SPOT_TTL. One ~25s fetch covers every A-share, so a
    universe quote run is a single call instead of N per-stock calls."""
    import time as _t
    now = _t.time()
    if _SPOT_CACHE["rows"] is not None and (now - _SPOT_CACHE["ts"]) < _SPOT_TTL:
        return _SPOT_CACHE["rows"]
    df = ak.stock_zh_a_spot()
    rows = {}
    for _, r in df.iterrows():
        canon = to_canonical(str(r.get("代码", "")))
        if not canon:
            continue
        price = float(r.get("最新价", 0) or 0)
        rows[canon] = {
            "stock_code": canon,
            "name": str(r.get("名称", "")),
            "price": price,
            "open": float(r.get("今开", 0) or 0),
            "high": float(r.get("最高", 0) or 0),
            "low": float(r.get("最低", 0) or 0),
            "prev_close": float(r.get("昨收", 0) or 0),
            "volume": int(float(r.get("成交量", 0) or 0)),
            "amount": float(r.get("成交额", 0) or 0),
            "pct_change": float(r.get("涨跌幅", 0) or 0),
        }
    _SPOT_CACHE["rows"] = rows
    _SPOT_CACHE["ts"] = now
    return rows


def fetch_spot_batch(codes):
    """Return quote dicts for the given canonical codes from the cached
    whole-market snapshot. Unknown codes are skipped."""
    rows = _spot_rows()
    out = []
    for c in codes:
        q = rows.get(c)
        if q:
            out.append(q)
    return out


def fetch_fundamental(code: str) -> dict:
    """PE/PB from baidu, ROE from sina, dividend_yield computed from the
    trailing-12-month cash dividend / latest close. Each metric is
    guarded so one failing upstream doesn't blank the whole row. name
    is required by the Go adapter (empty name => ErrNotImplemented), so
    fall back to the code itself."""
    import datetime as _dt
    d = _digits(code)
    if not d:
        return {}
    out = {"code": code, "name": code, "pe": 0.0, "pb": 0.0,
           "roe": 0.0, "dividend_yield": 0.0}

    def _baidu(ind):
        df = ak.stock_zh_valuation_baidu(symbol=d, indicator=ind, period="近一年")
        return float(df["value"].iloc[-1])

    try:
        out["pe"] = _baidu("市盈率(TTM)")
    except Exception:
        pass
    try:
        out["pb"] = _baidu("市净率")
    except Exception:
        pass
    try:
        df = ak.stock_financial_analysis_indicator(symbol=d, start_year="2024")
        roe = df.iloc[-1].get("净资产收益率(%)")
        if roe is not None and str(roe) != "nan":
            out["roe"] = float(roe)
    except Exception:
        pass
    try:
        # TTM cash dividend per share = sum(派息 in last 12 months) / 10.
        # 派息 is RMB per 10 shares. Yield = div_per_share / close * 100.
        dd = ak.stock_history_dividend_detail(symbol=d, indicator="分红")
        cutoff = (_dt.date.today() - _dt.timedelta(days=365)).isoformat()
        paid = 0.0
        for _, r in dd.iterrows():
            ex = str(r.get("除权除息日") or "")
            pay = r.get("派息")
            if ex and ex >= cutoff and pay is not None and str(pay) != "nan":
                paid += float(pay)
        if paid > 0:
            kl = fetch_klines(code, "", "")
            close = kl[-1]["close"] if kl else 0.0
            if close > 0:
                out["dividend_yield"] = round((paid / 10.0) / close * 100.0, 4)
    except Exception:
        pass
    return out


def _to_rfc3339(s: str) -> str:
    """akshare news timestamps look like '2026-06-21 18:05:00' (CST, no
    zone). The Go adapter unmarshals published_at into time.Time, which
    defaults to RFC3339 — so we MUST hand it '2026-06-21T18:05:00+08:00'
    or the whole GetNews response fails to parse. Returns '' on a value
    we can't parse (the Go side then gets a zero time, not an error)."""
    import datetime as _dt
    s = str(s or "").strip()
    if not s:
        return ""
    for fmt in ("%Y-%m-%d %H:%M:%S", "%Y-%m-%d %H:%M", "%Y-%m-%d"):
        try:
            dt = _dt.datetime.strptime(s, fmt)
            # Pin to China Standard Time (UTC+8); akshare feeds are CST.
            cst = _dt.timezone(_dt.timedelta(hours=8))
            return dt.replace(tzinfo=cst).isoformat()
        except ValueError:
            continue
    return ""


def fetch_news(code: str, limit: int):
    """Per-stock news via akshare.stock_news_em (eastmoney-backed but the
    NEWS endpoint is reachable from this IP, unlike the quote/push ones).
    Maps to the Go adapter's {title, content, url, published_at} shape.
    Returns [] on no data / failure (the Go adapter treats empty as
    ErrNotImplemented and the manager falls through)."""
    d = _digits(code)
    if not d:
        return []
    try:
        df = ak.stock_news_em(symbol=d)
    except Exception:
        return []
    if df is None or not hasattr(df, "iterrows"):
        return []
    out = []
    for _, r in df.iterrows():
        title = str(r.get("新闻标题", "") or "")
        if not title:
            continue
        out.append({
            "title": title,
            "content": str(r.get("新闻内容", "") or ""),
            "url": str(r.get("新闻链接", "") or ""),
            "published_at": _to_rfc3339(r.get("发布时间", "")),
        })
        if limit and limit > 0 and len(out) >= limit:
            break
    return out


class Handler(BaseHTTPRequestHandler):
    def _send(self, obj, status=200):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *a):
        try:
            print("REQ " + (fmt % a), flush=True)
        except Exception:
            pass

    def do_GET(self):
        u = urlparse(self.path)
        q = parse_qs(u.query)
        code = q.get("code", [""])[0]
        try:
            if u.path == "/kline":
                start = q.get("start", [""])[0]
                end = q.get("end", [""])[0]
                self._send({"klines": fetch_klines(code, start, end)})
            elif u.path == "/quote":
                # Derive a "quote" from the latest daily bar (spot_em is
                # eastmoney-backed and blocked from this IP). The stock
                # NAME comes from the whole-market spot snapshot
                # (_spot_rows → akshare stock_zh_a_spot 名称列): klines
                # carry no name column, so without this the Go side got
                # name="" and recommendations showed code-only.
                kl = fetch_klines(code, "", "")
                last = kl[-1] if kl else None
                prev = kl[-2]["close"] if len(kl) > 1 else (last["close"] if last else 0)
                spot = _spot_rows().get(code) if code else None
                nm = (spot or {}).get("name", "")
                if last:
                    self._send({
                        "code": code, "name": nm, "price": last["close"],
                        "open": last["open"], "high": last["high"], "low": last["low"],
                        "prev_close": prev, "volume": last["volume"], "amount": last["amount"],
                    })
                else:
                    self._send({"code": code, "name": nm, "price": 0})
            elif u.path == "/fundamental":
                f = fetch_fundamental(code)
                self._send(f if f else {"code": code, "name": ""})
            elif u.path == "/universe":
                key = q.get("key", [""])[0]
                limit = int(q.get("limit", ["0"])[0] or "0")
                codes = fetch_universe(key, limit)
                if codes is None:
                    self._send({"error": "unknown universe key", "key": key}, status=400)
                else:
                    self._send({"key": key, "count": len(codes), "codes": codes})
            elif u.path == "/spot":
                raw = q.get("codes", [""])[0]
                codes = [c.strip() for c in raw.split(",") if c.strip()]
                self._send({"quotes": fetch_spot_batch(codes)})
            elif u.path == "/news":
                limit = int(q.get("limit", ["10"])[0] or "10")
                self._send({"news": fetch_news(code, limit)})
            elif u.path in ("/", "/health", "/healthz"):
                self._send({"status": "ok", "sidecar": "akshare", "source": "sina"})
            else:
                self._send({"error": "not found", "path": u.path}, status=404)
        except Exception as e:
            # Surface as 502 so the adapter records a clean source failure.
            self._send({"error": type(e).__name__, "detail": str(e)[:200]}, status=502)


# _prewarm_spot keeps the whole-market spot cache warm in the background
# so /quote never pays the ~25s stock_zh_a_spot fetch on a cold/expired
# cache — that exceeded the datasource's 5s timeout and left recommendations
# name-less. Name is stable; price lags up to _SPOT_TTL, acceptable for a
# holdings/recommendations view.
def _prewarm_spot():
    import time as _t
    while True:
        try:
            _spot_rows()
        except Exception:
            pass
        _t.sleep(_SPOT_TTL * 0.8)


if __name__ == "__main__":
    import threading
    threading.Thread(target=_prewarm_spot, daemon=True).start()
    # Bind 0.0.0.0, NOT 127.0.0.1: in the multi-container prod topology
    # afd-app reaches us over the docker network as sidecar:PORT. A
    # loopback bind is only reachable INSIDE this container, which left
    # the akshare quote source silently "connection refused" (positions
    # showed empty current_price / P&L) while the /health probe — also
    # hitting 127.0.0.1 — still passed, masking the break.
    srv = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    print(f"akshare-sidecar listening on http://0.0.0.0:{PORT} (akshare/sina backend)")
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        srv.shutdown()
