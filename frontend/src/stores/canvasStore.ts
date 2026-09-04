// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Canvas store — the single source of truth for the strategy
 * editor's ReactFlow graph + selection + save state.
 *
 * Why a separate store vs. lifting everything into the page:
 *   1. NodePalette / NodeConfigPanel both need to read+write the
 *      graph; a store keeps the prop drilling honest.
 *   2. Save state ("idle / saving / saved / error") crosses both
 *      the toolbar and the dirty indicator.
 *   3. The trial-run panel wants the *current* graph even if the
 *      user has not yet clicked Save.
 *
 * Mirrors the structure of the Go `dag_json` shape:
 * `{nodes:[{id,type,position,data:{subtype,params}}], edges:[{...}]}`.
 */
import { create } from 'zustand';
import { applyNodeChanges, applyEdgeChanges, type EdgeChange, type NodeChange, type Connection } from 'reactflow';
import type { DAG, NodeType, RFEdge, RFNode } from '@/types/orchestrator';

export type SaveStatus = 'idle' | 'dirty' | 'saving' | 'saved' | 'error';
export type ValidationStatus = 'unknown' | 'valid' | 'invalid';

export interface CanvasState {
  // Graph data (mirrors `dag_json`).
  nodes: RFNode[];
  edges: RFEdge[];

  // Selection.
  selectedNodeId: string | null;
  selectedEdgeId: string | null;

  // Save + validation state.
  saveStatus: SaveStatus;
  saveError: string | null;
  validation: ValidationStatus;
  validationError: string | null;

  // Graph-level metadata (kept for the page, not persisted in the graph).
  strategyId: number | null;
  strategyName: string;
  strategyCode: string;

  // ---------------------------------------------------------------------------
  // Loaders
  // ---------------------------------------------------------------------------

  /** Replace the entire graph (called on page load + after Save). */
  loadFromDag: (dag: DAG, meta?: { id?: number; name?: string; code?: string }) => void;
  reset: () => void;

  // ---------------------------------------------------------------------------
  // ReactFlow change handlers
  // ---------------------------------------------------------------------------

  onNodesChange: (changes: NodeChange[]) => void;
  onEdgesChange: (changes: EdgeChange[]) => void;
  onConnect: (conn: Connection) => void;

  // ---------------------------------------------------------------------------
  // Selection
  // ---------------------------------------------------------------------------

  selectNode: (id: string | null) => void;
  selectEdge: (id: string | null) => void;

  // ---------------------------------------------------------------------------
  // Mutations
  // ---------------------------------------------------------------------------

  addNode: (type: NodeType, position: { x: number; y: number }) => string;
  updateNodeData: (id: string, patch: Partial<RFNode['data']>) => void;
  applyPositions: (positions: Record<string, { x: number; y: number }>) => void;
  removeNode: (id: string) => void;
  removeEdge: (id: string) => void;

  // ---------------------------------------------------------------------------
  // Save status
  // ---------------------------------------------------------------------------

  setSaveStatus: (s: SaveStatus, err?: string | null) => void;
  setValidation: (s: ValidationStatus, err?: string | null) => void;
}

const initialState = {
  nodes: [] as RFNode[],
  edges: [] as RFEdge[],
  selectedNodeId: null as string | null,
  selectedEdgeId: null as string | null,
  saveStatus: 'idle' as SaveStatus,
  saveError: null as string | null,
  validation: 'unknown' as ValidationStatus,
  validationError: null as string | null,
  strategyId: null as number | null,
  strategyName: '',
  strategyCode: '',
};

/** Pick a unique node id (e.g. "filter" -> "filter_2"). */
function uniqueId(base: string, taken: Set<string>): string {
  if (!taken.has(base)) return base;
  let i = 2;
  while (taken.has(`${base}_${i}`)) i++;
  return `${base}_${i}`;
}

export const useCanvasStore = create<CanvasState>((set, get) => ({
  ...initialState,

  loadFromDag: (dag, meta) =>
    set((s) => ({
      ...initialState,
      nodes: dag.nodes,
      edges: dag.edges,
      strategyId: meta?.id ?? s.strategyId,
      strategyName: meta?.name ?? s.strategyName,
      strategyCode: meta?.code ?? s.strategyCode,
      saveStatus: 'saved',
      validation: dag.nodes.length > 0 ? 'valid' : 'unknown',
      validationError: null,
    })),

  reset: () => set(initialState),

  onNodesChange: (changes) =>
    set((s) => ({
      nodes: applyNodeChanges(changes, s.nodes) as RFNode[],
      // Position/size/data changes mark the graph dirty; pure selection
      // changes don't. External callers (EdgeView's setEdges dispatch)
      // also land here, so this is where the delete-via-✕ dirty flag
      // originates — removeEdge via the hotkey path sets it itself.
      saveStatus:
        changes.some((c) => c.type !== 'select') ? 'dirty' : s.saveStatus,
    })),

  onEdgesChange: (changes) =>
    set((s) => ({
      edges: applyEdgeChanges(changes, s.edges) as RFEdge[],
      saveStatus:
        changes.some((c) => c.type !== 'select') ? 'dirty' : s.saveStatus,
    })),

  onConnect: (conn) =>
    set((s) => {
      if (!conn.source || !conn.target) return s;
      const taken = new Set(s.edges.map((e) => e.id));
      const id = uniqueId(`e_${conn.source}_${conn.target}`, taken);
      const newEdge: RFEdge = {
        id,
        source: conn.source,
        target: conn.target,
        sourceHandle: conn.sourceHandle ?? undefined,
        targetHandle: conn.targetHandle ?? undefined,
      };
      return {
        edges: [...s.edges, newEdge],
        saveStatus: 'dirty',
      };
    }),

  selectNode: (id) => set({ selectedNodeId: id, selectedEdgeId: null }),
  selectEdge: (id) => set({ selectedEdgeId: id, selectedNodeId: null }),

  addNode: (type, position) => {
    const id = uniqueId(type, new Set(get().nodes.map((n) => n.id)));
    const defaults = defaultParamsFor(type);
    const { subtype, params } = splitDefaults(defaults);
    const newNode: RFNode = {
      id,
      type,
      position,
      data: { subtype, params },
    };
    set((s) => ({ nodes: [...s.nodes, newNode], saveStatus: 'dirty' }));
    return id;
  },

  updateNodeData: (id, patch) =>
    set((s) => ({
      nodes: s.nodes.map((n) =>
        n.id === id ? { ...n, data: { ...n.data, ...patch } } : n,
      ),
      saveStatus: 'dirty',
    })),

  // Batch position update (auto-layout). One set() so the whole
  // reposition is a single dirty-flagging state transition.
  applyPositions: (positions) =>
    set((s) => ({
      nodes: s.nodes.map((n) =>
        positions[n.id] ? { ...n, position: positions[n.id] } : n,
      ),
      saveStatus: 'dirty',
    })),

  removeNode: (id) =>
    set((s) => ({
      nodes: s.nodes.filter((n) => n.id !== id),
      edges: s.edges.filter((e) => e.source !== id && e.target !== id),
      selectedNodeId: s.selectedNodeId === id ? null : s.selectedNodeId,
      saveStatus: 'dirty',
    })),

  removeEdge: (id) =>
    set((s) => ({
      edges: s.edges.filter((e) => e.id !== id),
      selectedEdgeId: s.selectedEdgeId === id ? null : s.selectedEdgeId,
      saveStatus: 'dirty',
    })),

  setSaveStatus: (saveStatus, saveError = null) => set({ saveStatus, saveError }),
  setValidation: (validation, validationError = null) => set({ validation, validationError }),
}));

// =============================================================================
// Defaults
// =============================================================================

/** Best-effort sensible defaults so a new node "just works" in trial-run. */
function defaultParamsFor(type: NodeType): Record<string, unknown> {
  switch (type) {
    case 'data_source':
      return {
        subtype: 'kline',
        stock_codes: ['600519.SH', '000858.SZ'],
        period: '1d',
        days: 60,
      };
    case 'indicator':
      return { subtype: 'ma', period: 20 };
    case 'filter':
      return { field: 'chg_pct', op: '>', value: 0 };
    case 'rank':
      return { field: 'chg_pct', order: 'desc', top: 20 };
    case 'dedupe':
      return { key: 'stock_code' };
    case 'session_tag':
      return {};
    case 'persist':
      return { extra_tags: [] };
    case 'notify':
      return { channel_type: 'morning' };
  }
}

/** Pull `subtype` out of the flat defaults blob so it sits at the top of `data`. */
function splitDefaults(
  defaults: Record<string, unknown>,
): { subtype?: string; params: Record<string, unknown> } {
  const { subtype, ...rest } = defaults as { subtype?: string } & Record<string, unknown>;
  return { subtype, params: rest };
}
