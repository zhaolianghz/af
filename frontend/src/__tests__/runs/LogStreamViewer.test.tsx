// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// Mock the openRunEventStream so we can drive the EventSource from tests
// without an actual network. We expose `simulateEvent` / `simulateError` /
// `simulateOpen` helpers that the tests use to push events.
type Handlers = {
  onOpen?: () => void;
  onEvent?: (e: { id: string | null; event: string; data: string }) => void;
  onError?: (err: unknown) => void;
};

let capturedHandlers: Handlers = {};
const closeMock = vi.fn();
const openRunEventStreamMock = vi.fn((_runId: number, handlers: Handlers) => {
  capturedHandlers = handlers;
  return { close: closeMock };
});

vi.mock('@/api/runs', () => ({
  openRunEventStream: (runId: number, handlers: Handlers) =>
    openRunEventStreamMock(runId, handlers),
}));

import LogStreamViewer from '@/components/runs/LogStreamViewer';

function renderAndConnect() {
  render(<LogStreamViewer runId={42} />);
  // The component opens the stream in useEffect; the mock captured the handlers.
  return capturedHandlers;
}

beforeEach(() => {
  openRunEventStreamMock.mockClear();
  closeMock.mockClear();
  capturedHandlers = {};
});

describe('LogStreamViewer', () => {
  it('opens the stream on mount with the runId', () => {
    renderAndConnect();
    expect(openRunEventStreamMock).toHaveBeenCalledTimes(1);
    expect(openRunEventStreamMock).toHaveBeenCalledWith(42, expect.any(Object));
  });

  it('shows "connecting" until onOpen fires', () => {
    renderAndConnect();
    expect(screen.getByText('连接中')).toBeInTheDocument();
    expect(screen.getByText(/0 条事件/)).toBeInTheDocument();
  });

  it('transitions to "online" and renders events when onEvent fires', () => {
    const h = renderAndConnect();
    act(() => h.onOpen?.());
    expect(screen.getByText('在线')).toBeInTheDocument();

    act(() => {
      h.onEvent?.({
        id: '1',
        event: 'node.started',
        data: JSON.stringify({
          run_id: 42,
          type: 'node.started',
          ts: '2026-06-17T10:00:00Z',
          node_id: 'ds1',
          data: { message: 'data source started' },
        }),
      });
    });
    expect(screen.getByText('1 条事件')).toBeInTheDocument();
    expect(screen.getByText('node.started')).toBeInTheDocument();
    expect(screen.getByText('[ds1]')).toBeInTheDocument();
    expect(screen.getByText('data source started')).toBeInTheDocument();
  });

  it('ignores events whose run_id does not match the page', () => {
    const h = renderAndConnect();
    act(() => h.onOpen?.());

    act(() => {
      h.onEvent?.({
        id: '1',
        event: 'node.started',
        data: JSON.stringify({
          run_id: 999, // different
          type: 'node.started',
          ts: '2026-06-17T10:00:00Z',
        }),
      });
    });
    expect(screen.getByText(/0 条事件/)).toBeInTheDocument();
  });

  it('caps the event buffer at 2000 entries (oldest dropped)', () => {
    const h = renderAndConnect();
    act(() => h.onOpen?.());

    act(() => {
      for (let i = 0; i < 2050; i++) {
        h.onEvent?.({
          id: String(i),
          event: 'log',
          data: JSON.stringify({
            run_id: 42,
            type: 'log',
            ts: '2026-06-17T10:00:00Z',
            node_id: `n${i}`,
            data: { message: `m${i}` },
          }),
        });
      }
    });
    // 2000 kept, 50 dropped — total stays 2000
    expect(screen.getByText(/2000 条事件/)).toBeInTheDocument();
  });

  it('pause/continue toggle suppresses incoming events', async () => {
    const user = userEvent.setup();
    const h = renderAndConnect();
    act(() => h.onOpen?.());

    // Add one event so the pause button is reachable
    act(() => {
      h.onEvent?.({
        id: '1',
        event: 'log',
        data: JSON.stringify({
          run_id: 42, type: 'log', ts: '2026-06-17T10:00:00Z',
          data: { message: 'first' },
        }),
      });
    });
    expect(screen.getByText(/1 条事件/)).toBeInTheDocument();

    // Pause
    await user.click(screen.getByRole('button', { name: '暂停' }));
    expect(screen.getByText(/已暂停/)).toBeInTheDocument();

    // New event while paused should NOT increase count
    act(() => {
      h.onEvent?.({
        id: '2',
        event: 'log',
        data: JSON.stringify({
          run_id: 42, type: 'log', ts: '2026-06-17T10:00:01Z',
          data: { message: 'second (dropped)' },
        }),
      });
    });
    expect(screen.getByText(/1 条事件/)).toBeInTheDocument();

    // Resume
    await user.click(screen.getByRole('button', { name: '继续' }));
    act(() => {
      h.onEvent?.({
        id: '3',
        event: 'log',
        data: JSON.stringify({
          run_id: 42, type: 'log', ts: '2026-06-17T10:00:02Z',
          data: { message: 'third' },
        }),
      });
    });
    expect(screen.getByText(/2 条事件/)).toBeInTheDocument();
  });

  it('clear button empties the buffer', async () => {
    const user = userEvent.setup();
    const h = renderAndConnect();
    act(() => h.onOpen?.());
    act(() => {
      h.onEvent?.({
        id: '1',
        event: 'log',
        data: JSON.stringify({
          run_id: 42, type: 'log', ts: '2026-06-17T10:00:00Z',
          data: { message: 'm' },
        }),
      });
    });
    await user.click(screen.getByRole('button', { name: '清空' }));
    expect(screen.getByText('0 条事件')).toBeInTheDocument();
  });

  it('manual reconnect closes the old stream and opens a new one', async () => {
    const user = userEvent.setup();
    renderAndConnect();
    expect(openRunEventStreamMock).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole('button', { name: '重连' }));
    expect(closeMock).toHaveBeenCalledTimes(1);
    expect(openRunEventStreamMock).toHaveBeenCalledTimes(2);
  });

  it('shows error banner when onError fires', () => {
    const h = renderAndConnect();
    act(() => {
      // Pass a non-Event error to hit the fallback message branch.
      h.onError?.(new Error('connection refused'));
    });
    expect(screen.getByText('SSE 连接异常')).toBeInTheDocument();
    expect(screen.getByText('异常')).toBeInTheDocument();
  });

  it('closes the stream on unmount', () => {
    const { unmount } = render(<LogStreamViewer runId={42} />);
    expect(closeMock).not.toHaveBeenCalled();
    unmount();
    expect(closeMock).toHaveBeenCalledTimes(1);
  });

  it('ignores malformed JSON frames silently', () => {
    const h = renderAndConnect();
    act(() => h.onOpen?.());

    act(() => {
      h.onEvent?.({ id: '1', event: 'message', data: 'not-json{' });
    });
    // No crash, no event added.
    expect(screen.getByText(/0 条事件/)).toBeInTheDocument();
  });

  it('toolbar background turns amber when paused (visual indicator)', async () => {
    const user = userEvent.setup();
    const h = renderAndConnect();
    act(() => h.onOpen?.());

    // Before pause: slate background.
    const toolbarBefore = document.querySelector('.bg-slate-50');
    expect(toolbarBefore).not.toBeNull();

    await user.click(screen.getByRole('button', { name: '暂停' }));

    // After pause: amber background.
    const toolbarAfter = document.querySelector('.bg-amber-50');
    expect(toolbarAfter).not.toBeNull();
  });
});
