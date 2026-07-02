// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * AI assistant API client (§11). Conversational strategy editing.
 *
 *   POST /strategies/:id/ai/preview  {instruction}            -> proposed DAG + diff
 *   POST /strategies/:id/ai/apply    {instruction, dag_json}  -> commit (new version)
 *
 * Two-stage: preview shows the diff; apply commits only on confirm.
 */
import { apiClient } from './client';

export interface AIPreviewResult {
  strategy_id: number;
  instruction: string;
  current_dag: string;
  proposed_dag: string;
  changes: string[];
  changed: boolean;
  audit_id: number;
  llm: string;
}

export interface AIApplyResult {
  strategy_id: number;
  version: number;
  audit_id: number;
}

export async function aiPreview(strategyId: number, instruction: string): Promise<AIPreviewResult> {
  const { data } = await apiClient.post<{ code: number; data: AIPreviewResult }>(
    `/strategies/${strategyId}/ai/preview`,
    { instruction },
  );
  return data.data;
}

export async function aiApply(
  strategyId: number,
  instruction: string,
  dagJson: string,
): Promise<AIApplyResult> {
  const { data } = await apiClient.post<{ code: number; data: AIApplyResult }>(
    `/strategies/${strategyId}/ai/apply`,
    { instruction, dag_json: dagJson },
  );
  return data.data;
}
