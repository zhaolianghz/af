// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * EdgeView — custom edge renderer. Smooth-step path with:
 *   - selected state highlighted in brand color
 *   - an "×" delete button at the midpoint, visible & clickable only
 *     while the edge is hovered or selected (CSS group-hover; an
 *     opacity-0-but-clickable button once swallowed edge clicks)
 *   - deletion flows through canvasStore.removeEdge so the dirty flag
 *     and selection clearing match the Delete-key path
 */
import { BaseEdge, EdgeLabelRenderer, getSmoothStepPath, type EdgeProps } from 'reactflow';
import clsx from 'clsx';
import { useCanvasStore } from '@/stores/canvasStore';

export default function EdgeView(props: EdgeProps): JSX.Element {
  const {
    id,
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    style,
    markerEnd,
    selected,
  } = props;
  // Same mutation path as the Delete hotkey: single source of truth for
  // dirty-tracking + selection. (reactflow's setEdges would dispatch an
  // edgesChange that our onEdgesChange doesn't dirty-flag.)
  const removeEdge = useCanvasStore((s) => s.removeEdge);

  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  return (
    <g className="group">
      <BaseEdge
        path={edgePath}
        markerEnd={markerEnd}
        style={{
          stroke: selected ? '#4f46e5' : '#94a3b8',
          strokeWidth: selected ? 2.5 : 1.5,
          ...style,
        }}
      />
      <EdgeLabelRenderer>
        <button
          type="button"
          onClick={() => removeEdge(id)}
          aria-label="删除连线"
          className={clsx(
            'nodrag nopan pointer-events-none absolute flex h-4 w-4 items-center justify-center rounded-full border text-[9px] leading-none',
            'border-slate-300 bg-white text-slate-500 hover:border-rose-400 hover:bg-rose-50 hover:text-rose-600',
            // Invisible & inert by default; the SVG <g.group> hover or
            // selection flips it visible + clickable in one place.
            selected
              ? 'pointer-events-auto opacity-100'
              : 'opacity-0 group-hover:pointer-events-auto group-hover:opacity-100',
          )}
          style={{
            // EdgeLabelRenderer has no context for the edge position —
            // transform places the button at the path midpoint.
            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
            pointerEvents: undefined, // governed by the classes above
          }}
        >
          ✕
        </button>
      </EdgeLabelRenderer>
    </g>
  );
}
