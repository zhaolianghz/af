// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { useCallback, useEffect, useState } from 'react';
import clsx from 'clsx';
import { getHealth, getPing } from '@/api/health';
import type { HealthResponse } from '@/types/api';
import { formatTimestamp, formatUptime } from '@/utils/format';

type Status = 'idle' | 'loading' | 'ok' | 'fail';

export default function Health(): JSX.Element {
  const [status, setStatus] = useState<Status>('idle');
  const [data, setData] = useState<HealthResponse | null>(null);
  const [ping, setPing] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchAll = useCallback(async () => {
    setStatus('loading');
    setError(null);
    try {
      const [h, p] = await Promise.all([getHealth(), getPing()]);
      setData(h);
      setPing(p);
      setStatus(h.status === 'ok' ? 'ok' : 'fail');
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'unknown error';
      setError(msg);
      setStatus('fail');
    }
  }, []);

  useEffect(() => {
    void fetchAll();
  }, [fetchAll]);

  const isOk = status === 'ok';
  const isFail = status === 'fail';

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">健康检查 / Health</h1>
          <p className="mt-1 text-sm text-slate-500">
            调用后端 <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">/healthz</code> 与
            <code className="ml-1 rounded bg-slate-100 px-1 py-0.5 text-xs">/api/v1/ping</code>
            ，用于本地联调验证。
          </p>
        </div>
        <button
          type="button"
          onClick={fetchAll}
          disabled={status === 'loading'}
          className="rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-sm text-slate-700 shadow-soft transition-colors hover:bg-slate-50 disabled:opacity-50"
        >
          {status === 'loading' ? '刷新中…' : '刷新'}
        </button>
      </div>

      <div
        className={clsx(
          'rounded-2xl border p-6 shadow-soft',
          isOk && 'border-emerald-200 bg-emerald-50',
          isFail && 'border-rose-200 bg-rose-50',
          status === 'idle' && 'border-slate-200 bg-white',
          status === 'loading' && 'border-slate-200 bg-white',
        )}
      >
        <div className="flex items-center gap-3">
          <span
            className={clsx(
              'inline-flex h-3 w-3 rounded-full',
              isOk && 'bg-emerald-500',
              isFail && 'bg-rose-500',
              (status === 'idle' || status === 'loading') && 'bg-slate-300',
            )}
          />
          <span className="text-base font-semibold text-slate-900">
            {isOk && '✓ OK — 后端运行正常'}
            {isFail && '✗ FAIL — 后端不可达'}
            {status === 'idle' && '尚未检查'}
            {status === 'loading' && '检查中…'}
          </span>
        </div>

        {data && (
          <dl className="mt-5 grid grid-cols-1 gap-4 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-xs uppercase tracking-wider text-slate-400">status</dt>
              <dd className="mt-1 font-mono text-slate-700">{data.status}</dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wider text-slate-400">version</dt>
              <dd className="mt-1 font-mono text-slate-700">{data.version}</dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wider text-slate-400">timestamp</dt>
              <dd className="mt-1 font-mono text-slate-700">
                {formatTimestamp(data.ts)}
              </dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wider text-slate-400">uptime</dt>
              <dd className="mt-1 font-mono text-slate-700">
                {formatUptime(data.uptime)}
              </dd>
            </div>
            <div className="sm:col-span-2">
              <dt className="text-xs uppercase tracking-wider text-slate-400">
                ping (/api/v1/ping)
              </dt>
              <dd className="mt-1 font-mono text-slate-700">{ping ?? '—'}</dd>
            </div>
          </dl>
        )}

        {error && (
          <p className="mt-4 text-sm text-rose-600">
            <span className="font-medium">错误：</span>
            {error}
          </p>
        )}
      </div>

      <div className="rounded-2xl border border-slate-200 bg-white p-5 text-xs text-slate-500 shadow-soft">
        <p>
          如出现 "Network Error"，请确认后端已启动：
          <code className="mx-1 rounded bg-slate-100 px-1 py-0.5">
            cd backend &amp;&amp; go run ./cmd/server
          </code>
          （默认监听 :8080）。
        </p>
      </div>
    </div>
  );
}
