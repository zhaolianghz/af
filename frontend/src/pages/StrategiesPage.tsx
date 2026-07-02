// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * StrategiesPage — list + filter + CRUD entry points.
 * CRUD actions: new (from template / blank), clone, export, delete,
 * and the row click navigates to the editor.
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import clsx from 'clsx';
import {
  deleteStrategy,
  exportStrategy,
  importStrategy,
  listStrategies,
  updateStrategy,
  type ListStrategiesParams,
} from '@/api/strategies';
import {
  STRATEGY_STATUS_COLORS,
  STRATEGY_STATUS_LABELS,
  type Strategy,
  type StrategyStatus,
} from '@/types/orchestrator';
import useConfirm from '@/hooks/useConfirm';
import { notifyError, notifySuccess, notifyUndo } from '@/lib/notify';

const PAGE_SIZE = 20;

export default function StrategiesPage(): JSX.Element {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const confirm = useConfirm();

  // Filter state — bound to URL query so it's bookmarkable.
  const statusFilter = (searchParams.get('status') ?? '') as StrategyStatus | '';
  const codeLike = searchParams.get('code_like') ?? '';

  const [items, setItems] = useState<Strategy[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(Number(searchParams.get('page') ?? '1'));
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Persist filter changes back to the URL.
  const setStatus = useCallback(
    (s: StrategyStatus | '') => {
      const next = new URLSearchParams(searchParams);
      if (s) next.set('status', s);
      else next.delete('status');
      next.delete('page');
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const setCodeLike = useCallback(
    (v: string) => {
      const next = new URLSearchParams(searchParams);
      if (v) next.set('code_like', v);
      else next.delete('code_like');
      next.delete('page');
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  // Load on filter / page change.
  useEffect(() => {
    let cancelled = false;
    const params: ListStrategiesParams = {
      page,
      page_size: PAGE_SIZE,
    };
    if (statusFilter) params.status = statusFilter;
    if (codeLike) params.code_like = codeLike;

    setLoading(true);
    setError(null);
    listStrategies(params)
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
  }, [statusFilter, codeLike, page]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const onDelete = useCallback(
    async (s: Strategy) => {
      const ok = await confirm({ title: '删除方案', message: `"${s.name}" — 该操作不可撤销`, danger: true });
      if (!ok) return;
      // Snapshot for undo before deleting.
      const snapshot = { ...s };
      try {
        await deleteStrategy(s.id);
      } catch (err) {
        notifyError(err, '删除失败');
        return;
      }

      // Refresh current page; if it became empty, fall back one.
      const remaining = items.length - 1;
      if (remaining <= 0 && page > 1) {
        setPage(page - 1);
      } else {
        setItems((prev) => prev.filter((it) => it.id !== s.id));
        setTotal((t) => Math.max(0, t - 1));
      }

      // Undo toast.
      const undo = await notifyUndo(`已删除方案 "${snapshot.name}"`);
      if (!undo) return;
      try {
        await importStrategy(JSON.stringify(snapshot));
      } catch (err) {
        notifyError(err, '撤销失败');
        return;
      }
      // Re-fetch the current page so the restored strategy shows up.
      try {
        const params: ListStrategiesParams = {
          status: statusFilter || undefined,
          code_like: codeLike || undefined,
          page,
          page_size: PAGE_SIZE,
        };
        const res = await listStrategies(params);
        setItems(res.items);
        setTotal(res.total);
      } catch (err) {
        notifyError(err, '刷新失败');
      }
    },
    [items.length, page, statusFilter, codeLike, confirm],
  );

  const onExport = useCallback(async (s: Strategy) => {
    try {
      const blob = await exportStrategy(s.id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${s.code}.json`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      notifyError(err, '导出失败');
    }
  }, []);

  // Toggle a strategy between active (scheduled) and disabled. Enabling
  // prompts for a cron expression — an active strategy needs one to fire.
  const onToggleActive = useCallback(async (s: Strategy) => {
    const dagJson = s.dag_json ?? '';
    if (s.status === 'active') {
      try {
        await updateStrategy(s.id, { name: s.name, dag_json: dagJson, status: 'disabled' });
        setItems((prev) => prev.map((it) => (it.id === s.id ? { ...it, status: 'disabled' } : it)));
        notifySuccess(`已停用「${s.name}」`);
      } catch (err) {
        notifyError(err, '停用失败');
      }
      return;
    }
    const def = s.cron_expression || '0 16 * * 1-5'; // 每日 16:00,周一到周五
    const cron = window.prompt('启用定时执行 — Cron 表达式(分 时 日 月 周)', def);
    if (!cron) return;
    try {
      await updateStrategy(s.id, {
        name: s.name,
        dag_json: dagJson,
        status: 'active',
        cron_expression: cron,
      });
      setItems((prev) =>
        prev.map((it) => (it.id === s.id ? { ...it, status: 'active', cron_expression: cron } : it)),
      );
      notifySuccess(`已启用「${s.name}」(${cron})`);
    } catch (err) {
      notifyError(err, '启用失败');
    }
  }, []);

  const summary = useMemo(() => {
    const active = items.filter((s) => s.status === 'active').length;
    const draft = items.filter((s) => s.status === 'draft').length;
    return { active, draft, total: items.length };
  }, [items]);

  return (
    <div className="space-y-5">
      {confirm.dialog}
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">方案管理</h1>
          <p className="mt-1 text-sm text-slate-500">
            编排选股策略 (ReactFlow DAG)，管理版本、克隆与导出。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Link to="/templates" className="btn-secondary">
            模板库
          </Link>
          <Link to="/strategies/new" className="btn-primary">
            + 新建方案
          </Link>
        </div>
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-3 gap-4">
        <SummaryCard label="当前页总数" value={summary.total} tone="slate" />
        <SummaryCard label="已启用" value={summary.active} tone="emerald" />
        <SummaryCard label="草稿" value={summary.draft} tone="amber" />
      </div>

      {/* Filters */}
      <div className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white p-3 shadow-soft">
        <div className="flex items-center gap-2">
          <span className="text-[11px] font-medium text-slate-500">状态</span>
          <select
            className="form-select w-32"
            value={statusFilter}
            onChange={(e) => setStatus(e.target.value as StrategyStatus | '')}
          >
            <option value="">全部</option>
            <option value="draft">草稿</option>
            <option value="active">已启用</option>
            <option value="disabled">已停用</option>
            <option value="archived">已归档</option>
          </select>
        </div>
        <div className="flex flex-1 items-center gap-2">
          <span className="text-[11px] font-medium text-slate-500">编码</span>
          <input
            type="text"
            className="form-input w-64"
            value={codeLike}
            onChange={(e) => setCodeLike(e.target.value)}
            placeholder="模糊搜索，如 mv_breakout"
          />
        </div>
        {(statusFilter || codeLike) && (
          <button
            type="button"
            onClick={() => {
              setStatus('');
              setCodeLike('');
            }}
            className="btn-secondary"
          >
            清除筛选
          </button>
        )}
      </div>

      {/* Table */}
      <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-soft">
        <table className="min-w-full divide-y divide-slate-200">
          <thead className="bg-slate-50 text-[11px] uppercase tracking-wider text-slate-500">
            <tr>
              <th className="px-4 py-3 text-left">名称 / 编码</th>
              <th className="px-4 py-3 text-left">状态</th>
              <th className="px-4 py-3 text-left">版本</th>
              <th className="px-4 py-3 text-left">Cron</th>
              <th className="px-4 py-3 text-left">更新时间</th>
              <th className="px-4 py-3 text-right">操作</th>
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
                <td colSpan={6} className="px-0 py-0">
                  <div className="flex flex-col items-center gap-3 py-14">
                    <span className="text-2xl">📋</span>
                    <span className="text-sm font-medium text-slate-500">
                      {total === 0 ? '还没有创建方案' : '没有匹配的方案'}
                    </span>
                    <span className="max-w-xs text-center text-xs text-slate-400">
                      {total === 0
                        ? '创建一个选股策略方案，通过 DAG 画布拖拽编排数据源、指标、筛选和通知节点。'
                        : '试试调整筛选条件，或清除当前筛选查看全部方案。'}
                    </span>
                    {total === 0 && (
                      <div className="mt-1 flex items-center gap-2">
                        <Link to="/strategies/new" className="btn-primary text-xs">
                          创建第一个方案
                        </Link>
                        <Link to="/templates" className="btn-secondary text-xs">
                          浏览模板库
                        </Link>
                      </div>
                    )}
                  </div>
                </td>
              </tr>
            ) : (
              items.map((s) => (
                <tr
                  key={s.id}
                  className="cursor-pointer hover:bg-slate-50"
                  onClick={() => navigate(`/strategies/${s.id}`)}
                >
                  <td className="px-4 py-3">
                    <div className="font-medium text-slate-900">{s.name}</div>
                    <div className="font-mono text-[11px] text-slate-400">{s.code}</div>
                  </td>
                  <td className="px-4 py-3">
                    <span
                      className={clsx(
                        'rounded-md px-2 py-0.5 text-[11px] font-medium',
                        STRATEGY_STATUS_COLORS[s.status],
                      )}
                    >
                      {STRATEGY_STATUS_LABELS[s.status]}
                    </span>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-slate-600">
                    v{s.current_version}
                  </td>
                  <td className="px-4 py-3 font-mono text-[11px] text-slate-500">
                    {s.cron_expression || '—'}
                  </td>
                  <td className="px-4 py-3 text-xs text-slate-500">
                    {fmtDate(s.updated_at)}
                  </td>
                  <td
                    className="px-4 py-3 text-right"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <button
                      type="button"
                      onClick={() => onToggleActive(s)}
                      className={clsx('mr-1', s.status === 'active' ? 'btn-secondary' : 'btn-primary')}
                    >
                      {s.status === 'active' ? '停用' : '启用'}
                    </button>
                    <button
                      type="button"
                      onClick={() => navigate(`/strategies/${s.id}`)}
                      className="btn-secondary mr-1"
                    >
                      编辑
                    </button>
                    <button
                      type="button"
                      onClick={() => onExport(s)}
                      className="btn-secondary mr-1"
                    >
                      导出
                    </button>
                    <button
                      type="button"
                      onClick={() => onDelete(s)}
                      className="btn-danger"
                    >
                      删除
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>

        {/* Pagination */}
        <div className="flex items-center justify-between border-t border-slate-100 bg-slate-50 px-4 py-2 text-[11px] text-slate-500">
          <span>
            共 {total} 条 · 第 {page} / {totalPages} 页
          </span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page <= 1}
              className="btn-secondary"
            >
              上一页
            </button>
            <button
              type="button"
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
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

// =============================================================================
// Helpers
// =============================================================================

function SummaryCard({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: 'slate' | 'emerald' | 'amber';
}): JSX.Element {
  const toneCls: Record<typeof tone, string> = {
    slate: 'text-slate-900',
    emerald: 'text-emerald-700',
    amber: 'text-amber-700',
  };
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-4 shadow-soft">
      <div className="text-[11px] uppercase tracking-wider text-slate-400">{label}</div>
      <div className={clsx('mt-2 text-2xl font-semibold', toneCls[tone])}>{value}</div>
    </div>
  );
}

function fmtDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const yyyy = d.getFullYear();
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  const hh = String(d.getHours()).padStart(2, '0');
  const mi = String(d.getMinutes()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd} ${hh}:${mi}`;
}