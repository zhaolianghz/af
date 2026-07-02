// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ConfirmDialog from '@/components/shared/ConfirmDialog';

describe('ConfirmDialog', () => {
  const onConfirm = vi.fn();
  const onCancel = vi.fn();

  beforeEach(() => {
    onConfirm.mockClear();
    onCancel.mockClear();
  });

  it('renders title and message when open', () => {
    render(
      <ConfirmDialog
        title="删除方案"
        message="该操作不可撤销"
        danger
        open
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );
    expect(screen.getByText('删除方案')).toBeInTheDocument();
    expect(screen.getByText('该操作不可撤销')).toBeInTheDocument();
  });

  it('does not render when open=false', () => {
    render(
      <ConfirmDialog
        title="删除方案"
        open={false}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('calls onConfirm when confirm button clicked', async () => {
    const user = userEvent.setup();
    render(
      <ConfirmDialog title="标题" open onConfirm={onConfirm} onCancel={onCancel} />,
    );
    await user.click(screen.getByRole('button', { name: '确认' }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onCancel).not.toHaveBeenCalled();
  });

  it('calls onCancel when cancel button clicked', async () => {
    const user = userEvent.setup();
    render(
      <ConfirmDialog title="标题" open onConfirm={onConfirm} onCancel={onCancel} />,
    );
    await user.click(screen.getByRole('button', { name: '取消' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('Escape key cancels the dialog', () => {
    render(
      <ConfirmDialog title="标题" open onConfirm={onConfirm} onCancel={onCancel} />,
    );
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('Enter key confirms for non-danger dialogs', () => {
    render(
      <ConfirmDialog
        title="标题"
        open
        danger={false}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );
    fireEvent.keyDown(window, { key: 'Enter' });
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('Enter key is blocked for danger dialogs', () => {
    render(
      <ConfirmDialog
        title="标题"
        open
        danger
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );
    fireEvent.keyDown(window, { key: 'Enter' });
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCancel).not.toHaveBeenCalled();
  });

  it('backdrop click cancels', async () => {
    const user = userEvent.setup();
    render(
      <ConfirmDialog title="标题" open onConfirm={onConfirm} onCancel={onCancel} />,
    );
    // Backdrop is the sibling div with the bg-black/30 class.
    const backdrop = document.querySelector('.bg-black\\/30');
    if (backdrop) await user.click(backdrop);
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('has role=dialog and aria-modal for a11y', () => {
    render(
      <ConfirmDialog title="标题" open onConfirm={onConfirm} onCancel={onCancel} />,
    );
    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(dialog).toHaveAttribute('aria-label', '标题');
  });

  it('uses custom confirm/cancel labels', () => {
    render(
      <ConfirmDialog
        title="标题"
        open
        confirmLabel="确定删除"
        cancelLabel="再想想"
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );
    expect(screen.getByText('确定删除')).toBeInTheDocument();
    expect(screen.getByText('再想想')).toBeInTheDocument();
  });
});
