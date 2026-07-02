// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { useState, type FormEvent } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { login } from '@/api/auth';
import { useAuthStore } from '@/stores/authStore';
import { notifyError } from '@/lib/notify';

export default function LoginPage(): JSX.Element {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const setSession = useAuthStore((s) => s.setSession);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (!username || !password) return;
    setBusy(true);
    try {
      const { token, user } = await login(username, password);
      setSession(token, user);
      const next = params.get('next');
      navigate(next ? decodeURIComponent(next) : '/dashboard', { replace: true });
    } catch (err) {
      notifyError(err, '登录失败,请检查用户名和密码');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex h-screen w-screen items-center justify-center bg-slate-50">
      <form
        onSubmit={onSubmit}
        className="w-full max-w-sm rounded-2xl border border-slate-200 bg-white p-8 shadow-soft"
      >
        <div className="mb-6 text-center">
          <h1 className="text-lg font-semibold text-slate-900">A 股选股系统</h1>
          <p className="mt-1 text-xs text-slate-400">AF Selector · 登录</p>
        </div>

        <label className="mb-1 block text-xs font-medium text-slate-600">用户名</label>
        <input
          autoFocus
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoComplete="username"
          className="mb-4 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500"
        />

        <label className="mb-1 block text-xs font-medium text-slate-600">密码</label>
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          className="mb-6 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500"
        />

        <button
          type="submit"
          disabled={busy || !username || !password}
          className="w-full rounded-lg bg-slate-900 py-2 text-sm font-medium text-white transition hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? '登录中…' : '登录'}
        </button>
      </form>
    </div>
  );
}
