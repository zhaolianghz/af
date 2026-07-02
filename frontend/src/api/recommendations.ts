// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Recommendation API client. Mirrors
 * `backend/internal/executor/handler.go::ListRecommendations`.
 *
 * The list endpoint is the only read endpoint in v1; mutations
 * happen implicitly via `persist` DAG nodes writing rows.
 */
import { apiClient } from './client';
import type {
  Recommendation,
  RecommendationListResponse,
} from '@/types/orchestrator';

export interface ListRecommendationsParams {
  strategy_code?: string;
  tag?: string;
  from?: string;
  to?: string;
  page?: number;
  page_size?: number;
}

export async function listRecommendations(
  params: ListRecommendationsParams = {},
): Promise<RecommendationListResponse> {
  const { data } = await apiClient.get<{
    code: number;
    data: RecommendationListResponse;
  }>('/recommendations', { params });
  return data.data;
}

/** Convert the current page to a CSV string for export. */
export function recommendationsToCsv(items: Recommendation[]): string {
  const header = [
    'id',
    'date',
    'stock_code',
    'stock_name',
    'entry_price_low',
    'entry_price_high',
    'strategy_code',
    'strategy_name',
    'tags',
  ];
  const rows = items.map((r) => {
    const tags = (r.tags ?? []).map((t) => t.tag).join('|');
    return [
      r.id,
      r.date,
      r.stock_code,
      r.stock_name,
      r.entry_price_low,
      r.entry_price_high,
      r.strategy_code,
      r.strategy_name,
      tags,
    ]
      .map(csvCell)
      .join(',');
  });
  return [header.join(','), ...rows].join('\n');
}

function csvCell(v: unknown): string {
  if (v === null || v === undefined) return '';
  const s = String(v);
  if (/[",\n]/.test(s)) return `"${s.replace(/"/g, '""')}"`;
  return s;
}