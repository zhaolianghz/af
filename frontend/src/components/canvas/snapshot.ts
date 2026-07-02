// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Snapshot the current canvas graph as the `dag_json` JSON string
 * the backend stores. Centralized here so Toolbar (a React
 * component module) can re-export only its UI piece without
 * dragging in non-component helpers that trip react-refresh.
 */
import { useCanvasStore } from '@/stores/canvasStore';
import { stringifyDag } from '@/api/strategies';

export function snapshotDagJson(): string {
  const { nodes, edges } = useCanvasStore.getState();
  return stringifyDag({ nodes, edges });
}