// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Smoke test: create a strategy from a template.
 *
 * Verifies the happy-path flow:
 *   /templates → click "使用模板" → POST /from-template → /strategies/:id
 *                                              ↓
 *                                       editor loads with the
 *                                       canonical 3-node DAG
 *
 * The whole backend is mocked via fixtures.ts.
 */
import { test, expect } from '@playwright/test';
import {
  envelope,
  mockApi,
  strategyDetailFixture,
  templatesFixture,
} from './fixtures';

test('create from template lands in the editor with nodes loaded', async ({ page }) => {
  const STRATEGY_ID = 42;

  await mockApi(page, {
    'GET /strategies/templates': templatesFixture,
    'POST /strategies/from-template/ma_breakout': envelope({
      strategy: {
        id: STRATEGY_ID,
        code: 'ma_breakout_copy',
        name: '均线突破 (副本)',
        status: 'draft',
        current_version: 1,
        dag_json: '',
        created_at: '2026-06-16T10:00:00Z',
        updated_at: '2026-06-16T10:00:00Z',
      },
      version: 1,
    }),
    'GET /strategies/42': strategyDetailFixture(STRATEGY_ID),
  });

  await page.goto('/templates');

  // Template card is visible
  await expect(page.getByRole('heading', { name: '均线突破' })).toBeVisible();

  // Click "使用模板"
  const useButton = page.getByRole('button', { name: '使用模板' });
  await useButton.click();

  // Navigation to the editor
  await expect(page).toHaveURL(new RegExp(`/strategies/${STRATEGY_ID}$`), {
    timeout: 10_000,
  });

  // The toolbar's node/edge counters are the cheapest assertion
  // that the canvas store loaded the canonical DAG correctly.
  await expect(page.getByText('节点 3')).toBeVisible();
  await expect(page.getByText('连线 2')).toBeVisible();
});
