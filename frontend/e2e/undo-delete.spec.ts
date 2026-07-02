// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Smoke test: delete with undo (E1 — Undo toast).
 *
 * Pins the contract for the M4-EXT E1 feature:
 *   1. /strategies loads with one item
 *   2. Click row's delete button → confirm dialog (danger) appears
 *   3. Click "确认" → row is removed from the list
 *      → "已删除方案" undo toast appears
 *   4. Click "撤销" → POST /strategies/import fires
 *      → list is re-fetched and the item is back
 */
import { test, expect } from '@playwright/test';
import { envelope, mockApi } from './fixtures';

const SAMPLE_STRATEGY = {
  id: 7,
  code: 'mv_breakout',
  name: '均线突破示例',
  status: 'draft' as const,
  current_version: 1,
  dag_json: '{}',
  created_at: '2026-06-17T08:00:00Z',
  updated_at: '2026-06-17T08:00:00Z',
};

const SAMPLE_STRATEGY_RESTORED = {
  ...SAMPLE_STRATEGY,
  // After undo, the import endpoint gives the strategy a new id and timestamp.
  id: 99,
  created_at: '2026-06-17T08:05:00Z',
  updated_at: '2026-06-17T08:05:00Z',
};

test('delete with undo: confirm → remove → undo → restore', async ({ page }) => {
  // Single static fixture: the same item shows on every list refresh.
  // The proof that undo re-fetched is that the row reappears after we
  // optimistically removed it.
  await mockApi(page, {
    'GET /strategies*': envelope({
      items: [SAMPLE_STRATEGY],
      total: 1,
      page: 1,
      page_size: 20,
    }),
    'DELETE /strategies/7': envelope({}),
    'POST /strategies/import': envelope({ strategy: SAMPLE_STRATEGY_RESTORED }),
  });

  await page.goto('/strategies');

  // Row appears (scoped to the table to avoid matching the toast text).
  const row = page.locator('tr', { hasText: '均线突破示例' });
  await expect(row).toBeVisible();

  // Click the row's delete button (in the actions cell).
  await row.getByRole('button', { name: '删除' }).click();

  // ConfirmDialog appears.
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.getByText('删除方案')).toBeVisible();

  // Click "确认" inside the dialog.
  await page.getByRole('button', { name: '确认' }).click();

  // Row is removed.
  await expect(row).not.toBeVisible();

  // Undo toast appears.
  await expect(page.getByText(/已删除方案 "均线突破示例"/)).toBeVisible();

  // Click "撤销".
  await page.getByRole('button', { name: '撤销' }).click();

  // Restore flow: import + re-fetch list. The row reappears because
  // listStrategies ran again and returned the same item.
  await expect(row).toBeVisible({ timeout: 5_000 });
});

test('cancel from confirm dialog keeps the row and does not call delete', async ({ page }) => {
  let deleteCalled = false;
  await mockApi(page, {
    'GET /strategies*': envelope({
      items: [SAMPLE_STRATEGY],
      total: 1,
      page: 1,
      page_size: 20,
    }),
    'DELETE /strategies/7': () => {
      deleteCalled = true;
      return envelope({});
    },
  });

  await page.goto('/strategies');
  const row = page.locator('tr', { hasText: '均线突破示例' });
  await expect(row).toBeVisible();

  await row.getByRole('button', { name: '删除' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();

  // Click "取消".
  await page.getByRole('button', { name: '取消' }).click();

  // Dialog dismissed, row still there, no delete fired.
  await expect(page.getByRole('dialog')).not.toBeVisible();
  await expect(row).toBeVisible();
  expect(deleteCalled).toBe(false);
});
