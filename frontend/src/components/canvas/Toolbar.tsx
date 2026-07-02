// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Toolbar — the save / validate / trial-run / cancel strip at the
 * top of the editor. Buttons drive side-effects via the canvas
 * store + the strategy API.
 */
import clsx from 'clsx';
import { useCanvasStore, type SaveStatus } from '@/stores/canvasStore';

export interface ToolbarProps {
  onSave: () => void | Promise<void>;
  onTrialRun: () => void | Promise<void>;
  onValidate: () => void | Promise<void>;
  onBack: () => void;
  saving: boolean;
  trialRunning: boolean;
}

export default function Toolbar({
  onSave,
  onTrialRun,
  onValidate,
  onBack,
  saving,
  trialRunning,
}: ToolbarProps): JSX.Element {
  const saveStatus = useCanvasStore((s) => s.saveStatus);
  const saveError = useCanvasStore((s) => s.saveError);
  const validation = useCanvasStore((s) => s.validation);
  const validationError = useCanvasStore((s) => s.validationError);
  const nodeCount = useCanvasStore((s) => s.nodes.length);
  const edgeCount = useCanvasStore((s) => s.edges.length);

  return (
    <div className="flex items-center justify-between border-b border-slate-200 bg-white px-4 py-2">
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={onBack}
          className="rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50"
        >
          ← 返回列表
        </button>

        <div className="flex items-center gap-2 text-[11px] text-slate-500">
          <span className="rounded-md bg-slate-100 px-1.5 py-0.5 font-mono text-slate-600">
            节点 {nodeCount}
          </span>
          <span className="rounded-md bg-slate-100 px-1.5 py-0.5 font-mono text-slate-600">
            连线 {edgeCount}
          </span>
        </div>

        <SaveIndicator status={saveStatus} error={saveError} />
        <ValidationIndicator status={validation} error={validationError} />
      </div>

      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={onValidate}
          className="rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50"
        >
          校验
        </button>
        <button
          type="button"
          onClick={onTrialRun}
          disabled={trialRunning}
          className="rounded-lg border border-indigo-200 bg-indigo-50 px-3 py-1.5 text-xs font-medium text-indigo-700 hover:bg-indigo-100 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {trialRunning ? '试运行中…' : '试运行'}
        </button>
        <button
          type="button"
          onClick={onSave}
          disabled={saving || saveStatus === 'saved'}
          className={clsx(
            'rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors',
            saving
              ? 'cursor-wait bg-slate-400'
              : 'bg-brand-600 hover:bg-brand-700 disabled:cursor-not-allowed disabled:bg-slate-300',
          )}
        >
          {saving ? '保存中…' : '保存'}
        </button>
      </div>
    </div>
  );
}

// =============================================================================
// Status indicators
// =============================================================================

function SaveIndicator({ status, error }: { status: SaveStatus; error: string | null }): JSX.Element {
  const map: Record<SaveStatus, { label: string; cls: string }> = {
    idle: { label: '未改动', cls: 'text-slate-400' },
    dirty: { label: '有未保存改动', cls: 'text-amber-600' },
    saving: { label: '保存中…', cls: 'text-slate-500' },
    saved: { label: '已保存', cls: 'text-emerald-600' },
    error: { label: `保存失败: ${error ?? '未知错误'}`, cls: 'text-rose-600' },
  };
  const v = map[status];
  return <span className={clsx('text-[11px]', v.cls)}>● {v.label}</span>;
}

function ValidationIndicator({
  status,
  error,
}: {
  status: ReturnType<typeof useCanvasStore.getState>['validation'];
  error: string | null;
}): JSX.Element | null {
  if (status === 'unknown') return null;
  const cls = status === 'valid' ? 'text-emerald-600' : 'text-rose-600';
  const label = status === 'valid' ? '校验通过' : error ?? '校验失败';
  return <span className={clsx('text-[11px]', cls)}>● {label}</span>;
}

// =============================================================================
// Helper: snapshot the current graph as a dag_json string.
//
// Lives in `snapshot.ts` so Toolbar can stay component-only
// (react-refresh friendliness).
// =============================================================================
