// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { FilterForm } from '@/components/canvas/NodeConfigPanel';
import type { RFNode } from '@/types/orchestrator';

const baseNode: RFNode = {
  id: 'filter_1',
  type: 'filter',
  position: { x: 0, y: 0 },
  data: { params: { field: 'chg_pct', op: '>', value: 0 } },
};

function setValue(value: string) {
  const valueInput = screen.getByDisplayValue('0') as HTMLInputElement;
  // Use native input value setter so React's onChange fires correctly.
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set;
  setter?.call(valueInput, value);
  fireEvent.change(valueInput, { target: { value } });
}

describe('FilterForm', () => {
  it('renders field, op, and value inputs', () => {
    render(<FilterForm node={baseNode} onUpdate={vi.fn()} />);
    expect(screen.getByDisplayValue('chg_pct')).toBeInTheDocument();
    expect(screen.getByDisplayValue('0')).toBeInTheDocument();
  });

  it('shows the helper text initially (no error)', () => {
    render(<FilterForm node={baseNode} onUpdate={vi.fn()} />);
    expect(screen.getByText(/数字直接写, 区间/)).toBeInTheDocument();
    expect(screen.queryByText(/JSON 格式错误/)).not.toBeInTheDocument();
  });

  it('shows inline JSON format error on invalid input (regression for the silent-catch bug)', () => {
    render(<FilterForm node={baseNode} onUpdate={vi.fn()} />);
    setValue('abc'); // not valid JSON
    expect(screen.getByText(/JSON 格式错误/)).toBeInTheDocument();
  });

  it('clears the error and calls onUpdate when valid JSON is entered', () => {
    const onUpdate = vi.fn();
    render(<FilterForm node={baseNode} onUpdate={onUpdate} />);

    setValue('bad');
    expect(screen.getByText(/JSON 格式错误/)).toBeInTheDocument();

    setValue('42');
    expect(screen.queryByText(/JSON 格式错误/)).not.toBeInTheDocument();
    expect(onUpdate).toHaveBeenCalled();
    // onUpdate should receive the parsed value
    const lastCall = onUpdate.mock.calls[onUpdate.mock.calls.length - 1][0];
    expect(lastCall.params.value).toBe(42);
  });

  it('accepts array values like [2, 5]', () => {
    render(<FilterForm node={baseNode} onUpdate={vi.fn()} />);
    setValue('[2, 5]');
    expect(screen.queryByText(/JSON 格式错误/)).not.toBeInTheDocument();
  });

  it('accepts string values like "hello"', () => {
    const onUpdate = vi.fn();
    render(<FilterForm node={baseNode} onUpdate={onUpdate} />);
    setValue('"hello"');
    expect(screen.queryByText(/JSON 格式错误/)).not.toBeInTheDocument();
    const lastCall = onUpdate.mock.calls[onUpdate.mock.calls.length - 1][0];
    expect(lastCall.params.value).toBe('hello');
  });
});
