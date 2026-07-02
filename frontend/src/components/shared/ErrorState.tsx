// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * ErrorState — inline error with retry button.
 */
export default function ErrorState({
  message = '加载失败',
  onRetry,
}: {
  message?: string;
  onRetry?: () => void;
}): JSX.Element {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
      <span className="text-sm font-medium text-rose-600">{message}</span>
      {onRetry && (
        <button type="button" onClick={onRetry} className="btn-secondary mt-1">
          重试
        </button>
      )}
    </div>
  );
}
