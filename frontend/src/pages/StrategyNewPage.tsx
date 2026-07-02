// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * StrategyNewPage — entry point for creating a new strategy.
 * Two paths:
 *   1. "From template" — pick one of the 5 built-in templates,
 *      backend creates a fully-wired DAG, redirect to editor.
 *   2. "Blank" — open a minimal DAG in the editor and let the
 *      user drag nodes from the palette.
 */
import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import clsx from 'clsx';
import {
  createFromTemplate,
  createStrategy,
  listTemplates,
  stringifyDag,
} from '@/api/strategies';
import type { StrategyTemplate } from '@/types/orchestrator';

export default function StrategyNewPage(): JSX.Element {
  const navigate = useNavigate();
  const [templates, setTemplates] = useState<StrategyTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busyCode, setBusyCode] = useState<string | null>(null);

  // Blank form state.
  const [blankName, setBlankName] = useState('');
  const [blankCode, setBlankCode] = useState('');
  const [blankDescription, setBlankDescription] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listTemplates()
      .then((res) => {
        if (cancelled) return;
        setTemplates(res.items ?? []);
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

  const onUseTemplate = useCallback(
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

  const onCreateBlank = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!blankName.trim()) {
        setError('请输入方案名称');
        return;
      }
      setSubmitting(true);
      setError(null);
      try {
        const { strategy } = await createStrategy({
          name: blankName.trim(),
          code: blankCode.trim() || undefined,
          description: blankDescription.trim() || undefined,
          dag_json: stringifyDag({ nodes: [], edges: [] }),
        });
        navigate(`/strategies/${strategy.id}`);
      } catch (err) {
        setError(err instanceof Error ? err.message : '创建失败');
        setSubmitting(false);
      }
    },
    [blankName, blankCode, blankDescription, navigate],
  );

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-3">
        <Link to="/strategies" className="btn-secondary">
          ← 返回列表
        </Link>
        <h1 className="text-2xl font-semibold text-slate-900">新建方案</h1>
      </div>

      {error && (
        <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-2 text-xs text-rose-700">
          {error}
        </div>
      )}

      <div className="grid grid-cols-2 gap-5">
          {/* From template */}
          <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-soft">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold text-slate-900">从模板创建</h2>
              <span className="rounded-md bg-brand-50 px-2 py-0.5 text-[10px] font-medium text-brand-700">
                推荐
              </span>
            </div>
            <p className="mt-1 text-xs text-slate-500">
              5 个内置策略模板，自动生成 DAG，可直接编辑或试运行。
            </p>

            <div className="mt-4 space-y-2">
              {loading ? (
                <div className="py-10 text-center text-xs text-slate-400">加载模板…</div>
              ) : templates.length === 0 ? (
                <div className="py-10 text-center text-xs text-slate-400">暂无模板</div>
              ) : (
                templates.map((t) => (
                  <TemplateCard
                    key={t.code}
                    template={t}
                    busy={busyCode === t.code}
                    onUse={() => onUseTemplate(t)}
                  />
                ))
              )}
            </div>
          </section>

          {/* Blank */}
          <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-soft">
            <h2 className="text-sm font-semibold text-slate-900">空白创建</h2>
            <p className="mt-1 text-xs text-slate-500">
              从一个空 DAG 开始，从左侧节点面板拖入节点。
            </p>
            <form className="mt-4 space-y-3" onSubmit={onCreateBlank}>
              <Field label="名称 *" hint="必填">
                <input
                  type="text"
                  className="form-input"
                  value={blankName}
                  onChange={(e) => setBlankName(e.target.value)}
                  placeholder="例如：尾盘抢筹策略"
                  required
                />
              </Field>
              <Field label="编码" hint="留空自动生成">
                <input
                  type="text"
                  className="form-input"
                  value={blankCode}
                  onChange={(e) => setBlankCode(e.target.value)}
                  placeholder="例如：late_session_rush"
                />
              </Field>
              <Field label="描述">
                <textarea
                  className="form-input min-h-[80px] resize-y"
                  value={blankDescription}
                  onChange={(e) => setBlankDescription(e.target.value)}
                  placeholder="可选"
                />
              </Field>
              <div className="flex justify-end gap-2 pt-2">
                <Link to="/strategies" className="btn-secondary">
                  取消
                </Link>
                <button
                  type="submit"
                  disabled={submitting}
                  className="btn-primary"
                >
                  {submitting ? '创建中…' : '创建并进入编辑器'}
                </button>
              </div>
            </form>
          </section>
        </div>
    </div>
  );
}

// =============================================================================
// Helpers
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
    <div className="rounded-xl border border-slate-200 bg-slate-50/60 p-3 hover:border-brand-300 hover:bg-brand-50/40">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium text-slate-900">{template.name}</div>
          <div className="mt-0.5 font-mono text-[11px] text-slate-400">{template.code}</div>
          {template.industry && (
            <div className="mt-1 inline-block rounded bg-slate-200 px-1.5 py-0.5 text-[10px] text-slate-600">
              {template.industry}
            </div>
          )}
          <p className="mt-1.5 line-clamp-2 text-xs text-slate-500">
            {template.description}
          </p>
        </div>
        <button
          type="button"
          onClick={onUse}
          disabled={busy}
          className={clsx('btn-primary shrink-0')}
        >
          {busy ? '创建中…' : '使用'}
        </button>
      </div>
    </div>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <label className="block">
      <div className="flex items-baseline justify-between">
        <span className="text-[11px] font-medium text-slate-600">{label}</span>
        {hint && <span className="text-[10px] text-slate-400">{hint}</span>}
      </div>
      <div className="mt-1">{children}</div>
    </label>
  );
}