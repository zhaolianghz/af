// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * useCanvasHotkeys — keyboard shortcuts for the canvas editor.
 *
 * - Delete / Backspace: remove the currently selected node
 * - Ctrl/Cmd + S:     trigger save via the provided callback
 *
 * Only active when a canvas node is selected (not when an input/textarea
 * is focused).
 *
 * The onSave callback is held in a ref so callers can pass a fresh
 * closure on every render without us rebinding the global keydown
 * listener. Only `selectedNodeId` should cause a re-bind (it gates
 * the Delete/Backspace branch).
 */
import { useEffect, useRef } from 'react';
import { useCanvasStore } from '@/stores/canvasStore';

export function useCanvasHotkeys(onSave: () => void, onFitView?: () => void): void {
  const selectedNodeId = useCanvasStore((s) => s.selectedNodeId);
  const selectedEdgeId = useCanvasStore((s) => s.selectedEdgeId);
  const removeNode = useCanvasStore((s) => s.removeNode);
  const removeEdge = useCanvasStore((s) => s.removeEdge);
  const onSaveRef = useRef(onSave);
  onSaveRef.current = onSave;
  const onFitViewRef = useRef(onFitView);
  onFitViewRef.current = onFitView;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Never intercept when the user is typing in an input.
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;

      const mod = e.metaKey || e.ctrlKey;

      // Ctrl/Cmd+S → save.
      if (mod && e.key === 's') {
        e.preventDefault();
        onSaveRef.current();
        return;
      }

      // Shift+1 / Shift+F → fit view (ReactFlow's convention).
      if ((e.key === '!' || e.key === '1') && e.shiftKey && !mod) {
        e.preventDefault();
        onFitViewRef.current?.();
        return;
      }
      if (e.key === 'F' && e.shiftKey && !mod) {
        e.preventDefault();
        onFitViewRef.current?.();
        return;
      }

      // Delete / Backspace → remove the selected node OR edge.
      if (e.key === 'Delete' || e.key === 'Backspace') {
        if (selectedNodeId) {
          e.preventDefault();
          removeNode(selectedNodeId);
        } else if (selectedEdgeId) {
          e.preventDefault();
          removeEdge(selectedEdgeId);
        }
      }
    };

    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selectedNodeId, selectedEdgeId, removeNode, removeEdge]);
}
