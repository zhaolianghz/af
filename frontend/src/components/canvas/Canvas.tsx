// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Canvas — ReactFlow container. Mounts the NodeView / EdgeView
 * renderers, wires the store's change handlers, and accepts drag-
 * drops from the NodePalette. Also hosts the floating graph tools
 * (auto-layout + fit-view) and reports fitView up to the page for
 * the Shift+1 / Shift+F hotkey.
 */
import { useCallback, useEffect, useMemo, useRef } from 'react';
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  ReactFlowProvider,
  type ReactFlowInstance,
  type Node,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { useCanvasStore } from '@/stores/canvasStore';
import { NODE_TYPES, type NodeType } from '@/types/orchestrator';
import { layeredLayout } from './layout';
import NodeView from './NodeView';
import EdgeView from './EdgeView';

// Register every node type so ReactFlow passes the real type
// (e.g. 'data_source') to NodeView rather than falling back to
// 'default', which would crash NODE_COLORS lookup in NodeView.
const nodeTypes = Object.fromEntries(
  NODE_TYPES.map((t) => [t, NodeView]),
);
const edgeTypes = { default: EdgeView };

export default function Canvas({
  onFitViewReady,
}: {
  /** Called once per fitView implementation so the page can bind Shift+1/Shift+F. */
  onFitViewReady?: (fit: () => void) => void;
}): JSX.Element {
  return (
    <ReactFlowProvider>
      <CanvasInner onFitViewReady={onFitViewReady} />
    </ReactFlowProvider>
  );
}

function CanvasInner({
  onFitViewReady,
}: {
  onFitViewReady?: (fit: () => void) => void;
}): JSX.Element {
  const nodes = useCanvasStore((s) => s.nodes);
  const edges = useCanvasStore((s) => s.edges);
  const selectedNodeId = useCanvasStore((s) => s.selectedNodeId);
  const selectedEdgeId = useCanvasStore((s) => s.selectedEdgeId);
  const onNodesChange = useCanvasStore((s) => s.onNodesChange);
  const onEdgesChange = useCanvasStore((s) => s.onEdgesChange);
  const onConnect = useCanvasStore((s) => s.onConnect);
  const selectNode = useCanvasStore((s) => s.selectNode);
  const selectEdge = useCanvasStore((s) => s.selectEdge);
  const addNode = useCanvasStore((s) => s.addNode);
  const removeEdge = useCanvasStore((s) => s.removeEdge);
  const applyPositions = useCanvasStore((s) => s.applyPositions);

  const wrapperRef = useRef<HTMLDivElement | null>(null);
  const rfRef = useRef<ReactFlowInstance | null>(null);

  const fitView = useCallback(() => {
    rfRef.current?.fitView({ padding: 0.2, duration: 200 });
  }, []);

  // Expose fitView to the page (hotkey binding) once mounted.
  useEffect(() => {
    onFitViewReady?.(fitView);
  }, [fitView, onFitViewReady]);

  // Re-fit the viewport whenever the node set changes identity (e.g. a
  // strategy loads its DAG after mount, or the AI assistant applies a
  // new version). The `fitView` prop only fits ONCE on init — when the
  // store was still empty — leaving a loaded DAG parked off-screen
  // (visible only in the minimap). A short delay lets ReactFlow measure
  // node sizes before fitting.
  const nodeCount = nodes.length;
  useEffect(() => {
    if (nodeCount === 0) return;
    const id = window.setTimeout(fitView, 60);
    return () => window.clearTimeout(id);
  }, [nodeCount, fitView]);

  // Auto-layout: layered left→right by longest-path level, then fit.
  const onAutoLayout = useCallback(() => {
    const { positions } = layeredLayout(
      nodes,
      edges.map((e) => ({ source: e.source, target: e.target })),
    );
    applyPositions(positions);
    // Let ReactFlow ingest the new positions before fitting.
    window.setTimeout(fitView, 30);
  }, [nodes, edges, applyPositions, fitView]);

  // Mark currently-selected node in the ReactFlow node array. Spread
  // the whole node — ReactFlow is controlled here, so the measured
  // width/height it writes back via onNodesChange MUST be preserved on
  // the nodes we feed in. Cherry-picking only id/type/position/data
  // dropped width/height, so ReactFlow never saw the nodes as measured
  // and left them visibility:hidden (canvas looked empty).
  const rfNodes: Node[] = useMemo(
    () =>
      nodes.map((n) => ({
        ...n,
        position: n.position ?? { x: 0, y: 0 },
        selected: n.id === selectedNodeId,
      })),
    [nodes, selectedNodeId],
  );

  const rfEdges = useMemo(
    () =>
      edges.map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        sourceHandle: e.sourceHandle,
        targetHandle: e.targetHandle,
        type: 'default',
        selected: e.id === selectedEdgeId,
      })),
    [edges, selectedEdgeId],
  );

  const onDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const onDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault();
      const type = event.dataTransfer.getData('application/af-node-type') as NodeType;
      if (!type) return;
      const bounds = wrapperRef.current?.getBoundingClientRect();
      const position = rfRef.current
        ? rfRef.current.screenToFlowPosition({
            x: event.clientX - (bounds?.left ?? 0),
            y: event.clientY - (bounds?.top ?? 0),
          })
        : { x: 100, y: 100 };
      addNode(type, position);
    },
    [addNode],
  );

  return (
    <div ref={wrapperRef} className="h-full w-full">
      <ReactFlow
        nodes={rfNodes}
        edges={rfEdges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onInit={(instance) => {
          rfRef.current = instance;
        }}
        onNodeClick={(_, n) => selectNode(n.id)}
        onEdgeClick={(_, e) => selectEdge(e.id)}
        onPaneClick={() => {
          selectNode(null);
          selectEdge(null);
        }}
        onEdgeDoubleClick={(_, e) => removeEdge(e.id)}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onDragOver={onDragOver}
        onDrop={onDrop}
        fitView
        minZoom={0.1}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={20} size={1} color="#e2e8f0" />
        <Controls className="!bottom-4 !left-4" showInteractive={false} />
        <MiniMap
          className="!bottom-4 !right-4"
          nodeColor={(n) => nodeColor(n.type as NodeType)}
          maskColor="rgba(241,245,249,0.7)"
          pannable
          zoomable
        />
        {/* Floating graph tools — above the canvas, left of the minimap. */}
        <div className="absolute right-4 top-4 z-10 flex gap-1.5">
          <button
            type="button"
            onClick={onAutoLayout}
            disabled={nodes.length === 0}
            title="按数据流向自动排版"
            className="rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-[11px] font-medium text-slate-600 shadow-sm hover:bg-slate-50 disabled:opacity-40"
          >
            ⌗ 自动排版
          </button>
          <button
            type="button"
            onClick={fitView}
            disabled={nodes.length === 0}
            title="适配视图 (Shift+1)"
            className="rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-[11px] font-medium text-slate-600 shadow-sm hover:bg-slate-50 disabled:opacity-40"
          >
            ⤢ 适配视图
          </button>
        </div>
      </ReactFlow>
    </div>
  );
}

function nodeColor(t: NodeType): string {
  switch (t) {
    case 'data_source':
      return '#7dd3fc';
    case 'indicator':
      return '#a5b4fc';
    case 'filter':
      return '#fcd34d';
    case 'rank':
      return '#fda4af';
    case 'dedupe':
      return '#cbd5e1';
    case 'session_tag':
      return '#5eead4';
    case 'persist':
      return '#6ee7b7';
    case 'notify':
      return '#f9a8d4';
  }
}
