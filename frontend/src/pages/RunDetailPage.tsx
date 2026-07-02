// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * RunDetailPage — three-pane run detail:
 *   - Top:   run metadata + status badge + actions (retry, back)
 *   - Left:  per-node timeline (RunTimeline)
 *   - Right: live SSE log stream (LogStreamViewer)
 *
 * Polls the run row every 5s so the status badge eventually
 * transitions from "running" -> "success/failed" even if the
 * EventSource missed the terminal event.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import LogStreamViewer from '@/components/runs/LogStreamViewer';
import RunStatusBadge from '@/components/runs/RunStatusBadge';
import RunTimeline from '@/components/runs/RunTimeline';
import { getRun, getRunLogs, retryRun } from '@/api/runs';
import type { Run, RunLog } from '@/types/orchestrator';
import useConfirm from '@/hooks/useConfirm';

const POLL_MS = 5000;
const TERMINAL_STATUSES = new Set(['success', 'failed', 'skipped']);

export default function RunDetailPage(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  const runId = id ? Number(id) : NaN;
  const navigate = useNavigate();
  const confirm = useConfirm();

  const [run, setRun] = useState<Run | null>(null);
  const [logs, setLogs] = useState<RunLog[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [retrying, setRetrying] = useState(false);
  const [retryError, setRetryError] = useState<string | null>(null);

  // Keep the polling loop only as long as the run is in a
  // non-terminal state.
  const isTerminal = run ? TERMINAL_STATUSES.has(run.status) : false;
  const pollRef = useRef<number | null>(null);

  const load = useCallback(async () => {
    if (!Number.isFinite(runId)) {
      setError('无效的 Run ID');
      return;
    }
    try {
      const [r, l] = await Promise.all([getRun(runId), getRunLogs(runId)]);
      setRun(r);
      setLogs(l);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败');
    }
  }, [runId]);

  // Initial load.
  useEffect(() => {
    load();
  }, [load]);

  // Poll while the run is in flight.
  useEffect(() => {
    if (isTerminal) {
      if (pollRef.current !== null) {
        window.clearInterval(pollRef.current);
        pollRef.current = null;
      }
      return;
    }
    pollRef.current = window.setInterval(load, POLL_MS);
    return () => {
      if (pollRef.current !== null) {
        window.clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
  }, [isTerminal, load]);

  const onRetry = useCallback(async () => {
    if (!run) return;
    const ok = await confirm({ title: '重试运行', message: `Run #${run.id} — 将创建一个新的 Run` });
    if (!ok) return;
    setRetrying(true);
    setRetryError(null);
    try {
      const r = await retryRun(run.id);
      navigate(`/runs/${r.run_id}`);
    } catch (err) {
      setRetryError(err instanceof Error ? err.message : String(err));
      setRetrying(false);
    }
  }, [run, navigate, confirm]);

  // Document-level hotkeys. Held in refs so the listener doesn't rebind
  // when onRetry or navigate change. Esc → back to list, Ctrl/Cmd+R → retry.
  const onRetryRef = useRef(onRetry);
  onRetryRef.current = onRetry;
  const navigateRef = useRef(navigate);
  navigateRef.current = navigate;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;

      if (e.key === 'Escape') {
        e.preventDefault();
        navigateRef.current('/runs');
        return;
      }
      if ((e.metaKey || e.ctrlKey) && (e.key === 'r' || e.key === 'R')) {
        e.preventDefault();
        onRetryRef.current();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  if (error) {
    return (
      <div className="space-y-3">
        <Link to="/runs" className="btn-secondary">
          ← 返回列表
        </Link>
        <div className="rounded-2xl border border-rose-200 bg-rose-50 p-6 text-sm text-rose-700">
          {error}
        </div>
      </div>
    );
  }

  if (!run) {
    return <div className="text-sm text-slate-400">加载中…</div>;
  }

  return (
    <div className="space-y-4">
      {confirm.dialog}
      {/* Header */}
      <div className="flex flex-wrap items-center gap-3">
        <Link to="/runs" className="btn-secondary">
          ← 返回列表
        </Link>
        <h1 className="text-2xl font-semibold text-slate-900">
          Run <span className="font-mono">#{run.id}</span>
        </h1>
        <RunStatusBadge status={run.status} />
        <span className="rounded-md bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-600">
          方案 #{run.strategy_id}
        </span>
        <span className="rounded-md bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-600">
          触发: {run.trigger_type}
          {run.attempts > 1 && ` ×${run.attempts}`}
        </span>
        <div className="ml-auto">
          <button
            type="button"
            onClick={onRetry}
            disabled={retrying}
            className="btn-primary"
          >
            {retrying ? '重试中…' : '重试'}
          </button>
        </div>
      </div>

      {retryError && (
        <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-2 text-xs text-rose-700">
          {retryError}
        </div>
      )}

      {/* Meta grid */}
      <div className="grid grid-cols-4 gap-3">
        <MetaCard label="开始时间" value={fmtTime(run.started_at) || '—'} />
        <MetaCard label="结束时间" value={fmtTime(run.finished_at) || '—'} />
        <MetaCard
          label="耗时"
          value={fmtDuration(run.started_at, run.finished_at)}
        />
        <MetaCard label="节点数" value={String(logs.length)} />
      </div>

      {run.error && (
        <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-2 text-xs text-rose-700">
          {run.error}
        </div>
      )}
      {run.skip_reason && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-2 text-xs text-amber-700">
          跳过原因: {run.skip_reason}
        </div>
      )}

      {/* Two columns: timeline + log stream */}
      <div className="grid grid-cols-2 gap-4">
        <section>
          <h2 className="mb-2 text-xs font-semibold uppercase tracking-wider text-slate-500">
            节点时间线
          </h2>
          <RunTimeline logs={logs} />
        </section>
        <section className="h-[480px]">
          <h2 className="mb-2 text-xs font-semibold uppercase tracking-wider text-slate-500">
            实时日志
          </h2>
          <LogStreamViewer runId={run.id} />
        </section>
      </div>
    </div>
  );
}

// =============================================================================
// Helpers
// =============================================================================

function MetaCard({ label, value }: { label: string; value: string }): JSX.Element {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-3 shadow-soft">
      <div className="text-[10px] uppercase tracking-wider text-slate-400">{label}</div>
      <div className="mt-1 font-mono text-xs text-slate-900">{value}</div>
    </div>
  );
}

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
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString('zh-CN', { hour12: false });
}