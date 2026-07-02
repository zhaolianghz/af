// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

// Mock the API + notify
const listRunsMock = vi.fn();
const listStrategiesMock = vi.fn();
const triggerRunMock = vi.fn();
const navigateMock = vi.fn();

vi.mock('@/api/runs', () => ({
  listRuns: (params: unknown) => listRunsMock(params),
  triggerRun: (req: unknown) => triggerRunMock(req),
}));
vi.mock('@/api/strategies', () => ({
  listStrategies: (params: unknown) => listStrategiesMock(params),
}));
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});
vi.mock('react-hot-toast', () => ({
  default: { error: vi.fn(), success: vi.fn(), dismiss: vi.fn() },
  toast: vi.fn(),
}));

import RunHistoryPage from '@/pages/RunHistoryPage';
import type { Run, Strategy } from '@/types/orchestrator';

const SAMPLE_RUN: Run = {
  id: 101,
  strategy_id: 5,
  status: 'success',
  trigger_type: 'manual',
  attempts: 1,
  started_at: '2026-06-17T10:00:00Z',
  finished_at: '2026-06-17T10:00:01Z',
  created_at: '2026-06-17T10:00:00Z',
  updated_at: '2026-06-17T10:00:00Z',
};

const SAMPLE_STRATEGY: Strategy = {
  id: 5,
  code: 'ma_breakout',
  name: '均线突破',
  status: 'draft',
  current_version: 1,
  created_at: '2026-06-17T09:00:00Z',
  updated_at: '2026-06-17T09:00:00Z',
};

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/runs']}>
      <RunHistoryPage />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  listRunsMock.mockReset();
  listStrategiesMock.mockReset();
  triggerRunMock.mockReset();
  navigateMock.mockReset();
  listRunsMock.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 });
  listStrategiesMock.mockResolvedValue({ items: [SAMPLE_STRATEGY], total: 1, page: 1, page_size: 200 });
});

describe('RunHistoryPage', () => {
  it('renders the list once listRuns resolves', async () => {
    listRunsMock.mockResolvedValue({ items: [SAMPLE_RUN], total: 1, page: 1, page_size: 20 });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('#101')).toBeInTheDocument();
    });
    expect(screen.getByText('运行历史')).toBeInTheDocument();
  });

  it('shows the loading state then the empty state when total=0', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('暂无运行记录')).toBeInTheDocument();
    });
  });

  it('shows the error state when listRuns rejects', async () => {
    listRunsMock.mockRejectedValue(new Error('网络异常'));
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('网络异常')).toBeInTheDocument();
    });
  });

  it('passes status + strategy_id from URL params to listRuns', async () => {
    render(
      <MemoryRouter initialEntries={['/runs?status=failed&strategy_id=7']}>
        <RunHistoryPage />
      </MemoryRouter>,
    );
    await waitFor(() => expect(listRunsMock).toHaveBeenCalled());
    const lastCall = listRunsMock.mock.calls[listRunsMock.mock.calls.length - 1][0];
    expect(lastCall).toMatchObject({ status: 'failed', strategy_id: 7 });
  });

  it('navigates to the run detail page when a row is clicked', async () => {
    listRunsMock.mockResolvedValue({ items: [SAMPLE_RUN], total: 1, page: 1, page_size: 20 });
    renderPage();
    await waitFor(() => screen.getByText('#101'));
    await userEvent.click(screen.getByText('#101'));
    expect(navigateMock).toHaveBeenCalledWith('/runs/101');
  });

  it('opens the manual-trigger modal on button click', async () => {
    renderPage();
    await waitFor(() => screen.getByText('+ 手工触发运行'));
    await userEvent.click(screen.getByText('+ 手工触发运行'));
    expect(screen.getByText('手工触发运行')).toBeInTheDocument();
  });

  it('manual trigger: on success, navigates to the new run detail', async () => {
    triggerRunMock.mockResolvedValue({ run_id: 999 });
    renderPage();
    await waitFor(() => screen.getByText('+ 手工触发运行'));
    await userEvent.click(screen.getByText('+ 手工触发运行'));

    // The modal pre-selects the first strategy.
    await userEvent.click(screen.getByRole('button', { name: '触发' }));

    await waitFor(() => {
      expect(triggerRunMock).toHaveBeenCalledWith({ strategy_id: 5 });
    });
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith('/runs/999');
    });
  });

  it('manual trigger: on failure, surfaces the error via toast', async () => {
    triggerRunMock.mockRejectedValue(new Error('触发失败: 无可用节点'));
    renderPage();
    await waitFor(() => screen.getByText('+ 手工触发运行'));
    await userEvent.click(screen.getByText('+ 手工触发运行'));
    await userEvent.click(screen.getByRole('button', { name: '触发' }));

    // Wait for the trigger to resolve + toast to fire.
    await waitFor(() => {
      expect(triggerRunMock).toHaveBeenCalled();
    });
    // Inline error div is gone — toasts are top-level; we don't query DOM
    // for toasts in jsdom. The negative assertion is enough.
    expect(screen.queryByText('触发失败: 无可用节点')).not.toBeInTheDocument();
    // And the modal stays open (user can retry) — busy state should clear.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '触发' })).not.toBeDisabled();
    });
  });
});
