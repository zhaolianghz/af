// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * useConfirm — Promise-based confirmation hook.
 *
 * Usage:
 *   const confirm = useConfirm();
 *   const ok = await confirm({ title: "删除", message: "...", danger: true });
 *   if (ok) { ... delete ... }
 *
 * Manages a single ConfirmDialog instance. Consecutive calls queue:
 * the second call waits for the first dialog to resolve.
 */
import { useCallback, useRef, useState } from 'react';
import ConfirmDialog, { type ConfirmDialogProps } from '@/components/shared/ConfirmDialog';

type ConfirmOpts = Omit<ConfirmDialogProps, 'open' | 'onConfirm' | 'onCancel'>;

type ConfirmFn = ((opts: ConfirmOpts) => Promise<boolean>) & { dialog: JSX.Element | null };

export default function useConfirm(): ConfirmFn {
  const [state, setState] = useState<ConfirmOpts | null>(null);
  const [open, setOpen] = useState(false);
  const resolveRef = useRef<((v: boolean) => void) | null>(null);

  const confirm = useCallback(
    (opts: ConfirmOpts): Promise<boolean> =>
      new Promise((resolve) => {
        // If a dialog is already open, resolve it as cancelled first.
        if (resolveRef.current) {
          resolveRef.current(false);
        }
        resolveRef.current = resolve;
        setState(opts);
        setOpen(true);
      }),
    [],
  );

  const handleConfirm = useCallback(() => {
    setOpen(false);
    resolveRef.current?.(true);
    resolveRef.current = null;
  }, []);

  const handleCancel = useCallback(() => {
    setOpen(false);
    resolveRef.current?.(false);
    resolveRef.current = null;
  }, []);

  const dialog = state ? (
    <ConfirmDialog
      {...state}
      open={open}
      onConfirm={handleConfirm}
      onCancel={handleCancel}
    />
  ) : null;

  return Object.assign(confirm, {
    /** The ConfirmDialog element — render this near the top of your component tree. */
    dialog,
  });
}
