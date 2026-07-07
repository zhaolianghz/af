// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Smoke test: trial-run shows the success banner.
 *
 * Pins the trial-run contract:
 *   1. Editor loads the canonical 3-node DAG
 *   2. Click "试运行" → POST /strategies/:id/trial-run
 *   3. The banner "试运行结果 · success" appears with 3 node results
 *
 * Backend is mocked to return a clean success summary.
 */
import { test, expect } from '@playwright/test';
import {
  envelope,
  mockApi,
  strategyDetailFixture,
  trialRunSuccessFixture,
} from './fixtures';

test('trial-run surfaces the success banner', async ({ page }) => {
  const STRATEGY_ID = 11;

  await mockApi(page, {
    'GET /strategies/11': strategyDetailFixture(STRATEGY_ID),
    'POST /strategies/11/trial-run': trialRunSuccessFixture,
  });

  await page.goto(`/strategies/${STRATEGY_ID}`);

  // Editor loaded
  await expect(page.getByText('节点 3')).toBeVisible();

  // Fire trial-run
  await page.getByRole('button', { name: '试运行' }).click();

  // Banner appears with success status + node count summary
  await expect(page.getByText('试运行结果 · success')).toBeVisible();
  await expect(page.getByText('成功 3')).toBeVisible();
  await expect(page.getByText('共 3')).toBeVisible();
});

test('trial-run surfaces a failed banner on backend error', async ({ page }) => {
  const STRATEGY_ID = 12;

  await mockApi(page, {
    'GET /strategies/12': strategyDetailFixture(STRATEGY_ID),
    'POST /strategies/12/trial-run': envelope({
      run_id: 0,
      strategy_id: 12,
      status: 'failed',
      dry_run: true,
      duration: 1_000_000,
      error: 'DAG contains a cycle',
      node_results: {},
    }),
  });

  await page.goto(`/strategies/${STRATEGY_ID}`);
  await page.getByRole('button', { name: '试运行' }).click();

  await expect(page.getByText('试运行结果 · failed')).toBeVisible();
  await expect(page.getByText('DAG contains a cycle')).toBeVisible();
});
