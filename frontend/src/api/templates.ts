// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Template API client. Mirrors
 * `backend/internal/executor/template_handler.go` + the
 * `templates.go` loader.
 *
 * The list endpoint returns the 5 built-in templates declared
 * under `backend/internal/executor/templates/`. The from-template
 * endpoint creates a new Strategy from a template's baked-in
 * `dag_json`.
 */
import { apiClient } from './client';
import type {
  StrategyTemplate,
  TemplateListResponse,
  Strategy,
} from '@/types/orchestrator';

export async function listTemplates(): Promise<StrategyTemplate[]> {
  const { data } = await apiClient.get<{ code: number; data: TemplateListResponse }>(
    '/strategies/templates',
  );
  return data.data.items ?? [];
}

export interface FromTemplateResponse {
  strategy: Strategy;
  version: number;
}

export async function createFromTemplate(code: string): Promise<FromTemplateResponse> {
  const { data } = await apiClient.post<{ code: number; data: FromTemplateResponse }>(
    `/strategies/from-template/${code}`,
  );
  return data.data;
}