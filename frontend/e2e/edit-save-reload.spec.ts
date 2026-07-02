// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Smoke test: edit → save → reload → consistent.
 *
 * Pins the core save/reload contract:
 *   1. Editor loads the canonical 3-node DAG from GET /strategies/:id
 *   2. Add a node (makes the graph dirty → save button enabled)
 *   3. Click "保存" → PUT /strategies/:id is fired with the
 *      current dag_json
 *   4. Reload the page → the original 3-node DAG comes back from GET
 *
 * Backend is mocked. The PUT handler echoes back the strategy
 * with the dag_json the editor sent.
 *
 * NOTE: Route registration order matters. mockApi is registered
 * FIRST, the PUT handler SECOND. Playwright checks routes in
 * reverse registration order (last = first), so the PUT handler
 * runs before mockApi and can intercept PUT specifically. Non-PUT
 * requests call route.fallback() and reach mockApi.
 */
import { test, expect } from '@playwright/test';
import {
  envelope,
  mockApi,
  strategyDetailFixture,
} from './fixtures';

test('save persists the DAG and reload restores it', async ({ page }) => {
  const STRATEGY_ID = 7;

  // 1. Register mockApi first (GET + OPTIONS preflight).
  await mockApi(page, {
    'GET /strategies/7': strategyDetailFixture(STRATEGY_ID),
  });

  // 2. Register the PUT handler second (checked first by Playwright).
  //    Capture the body so we can assert the serialized DAG was sent.
  let putBody: { dag_json?: string } | null = null;
  await page.route('**/api/v1/strategies/7', async (route) => {
    const req = route.request();
    if (req.method().toUpperCase() === 'PUT') {
      const raw = req.postData();
      if (raw) {
        try {
          putBody = JSON.parse(raw) as { dag_json?: string };
        } catch {
          /* keep null */
        }
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        headers: { 'Access-Control-Allow-Origin': '*' },
        body: JSON.stringify(
          envelope({
            strategy: {
              id: STRATEGY_ID,
              code: 'e2e_strategy',
              name: 'E2E Smoke 策略',
              status: 'draft',
              current_version: 2,
              dag_json: putBody?.dag_json ?? '',
              created_at: '2026-06-16T10:00:00Z',
              updated_at: '2026-06-16T10:00:30Z',
            },
          }),
        ),
      });
    }
    // Non-PUT (e.g. OPTIONS preflight, GET) → fall through to mockApi.
    return route.fallback();
  });

  await page.goto(`/strategies/${STRATEGY_ID}`);

  // The canonical 3-node DAG loaded.
  await expect(page.getByText('节点 3')).toBeVisible();

  // After loadFromDag the store is in 'saved' state, so the 保存
  // button is disabled. Add a node to make the graph dirty.
  await page.getByRole('button', { name: '添加 去重 节点' }).click();
  await expect(page.getByText('节点 4')).toBeVisible();

  // Save button is now enabled (store is 'dirty').
  const saveButton = page.getByRole('button', { name: '保存' });
  await expect(saveButton).toBeEnabled();
  await saveButton.click();

  // The "已保存" indicator confirms the PUT round-tripped.
  await expect(page.getByText('已保存')).toBeVisible();

  // The PUT must have carried a dag_json with 4 nodes.
  expect(putBody, 'PUT /strategies/:id body was not captured').not.toBeNull();
  const dagObj = JSON.parse(putBody!.dag_json!);
  expect(dagObj.nodes).toHaveLength(4);

  // Reload — the original 3-node DAG comes back from GET.
  await page.reload();
  await expect(page.getByText('节点 3')).toBeVisible();
  await expect(page.getByText('连线 2')).toBeVisible();
});
