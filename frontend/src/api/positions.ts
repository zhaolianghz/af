// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Positions API client (§10). Backs the holdings ledger.
 *
 *   GET    /positions        list open positions + live P&L + summary
 *   POST   /positions        mark/add a position (manual cost + qty)
 *   PATCH  /positions/:id     edit cost / quantity / note
 *   DELETE /positions/:id     close (soft-delete)
 */
import { apiClient } from './client';

export interface PositionView {
  id: number;
  stock_code: string;
  stock_name: string;
  cost_price: number;
  quantity: number;
  opened_at: string;
  source_recommendation_id: number;
  note: string;
  current_price: number | null;
  market_value: number | null;
  cost_value: number;
  pnl: number | null;
  pnl_pct: number | null;
}

export interface PositionSummary {
  count: number;
  total_cost_value: number;
  total_market_value: number | null;
  total_pnl: number | null;
  priced_count: number;
}

export interface PositionsResponse {
  items: PositionView[];
  summary: PositionSummary;
}

export interface CreatePositionInput {
  stock_code: string;
  stock_name?: string;
  cost_price: number;
  quantity: number;
  opened_at?: string;
  source_recommendation_id?: number;
  note?: string;
}

export async function listPositions(): Promise<PositionsResponse> {
  const { data } = await apiClient.get<{ code: number; data: PositionsResponse }>('/positions');
  return data.data;
}

export async function createPosition(input: CreatePositionInput): Promise<{ id: number }> {
  const { data } = await apiClient.post<{ code: number; data: { id: number } }>('/positions', input);
  return data.data;
}

export async function updatePosition(
  id: number,
  patch: { cost_price?: number; quantity?: number; note?: string },
): Promise<void> {
  await apiClient.patch(`/positions/${id}`, patch);
}

export async function closePosition(id: number): Promise<void> {
  await apiClient.delete(`/positions/${id}`);
}
