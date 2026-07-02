// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * RunHistoryPage — list of runs across all strategies with
 * status / strategy / date filters. Row click navigates to the
 * run detail page.
 *
 * A "新建运行" button lets the operator trigger a manual run
 * without first opening a strategy.
 */
import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import RunStatusBadge from '@/components/runs/RunStatusBadge';
import { listRuns, triggerRun, type ListRunsParams } from '@/api/runs';
import { listStrategies } from '@/api/strategies';
import { notifyError } from '@/lib/notify';
import type { Run, RunStatus, Strategy } from '@/types/orchestrator';

const PAGE_SIZE = 20;

export default function RunHistoryPage(): JSX.Element {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const statusFilter = (searchParams.get('status') ?? '') as RunStatus | '';
  const strategyFilter = searchParams.get('strategy_id') ?? '';
  const page = Number(searchParams.get('page') ?? '1');

  const [items, setItems] = useState<Run[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [strategies, setStrategies] = useState<Strategy[]>([]);
  const [triggerOpen, setTriggerOpen] = useState(false);
  const [triggering, setTriggering] = useState(false);

  const setStatus = useCallback(
    (s: RunStatus | '') => {
      const next = new URLSearchParams(searchParams);
      if (s) next.set('status', s);
      else next.delete('status');
      next.delete('page');
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const setStrategy = useCallback(
    (v: string) => {
      const next = new URLSearchParams(searchParams);
      if (v) next.set('strategy_id', v);
      else next.delete('strategy_id');
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
    const params: ListRunsParams = { page, page_size: PAGE_SIZE };
    if (statusFilter) params.status = statusFilter;
    if (strategyFilter) params.strategy_id = Number(strategyFilter);
    listRuns(params)
      .then((res) => {
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
  }, [statusFilter, strategyFilter, page]);

  // Lazy-load the strategy list once for the strategy filter dropdown
  // and the manual-trigger modal.
  useEffect(() => {
    let cancelled = false;
    listStrategies({ page: 1, page_size: 200 })
      .then((res) => {
        if (cancelled) return;
        setStrategies(res.items ?? []);
      })
      .catch(() => {
        /* silent: filter just shows an empty list */
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">运行历史</h1>
          <p className="mt-1 text-sm text-slate-500">
            全部方案的执行记录，状态 / 方案 / 时间筛选 + 手工触发。
          </p>
        </div>
        <button
          type="button"
          onClick={() => setTriggerOpen(true)}
          className="btn-primary"
        >
          + 手工触发运行
        </button>
      </div>

      <div className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white p-3 shadow-soft">
        <div className="flex items-center gap-2">
          <span className="text-[11px] font-medium text-slate-500">状态</span>
          <select
            className="form-select w-32"
            value={statusFilter}
            onChange={(e) => setStatus(e.target.value as RunStatus | '')}
          >
            <option value="">全部</option>
            <option value="pending">待执行</option>
            <option value="running">执行中</option>
            <option value="success">成功</option>
            <option value="failed">失败</option>
            <option value="partial">部分成功</option>
            <option value="skipped">已跳过</option>
          </select>
        </div>
        <div className="flex flex-1 items-center gap-2">
          <span className="text-[11px] font-medium text-slate-500">方案</span>
          <select
            className="form-select w-64"
            value={strategyFilter}
            onChange={(e) => setStrategy(e.target.value)}
          >
            <option value="">全部方案</option>
            {strategies.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} ({s.code})
              </option>
            ))}
          </select>
        </div>
        {(statusFilter || strategyFilter) && (
          <button
            type="button"
            onClick={() => {
              setStatus('');
              setStrategy('');
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
              <th className="px-4 py-3 text-left">Run ID</th>
              <th className="px-4 py-3 text-left">方案</th>
              <th className="px-4 py-3 text-left">状态</th>
              <th className="px-4 py-3 text-left">触发</th>
              <th className="px-4 py-3 text-left">耗时</th>
              <th className="px-4 py-3 text-left">开始时间</th>
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
                  暂无运行记录
                </td>
              </tr>
            ) : (
              items.map((r) => (
                <tr
                  key={r.id}
                  className="cursor-pointer hover:bg-slate-50"
                  onClick={() => navigate(`/runs/${r.id}`)}
                >
                  <td className="px-4 py-3 font-mono text-xs text-slate-700">#{r.id}</td>
                  <td className="px-4 py-3">
                    <span className="text-slate-900">#{r.strategy_id}</span>
                  </td>
                  <td className="px-4 py-3">
                    <RunStatusBadge status={r.status} />
                  </td>
                  <td className="px-4 py-3 text-xs text-slate-500">
                    {r.trigger_type}
                    {r.attempts > 1 && (
                      <span className="ml-1 text-amber-600">×{r.attempts}</span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-xs text-slate-600">
                    {fmtDuration(r.started_at, r.finished_at)}
                  </td>
                  <td className="px-4 py-3 text-xs text-slate-500">
                    {fmtTime(r.started_at)}
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

      {triggerOpen && (
        <TriggerModal
          strategies={strategies}
          busy={triggering}
          onClose={() => setTriggerOpen(false)}
          onTriggered={(r) => {
            setTriggerOpen(false);
            navigate(`/runs/${r.run_id}`);
          }}
          onBusyChange={setTriggering}
        />
      )}

      <div className="text-[11px] text-slate-400">
        <Link to="/recommendations" className="hover:underline">
          查看推荐结果 →
        </Link>
      </div>
    </div>
  );
}

// =============================================================================
// Trigger modal
// =============================================================================

function TriggerModal({
  strategies,
  busy,
  onClose,
  onTriggered,
  onBusyChange,
}: {
  strategies: Strategy[];
  busy: boolean;
  onClose: () => void;
  onTriggered: (r: { run_id: number }) => void;
  onBusyChange: (b: boolean) => void;
}): JSX.Element {
  const [strategyId, setStrategyId] = useState<string>(
    strategies[0] ? String(strategies[0].id) : '',
  );

  useEffect(() => {
    if (!strategyId && strategies[0]) setStrategyId(String(strategies[0].id));
  }, [strategies, strategyId]);

  const onSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!strategyId) return;
      onBusyChange(true);
      try {
        const r = await triggerRun({ strategy_id: Number(strategyId) });
        onTriggered(r);
      } catch (err) {
        notifyError(err, '触发失败');
        onBusyChange(false);
      }
    },
    [strategyId, onBusyChange, onTriggered],
  );

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4">
      <form
        onSubmit={onSubmit}
        className="w-full max-w-md space-y-4 rounded-2xl border border-slate-200 bg-white p-5 shadow-xl"
      >
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-900">手工触发运行</h2>
          <button
            type="button"
            onClick={onClose}
            className="text-slate-400 hover:text-slate-700"
          >
            ✕
          </button>
        </div>
        <label className="block">
          <span className="text-[11px] font-medium text-slate-600">方案</span>
          <select
            className="form-select mt-1"
            value={strategyId}
            onChange={(e) => setStrategyId(e.target.value)}
            required
          >
            <option value="">请选择</option>
            {strategies.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} ({s.code})
              </option>
            ))}
          </select>
        </label>
        <div className="flex justify-end gap-2 pt-2">
          <button type="button" onClick={onClose} className="btn-secondary">
            取消
          </button>
          <button type="submit" disabled={busy || !strategyId} className="btn-primary">
            {busy ? '触发中…' : '触发'}
          </button>
        </div>
      </form>
    </div>
  );
}

// =============================================================================
// Helpers
// =============================================================================

function fmtDuration(start?: string, end?: string): string {
  if (!start || !end) return '—';
  const s = new Date(start).getTime();
  const e = new Date(end).getTime();
  if (!Number.isFinite(s) || !Number.isFinite(e) || e < s) return '—';
  const ms = e - s;
  if (ms < 1000) return `${ms} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)} s`;
  return `${Math.floor(ms / 60_000)}m ${Math.floor((ms % 60_000) / 1000)}s`;
}

function fmtTime(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString('zh-CN', { hour12: false });
}