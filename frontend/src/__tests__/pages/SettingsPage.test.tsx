// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const getProvidersMock = vi.fn();
const saveProvidersMock = vi.fn();
const testProviderMock = vi.fn();
const listProviderModelsMock = vi.fn();
const notifyErrorMock = vi.fn();
const notifySuccessMock = vi.fn();
const changePasswordMock = vi.fn();

// authStore user is swappable per test (logged-in vs auth-disabled).
let mockUser: { id: number; username: string; role_id: number } | null = null;

vi.mock('@/api/settings', () => ({
  getProviders: () => getProvidersMock(),
  saveProviders: (p: unknown) => saveProvidersMock(p),
  testProvider: (p: unknown) => testProviderMock(p),
  listProviderModels: (p: unknown) => listProviderModelsMock(p),
}));
vi.mock('@/api/auth', () => ({
  changePassword: (o: string, n: string) => changePasswordMock(o, n),
}));
vi.mock('@/stores/authStore', () => ({
  // Mirror zustand selector usage: useAuthStore((s) => s.user).
  useAuthStore: (sel: (s: { user: typeof mockUser }) => unknown) => sel({ user: mockUser }),
}));
vi.mock('@/lib/notify', () => ({
  notifyError: (e: unknown, m?: string) => notifyErrorMock(e, m),
  notifySuccess: (m: string) => notifySuccessMock(m),
}));

import SettingsPage from '@/pages/SettingsPage';
import type { ChainView } from '@/api/settings';

const CHAIN: ChainView = {
  active: 'chain[deepseek:deepseek-chat>glm:glm-4.5]',
  providers: [
    {
      id: 1,
      priority: 0,
      enabled: true,
      provider: 'deepseek',
      base_url: 'https://api.deepseek.com/v1',
      api_key_set: true,
      api_key_masked: 'sk-a…5678',
      model: 'deepseek-chat',
    },
    {
      id: 2,
      priority: 1,
      enabled: true,
      provider: 'glm',
      base_url: 'https://open.bigmodel.cn/api/paas/v4',
      api_key_set: true,
      api_key_masked: 'abcd…wxyz',
      model: 'glm-4.5',
    },
  ],
};

beforeEach(() => {
  getProvidersMock.mockReset();
  saveProvidersMock.mockReset();
  testProviderMock.mockReset();
  listProviderModelsMock.mockReset();
  notifyErrorMock.mockReset();
  notifySuccessMock.mockReset();
  changePasswordMock.mockReset();
  mockUser = null;
});

describe('SettingsPage (multi-provider chain)', () => {
  it('renders the ordered provider list + active chain', async () => {
    getProvidersMock.mockResolvedValue(CHAIN);
    render(<SettingsPage />);
    await waitFor(() =>
      expect(screen.getByText(/chain\[deepseek/)).toBeInTheDocument(),
    );
    // Two rows numbered #1 and #2.
    expect(screen.getByText('#1')).toBeInTheDocument();
    expect(screen.getByText('#2')).toBeInTheDocument();
    // Provider selects reflect the saved providers.
    const selects = screen.getAllByRole('combobox');
    expect(selects).toHaveLength(2);
  });

  it('starts with one blank row when no providers saved', async () => {
    getProvidersMock.mockResolvedValue({ active: 'disabled', providers: [] });
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByText('#1')).toBeInTheDocument());
    expect(screen.queryByText('#2')).not.toBeInTheDocument();
  });

  it('adds a provider row', async () => {
    getProvidersMock.mockResolvedValue({ active: 'disabled', providers: [] });
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByText('#1')).toBeInTheDocument());
    await userEvent.click(screen.getByText('+ 添加服务商'));
    expect(screen.getByText('#2')).toBeInTheDocument();
  });

  it('saves the chain and shows the new active name', async () => {
    getProvidersMock.mockResolvedValue(CHAIN);
    saveProvidersMock.mockResolvedValue(CHAIN);
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByText('#1')).toBeInTheDocument());
    await userEvent.click(screen.getByText('保存全部'));
    await waitFor(() => expect(saveProvidersMock).toHaveBeenCalled());
    // Saved with 2 providers, deepseek first (priority order preserved).
    const arg = saveProvidersMock.mock.calls[0][0] as Array<{ provider: string }>;
    expect(arg).toHaveLength(2);
    expect(arg[0].provider).toBe('deepseek');
    expect(notifySuccessMock).toHaveBeenCalled();
  });

  it('tests a single provider', async () => {
    getProvidersMock.mockResolvedValue(CHAIN);
    testProviderMock.mockResolvedValue(undefined);
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByText('#1')).toBeInTheDocument());
    const testButtons = screen.getAllByText('测试此服务商');
    await userEvent.click(testButtons[0]);
    await waitFor(() => expect(testProviderMock).toHaveBeenCalled());
    expect(notifySuccessMock).toHaveBeenCalled();
  });

  it('removes a provider row', async () => {
    getProvidersMock.mockResolvedValue(CHAIN);
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByText('#2')).toBeInTheDocument());
    const removeButtons = screen.getAllByText('删除');
    await userEvent.click(removeButtons[1]); // remove the 2nd
    expect(screen.queryByText('#2')).not.toBeInTheDocument();
  });
});

describe('SettingsPage change-password card', () => {
  it('hides the card when auth is disabled (no user)', async () => {
    mockUser = null;
    getProvidersMock.mockResolvedValue({ active: 'disabled', providers: [] });
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByText('#1')).toBeInTheDocument());
    expect(screen.queryByText('修改密码')).not.toBeInTheDocument();
  });

  it('shows the card with the username when logged in', async () => {
    mockUser = { id: 1, username: 'admin', role_id: 0 };
    getProvidersMock.mockResolvedValue({ active: 'disabled', providers: [] });
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByLabelText('当前密码')).toBeInTheDocument());
    expect(screen.getByText('admin')).toBeInTheDocument();
  });

  it('rejects a too-short new password (no API call)', async () => {
    mockUser = { id: 1, username: 'admin', role_id: 0 };
    getProvidersMock.mockResolvedValue({ active: 'disabled', providers: [] });
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByLabelText('当前密码')).toBeInTheDocument());
    const pw = screen.getByLabelText('当前密码');
    const np = screen.getByLabelText('新密码(至少 8 位)');
    const cp = screen.getByLabelText('确认新密码');
    await userEvent.type(pw, 'oldpass1');
    await userEvent.type(np, 'short');
    await userEvent.type(cp, 'short');
    await userEvent.click(screen.getByRole('button', { name: '修改密码' }));
    expect(changePasswordMock).not.toHaveBeenCalled();
    expect(notifyErrorMock).toHaveBeenCalled();
  });

  it('rejects mismatched confirmation (no API call)', async () => {
    mockUser = { id: 1, username: 'admin', role_id: 0 };
    getProvidersMock.mockResolvedValue({ active: 'disabled', providers: [] });
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByLabelText('当前密码')).toBeInTheDocument());
    await userEvent.type(screen.getByLabelText('当前密码'), 'oldpass1');
    await userEvent.type(screen.getByLabelText('新密码(至少 8 位)'), 'newpassword1');
    await userEvent.type(screen.getByLabelText('确认新密码'), 'newpassword2');
    await userEvent.click(screen.getByRole('button', { name: '修改密码' }));
    expect(changePasswordMock).not.toHaveBeenCalled();
    expect(notifyErrorMock).toHaveBeenCalled();
  });

  it('submits a valid password change', async () => {
    mockUser = { id: 1, username: 'admin', role_id: 0 };
    getProvidersMock.mockResolvedValue({ active: 'disabled', providers: [] });
    changePasswordMock.mockResolvedValue(undefined);
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByLabelText('当前密码')).toBeInTheDocument());
    await userEvent.type(screen.getByLabelText('当前密码'), 'oldpass1');
    await userEvent.type(screen.getByLabelText('新密码(至少 8 位)'), 'newpassword1');
    await userEvent.type(screen.getByLabelText('确认新密码'), 'newpassword1');
    await userEvent.click(screen.getByRole('button', { name: '修改密码' }));
    await waitFor(() => expect(changePasswordMock).toHaveBeenCalledWith('oldpass1', 'newpassword1'));
    expect(notifySuccessMock).toHaveBeenCalled();
  });

  it('fetches models and switches the model field to a dropdown', async () => {
    getProvidersMock.mockResolvedValue(CHAIN);
    listProviderModelsMock.mockResolvedValue({
      all: ['deepseek-chat', 'deepseek-reasoner', 'text-embedding-3-small'],
      chat: ['deepseek-chat', 'deepseek-reasoner'],
    });
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByText('#1')).toBeInTheDocument());
    // The first row's model is currently a free input.
    const modelInput = screen.getAllByPlaceholderText('deepseek-chat')[0];
    expect(modelInput).toBeInTheDocument();
    // Click "拉取模型列表" on row #1.
    await userEvent.click(screen.getAllByRole('button', { name: '拉取模型列表' })[0]);
    await waitFor(() => expect(listProviderModelsMock).toHaveBeenCalled());
    // Success toast reports chat count.
    await waitFor(() =>
      expect(notifySuccessMock).toHaveBeenCalledWith(expect.stringContaining('对话类 2 个')),
    );
    // The model input became a select with the chat models; embedding
    // models are filtered out.
    const modelSelect = screen.getAllByRole('combobox')[1]; // 0 = provider preset
    expect(modelSelect).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'deepseek-chat' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'deepseek-reasoner' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'text-embedding-3-small' })).not.toBeInTheDocument();
    expect(screen.getByRole('option', { name: '手动输入…' })).toBeInTheDocument();
  });

  it('surfaces a fetch failure as an error toast and keeps the input', async () => {
    getProvidersMock.mockResolvedValue(CHAIN);
    listProviderModelsMock.mockRejectedValue(new Error('key 鉴权失败'));
    render(<SettingsPage />);
    await waitFor(() => expect(screen.getByText('#1')).toBeInTheDocument());
    await userEvent.click(screen.getAllByRole('button', { name: '拉取模型列表' })[0]);
    await waitFor(() => expect(notifyErrorMock).toHaveBeenCalled());
    // Still a free input.
    expect(screen.getAllByPlaceholderText('deepseek-chat')[0]).toBeInTheDocument();
  });
});
