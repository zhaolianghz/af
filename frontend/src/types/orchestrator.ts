// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Orchestrator (DAG) types — mirror Go's
 * `internal/orchestrator/types.go` so the ReactFlow canvas can
 * round-trip cleanly.
 *
 * The `dag_json` column on the backend stores the ReactFlow
 * native `{nodes, edges}` format. We keep `params` as a `Record`
 * for now — node-type-specific forms cast to their own shapes
 * before sending to the backend.
 */

export type NodeType =
  | 'data_source'
  | 'indicator'
  | 'filter'
  | 'rank'
  | 'dedupe'
  | 'session_tag'
  | 'persist'
  | 'notify';

export const NODE_TYPES: NodeType[] = [
  'data_source',
  'indicator',
  'filter',
  'rank',
  'dedupe',
  'session_tag',
  'persist',
  'notify',
];

/** Human-readable labels for the node palette / dropdowns. */
export const NODE_LABELS: Record<NodeType, string> = {
  data_source: '数据源',
  indicator: '技术指标',
  filter: '条件过滤',
  rank: '排序取 Top',
  dedupe: '去重',
  session_tag: '时段打标',
  persist: '持久化推荐',
  notify: '通知推送',
};

/** Per-type short description used in the palette card. */
export const NODE_DESCRIPTIONS: Record<NodeType, string> = {
  data_source: '拉取行情/财务/新闻数据',
  indicator: 'MA / MACD / KDJ / 量比 等',
  filter: '按字段做布尔过滤',
  rank: '按字段排序取 Top N',
  dedupe: '按 key 去重',
  session_tag: '打 MORNING/AFTERNOON/REVIEW 标签',
  persist: '写入 Recommendation 表',
  notify: '调飞书/钉钉 webhook',
};

/** Accent color per node type — used in the palette + canvas card. */
export const NODE_COLORS: Record<NodeType, { bg: string; border: string; text: string }> = {
  data_source: { bg: 'bg-sky-50', border: 'border-sky-300', text: 'text-sky-700' },
  indicator: { bg: 'bg-indigo-50', border: 'border-indigo-300', text: 'text-indigo-700' },
  filter: { bg: 'bg-amber-50', border: 'border-amber-300', text: 'text-amber-700' },
  rank: { bg: 'bg-rose-50', border: 'border-rose-300', text: 'text-rose-700' },
  dedupe: { bg: 'bg-slate-50', border: 'border-slate-300', text: 'text-slate-700' },
  session_tag: { bg: 'bg-teal-50', border: 'border-teal-300', text: 'text-teal-700' },
  persist: { bg: 'bg-emerald-50', border: 'border-emerald-300', text: 'text-emerald-700' },
  notify: { bg: 'bg-pink-50', border: 'border-pink-300', text: 'text-pink-700' },
};

// =============================================================================
// DAG nodes/edges (matches `internal/orchestrator/types.go`)
// =============================================================================

export interface Position {
  x: number;
  y: number;
}

export interface OrchNode {
  id: string;
  type: NodeType;
  /** Optional subtype (e.g. "ma" under "indicator"). */
  subtype?: string;
  /** Free-form JSON params; node-type-specific. */
  params?: Record<string, unknown>;
  position?: Position;
}

/**
 * ReactFlow's on-disk shape — the backend stores the
 * `dag_json` column as `{nodes, edges}` of this form.
 */
export interface RFNodeData {
  subtype?: string;
  params?: Record<string, unknown>;
}

export interface RFNode {
  id: string;
  type: NodeType;
  position: Position;
  data: RFNodeData;
}

export interface RFEdge {
  id: string;
  source: string;
  target: string;
  sourceHandle?: string;
  targetHandle?: string;
}

export interface DAG {
  nodes: RFNode[];
  edges: RFEdge[];
}

// =============================================================================
// Strategy (matches `model.Strategy` + `orchestrator.StrategyDetail`)
// =============================================================================

export type StrategyStatus = 'draft' | 'active' | 'disabled' | 'archived';

export const STRATEGY_STATUS_LABELS: Record<StrategyStatus, string> = {
  draft: '草稿',
  active: '已启用',
  disabled: '已停用',
  archived: '已归档',
};

export const STRATEGY_STATUS_COLORS: Record<StrategyStatus, string> = {
  draft: 'bg-slate-100 text-slate-600',
  active: 'bg-emerald-100 text-emerald-700',
  disabled: 'bg-rose-100 text-rose-700',
  archived: 'bg-slate-200 text-slate-500',
};

export interface Strategy {
  id: number;
  code: string;
  name: string;
  description?: string;
  status: StrategyStatus;
  tags?: string;
  current_version: number;
  /** JSON-encoded DAG — the ReactFlow shape. */
  dag_json?: string;
  cron_expression?: string;
  created_at: string;
  updated_at: string;
}

export interface StrategyDetail extends Strategy {
  current_version_dag?: string;
  current_version_note?: string;
}

export interface StrategyListResponse {
  items: Strategy[];
  total: number;
  page: number;
  page_size: number;
}

export interface CreateStrategyInput {
  code?: string;
  name: string;
  description?: string;
  tags?: string;
  dag_json: string;
}

export interface UpdateStrategyInput {
  name: string;
  description?: string;
  tags?: string;
  dag_json: string;
  change_note?: string;
  /** draft | active | disabled. Omit (undefined) to leave unchanged. */
  status?: StrategyStatus;
  /** 5-field cron. Active strategies need one to actually fire; omit to leave unchanged. */
  cron_expression?: string;
}

// =============================================================================
// Templates (built-in)
// =============================================================================

export interface StrategyTemplate {
  code: string;
  name: string;
  description: string;
  industry?: string;
  built_in: boolean;
  ai_explanation?: string;
  dag_json: string;
}

export interface TemplateListResponse {
  items: StrategyTemplate[];
  total: number;
}

// =============================================================================
// Trial run
// =============================================================================

export interface TrialRunRequest {
  inputs?: Record<string, unknown>;
  /** When set, runs the DAG only up to (and including) this node id. */
  target_node_id?: string;
}

export interface TrialRunNodeResult {
  node_id: string;
  status: 'success' | 'failed' | 'skipped';
  started_at?: string;
  finished_at?: string;
  duration_ms?: number;
  error?: string;
  payload?: Record<string, unknown>;
}

export interface TrialRunSummary {
  status: 'success' | 'failed' | 'partial' | 'skipped';
  started_at?: string;
  finished_at?: string;
  duration_ms?: number;
  error?: string;
  node_results: TrialRunNodeResult[];
}

export interface TrialRunResponse {
  summary: TrialRunSummary;
}

// =============================================================================
// Run / RunLog (matches `model.Run` + `model.RunLog`)
// =============================================================================

export type RunStatus = 'pending' | 'running' | 'success' | 'failed' | 'partial' | 'skipped';
export type RunTrigger = 'manual' | 'cron';
export type RunLogStatus = 'success' | 'failed' | 'skipped';

export const RUN_STATUS_LABELS: Record<RunStatus, string> = {
  pending: '待执行',
  running: '执行中',
  success: '成功',
  failed: '失败',
  partial: '部分成功',
  skipped: '已跳过',
};

export const RUN_STATUS_COLORS: Record<RunStatus, string> = {
  pending: 'bg-slate-100 text-slate-600',
  running: 'bg-blue-100 text-blue-700',
  success: 'bg-emerald-100 text-emerald-700',
  failed: 'bg-rose-100 text-rose-700',
  partial: 'bg-amber-100 text-amber-700',
  skipped: 'bg-slate-100 text-slate-500',
};

export const RUN_LOG_STATUS_COLORS: Record<RunLogStatus, string> = {
  success: 'text-emerald-600',
  failed: 'text-rose-600',
  skipped: 'text-slate-500',
};

export interface Run {
  id: number;
  strategy_id: number;
  trigger_type: RunTrigger;
  status: RunStatus;
  started_at?: string;
  finished_at?: string;
  skip_reason?: string;
  error?: string;
  log_url?: string;
  attempts: number;
  created_at: string;
  updated_at: string;
}

export interface RunLog {
  id: number;
  run_id: number;
  node_key: string;
  status: RunLogStatus;
  started_at: string;
  finished_at: string;
  payload_in?: string;
  payload_out?: string;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface RunListResponse {
  items: Run[];
  total: number;
  page: number;
  page_size: number;
}

export interface RunLogsResponse {
  items: RunLog[];
}

export interface TriggerRunRequest {
  strategy_id?: number;
  strategy_code?: string;
  inputs?: Record<string, unknown>;
}

export interface TriggerRunResponse {
  run_id: number;
  strategy_id: number;
  status: RunStatus;
}

export interface RetryRunResponse {
  run_id: number;
  retry_of: number;
}

// =============================================================================
// Recommendation (matches `model.Recommendation` + `model.RecommendationTag`)
// =============================================================================

export interface RecommendationTag {
  id: number;
  recommendation_id: number;
  tag: string;
  source: string;
  tagged_at: string;
}

export interface Recommendation {
  id: number;
  run_id: number;
  date: string;
  stock_code: string;
  stock_name: string;
  entry_price_low: number;
  entry_price_high: number;
  strategy_code: string;
  strategy_name: string;
  node_snapshot?: string;
  tags?: RecommendationTag[];
  created_at: string;
  updated_at: string;
}

export interface RecommendationListResponse {
  items: Recommendation[];
  total: number;
  page: number;
  page_size: number;
}

// =============================================================================
// SSE event (matches `orchestrator.Event`)
// =============================================================================

export type EventType =
  | 'run.started'
  | 'run.completed'
  | 'node.started'
  | 'node.success'
  | 'node.failed'
  | 'node.skipped'
  | 'log'
  | 'heartbeat'
  | 'ready';

export interface RunEvent {
  run_id: number;
  node_id?: string;
  type: EventType;
  ts: string;
  data?: Record<string, unknown>;
}
