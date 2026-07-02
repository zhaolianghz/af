// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * TemplateGalleryPage — grid of the 5 built-in strategy templates.
 * Each card shows the template's description + AI explanation and
 * a "使用模板" button that creates a new Strategy in draft state
 * and navigates to the editor.
 */
import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import clsx from 'clsx';
import { createFromTemplate, listTemplates } from '@/api/templates';
import type { StrategyTemplate } from '@/types/orchestrator';

export default function TemplateGalleryPage(): JSX.Element {
  const navigate = useNavigate();
  const [items, setItems] = useState<StrategyTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busyCode, setBusyCode] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listTemplates()
      .then((rows) => {
        if (cancelled) return;
        setItems(rows);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : '加载模板失败');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const onUse = useCallback(
    async (t: StrategyTemplate) => {
      setBusyCode(t.code);
      setError(null);
      try {
        const { strategy } = await createFromTemplate(t.code);
        navigate(`/strategies/${strategy.id}`);
      } catch (err) {
        setError(err instanceof Error ? err.message : '启用模板失败');
        setBusyCode(null);
      }
    },
    [navigate],
  );

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-3">
        <Link to="/strategies" className="btn-secondary">
          ← 返回方案
        </Link>
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">模板库</h1>
          <p className="mt-1 text-sm text-slate-500">
            5 个内置策略模板，开箱即用；点 "使用" 创建草稿后进入编辑器。
          </p>
        </div>
      </div>

      {error && (
        <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-2 text-xs text-rose-700">
          {error}
        </div>
      )}

      {loading ? (
        <div className="py-10 text-center text-sm text-slate-400">加载模板…</div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {items.map((t) => (
            <TemplateCard
              key={t.code}
              template={t}
              busy={busyCode === t.code}
              onUse={() => onUse(t)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// =============================================================================
// Card
// =============================================================================

function TemplateCard({
  template,
  busy,
  onUse,
}: {
  template: StrategyTemplate;
  busy: boolean;
  onUse: () => void;
}): JSX.Element {
  return (
    <article
      className={clsx(
        'flex h-full flex-col rounded-2xl border border-slate-200 bg-white p-5 shadow-soft transition-shadow',
        'hover:shadow-md',
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold text-slate-900">{template.name}</h2>
          <div className="mt-0.5 font-mono text-[11px] text-slate-400">
            {template.code}
          </div>
        </div>
        {template.industry && (
          <span className="rounded bg-slate-100 px-2 py-0.5 text-[10px] text-slate-600">
            {template.industry}
          </span>
        )}
      </div>

      <p className="mt-3 text-xs text-slate-600">{template.description}</p>

      {template.ai_explanation && (
        <div className="mt-3 rounded-lg border border-indigo-100 bg-indigo-50/60 px-3 py-2 text-[11px] leading-relaxed text-indigo-900">
          <div className="mb-0.5 font-semibold uppercase tracking-wider text-indigo-700">
            AI 解释
          </div>
          {template.ai_explanation}
        </div>
      )}

      <div className="mt-auto flex items-center justify-end gap-2 pt-4">
        <Link
          to={`/strategies?code_like=${encodeURIComponent(template.code)}`}
          className="btn-secondary"
        >
          查找使用本模板的方案
        </Link>
        <button
          type="button"
          onClick={onUse}
          disabled={busy}
          className="btn-primary"
        >
          {busy ? '创建中…' : '使用模板'}
        </button>
      </div>
    </article>
  );
}