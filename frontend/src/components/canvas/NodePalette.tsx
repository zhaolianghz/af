// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * NodePalette — the left rail of the strategy editor. Lists the 8
 * available node types; users drag a card onto the canvas (or
 * double-click to drop it at the center).
 */
import clsx from 'clsx';
import {
  NODE_COLORS,
  NODE_DESCRIPTIONS,
  NODE_LABELS,
  NODE_TYPES,
  type NodeType,
} from '@/types/orchestrator';

export interface NodePaletteProps {
  onAdd: (type: NodeType) => void;
}

export default function NodePalette({ onAdd }: NodePaletteProps): JSX.Element {
  return (
    <aside className="flex h-full w-60 flex-col border-r border-slate-200 bg-white">
      <div className="border-b border-slate-200 px-4 py-3">
        <h2 className="text-sm font-semibold text-slate-900">节点面板</h2>
        <p className="mt-0.5 text-[11px] text-slate-500">
          点击卡片或拖拽到画布添加节点
        </p>
      </div>

      <div className="flex-1 space-y-2 overflow-auto p-3">
        {NODE_TYPES.map((type) => {
          const colors = NODE_COLORS[type];
          return (
            <button
              key={type}
              type="button"
              onClick={() => onAdd(type)}
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData('application/af-node-type', type);
                e.dataTransfer.effectAllowed = 'move';
              }}
              className={clsx(
                'w-full rounded-xl border-2 px-3 py-2.5 text-left transition-all hover:shadow-sm',
                colors.bg,
                colors.border,
                'hover:-translate-y-0.5 active:translate-y-0',
              )}
              aria-label={`添加 ${NODE_LABELS[type]} 节点`}
            >
              <div className={clsx('text-sm font-semibold', colors.text)}>
                {NODE_LABELS[type]}
              </div>
              <div className="mt-0.5 text-[11px] text-slate-500">
                {NODE_DESCRIPTIONS[type]}
              </div>
              <div className="mt-1 font-mono text-[10px] uppercase tracking-wider text-slate-400">
                {type}
              </div>
            </button>
          );
        })}
      </div>

      <div className="border-t border-slate-200 p-3 text-[10px] text-slate-400">
        8 个内置节点 · v1 §6
      </div>
    </aside>
  );
}
