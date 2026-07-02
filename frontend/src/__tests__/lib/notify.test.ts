// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from 'vitest';

// Mock react-hot-toast before importing the module under test.
vi.mock('react-hot-toast', () => ({
  default: {
    error: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
    warning_ambiguous: vi.fn(),
    warning_msg: vi.fn(),
    dismiss: vi.fn(),
  },
  toast: Object.assign(vi.fn(), {
    error: vi.fn(),
    success: vi.fn(),
    dismiss: vi.fn(),
  }),
}));

import toast from 'react-hot-toast';
import { getErrorMessage, notifyError, notifySuccess } from '@/lib/notify';

const mockedToast = toast as unknown as { error: ReturnType<typeof vi.fn>; success: ReturnType<typeof vi.fn> };

describe('getErrorMessage', () => {
  it('returns fallback for undefined', () => {
    expect(getErrorMessage(undefined, '网络错误')).toBe('网络错误');
  });

  it('returns fallback for null', () => {
    expect(getErrorMessage(null, '服务器开小差')).toBe('服务器开小差');
  });

  it('returns the string itself if it is a non-empty string', () => {
    expect(getErrorMessage('plain text error', 'fallback')).toBe('plain text error');
  });

  it('returns fallback for empty string', () => {
    expect(getErrorMessage('', 'fallback')).toBe('fallback');
  });

  it('extracts message from a generic Error', () => {
    expect(getErrorMessage(new Error('something broke'), 'fallback')).toBe('something broke');
  });

  it('falls back when Error has empty message', () => {
    expect(getErrorMessage(new Error(''), 'fallback')).toBe('fallback');
  });

  it('extracts message from AxiosError-shaped object', () => {
    const axiosLike = new Error('Request failed with status code 404') as Error & {
      response: { data: { code: number; message: string } };
    };
    axiosLike.response = { data: { code: 404, message: '方案不存在' } };
    expect(getErrorMessage(axiosLike, 'fallback')).toBe('方案不存在');
  });

  it('falls back when AxiosError has no response data message', () => {
    const axiosLike = new Error('network error');
    expect(getErrorMessage(axiosLike, '网络异常')).toBe('network error');
  });

  it('handles plain object with message field', () => {
    expect(getErrorMessage({ message: 'from object' }, 'fallback')).toBe('from object');
  });

  it('handles numbers, booleans (returns fallback)', () => {
    expect(getErrorMessage(42, 'fallback')).toBe('fallback');
    expect(getErrorMessage(true, 'fallback')).toBe('fallback');
  });
});

describe('notifyError', () => {
  beforeEach(() => {
    mockedToast.error.mockClear();
  });

  it('calls toast.error with the extracted message', () => {
    notifyError(new Error('boom'), 'fallback');
    expect(mockedToast.error).toHaveBeenCalledWith('boom');
  });

  it('uses fallback for empty errors', () => {
    notifyError(null, '操作失败');
    expect(mockedToast.error).toHaveBeenCalledWith('操作失败');
  });
});

describe('notifySuccess', () => {
  beforeEach(() => {
    mockedToast.success.mockClear();
  });

  it('calls toast.success with the message', () => {
    notifySuccess('保存成功');
    expect(mockedToast.success).toHaveBeenCalledWith('保存成功');
  });
});
