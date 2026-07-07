// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * E2E fixtures + API mock helpers.
 *
 * Every spec intercepts /api/v1/* via `mockApi()` and serves
 * fixture JSON, so the real Go backend never has to be running.
 * This is the antidote to the "e2e 依赖外部数据源（A股实时行情）"
 * risk called out in FE1_PLAN.md §8.
 *
 * The shape of every fixture matches the contract in
 * `frontend/src/types/orchestrator.ts` and the `{code, data}`
 * envelope every backend handler returns.
 *
 * NOTE: types from @playwright/test are deliberately inlined
 * (not imported via `import type`) because Playwright 1.58's
 * esbuild transform does not always erase type-only imports,
 * which can pull @vitest/expect into the runtime and collide
 * with Playwright's own expect globals.
 */

// Inlined structural types for Playwright's Page / Route — avoids
// importing from @playwright/test (see NOTE above about the vitest
// collision). Correctness is enforced by the spec assertions.
interface FulfillOptions {
  status?: number;
  contentType?: string;
  headers?: Record<string, string>;
  body?: string;
}

interface RouteLike {
  request(): { method(): string; url(): string; postData(): string | null };
  fulfill(opts: FulfillOptions): Promise<void>;
  continue(): Promise<void>;
  fallback(): Promise<void>;
}

interface PageLike {
  route(url: string, handler: (route: RouteLike) => Promise<void>, options?: { times?: number }): Promise<void>;
}

// =============================================================================
// Envelope helper — every API response is wrapped in {code, data}.
// =============================================================================

export function envelope<T>(data: T, code = 0): { code: number; data: T } {
  return { code, data };
}

// =============================================================================
// A reusable 3-node DAG: data_source → filter → persist.
// Used by the template fixture, the strategy detail fixture,
// and the trial-run summary fixture so all three flows agree
// on the same shape.
// =============================================================================

export const canonicalDagJson = JSON.stringify({
  nodes: [
    {
      id: 'ds1',
      type: 'data_source',
      position: { x: 0, y: 0 },
      data: {
        subtype: 'kline',
        params: { stock_codes: ['600519.SH'], period: '1d', days: 30 },
      },
    },
    {
      id: 'f1',
      type: 'filter',
      position: { x: 250, y: 0 },
      data: { params: { field: 'close', op: '>', value: 10 } },
    },
    {
      id: 'p1',
      type: 'persist',
      position: { x: 500, y: 0 },
      data: { params: { extra_tags: [] } },
    },
  ],
  edges: [
    { id: 'e1', source: 'ds1', target: 'f1', sourceHandle: 'out', targetHandle: 'in' },
    { id: 'e2', source: 'f1', target: 'p1', sourceHandle: 'out', targetHandle: 'in' },
  ],
});

// =============================================================================
// Fixtures
// =============================================================================

export const templatesFixture = envelope({
  items: [
    {
      code: 'ma_breakout',
      name: '均线突破',
      description: '股价突破 20 日均线的简单动量策略',
      industry: '动量',
      built_in: true,
      ai_explanation: '当收盘价上穿 MA20 时买入，下穿时卖出。',
      dag_json: canonicalDagJson,
    },
  ],
  total: 1,
});

export function strategyDetailFixture(id = 42) {
  return envelope({
    id,
    code: 'e2e_strategy',
    name: 'E2E Smoke 策略',
    description: '由 playwright 创建的烟雾测试策略',
    status: 'draft' as const,
    tags: 'smoke,e2e',
    current_version: 1,
    dag_json: canonicalDagJson,
    current_version_dag: canonicalDagJson,
    current_version_note: 'initial',
    cron_expression: '',
    created_at: '2026-06-16T10:00:00Z',
    updated_at: '2026-06-16T10:00:00Z',
  });
}

// Mirrors the backend's FLAT RunSummary (trial_handler.go → httpresp.OK):
// no `summary` wrapper, `node_results` keyed by node id, `duration` in
// nanoseconds. The frontend's adaptTrialRun() bridges this to the UI shape.
export const trialRunSuccessFixture = envelope({
  run_id: 0,
  strategy_id: 11,
  status: 'success' as const,
  dry_run: true,
  started_at: '2026-06-16T10:00:00Z',
  finished_at: '2026-06-16T10:00:01Z',
  duration: 42_000_000, // 42ms in ns
  node_results: {
    ds1: { node_id: 'ds1', status: 'success' as const, duration: 10_000_000 },
    f1: { node_id: 'f1', status: 'success' as const, duration: 5_000_000 },
    p1: { node_id: 'p1', status: 'success' as const, duration: 2_000_000 },
  },
});

// =============================================================================
// Mock helper — installs a route table on the page.
//
// Usage:
//   await mockApi(page, {
//     'GET /strategies/templates':    templatesFixture,
//     'POST /strategies/from-template/ma_breakout': envelope({ strategy: {...}, version: 1 }),
//     'GET /strategies/42':           strategyDetailFixture(42),
//   });
//
// Unmatched /api/v1/* requests fall through to a 404 so a missing
// mock is loud rather than silently hitting the real network.
// =============================================================================

export type MockRoutes = Record<string, unknown>;

export async function mockApi(page: PageLike, routes: MockRoutes): Promise<void> {
  const apiBase = '/api/v1';

  // Match any origin — the axios baseURL defaults to
  // http://localhost:8080/api/v1 in dev (different origin than
  // the vite dev server on :5173), so a path-only pattern would
  // miss. `**/api/v1/**` catches both same-origin and cross-origin.
  // CORS headers on every response. The axios baseURL is
  // http://localhost:8080 (cross-origin from the vite dev server
  // on :5173), so without Access-Control-Allow-Origin the browser
  // blocks every response even though Playwright fulfilled it.
  const corsHeaders = { 'Access-Control-Allow-Origin': '*' };

  await page.route('**/api/v1/**', (route: RouteLike) => {
    const method = route.request().method().toUpperCase();
    const fullUrl = route.request().url();
    const path = fullUrl.replace(/^https?:\/\/[^/]+/, '').replace(apiBase, '');

    // Handle CORS preflight. POST/PUT with Content-Type: application/json
    // triggers an OPTIONS preflight; without a 2xx response carrying
    // Allow-Methods + Allow-Headers, the browser blocks the real request.
    if (method === 'OPTIONS') {
      return route.fulfill({
        status: 204,
        headers: {
          ...corsHeaders,
          'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, OPTIONS',
          'Access-Control-Allow-Headers': 'Content-Type, X-Request-ID',
        },
      });
    }

    // Match on "METHOD /path" keys; ignore query strings.
    const key = `${method} ${path}`;
    const matchKey = Object.keys(routes).find(
      (k) => k === key || pathMatches(k, method, path),
    );

    if (matchKey) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        headers: corsHeaders,
        body: JSON.stringify(routes[matchKey]),
      });
    }

    // Loud 404 — every API call must be explicitly mocked.
    return route.fulfill({
      status: 404,
      contentType: 'application/json',
      headers: corsHeaders,
      body: JSON.stringify({
        code: 404,
        error: `e2e mock not found: ${method} ${path}`,
      }),
    });
  });
}

/**
 * Path matcher supporting:
 *   - Exact match ("GET /strategies/42")
 *   - Prefix match with trailing "*" ("GET /strategies/*")
 */
function pathMatches(key: string, method: string, actualPath: string): boolean {
  const [keyMethod, keyPath] = key.split(' ');
  if (keyMethod.toUpperCase() !== method) return false;
  if (keyPath.endsWith('*')) {
    return actualPath.startsWith(keyPath.slice(0, -1));
  }
  return keyPath === actualPath;
}

// =============================================================================
// Convenience: PUT body capture
// =============================================================================

/**
 * Capture the body of a PUT/POST so a spec can assert on what
 * the canvas serialized. Returns the last parsed body seen.
 */
export async function captureLastBody(
  page: PageLike,
  methodPath: string,
): Promise<Record<string, unknown> | null> {
  const [m, p] = methodPath.split(' ');
  let captured: Record<string, unknown> | null = null;

  await page.route('**/api/v1/**', (route: RouteLike) => {
    const req = route.request();
    if (req.method().toUpperCase() === m.toUpperCase()) {
      const path = req.url().replace(/^https?:\/\/[^/]+/, '').replace('/api/v1', '');
      if (path === p) {
        const body = req.postData();
        if (body) {
          try {
            captured = JSON.parse(body) as Record<string, unknown>;
          } catch {
            captured = { _raw: body };
          }
        }
      }
    }
    return route.continue();
  }, { times: 1 });

  return captured;
}
