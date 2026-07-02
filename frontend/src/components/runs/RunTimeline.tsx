// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * RunTimeline — vertical timeline of the per-node execution of a
 * run, sorted by start time. Each entry shows node id, status,
 * duration, and (if present) the error message.
 *
 * Used inside the run detail page next to the live log stream.
 */
import clsx from 'clsx';
import {
  RUN_LOG_STATUS_COLORS,
  type RunLog,
} from '@/types/orchestrator';

export interface RunTimelineProps {
  logs: RunLog[];
}

export default function RunTimeline({ logs }: RunTimelineProps): JSX.Element {
  if (logs.length === 0) {
    return (
      <div className="rounded-xl border border-dashed border-slate-200 bg-slate-50 p-6 text-center text-xs text-slate-400">
        暂无节点日志
      </div>
    );
  }
  const sorted = [...logs].sort(
    (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
  );
  return (
    <ol className="space-y-2">
      {sorted.map((log) => {
        const duration = fmtDuration(log.started_at, log.finished_at);
        return (
          <li
            key={log.id}
            className="flex items-start gap-3 rounded-lg border border-slate-200 bg-white px-3 py-2"
          >
            <StatusDot status={log.status} />
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline justify-between gap-2">
                <span className="truncate font-mono text-xs text-slate-900">
                  {log.node_key}
                </span>
                <span
                  className={clsx(
                    'shrink-0 text-[10px] font-medium uppercase tracking-wider',
                    RUN_LOG_STATUS_COLORS[log.status],
                  )}
                >
                  {log.status}
                </span>
              </div>
              <div className="mt-0.5 text-[11px] text-slate-500">
                {fmtTime(log.started_at)} · {duration}
              </div>
              {log.error && (
                <div className="mt-1 rounded border border-rose-200 bg-rose-50 px-2 py-1 text-[11px] text-rose-700">
                  {log.error}
                </div>
              )}
            </div>
          </li>
        );
      })}
    </ol>
  );
}

// =============================================================================
// Helpers
// =============================================================================

function StatusDot({ status }: { status: RunLog['status'] }): JSX.Element {
  const bg =
    status === 'success'
      ? 'bg-emerald-500'
      : status === 'failed'
        ? 'bg-rose-500'
        : 'bg-slate-400';
  return <span className={clsx('mt-1.5 h-2 w-2 shrink-0 rounded-full', bg)} />;
}

function fmtDuration(start: string, end: string): string {
  const s = new Date(start).getTime();
  const e = new Date(end).getTime();
  if (!Number.isFinite(s) || !Number.isFinite(e) || e < s) return '—';
  const ms = e - s;
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString('zh-CN', { hour12: false });
}