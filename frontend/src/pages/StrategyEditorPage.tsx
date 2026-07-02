// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * StrategyEditorPage — three-column strategy editor.
 *   Left rail:  NodePalette (drag source for the 8 node types)
 *   Center:     Canvas (ReactFlow graph + drag target)
 *   Right rail: NodeConfigPanel (selected node's type-specific form)
 *   Top:        Toolbar (save / validate / trial-run / back)
 *
 * Save: snapshots the current canvas graph and PUTs `dag_json` +
 * metadata. Trial-run: fires a dry-run on the backend, surfaces the
 * node-by-node summary inline.
 */
import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import Canvas from '@/components/canvas/Canvas';
import NodeConfigPanel from '@/components/canvas/NodeConfigPanel';
import NodePalette from '@/components/canvas/NodePalette';
import Toolbar from '@/components/canvas/Toolbar';
import AIChatPanel from '@/components/canvas/AIChatPanel';
import { snapshotDagJson } from '@/components/canvas/snapshot';
import {
  getStrategy,
  trialRun,
  updateStrategy,
} from '@/api/strategies';
import { parseDag } from '@/api/strategies';
import { useCanvasStore } from '@/stores/canvasStore';
import { useCanvasHotkeys } from '@/hooks/useCanvasHotkeys';
import type { StrategyDetail, TrialRunSummary } from '@/types/orchestrator';

export default function StrategyEditorPage(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  const strategyId = id ? Number(id) : NaN;
  const navigate = useNavigate();

  const loadFromDag = useCanvasStore((s) => s.loadFromDag);
  const reset = useCanvasStore((s) => s.reset);
  const setSaveStatus = useCanvasStore((s) => s.setSaveStatus);
  const setValidation = useCanvasStore((s) => s.setValidation);
  const nodes = useCanvasStore((s) => s.nodes);
  const addNode = useCanvasStore((s) => s.addNode);

  const [strategy, setStrategy] = useState<StrategyDetail | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [trialRunning, setTrialRunning] = useState(false);
  const [trialSummary, setTrialSummary] = useState<TrialRunSummary | null>(null);
  const [aiOpen, setAiOpen] = useState(false);

  // loadStrategy fetches the strategy and loads its DAG into the canvas.
  // Reused on mount and after the AI assistant applies a change.
  const loadStrategy = useCallback(() => {
    if (!Number.isFinite(strategyId)) {
      setLoadError('无效的方案 ID');
      return;
    }
    setLoadError(null);
    getStrategy(strategyId)
      .then((detail) => {
        setStrategy(detail);
        const dagJson = detail.current_version_dag ?? detail.dag_json ?? '';
        loadFromDag(parseDag(dagJson), {
          id: detail.id,
          name: detail.name,
          code: detail.code,
        });
      })
      .catch((err: unknown) => {
        setLoadError(err instanceof Error ? err.message : '加载失败');
      });
  }, [strategyId, loadFromDag]);

  // Load strategy into the canvas store on mount / id change.
  useEffect(() => {
    loadStrategy();
    return () => {
      reset();
    };
  }, [loadStrategy, reset]);

  const onSave = useCallback(async () => {
    if (!strategy) return;
    setSaving(true);
    setSaveStatus('saving');
    try {
      const dagJson = snapshotDagJson();
      await updateStrategy(strategy.id, {
        name: strategy.name,
        description: strategy.description,
        tags: strategy.tags,
        dag_json: dagJson,
        change_note: 'editor save',
      });
      setSaveStatus('saved');
    } catch (err) {
      setSaveStatus('error', err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }, [strategy, setSaveStatus]);

  const onValidate = useCallback(async () => {
    // Cheap client-side validation: DAG must have at least one data_source
    // and at least one persist node for a meaningful pipeline.
    const hasDataSource = nodes.some((n) => n.type === 'data_source');
    const hasPersist = nodes.some((n) => n.type === 'persist');
    if (nodes.length === 0) {
      setValidation('invalid', '画布为空');
      return;
    }
    if (!hasDataSource) {
      setValidation('invalid', '缺少数据源节点');
      return;
    }
    if (!hasPersist) {
      setValidation('invalid', '缺少持久化节点');
      return;
    }
    setValidation('valid');
  }, [nodes, setValidation]);

  const onTrialRun = useCallback(async () => {
    if (!strategy) return;
    setTrialRunning(true);
    setTrialSummary(null);
    try {
      const res = await trialRun(strategy.id, {});
      setTrialSummary(res.summary);
    } catch (err) {
      setTrialSummary({
        status: 'failed',
        error: err instanceof Error ? err.message : String(err),
        node_results: [],
      });
    } finally {
      setTrialRunning(false);
    }
  }, [strategy]);

  // Keyboard shortcuts: Delete=remove node, Ctrl+S=save.
  useCanvasHotkeys(onSave);

  const onBack = useCallback(() => {
    navigate('/strategies');
  }, [navigate]);

  if (loadError) {
    return (
      <div className="space-y-3">
        <Link to="/strategies" className="btn-secondary">
          ← 返回列表
        </Link>
        <div className="rounded-2xl border border-rose-200 bg-rose-50 p-6 text-sm text-rose-700">
          加载失败: {loadError}
        </div>
      </div>
    );
  }

  return (
    <div className="-m-6 flex h-[calc(100vh-3.5rem)] flex-col">
      <Toolbar
        onSave={onSave}
        onTrialRun={onTrialRun}
        onValidate={onValidate}
        onBack={onBack}
        saving={saving}
        trialRunning={trialRunning}
      />

      <div className="flex flex-1 overflow-hidden">
        <NodePalette onAdd={(type) => addNode(type, { x: 240, y: 200 })} />
        <div className="flex flex-1 flex-col overflow-hidden">
          <Canvas />
          {trialSummary && <TrialRunBanner summary={trialSummary} />}
        </div>
        <NodeConfigPanel />
        {aiOpen && Number.isFinite(strategyId) && (
          <AIChatPanel
            strategyId={strategyId}
            onApplied={loadStrategy}
            onClose={() => setAiOpen(false)}
          />
        )}
      </div>

      {!aiOpen && (
        <button
          onClick={() => setAiOpen(true)}
          className="absolute bottom-6 right-6 rounded-full bg-brand-600 px-4 py-2 text-sm font-medium text-white shadow-lg hover:bg-brand-700"
        >
          ✨ AI 助手
        </button>
      )}

      {nodes.length === 0 && (
        <div className="pointer-events-none absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 rounded-xl border border-dashed border-slate-300 bg-white/80 px-6 py-3 text-center text-xs text-slate-400 shadow-sm">
          从左侧拖入节点开始编排 DAG
        </div>
      )}
    </div>
  );
}

// =============================================================================
// Trial-run banner (sits below the canvas)
// =============================================================================

function TrialRunBanner({ summary }: { summary: TrialRunSummary }): JSX.Element {
  const tone =
    summary.status === 'success'
      ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
      : summary.status === 'failed'
        ? 'border-rose-200 bg-rose-50 text-rose-700'
        : 'border-amber-200 bg-amber-50 text-amber-700';

  const succeeded = summary.node_results.filter((r) => r.status === 'success').length;
  const failed = summary.node_results.filter((r) => r.status === 'failed').length;
  const skipped = summary.node_results.filter((r) => r.status === 'skipped').length;

  return (
    <div className={`border-t ${tone} px-4 py-2 text-xs`}>
      <div className="flex items-center justify-between">
        <div>
          <span className="font-medium">试运行结果 · {summary.status}</span>
          {summary.duration_ms !== undefined && (
            <span className="ml-2 text-slate-500">{summary.duration_ms} ms</span>
          )}
          {summary.error && <span className="ml-2 text-rose-700">{summary.error}</span>}
        </div>
        <div className="flex items-center gap-3 text-[11px]">
          <span>成功 {succeeded}</span>
          <span>失败 {failed}</span>
          <span>跳过 {skipped}</span>
          <span>共 {summary.node_results.length}</span>
        </div>
      </div>
    </div>
  );
}