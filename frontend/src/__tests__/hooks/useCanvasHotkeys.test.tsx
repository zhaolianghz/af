// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { fireEvent } from '@testing-library/dom';
import { useCanvasHotkeys } from '@/hooks/useCanvasHotkeys';
import { useCanvasStore } from '@/stores/canvasStore';

function selectAndAddNode(type: 'filter' | 'rank' = 'filter'): string {
  const id = useCanvasStore.getState().addNode(type, { x: 0, y: 0 });
  useCanvasStore.getState().selectNode(id);
  return id;
}

describe('useCanvasHotkeys', () => {
  beforeEach(() => {
    useCanvasStore.getState().reset();
  });

  it('removes the selected node on Delete', () => {
    const id = selectAndAddNode();
    renderHook(() => useCanvasHotkeys(vi.fn()));

    fireEvent.keyDown(window, { key: 'Delete' });
    expect(useCanvasStore.getState().nodes.find((n) => n.id === id)).toBeUndefined();
  });

  it('removes the selected node on Backspace', () => {
    const id = selectAndAddNode();
    renderHook(() => useCanvasHotkeys(vi.fn()));

    fireEvent.keyDown(window, { key: 'Backspace' });
    expect(useCanvasStore.getState().nodes.find((n) => n.id === id)).toBeUndefined();
  });

  it('does NOT remove a node when nothing is selected', () => {
    const id = useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    renderHook(() => useCanvasHotkeys(vi.fn()));

    fireEvent.keyDown(window, { key: 'Delete' });
    expect(useCanvasStore.getState().nodes.find((n) => n.id === id)).toBeDefined();
  });

  it('does NOT remove a node when Delete is pressed inside an input', () => {
    const id = selectAndAddNode();
    renderHook(() => useCanvasHotkeys(vi.fn()));

    // Simulate a keydown originating from an <input> element.
    const input = document.createElement('input');
    document.body.appendChild(input);
    fireEvent.keyDown(input, { key: 'Delete' });
    document.body.removeChild(input);

    expect(useCanvasStore.getState().nodes.find((n) => n.id === id)).toBeDefined();
  });

  it('does NOT remove a node when Delete is pressed inside a textarea', () => {
    const id = selectAndAddNode();
    renderHook(() => useCanvasHotkeys(vi.fn()));

    const ta = document.createElement('textarea');
    document.body.appendChild(ta);
    fireEvent.keyDown(ta, { key: 'Delete' });
    document.body.removeChild(ta);

    expect(useCanvasStore.getState().nodes.find((n) => n.id === id)).toBeDefined();
  });

  it('does NOT remove a node when Delete is pressed inside a select', () => {
    const id = selectAndAddNode();
    renderHook(() => useCanvasHotkeys(vi.fn()));

    const sel = document.createElement('select');
    document.body.appendChild(sel);
    fireEvent.keyDown(sel, { key: 'Delete' });
    document.body.removeChild(sel);

    expect(useCanvasStore.getState().nodes.find((n) => n.id === id)).toBeDefined();
  });

  it('Ctrl+S triggers the onSave callback', () => {
    const onSave = vi.fn();
    renderHook(() => useCanvasHotkeys(onSave));

    fireEvent.keyDown(window, { key: 's', ctrlKey: true });
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it('Cmd+S triggers the onSave callback', () => {
    const onSave = vi.fn();
    renderHook(() => useCanvasHotkeys(onSave));

    fireEvent.keyDown(window, { key: 's', metaKey: true });
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it('plain "s" without modifier does NOT trigger save', () => {
    const onSave = vi.fn();
    renderHook(() => useCanvasHotkeys(onSave));

    fireEvent.keyDown(window, { key: 's' });
    expect(onSave).not.toHaveBeenCalled();
  });

  it('removes the global keydown listener on unmount', () => {
    const id = selectAndAddNode();
    const { unmount } = renderHook(() => useCanvasHotkeys(vi.fn()));

    unmount();
    fireEvent.keyDown(window, { key: 'Delete' });

    // Node should still be present — listener was removed.
    expect(useCanvasStore.getState().nodes.find((n) => n.id === id)).toBeDefined();
  });
});
