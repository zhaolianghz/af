// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Shared API types.
 *
 * The Go backend's /healthz response shape is defined in
 * `backend/internal/handler/health.go` — keep this in sync.
 */
export interface HealthResponse {
  status: string;
  version: string;
  /** ISO-8601 / RFC3339 timestamp (UTC). */
  ts: string;
  /** Server uptime as a Go time.Duration string, e.g. "1h2m3s". */
  uptime: string;
}
