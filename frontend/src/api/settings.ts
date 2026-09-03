// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * LLM settings API client. Runtime AI backend configuration.
 *
 *   GET  /settings/llm        current (api_key masked)
 *   PUT  /settings/llm        save + hot-swap
 *   POST /settings/llm/test   test connection (no persist)
 */
import { apiClient } from './client';

export interface LLMSettingView {
  enabled: boolean;
  provider: string;
  base_url: string;
  api_key_set: boolean;
  api_key_masked: string;
  model: string;
  active: string;
}

export interface LLMSaveInput {
  enabled: boolean;
  provider: string;
  base_url: string;
  api_key: string;
  model: string;
  keep_key: boolean;
}

export async function getLLMSetting(): Promise<LLMSettingView> {
  const { data } = await apiClient.get<{ code: number; data: LLMSettingView }>('/settings/llm');
  return data.data;
}

export async function saveLLMSetting(input: LLMSaveInput): Promise<LLMSettingView> {
  const { data } = await apiClient.put<{ code: number; data: LLMSettingView }>('/settings/llm', input);
  return data.data;
}

export async function testLLMSetting(input: LLMSaveInput): Promise<void> {
  await apiClient.post('/settings/llm/test', input);
}

// =============================================================================
// Multi-provider fallback chain (§11 multi-LLM). The assistant tries the
// providers top-to-bottom; when one is unavailable it falls through to
// the next. These back the new ordered-list settings UI.
// =============================================================================

export interface ProviderView {
  id: number;
  priority: number;
  enabled: boolean;
  provider: string;
  base_url: string;
  api_key_set: boolean;
  api_key_masked: string;
  model: string;
}

export interface ChainView {
  providers: ProviderView[];
  active: string;
}

export interface ProviderInput {
  id?: number;
  enabled: boolean;
  provider: string;
  base_url: string;
  api_key: string;
  model: string;
  keep_key: boolean;
}

export async function getProviders(): Promise<ChainView> {
  const { data } = await apiClient.get<{ code: number; data: ChainView }>('/settings/llm/providers');
  return data.data;
}

export async function saveProviders(providers: ProviderInput[]): Promise<ChainView> {
  const { data } = await apiClient.put<{ code: number; data: ChainView }>('/settings/llm/providers', {
    providers,
  });
  return data.data;
}

export async function testProvider(input: ProviderInput): Promise<void> {
  await apiClient.post('/settings/llm/providers/test', input);
}

// =============================================================================
// Model listing — populate the model dropdown from the provider's own
// GET /models (OpenAI-compatible), so operators pick instead of
// hand-typing model ids. The full list is returned, unfiltered.
// =============================================================================

export interface ModelListResult {
  models: string[];
}

export async function listProviderModels(input: ProviderInput): Promise<ModelListResult> {
  const { data } = await apiClient.post<{ code: number; data: ModelListResult }>(
    '/settings/llm/providers/models',
    input,
  );
  return data.data;
}
