// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * notify — thin toast wrapper so every page doesn't import react-hot-toast
 * directly. Also provides a shared getErrorMessage helper.
 */
import toast from 'react-hot-toast';
import type { AxiosError } from 'axios';

/** Map any error-like value to a safe, user-facing Chinese message. */
export function getErrorMessage(err: unknown, fallback = '操作失败'): string {
  if (!err) return fallback;
  if (typeof err === 'string') return err || fallback;
  if (err instanceof Error) {
    // Try to extract the backend's message field from an AxiosError.
    const ax = err as AxiosError<{ code?: number; message?: string }>;
    if (ax.response?.data?.message) return ax.response.data.message;
    return err.message || fallback;
  }
  if (typeof err === 'object' && err !== null) {
    const obj = err as Record<string, unknown>;
    if (typeof obj.message === 'string' && obj.message) return obj.message;
  }
  return fallback;
}

export function notifyError(err: unknown, fallback = '操作失败'): void {
  toast.error(getErrorMessage(err, fallback));
}

export function notifySuccess(msg: string): void {
  toast.success(msg);
}

export function notifyWarning(msg: string): void {
  // toast() with a custom icon so it renders as a neutral warning
  toast(msg, { icon: '⚠️', style: { background: '#fffbeb', color: '#92400e' } });
}

/**
 * Show an undo toast. Returns a promise that resolves to `true` if the user
 * clicked "undo", `false` if the toast dismissed naturally.
 */
export function notifyUndo(msg: string, durationMs = 5_000): Promise<boolean> {
  return new Promise((resolve) => {
    let settled = false;
    toast(
      (t) => (
        <div className="flex items-center gap-3">
          <span className="text-sm">{msg}</span>
          <button
            type="button"
            className="rounded-md bg-white px-2 py-0.5 text-xs font-medium text-brand-700 shadow-sm hover:bg-brand-50"
            onClick={() => {
              if (settled) return;
              settled = true;
              toast.dismiss(t.id);
              resolve(true);
            }}
          >
            撤销
          </button>
        </div>
      ),
      { duration: durationMs },
    );
    // If the toast times out and the user didn't click undo.
    setTimeout(() => {
      if (!settled) {
        settled = true;
        resolve(false);
      }
    }, durationMs + 500); // small pad after dismiss animation
  });
}
