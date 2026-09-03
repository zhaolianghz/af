// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * EdgeView — custom edge renderer. Smooth-step path with:
 *   - hover/click hit area (wide invisible stroke so thin edges are
 *     easy to select)
 *   - an "×" delete button at the midpoint (reactflow's EdgeLabelRenderer,
 *     shown on hover or when selected) — double-click delete also kept
 *   - selected state highlighted in brand color
 */
import { BaseEdge, EdgeLabelRenderer, getSmoothStepPath, useReactFlow, type EdgeProps } from 'reactflow';
import clsx from 'clsx';

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
  const { setEdges } = useReactFlow();

  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  const remove = () => {
    setEdges((es) => es.filter((e) => e.id !== id));
  };

  return (
    <>
      {/* Wide invisible hit path so the 1.5px edge is easy to grab. */}
      <path
        d={edgePath}
        fill="none"
        stroke="transparent"
        strokeWidth={12}
        className="react-flow__edge-interaction"
      />
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
          onClick={remove}
          aria-label="删除连线"
          className={clsx(
            'nodrag nopan absolute flex h-4 w-4 items-center justify-center rounded-full border text-[9px] leading-none transition-opacity',
            'border-slate-300 bg-white text-slate-500 hover:border-rose-400 hover:bg-rose-50 hover:text-rose-600',
            selected ? 'opacity-100' : 'opacity-0 hover:opacity-100',
          )}
          style={{
            // EdgeLabelRenderer has no context for the edge position —
            // transform places the button at the path midpoint.
            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
            pointerEvents: 'all',
          }}
        >
          ✕
        </button>
      </EdgeLabelRenderer>
    </>
  );
}
