// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * LogStreamViewer — live SSE log stream for a single run.
 *
 * - Opens an EventSource on mount, closes it on unmount.
 * - Buffers incoming events; the user can pause the buffer to
 *   read without the view jumping.
 * - Reconnects automatically on disconnect; the "重连" button
 *   manually forces a fresh EventSource.
 * - Auto-scrolls to the bottom unless the user has scrolled up.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import clsx from 'clsx';
import { openRunEventStream } from '@/api/runs';
import type { RunEvent } from '@/types/orchestrator';

export interface LogStreamViewerProps {
  runId: number;
  /** When true, suppress "no events yet" empty state — used while loading. */
  compact?: boolean;
}

interface BufferedEvent extends RunEvent {
  /** Set when the entry is received locally for sort/render purposes. */
  receivedAt: number;
}

export default function LogStreamViewer({
  runId,
  compact,
}: LogStreamViewerProps): JSX.Element {
  const [events, setEvents] = useState<BufferedEvent[]>([]);
  const [paused, setPaused] = useState(false);
  const [status, setStatus] = useState<'connecting' | 'open' | 'closed' | 'error'>(
    'connecting',
  );
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const streamRef = useRef<{ close: () => void } | null>(null);
  // Track whether the user has scrolled away from the bottom so
  // we know whether to keep auto-scrolling.
  const stickToBottomRef = useRef(true);

  // Stable connect fn used by the manual reconnect button.
  const connect = useCallback(() => {
    if (streamRef.current) streamRef.current.close();
    setStatus('connecting');
    setErrorMsg(null);
    const stream = openRunEventStream(runId, {
      onOpen: () => setStatus('open'),
      onEvent: ({ data }) => {
        if (pausedRef.current) return;
        let parsed: RunEvent | null = null;
        try {
          parsed = JSON.parse(data) as RunEvent;
        } catch {
          // Non-JSON frames (e.g. the "ready" placeholder) are fine to ignore.
          return;
        }
        if (parsed.run_id !== runId) return;
        // Control frames (the "ready" handshake, heartbeats) carry a
        // run_id but no type/ts — rendering them shows "Invalid Date"
        // and inflates the event count. Only buffer real run events.
        if (!parsed.type || !parsed.ts) return;
        setEvents((prev) => {
          const next = [...prev, { ...parsed!, receivedAt: Date.now() }];
          // Cap at 2000 events to keep the DOM light.
          return next.length > 2000 ? next.slice(next.length - 2000) : next;
        });
      },
      onError: (err) => {
        setStatus('error');
        const msg =
          err instanceof Event
            ? `SSE 连接异常 (readyState=${(err.target as EventSource).readyState})`
            : 'SSE 连接异常';
        setErrorMsg(msg);
      },
    });
    streamRef.current = stream;
  }, [runId]);

  // Keep a ref of `paused` so the onEvent closure always reads the
  // latest value without us having to recreate the EventSource.
  const pausedRef = useRef(paused);
  useEffect(() => {
    pausedRef.current = paused;
  }, [paused]);

  // Open / close the stream on mount + runId change.
  useEffect(() => {
    connect();
    return () => {
      streamRef.current?.close();
      streamRef.current = null;
    };
  }, [connect]);

  // Auto-scroll to bottom unless the user has scrolled up.
  useEffect(() => {
    if (!stickToBottomRef.current) return;
    const el = containerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [events]);

  const onScroll = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    stickToBottomRef.current = distance < 24;
  }, []);

  const clear = useCallback(() => setEvents([]), []);

  return (
    <div className="flex h-full flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-soft">
      {/* Toolbar */}
      <div
        className={clsx(
          'flex items-center justify-between border-b border-slate-200 px-3 py-1.5',
          paused ? 'bg-amber-50' : 'bg-slate-50',
        )}
      >
        <div className="flex items-center gap-2 text-[11px]">
          <StatusLight status={status} />
          <span className="text-slate-500">
            {events.length} 条事件
            {paused && ' · 已暂停'}
          </span>
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => setPaused((p) => !p)}
            className="btn-secondary"
          >
            {paused ? '继续' : '暂停'}
          </button>
          <button type="button" onClick={connect} className="btn-secondary">
            重连
          </button>
          <button
            type="button"
            onClick={clear}
            disabled={events.length === 0}
            className="btn-secondary"
          >
            清空
          </button>
        </div>
      </div>

      {errorMsg && (
        <div className="border-b border-rose-200 bg-rose-50 px-3 py-1.5 text-[11px] text-rose-700">
          {errorMsg}
        </div>
      )}

      {/* Log body */}
      <div
        ref={containerRef}
        onScroll={onScroll}
        className={clsx(
          'flex-1 overflow-auto bg-slate-900 font-mono text-[11px] leading-5 text-slate-100',
          compact ? 'max-h-72' : '',
        )}
      >
        {events.length === 0 ? (
          <div className="flex h-full items-center justify-center p-6 text-center text-slate-500">
            {status === 'connecting' ? '连接中…' : '暂无事件'}
          </div>
        ) : (
          <ul className="divide-y divide-slate-800/40">
            {events.map((e, idx) => (
              <li key={`${e.receivedAt}-${idx}`} className="flex gap-2 px-3 py-1">
                <span className="shrink-0 text-slate-500">
                  {new Date(e.ts).toLocaleTimeString('zh-CN', { hour12: false })}
                </span>
                <span
                  className={clsx(
                    'shrink-0 uppercase tracking-wider',
                    toneForEvent(e.type),
                  )}
                >
                  {e.type}
                </span>
                {e.node_id && (
                  <span className="shrink-0 text-slate-400">[{e.node_id}]</span>
                )}
                <span className="min-w-0 flex-1 truncate text-slate-200">
                  {summarize(e)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

// =============================================================================
// Helpers
// =============================================================================

function toneForEvent(t: RunEvent['type']): string {
  switch (t) {
    case 'run.completed':
    case 'node.success':
      return 'text-emerald-400';
    case 'node.failed':
      return 'text-rose-400';
    case 'node.skipped':
      return 'text-amber-400';
    case 'log':
      return 'text-sky-300';
    case 'heartbeat':
    case 'ready':
      return 'text-slate-500';
    default:
      return 'text-slate-300';
  }
}

function summarize(e: RunEvent): string {
  const d = e.data ?? {};
  if (typeof d.message === 'string') return d.message;
  if (d.error) return String(d.error);
  if (d.count !== undefined) return `count=${d.count}`;
  if (d.node_count !== undefined) return `node_count=${d.node_count}`;
  if (d.status) return `status=${d.status}`;
  return '';
}

function StatusLight({
  status,
}: {
  status: 'connecting' | 'open' | 'closed' | 'error';
}): JSX.Element {
  const map: Record<typeof status, { label: string; cls: string }> = {
    connecting: { label: '连接中', cls: 'bg-amber-400 animate-pulse' },
    open: { label: '在线', cls: 'bg-emerald-500' },
    closed: { label: '已关闭', cls: 'bg-slate-400' },
    error: { label: '异常', cls: 'bg-rose-500' },
  };
  const v = map[status];
  return (
    <span className="inline-flex items-center gap-1 text-slate-500">
      <span className={clsx('inline-block h-1.5 w-1.5 rounded-full', v.cls)} />
      <span>{v.label}</span>
    </span>
  );
}