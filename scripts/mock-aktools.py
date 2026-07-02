#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
"""
mock-aktools — a local stand-in for the akshare sidecar (aktools).

Why this exists
---------------
The three built-in free A-share sources (eastmoney / sina / akshare)
are all unreliable from a developer machine: eastmoney throttles rapid
requests (RemoteDisconnected after the first hit), sina times out, and
the real akshare sidecar needs `pip install aktools` + a running Python
service. That makes local end-to-end testing of the selection pipeline
impossible against live data.

This server speaks the minimal akshare wire contract the Go adapter
expects (see internal/datasource/source/akshare/akshare.go):

    GET /quote?code=600519.SH
        -> {"code","name","price","open","high","low","prev_close","volume","amount"}
    GET /kline?code=...&period=1d&start=YYYY-MM-DD&end=YYYY-MM-DD
        -> {"klines":[{"day","open","high","low","close","volume","amount"}, ...]}
    GET /fundamental?code=...   -> {"code","name","pe","pb","roe",...}
    GET /news?code=...&limit=N  -> {"news":[...]}

The data is DETERMINISTIC per code (seeded by the code) and trends
gently upward with noise, so MACD/MA indicators produce a positive
histogram → the macd_golden_cross template actually yields picks.

Usage:
    python3 scripts/mock-aktools.py            # listens on :18800
    MOCK_PORT=18801 python3 scripts/mock-aktools.py

Point the akshare source at it (already the default base_url) and put
akshare first in the local datasource chain for fast, reliable runs.
NOT for production — deterministic fake data only.
"""
import json
import math
import os
import random
from datetime import date, timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

PORT = int(os.environ.get("MOCK_PORT", "18800"))


def _seed(code: str) -> int:
    return sum(ord(c) for c in code)


def _name(code: str) -> str:
    names = {
        "600519": "贵州茅台", "000858": "五粮液", "601318": "中国平安",
        "000001": "平安银行", "300750": "宁德时代",
    }
    return names.get(code.split(".")[0], "MOCK-" + code)


def gen_klines(code: str, start: str, end: str):
    """Deterministic gently-rising OHLCV series with intraweek noise."""
    rng = random.Random(_seed(code))
    try:
        d0 = date.fromisoformat(start)
        d1 = date.fromisoformat(end)
    except ValueError:
        d1 = date.today()
        d0 = d1 - timedelta(days=60)
    if d1 < d0:
        d0, d1 = d1, d0
    base = 20.0 + (_seed(code) % 80)  # per-code starting price
    out = []
    day = d0
    i = 0
    while day <= d1:
        # Emit a bar for EVERY calendar day. The manager fetches
        # klines one day at a time (manager.GetKLine -> splitByDay),
        # so a weekend gap would make a per-day call return [] -> the
        # adapter maps that to ErrNotImplemented -> the whole fetch
        # fails "partial". Serving every day keeps each per-day call
        # non-empty, which is what the per-day fetch loop needs.
        # Accelerating (convex) uptrend so the latest MACD histogram
        # is POSITIVE (DIF pulls above DEA) — otherwise a purely
        # linear ramp converges DIF≈DEA→hist≈0 and the
        # macd_golden_cross filter (macd_hist > 0) matches nothing.
        trend = 0.04 * i + 0.0009 * i * i     # convex up → +MACD hist
        wave = 1.2 * math.sin(i / 5.0)         # mild oscillation
        noise = rng.uniform(-0.3, 0.3)
        close = round(base + trend + wave + noise, 2)
        openp = round(close - rng.uniform(-0.4, 0.4), 2)
        high = round(max(openp, close) + rng.uniform(0.1, 0.8), 2)
        low = round(min(openp, close) - rng.uniform(0.1, 0.8), 2)
        vol = 1_000_000 + rng.randint(0, 500_000) + i * 1000
        out.append({
            "day": day.isoformat(),
            "open": openp, "high": high, "low": low, "close": close,
            "volume": vol, "amount": round(close * vol, 2),
        })
        i += 1
        day += timedelta(days=1)
    return out


class Handler(BaseHTTPRequestHandler):
    def _send(self, obj, status=200):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *a):  # quiet
        pass

    def do_GET(self):
        u = urlparse(self.path)
        q = parse_qs(u.query)
        code = (q.get("code", [""])[0])
        if u.path == "/kline":
            start = q.get("start", ["2026-03-01"])[0]
            end = q.get("end", ["2026-06-23"])[0]
            self._send({"klines": gen_klines(code, start, end)})
        elif u.path == "/quote":
            kl = gen_klines(code, "2026-06-01", "2026-06-23")
            last = kl[-1] if kl else {"close": 0, "open": 0, "high": 0, "low": 0, "volume": 0, "amount": 0}
            prev = kl[-2]["close"] if len(kl) > 1 else last["close"]
            self._send({
                "code": code, "name": _name(code), "price": last["close"],
                "open": last["open"], "high": last["high"], "low": last["low"],
                "prev_close": prev, "volume": last["volume"], "amount": last["amount"],
            })
        elif u.path == "/fundamental":
            rng = random.Random(_seed(code))
            self._send({
                "code": code, "name": _name(code),
                "pe": round(rng.uniform(8, 40), 2), "pb": round(rng.uniform(1, 8), 2),
                "roe": round(rng.uniform(5, 25), 2),
                "dividend_yield": round(rng.uniform(0.5, 4), 2),
                "revenue": rng.randint(10**9, 10**11),
                "net_profit": rng.randint(10**8, 10**10),
            })
        elif u.path == "/news":
            self._send({"news": []})
        elif u.path in ("/", "/health", "/healthz"):
            self._send({"status": "ok", "mock": True})
        else:
            self._send({"error": "not found", "path": u.path}, status=404)

    def do_POST(self):
        u = urlparse(self.path)
        # Drain the request body so the client sees a clean response.
        try:
            n = int(self.headers.get("Content-Length", "0"))
            if n > 0:
                self.rfile.read(n)
        except (ValueError, TypeError):
            pass
        if u.path == "/feishu":
            # Feishu webhook success contract: 2xx + StatusCode 0.
            self._send({"StatusCode": 0, "StatusMessage": "success"})
        else:
            self._send({"ok": True})


if __name__ == "__main__":
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    print(f"mock-aktools listening on http://127.0.0.1:{PORT} (deterministic fake A-share data)")
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        srv.shutdown()
