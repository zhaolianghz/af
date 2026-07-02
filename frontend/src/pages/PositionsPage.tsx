// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  listPositions,
  updatePosition,
  closePosition,
  type PositionView,
  type PositionSummary,
} from '@/api/positions';
import { notifyError } from '@/lib/notify';

// Auto-refresh cadence for live quotes. The backend computes current
// price/P&L on every GET /positions (quote cache TTL ~5s), so polling
// here is what keeps the page "live" while you leave it open.
const REFRESH_MS = 30_000;

function fmtMoney(v: number | null | undefined): string {
  if (v === null || v === undefined) return '—';
  return v.toLocaleString('zh-CN', { maximumFractionDigits: 2 });
}

function fmtPct(v: number | null | undefined): string {
  if (v === null || v === undefined) return '—';
  return `${(v * 100).toFixed(2)}%`;
}

function pnlClass(v: number | null | undefined): string {
  if (v === null || v === undefined) return 'text-slate-400';
  if (v > 0) return 'text-red-600';
  if (v < 0) return 'text-green-600';
  return 'text-slate-600';
}

export default function PositionsPage(): JSX.Element {
  const [items, setItems] = useState<PositionView[]>([]);
  const [summary, setSummary] = useState<PositionSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    listPositions()
      .then((r) => {
        setItems(r.items);
        setSummary(r.summary);
        setLastUpdated(new Date());
      })
      .catch((e) => notifyError(e, '加载持仓失败'))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // Auto-refresh live quotes every REFRESH_MS while the tab is visible.
  // Pause when hidden (no point burning requests in the background) and
  // refresh once immediately on regaining visibility. loadRef keeps the
  // latest load without re-arming the timer on every render.
  const loadRef = useRef(load);
  loadRef.current = load;
  useEffect(() => {
    let timer: number | undefined;
    const stop = () => {
      if (timer !== undefined) {
        window.clearInterval(timer);
        timer = undefined;
      }
    };
    const start = () => {
      if (timer === undefined && !document.hidden) {
        timer = window.setInterval(() => loadRef.current(), REFRESH_MS);
      }
    };
    const onVis = () => {
      if (document.hidden) {
        stop();
      } else {
        loadRef.current();
        start();
      }
    };
    start();
    document.addEventListener('visibilitychange', onVis);
    return () => {
      stop();
      document.removeEventListener('visibilitychange', onVis);
    };
  }, []);

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">持仓 / Positions</h1>
          <p className="mt-1 text-sm text-slate-500">
            手工维护成本价与数量；现价与盈亏实时来自行情源（每 {REFRESH_MS / 1000}{' '}
            秒自动刷新）。红涨绿跌（A 股习惯）。
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-3 pt-1 text-xs text-slate-500">
          {lastUpdated && <span>最后更新 {lastUpdated.toLocaleTimeString('zh-CN')}</span>}
          <button
            type="button"
            onClick={() => load()}
            disabled={loading}
            className="rounded-lg border border-slate-200 bg-white px-3 py-1.5 font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-60"
          >
            ↻ 刷新
          </button>
        </div>
      </div>

      <SummaryCards summary={summary} />
      <PositionsTable items={items} loading={loading} onChanged={load} />
    </div>
  );
}

function SummaryCards({ summary }: { summary: PositionSummary | null }): JSX.Element {
  const cards = [
    { title: '持仓数', value: summary ? String(summary.count) : '—' },
    { title: '总成本', value: summary ? fmtMoney(summary.total_cost_value) : '—' },
    { title: '总市值', value: summary ? fmtMoney(summary.total_market_value) : '—' },
    {
      title: '总盈亏',
      value: summary ? fmtMoney(summary.total_pnl) : '—',
      cls: pnlClass(summary?.total_pnl),
    },
  ];
  return (
    <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
      {cards.map((c) => (
        <div key={c.title} className="rounded-2xl border border-slate-200 bg-white p-5 shadow-soft">
          <div className="text-sm font-medium text-slate-700">{c.title}</div>
          <div className={`mt-3 text-2xl font-semibold ${c.cls ?? 'text-slate-900'}`}>{c.value}</div>
        </div>
      ))}
    </div>
  );
}

function PositionsTable({
  items,
  loading,
  onChanged,
}: {
  items: PositionView[];
  loading: boolean;
  onChanged: () => void;
}): JSX.Element {
  const [editId, setEditId] = useState<number | null>(null);
  const [editCost, setEditCost] = useState('');
  const [editQty, setEditQty] = useState('');

  const startEdit = (p: PositionView) => {
    setEditId(p.id);
    setEditCost(String(p.cost_price));
    setEditQty(String(p.quantity));
  };

  const saveEdit = async (id: number) => {
    const cost = Number(editCost);
    const qty = Number(editQty);
    if (!(cost > 0) || !(qty > 0)) {
      notifyError(new Error('成本价和数量必须大于 0'), '输入有误');
      return;
    }
    try {
      await updatePosition(id, { cost_price: cost, quantity: Math.trunc(qty) });
      setEditId(null);
      onChanged();
    } catch (e) {
      notifyError(e, '保存失败');
    }
  };

  const close = async (id: number, code: string) => {
    if (!window.confirm(`确认平仓 ${code}？此操作会从持仓列表移除。`)) return;
    try {
      await closePosition(id);
      onChanged();
    } catch (e) {
      notifyError(e, '平仓失败');
    }
  };

  if (loading) {
    return <div className="rounded-2xl border border-slate-200 bg-white p-8 text-center text-sm text-slate-400">加载中…</div>;
  }
  if (items.length === 0) {
    return (
      <div className="rounded-2xl border border-dashed border-slate-200 bg-white p-10 text-center text-sm text-slate-400">
        暂无持仓。在「推荐」页点「标记建仓」把推荐加入持仓。
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded-2xl border border-slate-200 bg-white shadow-soft">
      <table className="min-w-full text-sm">
        <thead>
          <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wider text-slate-400">
            <th className="px-4 py-3">股票</th>
            <th className="px-4 py-3 text-right">成本价</th>
            <th className="px-4 py-3 text-right">数量</th>
            <th className="px-4 py-3 text-right">现价</th>
            <th className="px-4 py-3 text-right">市值</th>
            <th className="px-4 py-3 text-right">盈亏</th>
            <th className="px-4 py-3 text-right">盈亏%</th>
            <th className="px-4 py-3 text-right">操作</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100">
          {items.map((p) => (
            <tr key={p.id} className="hover:bg-slate-50">
              <td className="px-4 py-3">
                <div className="font-medium text-slate-900">{p.stock_code}</div>
                <div className="text-xs text-slate-400">{p.stock_name || '—'}</div>
              </td>
              <td className="px-4 py-3 text-right">
                {editId === p.id ? (
                  <input
                    value={editCost}
                    onChange={(e) => setEditCost(e.target.value)}
                    className="w-20 rounded border border-slate-300 px-2 py-1 text-right"
                  />
                ) : (
                  fmtMoney(p.cost_price)
                )}
              </td>
              <td className="px-4 py-3 text-right">
                {editId === p.id ? (
                  <input
                    value={editQty}
                    onChange={(e) => setEditQty(e.target.value)}
                    className="w-20 rounded border border-slate-300 px-2 py-1 text-right"
                  />
                ) : (
                  p.quantity
                )}
              </td>
              <td className="px-4 py-3 text-right">{fmtMoney(p.current_price)}</td>
              <td className="px-4 py-3 text-right">{fmtMoney(p.market_value)}</td>
              <td className={`px-4 py-3 text-right font-medium ${pnlClass(p.pnl)}`}>{fmtMoney(p.pnl)}</td>
              <td className={`px-4 py-3 text-right font-medium ${pnlClass(p.pnl)}`}>{fmtPct(p.pnl_pct)}</td>
              <td className="px-4 py-3 text-right">
                {editId === p.id ? (
                  <div className="flex justify-end gap-2">
                    <button onClick={() => saveEdit(p.id)} className="text-brand-600 hover:underline">保存</button>
                    <button onClick={() => setEditId(null)} className="text-slate-400 hover:underline">取消</button>
                  </div>
                ) : (
                  <div className="flex justify-end gap-2">
                    <button onClick={() => startEdit(p)} className="text-brand-600 hover:underline">编辑</button>
                    <button onClick={() => close(p.id, p.stock_code)} className="text-red-500 hover:underline">平仓</button>
                  </div>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
