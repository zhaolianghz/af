// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Strategy CRUD + template client. Endpoints documented in
 * `backend/internal/orchestrator/handler.go` and
 * `backend/internal/executor/template_handler.go`.
 */
import { apiClient } from './client';
import type {
  CreateStrategyInput,
  DAG,
  Strategy,
  StrategyDetail,
  StrategyListResponse,
  StrategyTemplate,
  TemplateListResponse,
  TrialRunRequest,
  TrialRunResponse,
  UpdateStrategyInput,
} from '@/types/orchestrator';

// =============================================================================
// CRUD
// =============================================================================

export interface ListStrategiesParams {
  status?: string;
  tags_contains?: string;
  code_like?: string;
  page?: number;
  page_size?: number;
}

export async function listStrategies(
  params: ListStrategiesParams = {},
): Promise<StrategyListResponse> {
  const { data } = await apiClient.get<{ code: number; data: StrategyListResponse }>(
    '/strategies',
    { params },
  );
  return data.data;
}

export async function getStrategy(id: number): Promise<StrategyDetail> {
  const { data } = await apiClient.get<{ code: number; data: StrategyDetail }>(
    `/strategies/${id}`,
  );
  return data.data;
}

export async function createStrategy(input: CreateStrategyInput): Promise<{
  strategy: Strategy;
}> {
  const { data } = await apiClient.post<{ code: number; data: { strategy: Strategy } }>(
    '/strategies',
    input,
  );
  return data.data;
}

export async function updateStrategy(
  id: number,
  input: UpdateStrategyInput,
): Promise<{ strategy: Strategy }> {
  const { data } = await apiClient.put<{ code: number; data: { strategy: Strategy } }>(
    `/strategies/${id}`,
    input,
  );
  return data.data;
}

export async function deleteStrategy(id: number): Promise<void> {
  await apiClient.delete(`/strategies/${id}`);
}

export async function cloneStrategy(
  id: number,
  newCode?: string,
): Promise<{ strategy: Strategy }> {
  const { data } = await apiClient.post<{ code: number; data: { strategy: Strategy } }>(
    `/strategies/${id}/clone`,
    newCode ? { new_code: newCode } : {},
  );
  return data.data;
}

export async function exportStrategy(id: number): Promise<Blob> {
  const res = await apiClient.get(`/strategies/${id}/export`, {
    responseType: 'blob',
  });
  return res.data;
}

export async function importStrategy(jsonStr: string): Promise<{ strategy: Strategy }> {
  const { data } = await apiClient.post<{ code: number; data: { strategy: Strategy } }>(
    '/strategies/import',
    { json_str: jsonStr },
  );
  return data.data;
}

// =============================================================================
// Templates (built-in)
// =============================================================================

export async function listTemplates(): Promise<TemplateListResponse> {
  const { data } = await apiClient.get<{ code: number; data: TemplateListResponse }>(
    '/strategies/templates',
  );
  return data.data;
}

export async function getTemplate(code: string): Promise<StrategyTemplate> {
  const { data } = await apiClient.get<{ code: number; data: StrategyTemplate }>(
    `/strategies/templates/${code}`,
  );
  return data.data;
}

export async function createFromTemplate(code: string): Promise<{ strategy: Strategy }> {
  const { data } = await apiClient.post<{ code: number; data: { strategy: Strategy } }>(
    `/strategies/from-template/${code}`,
  );
  return data.data;
}

// =============================================================================
// Trial run
// =============================================================================
//
// The backend (internal/orchestrator/trial_handler.go → RunSummary) returns a
// FLAT shape that does NOT match the frontend's TrialRunResponse:
//   - no `summary` wrapper (handler does httpresp.OK(c, summary))
//   - `node_results` is a MAP keyed by node id, not an array
//   - `duration` is in nanoseconds (Go time.Duration JSON), not `duration_ms`
// Reading `res.summary` therefore used to yield undefined and the TrialRunBanner
// never rendered (试运行 点击没反应). adaptTrialRun bridges the shapes at the
// API boundary so onTrialRun + TrialRunBanner can stay naive.

export interface RawNodeResult {
  node_id: string;
  status: 'success' | 'failed' | 'skipped';
  error?: string;
  skip_reason?: string;
  payload?: Record<string, unknown>;
  started_at?: string;
  finished_at?: string;
  duration: number; // nanoseconds (Go time.Duration)
}

export interface RawTrialRunResponse {
  run_id: number;
  strategy_id: number;
  status: 'success' | 'failed' | 'skipped';
  dry_run: boolean;
  node_results: Record<string, RawNodeResult>;
  started_at?: string;
  finished_at?: string;
  duration: number; // nanoseconds
  error?: string;
}

/** Nanoseconds (Go time.Duration JSON) → milliseconds, rounded. */
function nsToMs(ns: number | undefined): number | undefined {
  return typeof ns === 'number' ? Math.round(ns / 1e6) : undefined;
}

/** Convert the backend's flat RunSummary into the frontend's TrialRunResponse. */
export function adaptTrialRun(raw: RawTrialRunResponse): TrialRunResponse {
  return {
    summary: {
      status: raw.status,
      started_at: raw.started_at,
      finished_at: raw.finished_at,
      duration_ms: nsToMs(raw.duration),
      error: raw.error || undefined,
      node_results: Object.values(raw.node_results ?? {}).map((nr) => ({
        node_id: nr.node_id,
        status: nr.status,
        started_at: nr.started_at,
        finished_at: nr.finished_at,
        duration_ms: nsToMs(nr.duration),
        error: nr.error || undefined,
        payload: nr.payload,
      })),
    },
  };
}

export async function trialRun(
  strategyId: number,
  req: TrialRunRequest = {},
): Promise<TrialRunResponse> {
  const { data } = await apiClient.post<{ code: number; data: RawTrialRunResponse }>(
    `/strategies/${strategyId}/trial-run`,
    req,
  );
  return adaptTrialRun(data.data);
}

export async function trialRunToNode(
  strategyId: number,
  nodeId: string,
  req: TrialRunRequest = {},
): Promise<TrialRunResponse> {
  const { data } = await apiClient.post<{ code: number; data: RawTrialRunResponse }>(
    `/strategies/${strategyId}/trial-run/node/${nodeId}`,
    req,
  );
  return adaptTrialRun(data.data);
}

// =============================================================================
// Helpers — DAG JSON <-> object
// =============================================================================

/** Parse the backend's `dag_json` string into a DAG object. */
export function parseDag(dagJson: string | undefined | null): DAG {
  if (!dagJson) return { nodes: [], edges: [] };
  try {
    const obj = JSON.parse(dagJson) as DAG;
    if (!Array.isArray(obj.nodes)) obj.nodes = [];
    if (!Array.isArray(obj.edges)) obj.edges = [];
    return obj;
  } catch {
    return { nodes: [], edges: [] };
  }
}

/** Serialize a DAG object into the `dag_json` JSON string the backend wants. */
export function stringifyDag(dag: DAG): string {
  return JSON.stringify({ nodes: dag.nodes, edges: dag.edges });
}
