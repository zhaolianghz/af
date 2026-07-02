// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Vitest configuration — see FE1_PLAN.md §M1.
 *
 * - environment: 'node' is the default. The M1 test target
 *   (canvasStore) is a pure Zustand store with no DOM access,
 *   so a node env keeps startup < 100ms. When component tests
 *   land in M3, switch to 'jsdom' per-file with
 *   `// @vitest-environment jsdom`.
 * - globals: true lets test files use describe/it/expect without
 *   imports. We still import from 'vitest' for type-safe
 *   assertions in some files; both styles coexist.
 * - The '@' alias mirrors vite.config.ts so tests can import
 *   `from '@/stores/canvasStore'`.
 */
import { defineConfig } from 'vitest/config';
import path from 'node:path';

export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    globals: true,
    environment: 'node',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    exclude: ['node_modules', 'dist', 'e2e/**'],
    setupFiles: ['./src/setupTests.ts'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html', 'json-summary'],
      reportsDirectory: './coverage',
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.{test,spec}.{ts,tsx}',
        'src/main.tsx',
        'src/setupTests.ts',
        'src/**/*.d.ts',
        'src/types/**',
      ],
    },
  },
});
