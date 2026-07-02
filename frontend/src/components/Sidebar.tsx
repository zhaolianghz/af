// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { NavLink } from 'react-router-dom';
import clsx from 'clsx';

interface NavItem {
  to: string;
  label: string;
  hint: string;
}

const NAV: NavItem[] = [
  { to: '/dashboard', label: '仪表盘', hint: 'Dashboard' },
  { to: '/strategies', label: '方案', hint: 'Strategies' },
  { to: '/templates', label: '模板库', hint: 'Templates' },
  { to: '/runs', label: '运行历史', hint: 'Runs' },
  { to: '/recommendations', label: '推荐', hint: 'Picks' },
  { to: '/positions', label: '持仓', hint: 'Positions' },
  { to: '/reviews', label: '复盘', hint: 'Reviews' },
  { to: '/health', label: '健康检查', hint: 'Health' },
  { to: '/settings', label: '设置', hint: 'Settings' },
];

export default function Sidebar(): JSX.Element {
  return (
    <aside className="flex h-full w-[220px] flex-col border-r border-slate-200 bg-white">
      <div className="flex h-14 items-center gap-2 border-b border-slate-200 px-5">
        <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-brand-600 text-sm font-semibold text-white">
          AF
        </div>
        <div className="flex flex-col leading-tight">
          <span className="text-sm font-semibold text-slate-900">AF Selector</span>
          <span className="text-[10px] uppercase tracking-wider text-slate-400">
            v1.0.1
          </span>
        </div>
      </div>

      <nav className="flex-1 space-y-1 p-3">
        {NAV.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            className={({ isActive }) =>
              clsx(
                'flex flex-col rounded-lg px-3 py-2 text-sm transition-colors',
                isActive
                  ? 'bg-brand-50 text-brand-700'
                  : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900',
              )
            }
          >
            <span className="font-medium">{item.label}</span>
            <span className="text-[10px] uppercase tracking-wider text-slate-400">
              {item.hint}
            </span>
          </NavLink>
        ))}
      </nav>

      <div className="border-t border-slate-200 p-3 text-[11px] text-slate-400">
        <div>AF Selector · A7</div>
        <div className="mt-0.5">© 2026 skyzhao</div>
      </div>
    </aside>
  );
}
