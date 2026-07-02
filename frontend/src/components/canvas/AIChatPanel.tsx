// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * AIChatPanel — §11 conversational strategy editor.
 *
 * Two-stage: the user types an instruction → preview shows the proposed
 * DAG diff → on confirm, apply commits a new strategy version and the
 * editor reloads. The LLM only proposes; nothing changes until the user
 * clicks 应用.
 */
import { useState } from 'react';
import { aiPreview, aiApply, type AIPreviewResult } from '@/api/ai';
import { notifyError, notifySuccess } from '@/lib/notify';

interface Props {
  strategyId: number;
  onApplied: () => void;
  onClose: () => void;
}

const EXAMPLES = ['把 ma 周期改成 10', 'top 取 5', '保留前 3 只'];

export default function AIChatPanel({ strategyId, onApplied, onClose }: Props): JSX.Element {
  const [instruction, setInstruction] = useState('');
  const [preview, setPreview] = useState<AIPreviewResult | null>(null);
  const [busy, setBusy] = useState(false);

  const runPreview = async () => {
    const instr = instruction.trim();
    if (!instr) return;
    setBusy(true);
    setPreview(null);
    try {
      const r = await aiPreview(strategyId, instr);
      setPreview(r);
      if (!r.changed) {
        notifyError(new Error('AI 未对方案做出改动'), '无变化');
      }
    } catch (e) {
      notifyError(e, '预览失败');
    } finally {
      setBusy(false);
    }
  };

  const applyChange = async () => {
    if (!preview || !preview.changed) return;
    setBusy(true);
    try {
      const r = await aiApply(strategyId, preview.instruction, preview.proposed_dag);
      notifySuccess(`已应用，方案更新到版本 ${r.version}`);
      setPreview(null);
      setInstruction('');
      onApplied();
    } catch (e) {
      notifyError(e, '应用失败');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex h-full w-[340px] flex-col border-l border-slate-200 bg-white">
      <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3">
        <div>
          <div className="text-sm font-semibold text-slate-900">AI 助手</div>
          <div className="text-[11px] text-slate-400">自然语言改方案 · 预览后确认</div>
        </div>
        <button onClick={onClose} className="text-slate-400 hover:text-slate-700" aria-label="关闭">
          ✕
        </button>
      </div>

      <div className="flex-1 space-y-3 overflow-y-auto p-4">
        <div className="flex flex-wrap gap-1">
          {EXAMPLES.map((ex) => (
            <button
              key={ex}
              onClick={() => setInstruction(ex)}
              className="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] text-slate-600 hover:bg-slate-200"
            >
              {ex}
            </button>
          ))}
        </div>

        {preview && (
          <div className="rounded-lg border border-slate-200 p-3">
            <div className="text-xs font-medium text-slate-700">
              改动预览 <span className="text-slate-400">({preview.llm})</span>
            </div>
            {preview.changed ? (
              <ul className="mt-2 space-y-1 text-[11px] text-slate-600">
                {preview.changes.map((c, i) => (
                  <li key={i} className="rounded bg-amber-50 px-2 py-1 font-mono">
                    {c}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-2 text-[11px] text-slate-400">未检测到改动。换个说法试试。</p>
            )}
            {preview.changed && (
              <div className="mt-3 flex gap-2">
                <button onClick={applyChange} disabled={busy} className="btn-primary flex-1 text-xs">
                  应用
                </button>
                <button onClick={() => setPreview(null)} disabled={busy} className="btn-secondary text-xs">
                  放弃
                </button>
              </div>
            )}
          </div>
        )}
      </div>

      <div className="border-t border-slate-200 p-3">
        <textarea
          value={instruction}
          onChange={(e) => setInstruction(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) runPreview();
          }}
          rows={2}
          placeholder="例如：把 ma 周期改成 10（Cmd/Ctrl+Enter 预览）"
          className="w-full resize-none rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none"
        />
        <button onClick={runPreview} disabled={busy || !instruction.trim()} className="btn-primary mt-2 w-full text-sm">
          {busy ? '处理中…' : '预览改动'}
        </button>
      </div>
    </div>
  );
}
