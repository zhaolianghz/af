// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Smoke test: run detail page — load + retry confirm.
 */
import { test, expect } from '@playwright/test';
import { envelope, mockApi } from './fixtures';

const RUN_ID = 42;

const SAMPLE_RUN = {
  id: RUN_ID,
  strategy_id: 5,
  status: 'success',
  trigger_type: 'manual',
  attempts: 1,
  started_at: '2026-06-17T10:00:00Z',
  finished_at: '2026-06-17T10:00:01Z',
  created_at: '2026-06-17T10:00:00Z',
};

const SAMPLE_LOGS = [
  {
    id: 1,
    run_id: RUN_ID,
    node_key: 'data_source',
    status: 'success',
    started_at: '2026-06-17T10:00:00Z',
    finished_at: '2026-06-17T10:00:01Z',
  },
];

test('run detail loads run + logs and shows header + meta + timeline', async ({ page }) => {
  await mockApi(page, {
    [`GET /runs/${RUN_ID}`]: envelope(SAMPLE_RUN),
    [`GET /runs/${RUN_ID}/logs`]: envelope({ items: SAMPLE_LOGS }),
  });

  await page.goto(`/runs/${RUN_ID}`);

  await expect(page.getByText('成功').first()).toBeVisible();
  await expect(page.getByText(`#${RUN_ID}`)).toBeVisible();
  await expect(page.getByText('data_source')).toBeVisible();
  // Meta cards
  await expect(page.getByText('开始时间')).toBeVisible();
  await expect(page.getByText('结束时间')).toBeVisible();
  await expect(page.getByText('耗时')).toBeVisible();
  // Status badge
  await expect(page.getByText('方案 #5')).toBeVisible();
});

test('run detail retry: confirm → navigates to new run', async ({ page }) => {
  await mockApi(page, {
    [`GET /runs/${RUN_ID}`]: envelope(SAMPLE_RUN),
    [`GET /runs/${RUN_ID}/logs`]: envelope({ items: SAMPLE_LOGS }),
    [`POST /runs/${RUN_ID}/retry`]: envelope({ run_id: 43 }),
  });

  await page.goto(`/runs/${RUN_ID}`);
  await page.getByRole('button', { name: '重试' }).click();

  // ConfirmDialog
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.getByText('重试运行')).toBeVisible();
  await page.getByRole('button', { name: '确认' }).click();

  await expect(page).toHaveURL(/\/runs\/43$/);
});

test('run detail retry: cancel keeps the page', async ({ page }) => {
  await mockApi(page, {
    [`GET /runs/${RUN_ID}`]: envelope(SAMPLE_RUN),
    [`GET /runs/${RUN_ID}/logs`]: envelope({ items: SAMPLE_LOGS }),
  });

  await page.goto(`/runs/${RUN_ID}`);
  await page.getByRole('button', { name: '重试' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.getByRole('button', { name: '取消' }).click();

  await expect(page).toHaveURL(new RegExp(`/runs/${RUN_ID}$`));
  await expect(page.getByRole('dialog')).not.toBeVisible();
});
