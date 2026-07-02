// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const listPositionsMock = vi.fn();
const updatePositionMock = vi.fn();
const closePositionMock = vi.fn();
const notifyErrorMock = vi.fn();

vi.mock('@/api/positions', () => ({
  listPositions: () => listPositionsMock(),
  updatePosition: (id: number, body: unknown) => updatePositionMock(id, body),
  closePosition: (id: number) => closePositionMock(id),
}));
vi.mock('@/lib/notify', () => ({
  notifyError: (e: unknown, m?: string) => notifyErrorMock(e, m),
  notifySuccess: vi.fn(),
}));

import PositionsPage from '@/pages/PositionsPage';
import type { PositionView, PositionSummary } from '@/api/positions';

const POS: PositionView = {
  id: 1,
  stock_code: '600519.SH',
  stock_name: '贵州茅台',
  cost_price: 100,
  quantity: 200,
  opened_at: '2026-06-20',
  source_recommendation_id: 0,
  note: '',
  current_price: 120,
  market_value: 24000,
  cost_value: 20000,
  pnl: 4000,
  pnl_pct: 0.2,
};

const SUMMARY: PositionSummary = {
  count: 1,
  total_cost_value: 20000,
  total_market_value: 24000,
  total_pnl: 4000,
  priced_count: 1,
};

beforeEach(() => {
  listPositionsMock.mockReset();
  updatePositionMock.mockReset();
  closePositionMock.mockReset();
  notifyErrorMock.mockReset();
});

describe('PositionsPage', () => {
  it('shows loading then renders rows + summary', async () => {
    listPositionsMock.mockResolvedValue({ items: [POS], summary: SUMMARY });
    render(<PositionsPage />);
    // Loading state first.
    expect(screen.getByText('加载中…')).toBeInTheDocument();
    // Then the row.
    await waitFor(() => expect(screen.getByText('600519.SH')).toBeInTheDocument());
    expect(screen.getByText('贵州茅台')).toBeInTheDocument();
    expect(screen.getByText('20.00%')).toBeInTheDocument(); // pnl_pct
    // Summary card: 持仓数 = 1.
    expect(screen.getByText('持仓数')).toBeInTheDocument();
  });

  it('renders empty state when no positions', async () => {
    listPositionsMock.mockResolvedValue({
      items: [],
      summary: { count: 0, total_cost_value: 0, total_market_value: null, total_pnl: null, priced_count: 0 },
    });
    render(<PositionsPage />);
    await waitFor(() => expect(screen.getByText(/暂无持仓/)).toBeInTheDocument());
  });

  it('toasts on load failure', async () => {
    listPositionsMock.mockRejectedValue(new Error('boom'));
    render(<PositionsPage />);
    await waitFor(() => expect(notifyErrorMock).toHaveBeenCalled());
  });

  it('closes a position after confirm and reloads', async () => {
    listPositionsMock.mockResolvedValue({ items: [POS], summary: SUMMARY });
    closePositionMock.mockResolvedValue(undefined);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<PositionsPage />);
    await waitFor(() => expect(screen.getByText('600519.SH')).toBeInTheDocument());
    await userEvent.click(screen.getByText('平仓'));
    await waitFor(() => expect(closePositionMock).toHaveBeenCalledWith(1));
    // reload → listPositions called twice (initial + after close).
    expect(listPositionsMock).toHaveBeenCalledTimes(2);
  });

  it('does NOT close when confirm is cancelled', async () => {
    listPositionsMock.mockResolvedValue({ items: [POS], summary: SUMMARY });
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    render(<PositionsPage />);
    await waitFor(() => expect(screen.getByText('600519.SH')).toBeInTheDocument());
    await userEvent.click(screen.getByText('平仓'));
    expect(closePositionMock).not.toHaveBeenCalled();
  });

  it('rejects invalid edit (cost/qty must be > 0)', async () => {
    listPositionsMock.mockResolvedValue({ items: [POS], summary: SUMMARY });
    render(<PositionsPage />);
    await waitFor(() => expect(screen.getByText('600519.SH')).toBeInTheDocument());
    await userEvent.click(screen.getByText('编辑'));
    const inputs = screen.getAllByRole('textbox');
    await userEvent.clear(inputs[0]); // cost
    await userEvent.type(inputs[0], '0');
    await userEvent.click(screen.getByText('保存'));
    expect(updatePositionMock).not.toHaveBeenCalled();
    expect(notifyErrorMock).toHaveBeenCalled();
  });

  it('sets up a 30s auto-refresh interval on mount', async () => {
    const setIntervalSpy = vi.spyOn(window, 'setInterval');
    try {
      listPositionsMock.mockResolvedValue({ items: [POS], summary: SUMMARY });
      render(<PositionsPage />);
      await waitFor(() => expect(listPositionsMock).toHaveBeenCalled());
      expect(setIntervalSpy).toHaveBeenCalledWith(expect.any(Function), 30_000);
    } finally {
      setIntervalSpy.mockRestore();
    }
  });

  it('clears the interval when the tab becomes hidden', async () => {
    const clearIntervalSpy = vi.spyOn(window, 'clearInterval');
    try {
      listPositionsMock.mockResolvedValue({ items: [POS], summary: SUMMARY });
      render(<PositionsPage />);
      await waitFor(() => expect(listPositionsMock).toHaveBeenCalled());
      Object.defineProperty(document, 'hidden', { value: true, configurable: true });
      document.dispatchEvent(new Event('visibilitychange'));
      expect(clearIntervalSpy).toHaveBeenCalled();
    } finally {
      clearIntervalSpy.mockRestore();
      Object.defineProperty(document, 'hidden', { value: false, configurable: true });
    }
  });

  it('manual 刷新 button reloads the list', async () => {
    listPositionsMock.mockResolvedValue({ items: [POS], summary: SUMMARY });
    render(<PositionsPage />);
    await waitFor(() => expect(screen.getByText('600519.SH')).toBeInTheDocument());
    expect(listPositionsMock).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole('button', { name: /刷新/ }));
    await waitFor(() => expect(listPositionsMock).toHaveBeenCalledTimes(2));
  });
});
