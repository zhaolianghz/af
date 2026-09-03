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

  // ---------------------------------------------------------------------------
  // Edge deletion + fit-view hotkeys
  // ---------------------------------------------------------------------------

  it('removes the selected edge on Delete', () => {
    const a = useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    const b = useCanvasStore.getState().addNode('rank', { x: 300, y: 0 });
    useCanvasStore.getState().onConnect({ source: a, target: b, sourceHandle: null, targetHandle: null });
    const edgeId = useCanvasStore.getState().edges[0].id;
    useCanvasStore.getState().selectEdge(edgeId);
    renderHook(() => useCanvasHotkeys(vi.fn()));

    fireEvent.keyDown(window, { key: 'Delete' });
    expect(useCanvasStore.getState().edges).toHaveLength(0);
    // Both nodes survive.
    expect(useCanvasStore.getState().nodes).toHaveLength(2);
  });

  it('removes the selected edge on Backspace', () => {
    const a = useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    const b = useCanvasStore.getState().addNode('rank', { x: 300, y: 0 });
    useCanvasStore.getState().onConnect({ source: a, target: b, sourceHandle: null, targetHandle: null });
    useCanvasStore.getState().selectEdge(useCanvasStore.getState().edges[0].id);
    renderHook(() => useCanvasHotkeys(vi.fn()));

    fireEvent.keyDown(window, { key: 'Backspace' });
    expect(useCanvasStore.getState().edges).toHaveLength(0);
  });

  it('node deletion wins when both a node and an edge are selected', () => {
    const a = useCanvasStore.getState().addNode('filter', { x: 0, y: 0 });
    const b = useCanvasStore.getState().addNode('rank', { x: 300, y: 0 });
    useCanvasStore.getState().onConnect({ source: a, target: b, sourceHandle: null, targetHandle: null });
    useCanvasStore.getState().selectEdge(useCanvasStore.getState().edges[0].id);
    useCanvasStore.getState().selectNode(a);
    renderHook(() => useCanvasHotkeys(vi.fn()));

    fireEvent.keyDown(window, { key: 'Delete' });
    expect(useCanvasStore.getState().nodes).toHaveLength(1);
  });

  it('Shift+1 and Shift+F trigger fit view', () => {
    const fit = vi.fn();
    renderHook(() => useCanvasHotkeys(vi.fn(), fit));

    fireEvent.keyDown(window, { key: '!', shiftKey: true });
    fireEvent.keyDown(window, { key: 'F', shiftKey: true });
    expect(fit).toHaveBeenCalledTimes(2);
  });

  it('does not hijack plain 1/F without shift', () => {
    const fit = vi.fn();
    renderHook(() => useCanvasHotkeys(vi.fn(), fit));

    fireEvent.keyDown(window, { key: '1' });
    fireEvent.keyDown(window, { key: 'F' });
    expect(fit).not.toHaveBeenCalled();
  });
});
