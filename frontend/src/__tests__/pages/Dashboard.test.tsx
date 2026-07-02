// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

const getDashboardMock = vi.fn();
const getAggregationsMock = vi.fn();
const listPositionsMock = vi.fn();

vi.mock('@/api/dashboard', () => ({
  getDashboard: (d: number) => getDashboardMock(d),
  getAggregations: (g: string) => getAggregationsMock(g),
}));
vi.mock('@/api/positions', () => ({
  listPositions: () => listPositionsMock(),
}));
// ECharts renders to canvas (unavailable in jsdom) — stub it.
vi.mock('echarts-for-react', () => ({
  default: () => null,
}));

import Dashboard from '@/pages/Dashboard';
import type { DashboardSummary } from '@/api/dashboard';

const SUMMARY: DashboardSummary = {
  today_recommendations: 3,
  total_recommendations: 42,
  runs_window_days: 7,
  runs_total: 10,
  runs_success: 9,
  runs_failed: 1,
  success_rate: 0.9,
  recent_errors: [],
};

function renderPage() {
  return render(
    <MemoryRouter>
      <Dashboard />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  getDashboardMock.mockReset();
  getAggregationsMock.mockReset();
  listPositionsMock.mockReset();
  getAggregationsMock.mockResolvedValue({ items: [] });
  listPositionsMock.mockResolvedValue({ summary: { count: 0, total_cost_value: 0, total_market_value: null, total_pnl: null, priced_count: 0 } });
});

describe('Dashboard', () => {
  it('renders stat cards from the summary', async () => {
    getDashboardMock.mockResolvedValue(SUMMARY);
    renderPage();
    await waitFor(() => expect(screen.getByText('42')).toBeInTheDocument()); // total_recommendations
    expect(screen.getByText('今日新增推荐')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument(); // today
    expect(screen.getByText('90.0%')).toBeInTheDocument(); // success_rate
  });

  it('shows an error banner when the core dashboard call fails', async () => {
    getDashboardMock.mockRejectedValue(new Error('backend down'));
    renderPage();
    await waitFor(() => expect(screen.getByText(/加载失败/)).toBeInTheDocument());
    expect(screen.getByText(/backend down/)).toBeInTheDocument();
  });

  it('still renders when aggregations + positions fail (best-effort)', async () => {
    getDashboardMock.mockResolvedValue(SUMMARY);
    getAggregationsMock.mockRejectedValue(new Error('perf disabled'));
    listPositionsMock.mockRejectedValue(new Error('no positions'));
    renderPage();
    // Core stats still render; no error banner (those two are best-effort).
    await waitFor(() => expect(screen.getByText('42')).toBeInTheDocument());
    expect(screen.queryByText(/加载失败/)).not.toBeInTheDocument();
  });

  it('shows empty win-rate state when no aggregations', async () => {
    getDashboardMock.mockResolvedValue(SUMMARY);
    getAggregationsMock.mockResolvedValue({ items: [] });
    renderPage();
    await waitFor(() => expect(screen.getByText('暂无胜率数据')).toBeInTheDocument());
  });
});
