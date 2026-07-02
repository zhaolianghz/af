// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import RunStatusBadge from '@/components/runs/RunStatusBadge';
import type { RunStatus } from '@/types/orchestrator';

describe('RunStatusBadge', () => {
  const cases: Array<{ status: RunStatus; label: string }> = [
    { status: 'pending', label: '待执行' },
    { status: 'running', label: '执行中' },
    { status: 'success', label: '成功' },
    { status: 'failed', label: '失败' },
    { status: 'partial', label: '部分成功' },
    { status: 'skipped', label: '已跳过' },
  ];

  it.each(cases)('renders label for status=$status', ({ status, label }) => {
    render(<RunStatusBadge status={status} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it('shows a pulsing dot only when status=running', () => {
    const { container: runningContainer, rerender } = render(<RunStatusBadge status="running" />);
    const runningDot = runningContainer.querySelector('.animate-pulse');
    expect(runningDot).not.toBeNull();

    rerender(<RunStatusBadge status="success" />);
    expect(runningContainer.querySelector('.animate-pulse')).toBeNull();
  });

  it('applies the status color class for each status', () => {
    const { container } = render(<RunStatusBadge status="failed" />);
    const span = container.querySelector('span');
    // bg-rose-100 + text-rose-700 per RUN_STATUS_COLORS
    expect(span?.className).toContain('bg-rose-100');
    expect(span?.className).toContain('text-rose-700');
  });

  it('merges custom className', () => {
    const { container } = render(<RunStatusBadge status="success" className="ml-2" />);
    const span = container.querySelector('span');
    expect(span?.className).toContain('ml-2');
  });
});
