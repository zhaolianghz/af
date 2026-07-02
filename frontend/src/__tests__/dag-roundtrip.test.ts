// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * DAG round-trip tests — pin the contract that the ReactFlow
 * canvas state, after `stringifyDag → parseDag`, is identical
 * to the original. This is the core save/reload guarantee.
 *
 * The backend stores the `dag_json` column as the ReactFlow
 * native `{nodes, edges}` shape (see
 * `backend/internal/orchestrator/dag.go` ParseReactFlowJSON).
 * Any field the canvas emits must survive a round-trip; any
 * field ParseReactFlowJSON drops must be a deliberate choice
 * documented here.
 */
import { describe, it, expect } from 'vitest';
import { parseDag, stringifyDag } from '@/api/strategies';
import type { DAG, RFNode, RFEdge, NodeType } from '@/types/orchestrator';

// =============================================================================
// parseDag — input handling
// =============================================================================

describe('parseDag — input handling', () => {
  it('returns empty DAG for undefined', () => {
    expect(parseDag(undefined)).toEqual({ nodes: [], edges: [] });
  });

  it('returns empty DAG for null', () => {
    expect(parseDag(null)).toEqual({ nodes: [], edges: [] });
  });

  it('returns empty DAG for empty string', () => {
    expect(parseDag('')).toEqual({ nodes: [], edges: [] });
  });

  it('returns empty DAG for invalid JSON', () => {
    expect(parseDag('{not json')).toEqual({ nodes: [], edges: [] });
  });

  it('returns empty DAG for JSON missing nodes/edges keys', () => {
    expect(parseDag('{}')).toEqual({ nodes: [], edges: [] });
  });

  it('coerces non-array nodes to []', () => {
    const out = parseDag(JSON.stringify({ nodes: 'not-an-array', edges: [] }));
    expect(out.nodes).toEqual([]);
    expect(out.edges).toEqual([]);
  });

  it('coerces non-array edges to []', () => {
    const out = parseDag(JSON.stringify({ nodes: [], edges: null }));
    expect(out.nodes).toEqual([]);
    expect(out.edges).toEqual([]);
  });

  it('preserves extra top-level fields silently (forward-compat)', () => {
    // The backend's ReactFlow-on-disk shape may grow new
    // top-level keys (e.g. "viewport"); parseDag must not
    // crash, and the two keys we care about remain arrays.
    const out = parseDag(JSON.stringify({
      nodes: [],
      edges: [],
      viewport: { x: 10, y: 20, zoom: 1.5 },
    }));
    expect(out.nodes).toEqual([]);
    expect(out.edges).toEqual([]);
  });
});

// =============================================================================
// stringifyDag — output shape
// =============================================================================

describe('stringifyDag — output shape', () => {
  it('always produces a JSON object with nodes and edges keys', () => {
    const out = stringifyDag({ nodes: [], edges: [] });
    const parsed = JSON.parse(out);
    expect(Array.isArray(parsed.nodes)).toBe(true);
    expect(Array.isArray(parsed.edges)).toBe(true);
  });

  it('serializes empty DAG compactly', () => {
    expect(stringifyDag({ nodes: [], edges: [] })).toBe('{"nodes":[],"edges":[]}');
  });

  it('drops undefined optional fields rather than emitting them', () => {
    const node: RFNode = {
      id: 'n1',
      type: 'filter',
      position: { x: 0, y: 0 },
      data: { params: { field: 'close' } }, // subtype intentionally absent
    };
    const out = stringifyDag({ nodes: [node], edges: [] });
    const parsed = JSON.parse(out) as DAG;
    expect(parsed.nodes[0].data.subtype).toBeUndefined();
    expect(parsed.nodes[0].data.params).toEqual({ field: 'close' });
  });
});

// =============================================================================
// Round-trip identity (the core save/reload guarantee)
// =============================================================================

describe('round-trip identity (stringifyDag ∘ parseDag = identity)', () => {
  it('round-trips an empty DAG', () => {
    const dag: DAG = { nodes: [], edges: [] };
    expect(parseDag(stringifyDag(dag))).toEqual(dag);
  });

  it('round-trips a single node without edges', () => {
    const dag: DAG = {
      nodes: [
        {
          id: 'ds1',
          type: 'data_source',
          position: { x: 100, y: 200 },
          data: { subtype: 'kline', params: { stock_codes: ['600519.SH'], period: '1d', days: 30 } },
        },
      ],
      edges: [],
    };
    expect(parseDag(stringifyDag(dag))).toEqual(dag);
  });

  it('round-trips a linear 3-node DAG', () => {
    const dag: DAG = {
      nodes: [
        { id: 'a', type: 'data_source', position: { x: 0, y: 0 }, data: { subtype: 'kline', params: { stock_codes: ['000001.SZ'] } } },
        { id: 'b', type: 'filter', position: { x: 200, y: 0 }, data: { params: { field: 'close', op: '>', value: 10 } } },
        { id: 'c', type: 'persist', position: { x: 400, y: 0 }, data: { params: { extra_tags: [] } } },
      ],
      edges: [
        { id: 'e1', source: 'a', target: 'b', sourceHandle: 'out', targetHandle: 'in' },
        { id: 'e2', source: 'b', target: 'c', sourceHandle: 'out', targetHandle: 'in' },
      ],
    };
    expect(parseDag(stringifyDag(dag))).toEqual(dag);
  });

  it('round-trips negative and fractional positions', () => {
    const dag: DAG = {
      nodes: [
        { id: 'n', type: 'filter', position: { x: -123.45, y: -0.001 }, data: {} },
      ],
      edges: [],
    };
    expect(parseDag(stringifyDag(dag))).toEqual(dag);
  });

  it('round-trips edges without handles (handle-less links)', () => {
    const dag: DAG = {
      nodes: [
        { id: 'a', type: 'data_source', position: { x: 0, y: 0 }, data: {} },
        { id: 'b', type: 'filter', position: { x: 1, y: 1 }, data: {} },
      ],
      edges: [{ id: 'e1', source: 'a', target: 'b' }],
    };
    expect(parseDag(stringifyDag(dag))).toEqual(dag);
  });

  it('round-trips duplicate edge IDs (the serializer does not dedupe)', () => {
    // The serializer is a pure pass-through; deduping is the
    // canvas's responsibility. Pinning this so a future "helpful"
    // change to stringifyDag does not silently drop dupes.
    const dag: DAG = {
      nodes: [
        { id: 'a', type: 'data_source', position: { x: 0, y: 0 }, data: {} },
        { id: 'b', type: 'filter', position: { x: 1, y: 1 }, data: {} },
      ],
      edges: [
        { id: 'e1', source: 'a', target: 'b' },
        { id: 'e1', source: 'a', target: 'b' },
      ],
    };
    expect(parseDag(stringifyDag(dag))).toEqual(dag);
  });
});

// =============================================================================
// All 8 node types × canonical defaults round-trip
// =============================================================================

describe('all 8 node types round-trip with their canvas defaults', () => {
  const types: NodeType[] = [
    'data_source', 'indicator', 'filter', 'rank',
    'dedupe', 'session_tag', 'persist', 'notify',
  ];

  for (const type of types) {
    it(`round-trips a ${type} node`, () => {
      const node = buildDefaultNode(type, 0);
      const dag: DAG = { nodes: [node], edges: [] };
      const rt = parseDag(stringifyDag(dag));
      expect(rt.nodes).toHaveLength(1);
      expect(rt.nodes[0]).toEqual(node);
    });
  }

  it('round-trips all 8 node types in a single DAG', () => {
    const nodes = types.map((t, i) => buildDefaultNode(t, i));
    const dag: DAG = { nodes, edges: [] };
    expect(parseDag(stringifyDag(dag))).toEqual(dag);
  });
});

// =============================================================================
// All 7 indicator subtypes round-trip
// =============================================================================

describe('all 7 indicator subtypes round-trip', () => {
  const subtypes = ['ma', 'ema', 'macd', 'kdj', 'boll', 'volume_ratio', 'turnover_rate'];

  for (const sub of subtypes) {
    it(`round-trips indicator subtype=${sub}`, () => {
      const node: RFNode = {
        id: `ind_${sub}`,
        type: 'indicator',
        position: { x: 0, y: 0 },
        data: { subtype: sub, params: indicatorParamsFor(sub) },
      };
      const dag: DAG = { nodes: [node], edges: [] };
      const rt = parseDag(stringifyDag(dag));
      expect(rt.nodes[0]).toEqual(node);
    });
  }
});

// =============================================================================
// Helpers
// =============================================================================

function buildDefaultNode(type: NodeType, offset: number): RFNode {
  const pos = { x: offset * 100, y: offset * 50 };
  switch (type) {
    case 'data_source':
      return {
        id: `n_${type}`,
        type,
        position: pos,
        data: { subtype: 'kline', params: { stock_codes: ['600519.SH', '000858.SZ'], period: '1d', days: 60 } },
      };
    case 'indicator':
      return {
        id: `n_${type}`,
        type,
        position: pos,
        data: { subtype: 'ma', params: { period: 20 } },
      };
    case 'filter':
      return {
        id: `n_${type}`,
        type,
        position: pos,
        data: { params: { field: 'chg_pct', op: '>', value: 0 } },
      };
    case 'rank':
      return {
        id: `n_${type}`,
        type,
        position: pos,
        data: { params: { field: 'chg_pct', order: 'desc', top: 20 } },
      };
    case 'dedupe':
      return {
        id: `n_${type}`,
        type,
        position: pos,
        data: { params: { key: 'stock_code' } },
      };
    case 'session_tag':
      return {
        id: `n_${type}`,
        type,
        position: pos,
        data: {},
      };
    case 'persist':
      return {
        id: `n_${type}`,
        type,
        position: pos,
        data: { params: { extra_tags: [] } },
      };
    case 'notify':
      return {
        id: `n_${type}`,
        type,
        position: pos,
        data: { subtype: 'morning' },
      };
  }
}

function indicatorParamsFor(sub: string): Record<string, unknown> {
  switch (sub) {
    case 'ma': return { period: 5 };
    case 'ema': return { period: 12 };
    case 'macd': return { fast: 12, slow: 26, signal: 9 };
    case 'kdj': return { n: 9, m1: 3, m2: 3 };
    case 'boll': return { period: 20, k_stddev: 2 };
    case 'volume_ratio': return { period: 5 };
    case 'turnover_rate': return { period: 5 };
    default: return {};
  }
}

// Sanity: the edge-type import is exercised to prevent
// accidental removal of RFEdge from the public surface.
const _edgeTypeCheck: RFEdge = {
  id: 'x', source: 'a', target: 'b',
};
void _edgeTypeCheck;
