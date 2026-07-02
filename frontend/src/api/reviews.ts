// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Review reports API client (§14.9). Auto daily/weekly performance
 * reviews.
 *
 *   GET  /reviews[?kind=&limit=]   list recent reports
 *   GET  /reviews/:id              one report
 *   POST /reviews/generate {kind}  manual trigger
 */
import { apiClient } from './client';

export interface ReviewReport {
  id: number;
  kind: 'daily' | 'weekly';
  period_start: string;
  period_end: string;
  recommendation_count: number;
  win_rate_t5: number | null;
  avg_t5_return: number | null;
  summary: string;
  llm: string;
  created_at: string;
}

export async function listReviews(kind = '', limit = 30): Promise<ReviewReport[]> {
  const { data } = await apiClient.get<{ code: number; data: { items: ReviewReport[] } }>(
    '/reviews',
    { params: { kind: kind || undefined, limit } },
  );
  return data.data.items ?? [];
}

export async function generateReview(kind: 'daily' | 'weekly'): Promise<ReviewReport> {
  const { data } = await apiClient.post<{ code: number; data: ReviewReport }>(
    '/reviews/generate',
    { kind },
  );
  return data.data;
}
