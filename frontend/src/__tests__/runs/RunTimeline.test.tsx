// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import RunTimeline from '@/components/runs/RunTimeline';
import type { RunLog } from '@/types/orchestrator';

const log = (
  id: number,
  nodeKey: string,
  status: RunLog['status'],
  startedAt: string,
  finishedAt: string,
  error?: string,
): RunLog => ({
  id,
  run_id: 1,
  node_key: nodeKey,
  status,
  started_at: startedAt,
  finished_at: finishedAt,
  error,
  created_at: '2026-06-17T10:00:00Z',
  updated_at: '2026-06-17T10:00:00Z',
});

describe('RunTimeline', () => {
  it('renders the empty state when logs is empty', () => {
    render(<RunTimeline logs={[]} />);
    expect(screen.getByText('暂无节点日志')).toBeInTheDocument();
  });

  it('renders each log entry with node_key + status', () => {
    const logs = [
      log(1, 'data_source', 'success', '2026-06-17T10:00:00Z', '2026-06-17T10:00:01Z'),
      log(2, 'filter', 'failed', '2026-06-17T10:00:02Z', '2026-06-17T10:00:03Z', 'value 错'),
    ];
    render(<RunTimeline logs={logs} />);
    expect(screen.getByText('data_source')).toBeInTheDocument();
    expect(screen.getByText('filter')).toBeInTheDocument();
    expect(screen.getByText('success')).toBeInTheDocument();
    expect(screen.getByText('failed')).toBeInTheDocument();
    expect(screen.getByText('value 错')).toBeInTheDocument();
  });

  it('sorts logs by started_at ascending', () => {
    // Intentionally pass them out of order.
    const logs = [
      log(3, 'persist', 'success', '2026-06-17T10:00:10Z', '2026-06-17T10:00:11Z'),
      log(1, 'data_source', 'success', '2026-06-17T10:00:00Z', '2026-06-17T10:00:01Z'),
      log(2, 'filter', 'success', '2026-06-17T10:00:05Z', '2026-06-17T10:00:06Z'),
    ];
    const { container } = render(<RunTimeline logs={logs} />);
    const listItems = Array.from(container.querySelectorAll('li'));
    const keysInOrder = listItems.map((li) =>
      li.querySelector('.font-mono')?.textContent?.trim(),
    );
    expect(keysInOrder).toEqual(['data_source', 'filter', 'persist']);
  });

  it('does not render the error block when log.error is absent', () => {
    const logs = [
      log(1, 'data_source', 'success', '2026-06-17T10:00:00Z', '2026-06-17T10:00:01Z'),
    ];
    const { container } = render(<RunTimeline logs={logs} />);
    expect(container.querySelector('.text-rose-700')).toBeNull();
  });

  it('formats duration under 1s as ms, otherwise as seconds', () => {
    const logs = [
      log(1, 'fast', 'success', '2026-06-17T10:00:00.000Z', '2026-06-17T10:00:00.500Z'),
      log(2, 'slow', 'success', '2026-06-17T10:00:00.000Z', '2026-06-17T10:00:02.000Z'),
    ];
    render(<RunTimeline logs={logs} />);
    expect(screen.getByText(/500 ms/)).toBeInTheDocument();
    expect(screen.getByText(/2\.00 s/)).toBeInTheDocument();
  });

  it('renders "—" for duration when end < start (clock skew)', () => {
    const logs = [
      log(1, 'skewed', 'success', '2026-06-17T10:00:05Z', '2026-06-17T10:00:00Z'),
    ];
    const { container } = render(<RunTimeline logs={logs} />);
    expect(container.textContent).toContain('—');
  });
});
