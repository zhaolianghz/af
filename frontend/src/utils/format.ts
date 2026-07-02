// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Format a Go time.Duration string (e.g. "1h2m3.456s") into a
 * short, human-readable form.
 */
export function formatUptime(duration: string): string {
  if (!duration) return '—';
  // Go durations look like: "1h2m3.456s", "2m5s", "0.5s"
  // We just trim sub-second precision and replace the "µ" remnants if any.
  return duration.replace(/\.\d+(ms|us|µs|ns)/g, '').trim() || '0s';
}

/** Format an ISO timestamp into a YYYY-MM-DD HH:mm:ss (local) string. */
export function formatTimestamp(iso: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, '0');
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  );
}
