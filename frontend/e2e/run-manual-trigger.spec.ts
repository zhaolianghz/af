// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Smoke test: manual run trigger — open modal, choose strategy, trigger.
 *
 * Pins the contract:
 *   1. /runs loads with the strategy list
 *   2. Click "手工触发运行" → modal opens with first strategy pre-selected
 *   3. Click "触发" → POST /runs → navigates to the new run's detail page
 */
import { test, expect } from '@playwright/test';
import { envelope, mockApi } from './fixtures';

const STRATEGIES = [
  {
    id: 5,
    code: 'ma_breakout',
    name: '均线突破',
    status: 'draft',
    current_version: 1,
    created_at: '2026-06-17T09:00:00Z',
    updated_at: '2026-06-17T09:00:00Z',
  },
];

test('manual trigger: open modal → select strategy → trigger → navigate to detail', async ({ page }) => {
  await mockApi(page, {
    'GET /runs*': envelope({ items: [], total: 0, page: 1, page_size: 20 }),
    'GET /strategies*': envelope({ items: STRATEGIES, total: 1, page: 1, page_size: 200 }),
    'POST /runs': envelope({ run_id: 999 }),
  });

  await page.goto('/runs');
  await page.getByText('+ 手工触发运行').click();

  // Modal opens
  await expect(page.getByText('手工触发运行').first()).toBeVisible();
  // Scope to the modal to avoid matching the page-level status/strategy filters.
  // The modal contains the form; the strategy select is the one wrapped in the
  // "方案" label inside the modal.
  const modal = page.locator('form');
  const select = modal.getByLabel('方案');
  await expect(select).toHaveValue('5');

  await page.getByRole('button', { name: '触发', exact: true }).click();

  await expect(page).toHaveURL(/\/runs\/999$/);
});
