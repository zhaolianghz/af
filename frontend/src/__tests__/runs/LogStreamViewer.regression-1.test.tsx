// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
// Regression: ISSUE-003 — SSE control frames (the "ready" handshake:
// {"run_id":42} with no type/ts) were buffered and rendered as
// "Invalid Date" in the live log, and inflated the event count ("1 条事件"
// before any real event). Control frames must be dropped.
// Found by /qa on 2026-07-07
// Report: .gstack/qa-reports/qa-report-localhost-2026-07-07.md
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';

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
  return capturedHandlers;
}

beforeEach(() => {
  openRunEventStreamMock.mockClear();
  closeMock.mockClear();
  capturedHandlers = {};
});

describe('LogStreamViewer — ISSUE-003 control frames', () => {
  it('drops the "ready" handshake frame (run_id only, no type/ts)', () => {
    const h = renderAndConnect();
    act(() => h.onOpen?.());

    // The exact frame the backend sends on stream open:
    // event: ready / data: {"run_id":42}
    act(() => {
      h.onEvent?.({ id: null, event: 'ready', data: JSON.stringify({ run_id: 42 }) });
    });
    expect(screen.getByText(/0 条事件/)).toBeInTheDocument();
    expect(screen.queryByText('Invalid Date')).not.toBeInTheDocument();
  });

  it('drops a frame with type but missing ts', () => {
    const h = renderAndConnect();
    act(() => h.onOpen?.());

    act(() => {
      h.onEvent?.({
        id: '1',
        event: 'log',
        data: JSON.stringify({ run_id: 42, type: 'log' }), // no ts
      });
    });
    expect(screen.getByText(/0 条事件/)).toBeInTheDocument();
    expect(screen.queryByText('Invalid Date')).not.toBeInTheDocument();
  });

  it('still renders a complete run event after a control frame', () => {
    const h = renderAndConnect();
    act(() => h.onOpen?.());

    act(() => {
      h.onEvent?.({ id: null, event: 'ready', data: JSON.stringify({ run_id: 42 }) });
      h.onEvent?.({
        id: '1',
        event: 'node.started',
        data: JSON.stringify({
          run_id: 42,
          type: 'node.started',
          ts: '2026-07-07T10:00:00Z',
          node_id: 'ds1',
          data: { message: 'started' },
        }),
      });
    });
    expect(screen.getByText('1 条事件')).toBeInTheDocument();
    expect(screen.getByText('node.started')).toBeInTheDocument();
    expect(screen.queryByText('Invalid Date')).not.toBeInTheDocument();
  });
});
