// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * canvasStore.test.ts — unit tests for the ReactFlow strategy
 * editor's Zustand store. M1 deliverable per FE1_PLAN.md.
 *
 * Scope:
 *   - addNode: returns unique id, sets per-type defaults, marks dirty
 *   - updateNodeData: immutability + dirty flag
 *   - loadFromDag: replaces entire graph, sets saveStatus='saved',
 *     sets validation='valid' iff nodes exist
 *   - removeNode: also removes connected edges
 *   - onConnect: creates edge, marks dirty
 *   - reset: clears all state
 *   - setSaveStatus / setValidation: state machine transitions
 *   - selectNode / selectEdge: mutually exclusive
 *   - 8 node type defaults: every type produces a usable node
 *
 * Notes:
 *   - We exercise the store via `useCanvasStore.getState()` rather
 *     than React's `useSyncExternalStore` to keep the test pure
 *     and synchronous.
 *   - `beforeEach(reset)` is mandatory — Zustand is a singleton
 *     across tests, and the M2 round-trip tests will rely on
 *     a clean slate.
 *   - These tests do not import React, do not touch the DOM, and
 *     do not mock anything. They run in < 50ms.
 */
import { describe, it, expect, beforeEach } from 'vitest';
import { useCanvasStore, type CanvasState } from './canvasStore';
import type { DAG, NodeType, RFEdge, RFNode } from '@/types/orchestrator';

const ALL_NODE_TYPES: NodeType[] = [
  'data_source',
  'indicator',
  'filter',
  'rank',
  'dedupe',
  'session_tag',
  'persist',
  'notify',
];

// =============================================================================
// Fixtures
// =============================================================================

function makeNode(id: string, type: NodeType = 'filter', position = { x: 0, y: 0 }): RFNode {
  return {
    id,
    type,
    position,
    data: { subtype: undefined, params: {} },
  };
}

function makeEdge(id: string, source: string, target: string): RFEdge {
  return { id, source, target };
}

function makeDag(nodes: RFNode[] = [], edges: RFEdge[] = []): DAG {
  return { nodes, edges };
}

// =============================================================================
// Lifecycle
// =============================================================================

describe('canvasStore — lifecycle', () => {
  beforeEach(() => {
    useCanvasStore.getState().reset();
  });

  it('starts in a clean idle state', () => {
    const s: CanvasState = useCanvasStore.getState();
    expect(s.nodes).toEqual([]);
    expect(s.edges).toEqual([]);
    expect(s.selectedNodeId).toBeNull();
    expect(s.selectedEdgeId).toBeNull();
    expect(s.saveStatus).toBe('idle');
    expect(s.saveError).toBeNull();
    expect(s.validation).toBe('unknown');
    expect(s.validationError).toBeNull();
    expect(s.strategyId).toBeNull();
    expect(s.strategyName).toBe('');
    expect(s.strategyCode).toBe('');
  });

  it('reset() returns the store to initial state', () => {
    const store = useCanvasStore.getState();
    store.addNode('filter', { x: 10, y: 20 });
    store.setSaveStatus('saving');
    store.setValidation('invalid', 'no data_source');
    expect(useCanvasStore.getState().nodes).toHaveLength(1);

    store.reset();

    const s = useCanvasStore.getState();
    expect(s.nodes).toEqual([]);
    expect(s.saveStatus).toBe('idle');
    expect(s.validation).toBe('unknown');
    expect(s.validationError).toBeNull();
  });
});

// =============================================================================
// addNode
// =============================================================================

describe('canvasStore — addNode', () => {
  beforeEach(() => {
    useCanvasStore.getState().reset();
  });

  it('returns the id of the new node', () => {
    const id = useCanvasStore.getState().addNode('filter', { x: 10, y: 20 });
    expect(id).toBe('filter');
    expect(useCanvasStore.getState().nodes).toHaveLength(1);
  });

  it('appends to the existing node list (does not replace)', () => {
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('rank', { x: 0, y: 0 });
    const { nodes } = useCanvasStore.getState();
    expect(nodes).toHaveLength(2);
    expect(nodes.map((n) => n.id)).toEqual(['filter', 'rank']);
  });

  it('disambiguates the id when the base is already taken', () => {
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    const second = useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    expect(second).toBe('filter_2');
    const third = useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    expect(third).toBe('filter_3');
  });

  it('sets the position the caller provided', () => {
    useCanvasStore.getState().addNode('rank', { x: 123, y: 456 });
    const [node] = useCanvasStore.getState().nodes;
    expect(node.position).toEqual({ x: 123, y: 456 });
  });

  it('marks saveStatus as dirty', () => {
    expect(useCanvasStore.getState().saveStatus).toBe('idle');
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    expect(useCanvasStore.getState().saveStatus).toBe('dirty');
  });

  // -------------------------------------------------------------------------
  // Per-type defaults
  // -------------------------------------------------------------------------

  it('data_source: defaults to kline, two A-share codes, 60 days, 1d period', () => {
    useCanvasStore.getState().addNode('data_source', { x: 0, y: 0 });
    const [{ data }] = useCanvasStore.getState().nodes;
    expect(data.subtype).toBe('kline');
    expect(data.params).toEqual({
      stock_codes: ['600519.SH', '000858.SZ'],
      period: '1d',
      days: 60,
    });
  });

  it('indicator: defaults to ma with period=20', () => {
    useCanvasStore.getState().addNode('indicator', { x: 0, y: 0 });
    const [{ data }] = useCanvasStore.getState().nodes;
    expect(data.subtype).toBe('ma');
    expect(data.params).toEqual({ period: 20 });
  });

  it('filter: defaults to chg_pct > 0', () => {
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    const [{ data }] = useCanvasStore.getState().nodes;
    expect(data.subtype).toBeUndefined();
    expect(data.params).toEqual({ field: 'chg_pct', op: '>', value: 0 });
  });

  it('rank: defaults to chg_pct desc top 20', () => {
    useCanvasStore.getState().addNode('rank', { x: 0, y: 0 });
    const [{ data }] = useCanvasStore.getState().nodes;
    expect(data.params).toEqual({ field: 'chg_pct', order: 'desc', top: 20 });
  });

  it('dedupe: defaults to key=stock_code', () => {
    useCanvasStore.getState().addNode('dedupe', { x: 0, y: 0 });
    const [{ data }] = useCanvasStore.getState().nodes;
    expect(data.params).toEqual({ key: 'stock_code' });
  });

  it('session_tag: empty params (no config required)', () => {
    useCanvasStore.getState().addNode('session_tag', { x: 0, y: 0 });
    const [{ data }] = useCanvasStore.getState().nodes;
    expect(data.params).toEqual({});
  });

  it('persist: defaults to extra_tags=[]', () => {
    useCanvasStore.getState().addNode('persist', { x: 0, y: 0 });
    const [{ data }] = useCanvasStore.getState().nodes;
    expect(data.params).toEqual({ extra_tags: [] });
  });

  it('notify: defaults to channel_type=morning', () => {
    useCanvasStore.getState().addNode('notify', { x: 0, y: 0 });
    const [{ data }] = useCanvasStore.getState().nodes;
    expect(data.params).toEqual({ channel_type: 'morning' });
  });

  it('every NodeType produces a node (exhaustive coverage of the 8-type contract)', () => {
    for (const t of ALL_NODE_TYPES) {
      useCanvasStore.getState().reset();
      const id = useCanvasStore.getState().addNode(t, { x: 0, y: 0 });
      expect(id).toBe(t);
      const [node] = useCanvasStore.getState().nodes;
      expect(node.type).toBe(t);
      expect(node.data).toBeDefined();
      expect(node.data.params).toBeDefined();
    }
  });
});

// =============================================================================
// updateNodeData
// =============================================================================

describe('canvasStore — updateNodeData', () => {
  beforeEach(() => {
    useCanvasStore.getState().reset();
  });

  it('patches the matched node data and leaves others alone', () => {
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('rank', { x: 0, y: 0 });
    useCanvasStore.getState().updateNodeData('filter', {
      params: { field: 'pe', op: '<', value: 30 },
    });
    const { nodes } = useCanvasStore.getState();
    expect(nodes[0].data.params).toEqual({ field: 'pe', op: '<', value: 30 });
    // rank node untouched
    expect(nodes[1].data.params).toEqual({ field: 'chg_pct', order: 'desc', top: 20 });
  });

  it('is immutable: returns a new array and a new node object', () => {
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    const before = useCanvasStore.getState().nodes;
    useCanvasStore.getState().updateNodeData('filter', { params: { field: 'pe' } });
    const after = useCanvasStore.getState().nodes;
    expect(after).not.toBe(before);
    expect(after[0]).not.toBe(before[0]);
    expect(after[0].data).not.toBe(before[0].data);
  });

  it('unknown id is a no-op (no throw, no mutation)', () => {
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    const before = useCanvasStore.getState().nodes;
    useCanvasStore.getState().updateNodeData('nope', { params: {} });
    const after = useCanvasStore.getState().nodes;
    expect(after).toEqual(before);
  });

  it('marks saveStatus as dirty', () => {
    useCanvasStore.getState().setSaveStatus('saved');
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().setSaveStatus('saved');
    useCanvasStore.getState().updateNodeData('filter', { params: {} });
    expect(useCanvasStore.getState().saveStatus).toBe('dirty');
  });
});

// =============================================================================
// loadFromDag
// =============================================================================

describe('canvasStore — loadFromDag', () => {
  beforeEach(() => {
    useCanvasStore.getState().reset();
  });

  it('replaces the entire graph (no append)', () => {
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('rank', { x: 0, y: 0 });
    expect(useCanvasStore.getState().nodes).toHaveLength(2);

    useCanvasStore.getState().loadFromDag(
      makeDag([makeNode('ds1', 'data_source')], [makeEdge('e1', 'ds1', 'filter')]),
      { id: 42, name: 'morning_breakout', code: 'mb' },
    );
    const { nodes, edges, strategyId, strategyName, strategyCode } = useCanvasStore.getState();
    expect(nodes).toHaveLength(1);
    expect(nodes[0].id).toBe('ds1');
    expect(edges).toHaveLength(1);
    expect(edges[0].source).toBe('ds1');
    expect(strategyId).toBe(42);
    expect(strategyName).toBe('morning_breakout');
    expect(strategyCode).toBe('mb');
  });

  it('sets saveStatus to saved (we just loaded a clean graph)', () => {
    useCanvasStore.getState().setSaveStatus('dirty');
    useCanvasStore.getState().loadFromDag(makeDag([makeNode('a')], []));
    expect(useCanvasStore.getState().saveStatus).toBe('saved');
    expect(useCanvasStore.getState().saveError).toBeNull();
  });

  it('sets validation to valid when the graph has nodes, unknown when empty', () => {
    useCanvasStore.getState().loadFromDag(makeDag([makeNode('a')], []));
    expect(useCanvasStore.getState().validation).toBe('valid');

    useCanvasStore.getState().loadFromDag(makeDag([], []));
    expect(useCanvasStore.getState().validation).toBe('unknown');
  });

  it('clears validationError on load', () => {
    useCanvasStore.getState().setValidation('invalid', 'no persist');
    useCanvasStore.getState().loadFromDag(makeDag([makeNode('a')], []));
    expect(useCanvasStore.getState().validationError).toBeNull();
  });
});

// =============================================================================
// removeNode
// =============================================================================

describe('canvasStore — removeNode', () => {
  beforeEach(() => {
    useCanvasStore.getState().reset();
  });

  it('removes the matched node and no others', () => {
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('rank', { x: 0, y: 0 });
    useCanvasStore.getState().removeNode('filter');
    const { nodes } = useCanvasStore.getState();
    expect(nodes).toHaveLength(1);
    expect(nodes[0].id).toBe('rank');
  });

  it('also removes edges connected to the removed node', () => {
    useCanvasStore.getState().addNode('data_source', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('rank', { x: 0, y: 0 });
    // Manually wire edges via loadFromDag (onConnect is tested below)
    useCanvasStore.getState().loadFromDag(
      makeDag(
        useCanvasStore.getState().nodes,
        [makeEdge('e1', 'data_source', 'filter'), makeEdge('e2', 'filter', 'rank')],
      ),
    );
    expect(useCanvasStore.getState().edges).toHaveLength(2);

    useCanvasStore.getState().removeNode('filter');
    const { edges, nodes } = useCanvasStore.getState();
    expect(nodes).toHaveLength(2);
    expect(edges).toHaveLength(0); // both edges touched filter — gone
  });

  it('clears selectedNodeId if the removed node was selected', () => {
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().selectNode('filter');
    expect(useCanvasStore.getState().selectedNodeId).toBe('filter');
    useCanvasStore.getState().removeNode('filter');
    expect(useCanvasStore.getState().selectedNodeId).toBeNull();
  });

  it('preserves selectedNodeId when a different node is removed', () => {
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('rank', { x: 0, y: 0 });
    useCanvasStore.getState().selectNode('rank');
    useCanvasStore.getState().removeNode('filter');
    expect(useCanvasStore.getState().selectedNodeId).toBe('rank');
  });
});

// =============================================================================
// removeEdge
// =============================================================================

describe('canvasStore — removeEdge', () => {
  beforeEach(() => {
    useCanvasStore.getState().reset();
  });

  it('removes the matched edge and no others', () => {
    useCanvasStore.getState().addNode('data_source', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('rank', { x: 0, y: 0 });
    useCanvasStore.getState().loadFromDag(
      useCanvasStore.getState().nodes
        ? {
            nodes: useCanvasStore.getState().nodes,
            edges: [
              makeEdge('e1', 'data_source', 'filter'),
              makeEdge('e2', 'filter', 'rank'),
            ],
          }
        : { nodes: [], edges: [] },
    );

    useCanvasStore.getState().removeEdge('e1');
    const { edges } = useCanvasStore.getState();
    expect(edges).toHaveLength(1);
    expect(edges[0].id).toBe('e2');
  });

  it('marks saveStatus as dirty', () => {
    useCanvasStore.getState().setSaveStatus('saved');
    useCanvasStore.getState().addNode('data_source', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().onConnect({ source: 'data_source', target: 'filter', sourceHandle: null, targetHandle: null });
    useCanvasStore.getState().setSaveStatus('saved');
    useCanvasStore.getState().removeEdge('e_data_source_filter');
    expect(useCanvasStore.getState().saveStatus).toBe('dirty');
  });

  it('clears selectedEdgeId if the removed edge was selected', () => {
    useCanvasStore.getState().addNode('data_source', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().onConnect({ source: 'data_source', target: 'filter', sourceHandle: null, targetHandle: null });
    useCanvasStore.getState().selectEdge('e_data_source_filter');
    useCanvasStore.getState().removeEdge('e_data_source_filter');
    expect(useCanvasStore.getState().selectedEdgeId).toBeNull();
  });
});

// =============================================================================
// onConnect
// =============================================================================

describe('canvasStore — onConnect', () => {
  beforeEach(() => {
    useCanvasStore.getState().reset();
  });

  it('creates a new edge from source to target', () => {
    useCanvasStore.getState().addNode('data_source', { x: 0, y: 0 });
    useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    useCanvasStore.getState().onConnect({
      source: 'data_source',
      target: 'filter',
      sourceHandle: null,
      targetHandle: null,
    });
    const { edges } = useCanvasStore.getState();
    expect(edges).toHaveLength(1);
    expect(edges[0].source).toBe('data_source');
    expect(edges[0].target).toBe('filter');
  });

  it('marks saveStatus as dirty', () => {
    useCanvasStore.getState().setSaveStatus('saved');
    useCanvasStore.getState().onConnect({ source: 'a', target: 'b', sourceHandle: null, targetHandle: null });
    expect(useCanvasStore.getState().saveStatus).toBe('dirty');
  });

  it('ignores connection with no source or no target', () => {
    const before = useCanvasStore.getState();
    before.onConnect({ source: null, target: 'b', sourceHandle: null, targetHandle: null });
    before.onConnect({ source: 'a', target: null, sourceHandle: null, targetHandle: null });
    expect(useCanvasStore.getState().edges).toEqual([]);
  });

  it('disambiguates the edge id when the same connection is made twice', () => {
    useCanvasStore.getState().onConnect({ source: 'a', target: 'b', sourceHandle: null, targetHandle: null });
    useCanvasStore.getState().onConnect({ source: 'a', target: 'b', sourceHandle: null, targetHandle: null });
    const { edges } = useCanvasStore.getState();
    expect(edges).toHaveLength(2);
    expect(edges[0].id).not.toBe(edges[1].id);
  });
});

// =============================================================================
// Selection
// =============================================================================

describe('canvasStore — selection', () => {
  beforeEach(() => {
    useCanvasStore.getState().reset();
  });

  it('selectNode clears selectedEdgeId (mutually exclusive)', () => {
    useCanvasStore.getState().selectEdge('e1');
    expect(useCanvasStore.getState().selectedEdgeId).toBe('e1');
    useCanvasStore.getState().selectNode('n1');
    expect(useCanvasStore.getState().selectedNodeId).toBe('n1');
    expect(useCanvasStore.getState().selectedEdgeId).toBeNull();
  });

  it('selectEdge clears selectedNodeId (mutually exclusive)', () => {
    useCanvasStore.getState().selectNode('n1');
    useCanvasStore.getState().selectEdge('e1');
    expect(useCanvasStore.getState().selectedEdgeId).toBe('e1');
    expect(useCanvasStore.getState().selectedNodeId).toBeNull();
  });

  it('passing null clears the selection', () => {
    useCanvasStore.getState().selectNode('n1');
    useCanvasStore.getState().selectNode(null);
    expect(useCanvasStore.getState().selectedNodeId).toBeNull();
  });
});

// =============================================================================
// Save / validation state machine
// =============================================================================

describe('canvasStore — setSaveStatus / setValidation', () => {
  beforeEach(() => {
    useCanvasStore.getState().reset();
  });

  it('setSaveStatus transitions across the state machine', () => {
    const store = useCanvasStore.getState();
    store.setSaveStatus('saving');
    expect(useCanvasStore.getState().saveStatus).toBe('saving');
    store.setSaveStatus('saved');
    expect(useCanvasStore.getState().saveStatus).toBe('saved');
    store.setSaveStatus('error', 'network down');
    expect(useCanvasStore.getState().saveStatus).toBe('error');
    expect(useCanvasStore.getState().saveError).toBe('network down');
  });

  it('setSaveStatus defaults saveError to null when omitted', () => {
    useCanvasStore.getState().setSaveStatus('error', 'bad');
    useCanvasStore.getState().setSaveStatus('idle');
    expect(useCanvasStore.getState().saveError).toBeNull();
  });

  it('setValidation transitions across the state machine', () => {
    const store = useCanvasStore.getState();
    store.setValidation('valid');
    expect(useCanvasStore.getState().validation).toBe('valid');
    store.setValidation('invalid', 'no data_source');
    expect(useCanvasStore.getState().validation).toBe('invalid');
    expect(useCanvasStore.getState().validationError).toBe('no data_source');
  });
});
