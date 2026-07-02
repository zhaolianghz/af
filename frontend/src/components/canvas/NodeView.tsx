// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * NodeView — the ReactFlow node renderer. Each `type` gets its
 * own colour + label; the body shows a short summary of the
 * node's params (truncated to 60 chars).
 */
import { Handle, Position, type NodeProps } from 'reactflow';
import clsx from 'clsx';
import { NODE_COLORS, NODE_LABELS, type NodeType } from '@/types/orchestrator';

export interface NodeViewData {
  subtype?: string;
  params?: Record<string, unknown>;
}

export default function NodeView({ data, type, selected }: NodeProps<NodeViewData>): JSX.Element {
  const t = type as NodeType;
  const colors = NODE_COLORS[t];
  const summary = summarizeParams(data.params);

  return (
    <div
      className={clsx(
        'min-w-[180px] max-w-[240px] rounded-xl border-2 bg-white shadow-sm transition-shadow',
        colors.border,
        selected ? 'ring-2 ring-brand-400 ring-offset-1' : '',
      )}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="!h-2.5 !w-2.5 !border-2 !border-white !bg-slate-400"
      />

      <div className={clsx('rounded-t-[10px] px-3 py-1.5', colors.bg)}>
        <div className="flex items-center justify-between">
          <span className={clsx('text-xs font-semibold', colors.text)}>
            {NODE_LABELS[t]}
          </span>
          {data.subtype && (
            <span className="rounded-full bg-white px-1.5 py-0.5 text-[9px] font-mono uppercase tracking-wider text-slate-500">
              {data.subtype}
            </span>
          )}
        </div>
      </div>

      <div className="px-3 py-2">
        {summary ? (
          <p className="font-mono text-[10px] leading-relaxed text-slate-600 break-all">
            {summary}
          </p>
        ) : (
          <p className="text-[10px] italic text-slate-400">无参数</p>
        )}
      </div>

      <Handle
        type="source"
        position={Position.Right}
        className="!h-2.5 !w-2.5 !border-2 !border-white !bg-slate-400"
      />
    </div>
  );
}

function summarizeParams(params: Record<string, unknown> | undefined): string {
  if (!params) return '';
  const entries = Object.entries(params).filter(([, v]) => v !== '' && v != null);
  if (entries.length === 0) return '';
  return entries
    .slice(0, 4)
    .map(([k, v]) => {
      const s = typeof v === 'string' ? v : JSON.stringify(v);
      return `${k}=${truncate(s, 18)}`;
    })
    .join('\n');
}

function truncate(s: string, n: number): string {
  if (s.length <= n) return s;
  return s.slice(0, n - 1) + '…';
}
