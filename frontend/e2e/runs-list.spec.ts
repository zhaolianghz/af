// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Smoke test: run history list — load + filter + row click.
 */
import { test, expect } from '@playwright/test';
import { envelope, mockApi } from './fixtures';

const SAMPLE_RUNS = [
  {
    id: 101,
    strategy_id: 5,
    status: 'success',
    trigger_type: 'manual',
    attempts: 1,
    started_at: '2026-06-17T10:00:00Z',
    finished_at: '2026-06-17T10:00:01Z',
    created_at: '2026-06-17T10:00:00Z',
  },
  {
    id: 102,
    strategy_id: 6,
    status: 'failed',
    trigger_type: 'cron',
    attempts: 2,
    started_at: '2026-06-17T11:00:00Z',
    finished_at: '2026-06-17T11:00:03Z',
    created_at: '2026-06-17T11:00:00Z',
  },
];

test('runs list loads and shows rows', async ({ page }) => {
  await mockApi(page, {
    'GET /runs*': envelope({
      items: SAMPLE_RUNS,
      total: 2,
      page: 1,
      page_size: 20,
    }),
    'GET /strategies*': envelope({
      items: [
        { id: 5, code: 'ma_breakout', name: '均线突破', status: 'draft', current_version: 1, created_at: '2026-06-17T09:00:00Z', updated_at: '2026-06-17T09:00:00Z' },
      ],
      total: 1,
      page: 1,
      page_size: 200,
    }),
  });

  await page.goto('/runs');

  await expect(page.getByRole('heading', { name: '运行历史' })).toBeVisible();
  // Scope to the table rows to avoid matching <option> elements in the status filter.
  const row1 = page.locator('tr', { hasText: '#101' });
  const row2 = page.locator('tr', { hasText: '#102' });
  await expect(row1).toBeVisible();
  await expect(row2).toBeVisible();
  await expect(row1.getByText('成功')).toBeVisible();
  await expect(row2.getByText('失败')).toBeVisible();
});

test('runs list: clicking a row navigates to the detail page', async ({ page }) => {
  await mockApi(page, {
    'GET /runs*': envelope({
      items: SAMPLE_RUNS,
      total: 2,
      page: 1,
      page_size: 20,
    }),
    'GET /strategies*': envelope({ items: [], total: 0, page: 1, page_size: 200 }),
  });

  await page.goto('/runs');
  await page.getByText('#101').click();
  await expect(page).toHaveURL(/\/runs\/101$/);
});

test('runs list: empty state when there are no runs', async ({ page }) => {
  await mockApi(page, {
    'GET /runs*': envelope({ items: [], total: 0, page: 1, page_size: 20 }),
    'GET /strategies*': envelope({ items: [], total: 0, page: 1, page_size: 200 }),
  });

  await page.goto('/runs');
  await expect(page.getByText('暂无运行记录')).toBeVisible();
});
