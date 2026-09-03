// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { layeredLayout } from '@/components/canvas/layout';
import type { RFNode } from '@/types/orchestrator';

function mkNodes(ids: string[]): RFNode[] {
  return ids.map((id) => ({
    id,
    type: 'filter',
    position: { x: 0, y: 0 },
    data: { params: {} },
  }));
}

describe('layeredLayout', () => {
  it('places an empty graph without positions', () => {
    expect(layeredLayout([], []).positions).toEqual({});
  });

  it('lines a linear chain left to right', () => {
    const nodes = mkNodes(['a', 'b', 'c']);
    const { positions } = layeredLayout(nodes, [
      { source: 'a', target: 'b' },
      { source: 'b', target: 'c' },
    ]);
    expect(positions.a.x).toBeLessThan(positions.b.x);
    expect(positions.b.x).toBeLessThan(positions.c.x);
    // One node per column → all vertically centered at the same y.
    expect(positions.a.y).toBe(positions.b.y);
    expect(positions.b.y).toBe(positions.c.y);
  });

  it('puts parallel branches in the same column and joins downstream', () => {
    // a → b, a → c, b → d, c → d (diamond)
    const nodes = mkNodes(['a', 'b', 'c', 'd']);
    const { positions } = layeredLayout(nodes, [
      { source: 'a', target: 'b' },
      { source: 'a', target: 'c' },
      { source: 'b', target: 'd' },
      { source: 'c', target: 'd' },
    ]);
    // b and c share the middle column (same x), stacked vertically.
    expect(positions.b.x).toBe(positions.c.x);
    expect(positions.b.y).not.toBe(positions.c.y);
    // a leftmost, d rightmost.
    expect(positions.a.x).toBeLessThan(positions.b.x);
    expect(positions.d.x).toBeGreaterThan(positions.b.x);
  });

  it('survives a cycle without hanging', () => {
    const nodes = mkNodes(['a', 'b']);
    const { positions } = layeredLayout(nodes, [
      { source: 'a', target: 'b' },
      { source: 'b', target: 'a' },
    ]);
    // Both nodes got positions (no infinite loop / missing entries).
    expect(Object.keys(positions).sort()).toEqual(['a', 'b']);
  });

  it('ignores edges referencing unknown nodes', () => {
    const nodes = mkNodes(['a']);
    const { positions } = layeredLayout(nodes, [{ source: 'ghost', target: 'a' }]);
    expect(Object.keys(positions)).toEqual(['a']);
  });
});
