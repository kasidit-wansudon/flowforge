import React, { useMemo } from 'react';
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  type Node,
  type Edge,
  MarkerType,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { nodeTypes, type TaskNodeData } from './NodeTypes';
import type { TaskDefinition, TaskState } from '../../types';

interface DAGViewerProps {
  tasks: TaskDefinition[];
  taskStates?: TaskState[];
  className?: string;
}

function layoutNodes(tasks: TaskDefinition[]): Node<TaskNodeData>[] {
  // Simple layered layout algorithm
  const nodeMap = new Map<string, TaskDefinition>();
  tasks.forEach((t) => nodeMap.set(t.id, t));

  // Compute layers by topological sort
  const layers: string[][] = [];
  const assigned = new Set<string>();

  const getLayer = (taskId: string, visited = new Set<string>()): number => {
    if (visited.has(taskId)) return 0;
    visited.add(taskId);
    const task = nodeMap.get(taskId);
    if (!task || task.dependsOn.length === 0) return 0;
    return Math.max(...task.dependsOn.map((dep) => getLayer(dep, visited))) + 1;
  };

  tasks.forEach((task) => {
    const layer = getLayer(task.id);
    while (layers.length <= layer) layers.push([]);
    layers[layer].push(task.id);
    assigned.add(task.id);
  });

  const nodeWidth = 200;
  const nodeHeight = 80;
  const horizontalGap = 60;
  const verticalGap = 100;

  const nodes: Node<TaskNodeData>[] = [];

  layers.forEach((layerNodes, layerIndex) => {
    const totalWidth = layerNodes.length * nodeWidth + (layerNodes.length - 1) * horizontalGap;
    const startX = -totalWidth / 2;

    layerNodes.forEach((nodeId, nodeIndex) => {
      const task = nodeMap.get(nodeId)!;
      nodes.push({
        id: task.id,
        type: 'taskNode',
        position: {
          x: startX + nodeIndex * (nodeWidth + horizontalGap),
          y: layerIndex * (nodeHeight + verticalGap),
        },
        data: {
          label: task.name,
          taskType: task.type,
        },
      });
    });
  });

  return nodes;
}

const DAGViewer: React.FC<DAGViewerProps> = ({ tasks, taskStates, className = '' }) => {
  const { nodes, edges } = useMemo(() => {
    const baseNodes = layoutNodes(tasks);

    // Apply task states if available
    const stateMap = new Map<string, TaskState>();
    taskStates?.forEach((s) => stateMap.set(s.taskId, s));

    const nodes = baseNodes.map((node) => {
      const state = stateMap.get(node.id);
      if (state) {
        return {
          ...node,
          data: { ...node.data, status: state.status },
        };
      }
      return node;
    });

    const edges: Edge[] = [];
    tasks.forEach((task) => {
      task.dependsOn.forEach((dep) => {
        const sourceState = stateMap.get(dep);
        const isAnimated = sourceState?.status === 'running' || sourceState?.status === 'success';
        edges.push({
          id: `${dep}-${task.id}`,
          source: dep,
          target: task.id,
          type: 'smoothstep',
          animated: isAnimated,
          markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
          style: {
            stroke: sourceState?.status === 'success' ? '#22c55e' :
                    sourceState?.status === 'running' ? '#3b82f6' :
                    sourceState?.status === 'failed' ? '#ef4444' : '#94a3b8',
            strokeWidth: 2,
          },
        });
      });
    });

    return { nodes, edges };
  }, [tasks, taskStates]);

  return (
    <div className={`h-[400px] rounded-lg border border-slate-200 bg-white ${className}`}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        panOnDrag
        zoomOnScroll
        minZoom={0.3}
        maxZoom={1.5}
      >
        <Background color="#e2e8f0" gap={20} size={1} />
        <Controls
          showInteractive={false}
          className="!border-slate-200 !bg-white !shadow-sm"
        />
        <MiniMap
          nodeColor="#94a3b8"
          maskColor="rgb(241, 245, 249, 0.7)"
          className="!border-slate-200 !bg-white !shadow-sm"
        />
      </ReactFlow>
    </div>
  );
};

export default DAGViewer;
