// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * RecommendationsPage — list of persisted recommendations.
 * Filters: strategy_code, tag, date range. "导出 CSV" button
 * downloads the current page as a CSV.
 */
import { useCallback, useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { listRecommendations, recommendationsToCsv } from '@/api/recommendations';
import { createPosition } from '@/api/positions';
import { notifyError, notifySuccess } from '@/lib/notify';
import type {
  Recommendation,
  RecommendationListResponse,
} from '@/types/orchestrator';

const PAGE_SIZE = 20;

export default function RecommendationsPage(): JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const strategyCode = searchParams.get('strategy_code') ?? '';
  const tag = searchParams.get('tag') ?? '';
  const from = searchParams.get('from') ?? '';
  const to = searchParams.get('to') ?? '';
  const page = Number(searchParams.get('page') ?? '1');

  const [items, setItems] = useState<Recommendation[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const setQuery = useCallback(
    (key: string, value: string) => {
      const next = new URLSearchParams(searchParams);
      if (value) next.set(key, value);
      else next.delete(key);
      next.delete('page');
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const setPage = useCallback(
    (n: number) => {
      const next = new URLSearchParams(searchParams);
      if (n > 1) next.set('page', String(n));
      else next.delete('page');
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    listRecommendations({
      strategy_code: strategyCode || undefined,
      tag: tag || undefined,
      from: from || undefined,
      to: to || undefined,
      page,
      page_size: PAGE_SIZE,
    })
      .then((res: RecommendationListResponse) => {
        if (cancelled) return;
        setItems(res.items ?? []);
        setTotal(res.total ?? 0);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : '加载失败');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [strategyCode, tag, from, to, page]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const onExport = useCallback(() => {
    if (items.length === 0) {
      import('@/lib/notify').then(({ notifyWarning }) => notifyWarning('当前页没有数据可导出'));
      return;
    }
    const csv = '\ufeff' + recommendationsToCsv(items);
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    const ts = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-');
    a.href = url;
    a.download = `recommendations-${ts}.csv`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }, [items]);

  // Mark a recommendation as bought → create a position. Cost defaults
  // to the rec's entry price; the user enters real cost + quantity by
  // hand (single-user, so simple prompts are fine).
  const markPosition = useCallback((r: Recommendation) => {
    const defCost = r.entry_price_low > 0 ? r.entry_price_low : '';
    const costStr = window.prompt(`建仓成本价（${r.stock_code} ${r.stock_name}）`, String(defCost));
    if (costStr === null) return;
    const qtyStr = window.prompt('持有数量（股）', '100');
    if (qtyStr === null) return;
    const cost = Number(costStr);
    const qty = Number(qtyStr);
    if (!(cost > 0) || !(qty > 0)) {
      notifyError(new Error('成本价和数量必须大于 0'), '输入有误');
      return;
    }
    createPosition({
      stock_code: r.stock_code,
      stock_name: r.stock_name,
      cost_price: cost,
      quantity: Math.trunc(qty),
      source_recommendation_id: typeof r.id === 'number' ? r.id : Number(r.id),
    })
      .then(() => notifySuccess(`已建仓 ${r.stock_code}，可在「持仓」页查看`))
      .catch((e) => notifyError(e, '建仓失败'));
  }, []);

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">推荐结果</h1>
          <p className="mt-1 text-sm text-slate-500">
            各方案落库后的选股清单，可按方案 / 时段标签 / 日期筛选。
          </p>
        </div>
        <button
          type="button"
          onClick={onExport}
          disabled={items.length === 0}
          className="btn-primary"
        >
          导出当前页 CSV
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-3 rounded-2xl border border-slate-200 bg-white p-3 shadow-soft">
        <div className="flex items-center gap-2">
          <span className="text-[11px] font-medium text-slate-500">方案编码</span>
          <input
            type="text"
            className="form-input w-48"
            value={strategyCode}
            onChange={(e) => setQuery('strategy_code', e.target.value)}
            placeholder="mv_breakout"
          />
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[11px] font-medium text-slate-500">标签</span>
          <select
            className="form-select w-32"
            value={tag}
            onChange={(e) => setQuery('tag', e.target.value)}
          >
            <option value="">全部</option>
            <option value="MORNING">MORNING</option>
            <option value="AFTERNOON">AFTERNOON</option>
            <option value="NO_POST">NO_POST</option>
            <option value="REVIEW">REVIEW</option>
          </select>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[11px] font-medium text-slate-500">起始</span>
          <input
            type="date"
            className="form-input"
            value={from}
            onChange={(e) => setQuery('from', e.target.value)}
          />
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[11px] font-medium text-slate-500">结束</span>
          <input
            type="date"
            className="form-input"
            value={to}
            onChange={(e) => setQuery('to', e.target.value)}
          />
        </div>
        {(strategyCode || tag || from || to) && (
          <button
            type="button"
            onClick={() => {
              setQuery('strategy_code', '');
              setQuery('tag', '');
              setQuery('from', '');
              setQuery('to', '');
            }}
            className="btn-secondary"
          >
            清除筛选
          </button>
        )}
      </div>

      <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-soft">
        <table className="min-w-full divide-y divide-slate-200">
          <thead className="bg-slate-50 text-[11px] uppercase tracking-wider text-slate-500">
            <tr>
              <th className="px-4 py-3 text-left">日期</th>
              <th className="px-4 py-3 text-left">股票</th>
              <th className="px-4 py-3 text-left">入场价区间</th>
              <th className="px-4 py-3 text-left">方案</th>
              <th className="px-4 py-3 text-left">标签</th>
              <th className="px-4 py-3 text-right">Run</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 text-sm">
            {loading && items.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-4 py-10 text-center text-slate-400">
                  加载中…
                </td>
              </tr>
            ) : error ? (
              <tr>
                <td colSpan={6} className="px-4 py-10 text-center text-rose-600">
                  {error}
                </td>
              </tr>
            ) : items.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-4 py-10 text-center text-slate-400">
                  暂无推荐结果。运行一次方案后会显示在这里。
                </td>
              </tr>
            ) : (
              items.map((r) => (
                <tr key={r.id} className="hover:bg-slate-50">
                  <td className="px-4 py-3 text-xs text-slate-600">{r.date}</td>
                  <td className="px-4 py-3">
                    <div className="font-medium text-slate-900">{r.stock_name}</div>
                    <div className="font-mono text-[11px] text-slate-400">
                      {r.stock_code}
                    </div>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-slate-700">
                    {r.entry_price_low.toFixed(2)} ~ {r.entry_price_high.toFixed(2)}
                  </td>
                  <td className="px-4 py-3">
                    <div className="text-xs text-slate-700">{r.strategy_name}</div>
                    <div className="font-mono text-[10px] text-slate-400">
                      {r.strategy_code}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {(r.tags ?? []).map((t) => (
                        <span
                          key={t.id}
                          className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-medium text-slate-600"
                        >
                          {t.tag}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-3">
                      <button
                        type="button"
                        onClick={() => markPosition(r)}
                        className="text-xs font-medium text-brand-600 hover:underline"
                      >
                        标记建仓
                      </button>
                      <Link
                        to={`/runs/${r.run_id}`}
                        className="font-mono text-xs text-slate-400 hover:underline"
                      >
                        #{r.run_id}
                      </Link>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
        <div className="flex items-center justify-between border-t border-slate-100 bg-slate-50 px-4 py-2 text-[11px] text-slate-500">
          <span>
            共 {total} 条 · 第 {page} / {totalPages} 页
          </span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setPage(Math.max(1, page - 1))}
              disabled={page <= 1}
              className="btn-secondary"
            >
              上一页
            </button>
            <button
              type="button"
              onClick={() => setPage(Math.min(totalPages, page + 1))}
              disabled={page >= totalPages}
              className="btn-secondary"
            >
              下一页
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}