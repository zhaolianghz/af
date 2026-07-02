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

export function useCanvasHotkeys(onSave: () => void): void {
  const selectedNodeId = useCanvasStore((s) => s.selectedNodeId);
  const removeNode = useCanvasStore((s) => s.removeNode);
  const onSaveRef = useRef(onSave);
  onSaveRef.current = onSave;

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

      // Delete / Backspace → remove selected node.
      if ((e.key === 'Delete' || e.key === 'Backspace') && selectedNodeId) {
        e.preventDefault();
        removeNode(selectedNodeId);
      }
    };

    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selectedNodeId, removeNode]);
}
