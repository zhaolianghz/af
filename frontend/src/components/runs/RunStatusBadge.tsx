// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * RunStatusBadge — colour-coded pill for a run's status. Animates
 * the "running" state with a small pulse so live-updating lists
 * are easy to scan.
 */
import clsx from 'clsx';
import {
  RUN_STATUS_COLORS,
  RUN_STATUS_LABELS,
  type RunStatus,
} from '@/types/orchestrator';

export interface RunStatusBadgeProps {
  status: RunStatus;
  className?: string;
}

export default function RunStatusBadge({
  status,
  className,
}: RunStatusBadgeProps): JSX.Element {
  const isRunning = status === 'running';
  return (
    <span
      className={clsx(
        'inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-[11px] font-medium',
        RUN_STATUS_COLORS[status],
        className,
      )}
    >
      {isRunning && (
        <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
      )}
      {RUN_STATUS_LABELS[status]}
    </span>
  );
}