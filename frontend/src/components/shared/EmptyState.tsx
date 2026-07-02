// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * EmptyState — "no data" placeholder with optional action.
 */
export default function EmptyState({
  icon,
  title = '暂无数据',
  description,
  action,
}: {
  icon?: string;
  title?: string;
  description?: string;
  action?: { label: string; onClick: () => void };
}): JSX.Element {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
      {icon && <span className="text-2xl">{icon}</span>}
      <span className="text-sm font-medium text-slate-500">{title}</span>
      {description && <span className="max-w-xs text-xs text-slate-400">{description}</span>}
      {action && (
        <button type="button" onClick={action.onClick} className="btn-primary mt-2">
          {action.label}
        </button>
      )}
    </div>
  );
}
