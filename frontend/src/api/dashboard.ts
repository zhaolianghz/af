// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Dashboard API client. Backs the landing page (§10).
 *
 * - GET /dashboard            — executor.Handler.Dashboard
 * - GET /perf/aggregations    — perf.Handler.Aggregations (win-rate by group)
 */
import { apiClient } from './client';

export interface DashboardRunError {
  run_id: number;
  strategy_id: number;
  error: string;
  finished_at: string;
}

export interface DashboardSummary {
  today_recommendations: number;
  total_recommendations: number;
  runs_window_days: number;
  runs_total: number;
  runs_success: number;
  runs_failed: number;
  success_rate: number | null;
  recent_errors: DashboardRunError[];
}

export interface AggregationRow {
  key: string;
  count: number;
  avg_t1_return: number | null;
  avg_t3_return: number | null;
  avg_t5_return: number | null;
  avg_max_drawdown: number | null;
  win_rate_t5: number | null;
}

export interface AggregationsResponse {
  group_by: string;
  total: number;
  items: AggregationRow[];
}

export async function getDashboard(windowDays = 7): Promise<DashboardSummary> {
  const { data } = await apiClient.get<{ code: number; data: DashboardSummary }>(
    '/dashboard',
    { params: { window_days: windowDays } },
  );
  return data.data;
}

export async function getAggregations(
  groupBy: 'strategy' | 'session_tag' | 'stock' = 'strategy',
): Promise<AggregationsResponse> {
  const { data, status } = await apiClient.get<{ code: number; data: AggregationsResponse }>(
    '/perf/aggregations',
    {
      params: { group_by: groupBy },
      // The perf engine is optional (perf.enabled=false in sqlite dev):
      // its routes are then simply not mounted and the call 404s. That
      // is an expected deployment shape, not an error — accepting 404
      // here keeps the axios error interceptor (and its console.error)
      // out of every dashboard load on a perf-less install.
      validateStatus: (s) => s === 200 || s === 404,
    },
  );
  if (status === 404) {
    return { group_by: groupBy, total: 0, items: [] };
  }
  return data.data;
}
