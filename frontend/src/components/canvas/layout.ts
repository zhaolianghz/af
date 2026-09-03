// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * layout.ts — zero-dependency layered layout for the strategy DAG.
 *
 * Assigns each node a "level" = longest path from any root (Kahn
 * topological layers), then packs nodes column-by-column left→right,
 * centering each column vertically around the widest column's midline.
 * Cross edges (cycles / joins) just follow node levels, so the result
 * always flows source → sink left to right like the mental model of a
 * pipeline.
 *
 * Cycles can't happen in saved DAGs (backend rejects them) but the
 * layout must not hang on one anyway — the Kahn queue simply stops,
 * and remaining nodes get placed after the last computed layer.
 */
import type { RFNode } from '@/types/orchestrator';

const NODE_W = 220; // ~ max-w-[240px] card + gap
const NODE_H = 90; // approx card height + vertical gap

export interface LayoutResult {
  positions: Record<string, { x: number; y: number }>;
}

export function layeredLayout(
  nodes: RFNode[],
  edges: { source: string; target: string }[],
): LayoutResult {
  if (nodes.length === 0) return { positions: {} };

  // ---- longest-path layering via Kahn peeling ----
  const idSet = new Set(nodes.map((n) => n.id));
  const out = new Map<string, string[]>();
  const indeg = new Map<string, number>();
  for (const n of nodes) {
    out.set(n.id, []);
    indeg.set(n.id, 0);
  }
  for (const e of edges) {
    if (!idSet.has(e.source) || !idSet.has(e.target) || e.source === e.target) continue;
    out.get(e.source)!.push(e.target);
    indeg.set(e.target, (indeg.get(e.target) ?? 0) + 1);
  }

  const level = new Map<string, number>();
  let queue = nodes.filter((n) => (indeg.get(n.id) ?? 0) === 0).map((n) => n.id);
  for (const id of queue) level.set(id, 0);
  while (queue.length > 0) {
    const next: string[] = [];
    for (const id of queue) {
      for (const t of out.get(id) ?? []) {
        // target sits at least one layer below its deepest source
        level.set(t, Math.max(level.get(t) ?? 0, (level.get(id) ?? 0) + 1));
        indeg.set(t, (indeg.get(t) ?? 0) - 1);
        if ((indeg.get(t) ?? 0) === 0) next.push(t);
      }
    }
    queue = next;
  }
  // Cycle leftovers (shouldn't happen): park them one layer past the max.
  let maxLevel = 0;
  for (const v of level.values()) if (v > maxLevel) maxLevel = v;
  for (const n of nodes) if (!level.has(n.id)) level.set(n.id, maxLevel + 1);

  // ---- bucket by level, pack columns ----
  const columns = new Map<number, string[]>();
  for (const n of nodes) {
    const l = level.get(n.id) ?? 0;
    if (!columns.has(l)) columns.set(l, []);
    columns.get(l)!.push(n.id);
  }

  const sortedLevels = [...columns.keys()].sort((a, b) => a - b);
  const tallest = Math.max(...sortedLevels.map((l) => columns.get(l)!.length));
  const positions: Record<string, { x: number; y: number }> = {};

  for (const l of sortedLevels) {
    const col = columns.get(l)!;
    const startY = ((tallest - col.length) / 2) * NODE_H; // center vs tallest column
    col.forEach((id, i) => {
      positions[id] = { x: l * NODE_W, y: startY + i * NODE_H };
    });
  }
  return { positions };
}
