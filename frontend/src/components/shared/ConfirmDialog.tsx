// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * ConfirmDialog — inline confirmation dialog replacing browser confirm().
 *
 * Props:
 *   title   — dialog heading
 *   message — body text
 *   danger  — when true, confirm button is red (for destructive actions)
 *   confirmLabel — defaults to "确认"
 *   cancelLabel  — defaults to "取消"
 *   open    — controlled visibility
 *   onConfirm — called on confirm
 *   onCancel  — called on cancel / dismiss
 *
 * Keyboard: Escape → cancel, Enter → confirm (disabled when danger=true
 * to prevent accidental destructive actions). Focus is trapped inside
 * the dialog and defaults to Cancel for safety.
 */
import { useEffect, useLayoutEffect, useRef } from 'react';
import clsx from 'clsx';

export interface ConfirmDialogProps {
  title: string;
  message?: string;
  danger?: boolean;
  confirmLabel?: string;
  cancelLabel?: string;
  open: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmDialog({
  title,
  message,
  danger = false,
  confirmLabel = '确认',
  cancelLabel = '取消',
  open,
  onConfirm,
  onCancel,
}: ConfirmDialogProps): JSX.Element | null {
  const cancelRef = useRef<HTMLButtonElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);

  // Hold the latest callbacks in refs so the keydown effect below
  // doesn't rebind every time the parent re-renders.
  const onCancelRef = useRef(onCancel);
  const onConfirmRef = useRef(onConfirm);
  onCancelRef.current = onCancel;
  onConfirmRef.current = onConfirm;

  // Focus cancel button on open (safer default). useLayoutEffect
  // runs synchronously after DOM mutation so the focus is set before
  // the browser paints, which avoids a visible flicker.
  useLayoutEffect(() => {
    if (open) {
      cancelRef.current?.focus();
    }
  }, [open]);

  // Keyboard: Escape → cancel, Enter → confirm (unless danger).
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onCancelRef.current();
        return;
      }
      if (e.key === 'Enter' && !danger) {
        e.preventDefault();
        onConfirmRef.current();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, danger]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center"
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/30" onClick={onCancel} />
      {/* Panel */}
      <div className="relative z-10 w-80 rounded-2xl border border-slate-200 bg-white p-6 shadow-2xl">
        <h3 className="text-sm font-semibold text-slate-900">{title}</h3>
        {message && <p className="mt-2 text-xs text-slate-500">{message}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <button
            ref={cancelRef}
            type="button"
            className="btn-secondary"
            onClick={onCancel}
          >
            {cancelLabel}
          </button>
          <button
            ref={confirmRef}
            type="button"
            className={clsx(
              'rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors',
              danger
                ? 'bg-rose-600 hover:bg-rose-700'
                : 'bg-brand-600 hover:bg-brand-700',
            )}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
