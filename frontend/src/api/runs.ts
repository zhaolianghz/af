// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Run API client. Mirrors `backend/internal/executor/handler.go`.
 *
 * Standard CRUD via `apiClient` (axios); SSE via a thin wrapper
 * around the browser-native `EventSource` (POST isn't supported
 * by EventSource, so trigger / retry go through axios like the
 * other endpoints).
 */
import { apiClient, API_BASE_URL } from './client';
import { getToken } from '@/stores/authStore';
import type {
  RetryRunResponse,
  Run,
  RunListResponse,
  RunLog,
  RunLogsResponse,
  TriggerRunRequest,
  TriggerRunResponse,
} from '@/types/orchestrator';

// =============================================================================
// CRUD
// =============================================================================

export interface ListRunsParams {
  strategy_id?: number;
  status?: string;
  /** RFC3339 or YYYY-MM-DD */
  from?: string;
  /** RFC3339 or YYYY-MM-DD */
  to?: string;
  page?: number;
  page_size?: number;
}

export async function listRuns(params: ListRunsParams = {}): Promise<RunListResponse> {
  const { data } = await apiClient.get<{ code: number; data: RunListResponse }>(
    '/runs',
    { params },
  );
  return data.data;
}

export async function getRun(id: number): Promise<Run> {
  const { data } = await apiClient.get<{ code: number; data: Run }>(`/runs/${id}`);
  return data.data;
}

export async function getRunLogs(id: number): Promise<RunLog[]> {
  const { data } = await apiClient.get<{ code: number; data: RunLogsResponse }>(
    `/runs/${id}/logs`,
  );
  return data.data.items ?? [];
}

export async function triggerRun(req: TriggerRunRequest): Promise<TriggerRunResponse> {
  const { data } = await apiClient.post<{ code: number; data: TriggerRunResponse }>(
    '/runs',
    req,
  );
  return data.data;
}

export async function retryRun(id: number): Promise<RetryRunResponse> {
  const { data } = await apiClient.post<{ code: number; data: RetryRunResponse }>(
    `/runs/${id}/retry`,
  );
  return data.data;
}

// =============================================================================
// SSE — event stream for a single run.
//
// The backend speaks text/event-stream with frames like:
//
//   event: node.started
//   id: 1737
//   data: {"run_id":1,"type":"node.started","ts":"...","node_id":"..."}
//
// EventSource reconnects automatically; we surface the EventSource
// instance so callers can `.close()` it on unmount. `lastEventId`
// resume is supported by the browser, so reconnection picks up
// where the run left off (modulo the bus buffer).
// =============================================================================

export interface RunEventStream {
  close: () => void;
}

export interface RunEventHandlers {
  onEvent?: (e: { id: string | null; event: string; data: string }) => void;
  onError?: (err: unknown) => void;
  onOpen?: () => void;
}

export function openRunEventStream(
  runId: number,
  handlers: RunEventHandlers,
): RunEventStream {
  // Native EventSource cannot send an Authorization header, so the
  // token rides as an `access_token` query param — the backend's auth
  // middleware accepts it as a Bearer fallback specifically for SSE.
  // Omitted when auth is off (no token) or this is a cross-origin
  // deployment without a stored session.
  let url = `${API_BASE_URL}/runs/${runId}/events`;
  const token = getToken();
  if (token) {
    url += `?access_token=${encodeURIComponent(token)}`;
  }
  const es = new EventSource(url, { withCredentials: false });
  if (handlers.onOpen) es.addEventListener('open', () => handlers.onOpen?.());

  // We register a single 'message' listener (the default). Other
  // named events (node.started, run.completed, ...) ALSO fire
  // 'message' in most browsers — the `event` field on the message
  // object carries the original name. We still register a
  // per-event listener so callers can hook specific event types
  // by name if they need.
  es.addEventListener('message', (ev) => {
    handlers.onEvent?.({
      id: ev.lastEventId,
      event: (ev as MessageEvent).type || 'message',
      data: ev.data,
    });
  });
  // Named event shortcuts — these also reach onEvent above, but
  // surfacing them separately makes the call site cleaner.
  const namedTypes = [
    'ready',
    'heartbeat',
    'run.started',
    'run.completed',
    'node.started',
    'node.success',
    'node.failed',
    'node.skipped',
    'log',
  ];
  for (const t of namedTypes) {
    es.addEventListener(t, (ev) => {
      const me = ev as MessageEvent;
      handlers.onEvent?.({ id: me.lastEventId, event: t, data: me.data });
    });
  }
  es.addEventListener('error', (err) => handlers.onError?.(err));
  return { close: () => es.close() };
}