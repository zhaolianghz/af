// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Regression test for the "试运行 点击没反应" bug.
//
// The Go backend returns a flat RunSummary (internal/orchestrator/types.go):
//   - NO `summary` wrapper (handler does httpresp.OK(c, summary))
//   - `node_results` is a MAP keyed by node id, not an array
//   - `duration` is in nanoseconds (Go time.Duration JSON), not `duration_ms`
//
// The frontend used to read `res.summary` → undefined → the TrialRunBanner
// never rendered → clicking 试运行 showed nothing. adaptTrialRun bridges the
// two shapes at the API boundary.
import { describe, expect, it } from 'vitest';
import { adaptTrialRun } from '@/api/strategies';
import type { RawTrialRunResponse } from '@/api/strategies';

describe('adaptTrialRun', () => {
  // Shape captured from the real backend (POST /strategies/5/trial-run).
  const rawSuccess: RawTrialRunResponse = {
    run_id: 0,
    strategy_id: 5,
    status: 'success',
    dry_run: true,
    node_results: {
      ds_news: {
        node_id: 'ds_news',
        status: 'success',
        duration: 36884,
        started_at: '2026-06-30T16:04:42.280490135+08:00',
        finished_at: '2026-06-30T16:04:42.280527016+08:00',
        payload: { count: 0, dry_run: true, stock_codes: ['600519.SH'] },
      },
      filt_kw: {
        node_id: 'filt_kw',
        status: 'success',
        duration: 20528,
        payload: { dropped: 0, matched: 0 },
      },
    },
    started_at: '2026-06-30T16:04:42.280463739+08:00',
    finished_at: '2026-06-30T16:04:42.280561566+08:00',
    duration: 97813,
  };

  it('wraps the flat RunSummary under `summary`', () => {
    const out = adaptTrialRun(rawSuccess);
    expect(out.summary).toBeDefined();
    expect(out.summary.status).toBe('success');
  });

  it('converts node_results map -> array', () => {
    const out = adaptTrialRun(rawSuccess);
    expect(Array.isArray(out.summary.node_results)).toBe(true);
    expect(out.summary.node_results).toHaveLength(2);
    expect(out.summary.node_results.map((n) => n.node_id).sort()).toEqual([
      'ds_news',
      'filt_kw',
    ]);
  });

  it('converts nanosecond duration -> milliseconds (summary + per-node)', () => {
    const out = adaptTrialRun({
      ...rawSuccess,
      duration: 5_000_000,
      node_results: { x: { node_id: 'x', status: 'success', duration: 5_000_000 } },
    });
    expect(out.summary.duration_ms).toBe(5);
    expect(out.summary.node_results[0].duration_ms).toBe(5);
  });

  it('surfaces summary + node errors on failure', () => {
    const out = adaptTrialRun({
      run_id: 0,
      strategy_id: 5,
      status: 'failed',
      dry_run: true,
      node_results: { x: { node_id: 'x', status: 'failed', duration: 0, error: 'boom' } },
      duration: 0,
      error: 'node x failed',
    });
    expect(out.summary.status).toBe('failed');
    expect(out.summary.error).toBe('node x failed');
    expect(out.summary.node_results[0].error).toBe('boom');
  });

  it('handles empty / missing node_results', () => {
    const out = adaptTrialRun({
      run_id: 0,
      strategy_id: 5,
      status: 'success',
      dry_run: true,
      node_results: {},
      duration: 0,
    });
    expect(out.summary.node_results).toEqual([]);
  });
});
