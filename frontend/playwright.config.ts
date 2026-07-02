// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Playwright config — M3 e2e smoke tests.
 *
 * The tests are hermetic: every spec intercepts /api/v1/* via
 * page.route() and serves fixture JSON, so no Go backend needs
 * to be running. webServer auto-starts vite dev (port 5173) and
 * tears it down at the end of the run.
 *
 * Run locally:
 *   npx playwright test           # headless
 *   npx playwright test --headed  # watch it run
 *   npx playwright show-report    # open the last HTML report
 */
import { defineConfig, devices } from '@playwright/test';

const PORT = 5173;
const baseURL = `http://localhost:${PORT}`;

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : 'list',
  timeout: 30_000,
  expect: { timeout: 5_000 },

  use: {
    baseURL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    navigationTimeout: 10_000,
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  webServer: {
    command: 'npm run dev',
    url: baseURL,
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
