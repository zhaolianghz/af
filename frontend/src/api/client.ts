// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import axios, { type AxiosError, type InternalAxiosRequestConfig } from 'axios';
import { getToken, clearSession } from '@/stores/authStore';

const rawBase = import.meta.env.VITE_API_BASE_URL as string | undefined;
// Relative by default so the browser makes SAME-ORIGIN requests:
// Vite proxies /api → backend in dev, nginx proxies it in prod. This
// dodges CORS entirely (no allow-list dance) and works regardless of
// which port the frontend dev server lands on. Set VITE_API_BASE_URL
// to an absolute URL only if you're pointing at a remote API with no
// same-origin proxy in front of it.
export const API_BASE_URL: string = rawBase || '/api/v1';

const debug = (import.meta.env.VITE_API_DEBUG as string | undefined) === 'true';

// Cold-start retry: during a fresh `make run`, the backend takes a few
// seconds to come up. The vite proxy returns 503 (backend not ready) and
// a connection that dies mid-flight surfaces as a network error (no
// response). For idempotent GETs we retry a couple times with backoff so
// the first paint recovers on its own instead of showing an error.
const MAX_RETRIES = 3;
const RETRY_BASE_MS = 400;

function isRetriable(err: AxiosError): boolean {
  const method = (err.config?.method ?? 'get').toLowerCase();
  if (method !== 'get') return false; // only idempotent reads
  // No response = network/connection error (backend socket not up yet).
  if (!err.response) return true;
  // 503 = our proxy's "backend not ready" signal; 502 = transient proxy hiccup.
  return err.response.status === 503 || err.response.status === 502;
}

export const apiClient = axios.create({
  baseURL: API_BASE_URL,
  timeout: 10_000,
  headers: { 'Content-Type': 'application/json' },
});

apiClient.interceptors.request.use((cfg: InternalAxiosRequestConfig) => {
  // X-Request-ID lets us correlate browser-side requests with server logs.
  cfg.headers.set('X-Request-ID', requestId());
  // §12: attach the Bearer token when present. When auth is disabled on
  // the backend there is no token and the header is simply omitted.
  const token = getToken();
  if (token) {
    cfg.headers.set('Authorization', `Bearer ${token}`);
  }
  if (debug) {
    // eslint-disable-next-line no-console
    console.debug('[api]', cfg.method?.toUpperCase(), cfg.url);
  }
  return cfg;
});

// requestId returns a correlation id for the X-Request-ID header.
// crypto.randomUUID() only exists in a SECURE context (HTTPS or
// localhost) — over plain HTTP to an IP (e.g. http://1.2.3.4:9091) it is
// undefined and calling it throws, which previously broke the very first
// request ("crypto.randomUUID is not a function"). This is only a log
// correlation id, so a non-crypto fallback is fine.
function requestId(): string {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
      return crypto.randomUUID();
    }
  } catch {
    /* fall through */
  }
  // RFC4122-ish v4 from Math.random — good enough for tracing.
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

apiClient.interceptors.response.use(
  (res) => res,
  async (err: AxiosError) => {
    // Cold-start retry FIRST: if the backend is still starting (503 / no
    // response) and this is an idempotent GET, retry a few times with
    // linear backoff before treating it as a real error. We stash the
    // attempt count on the request config.
    const cfg = err.config as (InternalAxiosRequestConfig & { _retryCount?: number }) | undefined;
    if (cfg && isRetriable(err)) {
      const attempt = cfg._retryCount ?? 0;
      if (attempt < MAX_RETRIES) {
        cfg._retryCount = attempt + 1;
        const delay = RETRY_BASE_MS * (attempt + 1);
        if (debug) {
          // eslint-disable-next-line no-console
          console.debug(`[api] retry ${cfg._retryCount}/${MAX_RETRIES} in ${delay}ms`, cfg.url);
        }
        await new Promise((r) => setTimeout(r, delay));
        return apiClient(cfg);
      }
    }
    // Keep structured logs in the console so we can reconstruct the call.
    // We intentionally do NOT toast here: every caller catches its own
    // errors and decides the user-facing message. Toasting here would
    // produce double toasts on the same error.
    // eslint-disable-next-line no-console
    console.error('[api] error:', err.response?.status, err.message);
    // §12: a 401 means the token is missing/expired/invalid. Clear the
    // session and bounce to /login — but never loop on the login call
    // itself (a bad password is a 401 the LoginPage handles inline).
    if (err.response?.status === 401) {
      const url = err.config?.url ?? '';
      if (!url.includes('/auth/login')) {
        clearSession();
        if (window.location.pathname !== '/login') {
          const next = encodeURIComponent(window.location.pathname + window.location.search);
          window.location.assign(`/login?next=${next}`);
        }
      }
    }
    return Promise.reject(err);
  },
);
