// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * EdgeView — custom edge renderer. Default ReactFlow edges are
 * fine; we just add a stroke colour + animated dash for "active"
 * (currently firing) edges. v1 keeps it static.
 */
import { BaseEdge, getSmoothStepPath, type EdgeProps } from 'reactflow';

export default function EdgeView(props: EdgeProps): JSX.Element {
  const {
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    style,
    markerEnd,
  } = props;

  const [edgePath] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  return (
    <BaseEdge
      path={edgePath}
      markerEnd={markerEnd}
      style={{
        stroke: '#94a3b8',
        strokeWidth: 1.5,
        ...style,
      }}
    />
  );
}
