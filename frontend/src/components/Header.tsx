// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/stores/authStore';

export default function Header(): JSX.Element {
  const navigate = useNavigate();
  const user = useAuthStore((s) => s.user);
  const clear = useAuthStore((s) => s.clear);

  const onLogout = () => {
    clear();
    navigate('/login', { replace: true });
  };

  return (
    <header className="flex h-14 items-center justify-between border-b border-slate-200 bg-white px-6">
      <div className="flex flex-col leading-tight">
        <span className="text-sm font-semibold text-slate-900">A 股选股系统</span>
        <span className="text-[11px] text-slate-400">
          AF Selector · v1.0.1
        </span>
      </div>

      <div className="flex items-center gap-4 text-[11px] text-slate-400">
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-flex h-2 w-2 rounded-full bg-emerald-500" />
          ready
        </span>
        {/* §12: show the logged-in user + logout only when auth is active
            (a token/user is present). When auth is disabled there is no
            user and this block stays hidden. */}
        {user && (
          <>
            <span className="text-slate-500">{user.username}</span>
            <button
              type="button"
              onClick={onLogout}
              className="rounded-md border border-slate-200 px-2 py-1 text-slate-500 transition hover:bg-slate-50 hover:text-slate-900"
            >
              退出
            </button>
          </>
        )}
      </div>
    </header>
  );
}
