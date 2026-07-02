// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: process.env.VITE_PROXY_TARGET || 'http://localhost:8080',
        changeOrigin: true,
        // During a cold `make run`, the Go backend takes a few seconds to
        // compile + migrate + listen. Requests that arrive in that window
        // hit a closed socket; http-proxy's default is to surface that as
        // a 500, which the browser console reports as a hard error. That's
        // misleading — the backend is *starting*, not *broken*. Translate
        // the connection-refused/reset/timeout cases into a 503 with a
        // Retry-After so the SPA's loading states (and any retry) treat it
        // as "not ready yet" rather than a failure.
        configure: (proxy) => {
          proxy.on('error', (err, _req, res) => {
            const transient =
              err && ['ECONNREFUSED', 'ECONNRESET', 'ETIMEDOUT', 'EHOSTUNREACH'].includes(
                (err as NodeJS.ErrnoException).code ?? '',
              );
            // `res` is a ServerResponse for HTTP (not WebSocket upgrades).
            const r = res as import('node:http').ServerResponse | undefined;
            if (!r || r.headersSent || typeof r.writeHead !== 'function') return;
            if (transient) {
              r.writeHead(503, { 'Content-Type': 'application/json', 'Retry-After': '1' });
              r.end(JSON.stringify({ code: 503, message: '后端正在启动，请稍候…', detail: 'backend not ready' }));
            } else {
              r.writeHead(502, { 'Content-Type': 'application/json' });
              r.end(JSON.stringify({ code: 502, message: 'proxy error', detail: String(err?.message ?? err) }));
            }
          });
        },
      },
    },
  },
  preview: {
    port: 5173,
    host: '0.0.0.0',
  },
});
