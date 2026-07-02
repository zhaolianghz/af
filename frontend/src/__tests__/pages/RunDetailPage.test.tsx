// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';

const getRunMock = vi.fn();
const getRunLogsMock = vi.fn();
const retryRunMock = vi.fn();
const openRunEventStreamMock = vi.fn((_runId: number, _handlers: unknown) => ({ close: vi.fn() }));
const navigateMock = vi.fn();

vi.mock('@/api/runs', () => ({
  getRun: (id: number) => getRunMock(id),
  getRunLogs: (id: number) => getRunLogsMock(id),
  retryRun: (id: number) => retryRunMock(id),
  openRunEventStream: (runId: number, handlers: unknown) => openRunEventStreamMock(runId, handlers),
}));
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});
vi.mock('react-hot-toast', () => ({
  default: { error: vi.fn(), success: vi.fn(), dismiss: vi.fn() },
  toast: vi.fn(),
}));

import RunDetailPage from '@/pages/RunDetailPage';
import type { Run, RunLog } from '@/types/orchestrator';

const SAMPLE_RUN: Run = {
  id: 42,
  strategy_id: 5,
  status: 'success',
  trigger_type: 'manual',
  attempts: 1,
  started_at: '2026-06-17T10:00:00Z',
  finished_at: '2026-06-17T10:00:01Z',
  created_at: '2026-06-17T10:00:00Z',
  updated_at: '2026-06-17T10:00:00Z',
};

const SAMPLE_LOG: RunLog = {
  id: 1,
  run_id: 42,
  node_key: 'data_source',
  status: 'success',
  started_at: '2026-06-17T10:00:00Z',
  finished_at: '2026-06-17T10:00:01Z',
  created_at: '2026-06-17T10:00:00Z',
  updated_at: '2026-06-17T10:00:00Z',
};

function renderDetail(id = '42') {
  return render(
    <MemoryRouter initialEntries={[`/runs/${id}`]}>
      <Routes>
        <Route path="/runs/:id" element={<RunDetailPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  getRunMock.mockReset();
  getRunLogsMock.mockReset();
  retryRunMock.mockReset();
  navigateMock.mockReset();
  getRunMock.mockResolvedValue(SAMPLE_RUN);
  getRunLogsMock.mockResolvedValue([SAMPLE_LOG]);
});

describe('RunDetailPage', () => {
  it('loads run + logs and renders the header + meta + timeline + log viewer', async () => {
    renderDetail();
    await waitFor(() => {
      expect(screen.getByText('成功')).toBeInTheDocument();
    });
    expect(screen.getByText('#42')).toBeInTheDocument();
    expect(screen.getByText('data_source')).toBeInTheDocument();
  });

  it('shows an error UI when the run id is invalid', async () => {
    renderDetail('not-a-number');
    await waitFor(() => {
      expect(screen.getByText('无效的 Run ID')).toBeInTheDocument();
    });
  });

  it('shows an error UI when getRun rejects', async () => {
    getRunMock.mockRejectedValue(new Error('not found'));
    renderDetail();
    await waitFor(() => {
      expect(screen.getByText('not found')).toBeInTheDocument();
    });
  });

  it('retry button shows confirm dialog; on confirm, navigates to the new run', async () => {
    const user = userEvent.setup();
    retryRunMock.mockResolvedValue({ run_id: 43 });
    renderDetail();
    await waitFor(() => screen.getByText('重试'));
    await user.click(screen.getByText('重试'));

    await waitFor(() => screen.getByRole('dialog'));
    expect(screen.getByText('重试运行')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '确认' }));

    await waitFor(() => {
      expect(retryRunMock).toHaveBeenCalledWith(42);
    });
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith('/runs/43');
    });
  });

  it('cancel from confirm dialog does not call retryRun', async () => {
    const user = userEvent.setup();
    renderDetail();
    await waitFor(() => screen.getByText('重试'));
    await user.click(screen.getByText('重试'));
    await waitFor(() => screen.getByRole('dialog'));
    await user.click(screen.getByRole('button', { name: '取消' }));

    expect(retryRunMock).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('retry failure shows an inline error', async () => {
    const user = userEvent.setup();
    retryRunMock.mockRejectedValue(new Error('节点 graph 校验失败'));
    renderDetail();
    await waitFor(() => screen.getByText('重试'));
    await user.click(screen.getByText('重试'));
    await user.click(screen.getByRole('button', { name: '确认' }));

    await waitFor(() => {
      expect(screen.getByText('节点 graph 校验失败')).toBeInTheDocument();
    });
  });

  it('Escape key navigates back to /runs', async () => {
    renderDetail();
    await waitFor(() => screen.getByText('重试'));
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(navigateMock).toHaveBeenCalledWith('/runs');
  });

  it('Ctrl+R triggers the retry flow (opens confirm dialog)', () => {
    renderDetail();
    return waitFor(() => screen.getByText('重试')).then(() => {
      fireEvent.keyDown(window, { key: 'r', ctrlKey: true });
      return waitFor(() => screen.getByRole('dialog'));
    });
  });

  it('Cmd+R triggers the retry flow too', () => {
    renderDetail();
    return waitFor(() => screen.getByText('重试')).then(() => {
      fireEvent.keyDown(window, { key: 'R', metaKey: true });
      return waitFor(() => screen.getByRole('dialog'));
    });
  });

  it('plain "r" (no modifier) does not open the confirm dialog', async () => {
    renderDetail();
    await waitFor(() => screen.getByText('重试'));
    fireEvent.keyDown(window, { key: 'r' });
    // No dialog should appear.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
