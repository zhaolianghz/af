// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { apiClient } from './client';
import type { HealthResponse } from '@/types/api';

/**
 * GET /api/v1/healthz — backend liveness probe.
 *
 * The server also exposes /healthz at the root for backward
 * compatibility (load balancers / k8s probes that hit the root),
 * but the canonical path is under /api/v1 for consistency with
 * the rest of the API. Both paths hit the same handler.
 */
export async function getHealth(): Promise<HealthResponse> {
  const { data } = await apiClient.get<HealthResponse>('/healthz');
  return data;
}

/**
 * GET /api/v1/ping — lightweight readiness probe that returns the text "pong".
 */
export async function getPing(): Promise<string> {
  const { data } = await apiClient.get<string>('/ping');
  return data;
}
