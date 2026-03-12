import React, { useState, useCallback, useRef } from 'react';
import ReactFlow, {
  Background,
  Controls,
  addEdge,
  useNodesState,
  useEdgesState,
  type Connection,
  type Edge,
  type Node,
  type OnConnect,
  MarkerType,
  Panel,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { nodeTypes, type TaskNodeData } from './NodeTypes';
import type { TaskType, TaskDefinition } from '../../types';

interface DAGEditorProps {
  initialTasks?: TaskDefinition[];
  onSave: (tasks: TaskDefinition[]) => void;
  onValidationError?: (error: string) => void;
}

const taskTypeOptions: { type: TaskType; label: string; color: string }[] = [
  { type: 'http', label: 'HTTP Request', color: 'bg-blue-100 text-blue-700 border-blue-300' },
  { type: 'script', label: 'Script', color: 'bg-green-100 text-green-700 border-green-300' },
  { type: 'condition', label: 'Condition', color: 'bg-yellow-100 text-yellow-700 border-yellow-300' },
  { type: 'parallel', label: 'Parallel', color: 'bg-purple-100 text-purple-700 border-purple-300' },
  { type: 'delay', label: 'Delay', color: 'bg-slate-100 text-slate-600 border-slate-300' },
];

function tasksToNodesEdges(tasks: TaskDefinition[]): {
  nodes: Node<TaskNodeData>[];
  edges: Edge[];
} {
  const nodeWidth = 200;
  const nodeHeight = 80;
  const horizontalGap = 60;
  const verticalGap = 120;

  const nodeMap = new Map<string, TaskDefinition>();
  tasks.forEach((t) => nodeMap.set(t.id, t));

  const getLayer = (taskId: string, visited = new Set<string>()): number => {
    if (visited.has(taskId)) return 0;
    visited.add(taskId);
    const task = nodeMap.get(taskId);
    if (!task || task.dependsOn.length === 0) return 0;
    return Math.max(...task.dependsOn.map((dep) => getLayer(dep, visited))) + 1;
  };

  const layers: string[][] = [];
  tasks.forEach((task) => {
    const layer = getLayer(task.id);
    while (layers.length <= layer) layers.push([]);
    layers[layer].push(task.id);
  });

  const nodes: Node<TaskNodeData>[] = [];
  layers.forEach((layerNodes, layerIndex) => {
    const totalWidth = layerNodes.length * nodeWidth + (layerNodes.length - 1) * horizontalGap;
    const startX = -totalWidth / 2 + 300;

    layerNodes.forEach((nodeId, nodeIndex) => {
      const task = nodeMap.get(nodeId)!;
      nodes.push({
        id: task.id,
        type: 'taskNode',
        position: {
          x: startX + nodeIndex * (nodeWidth + horizontalGap),
          y: 50 + layerIndex * (nodeHeight + verticalGap),
        },
        data: {
          label: task.name,
          taskType: task.type,
          isEditor: true,
        },
      });
    });
  });

  const edges: Edge[] = [];
  tasks.forEach((task) => {
    task.dependsOn.forEach((dep) => {
      edges.push({
        id: `${dep}-${task.id}`,
        source: dep,
        target: task.id,
        type: 'smoothstep',
        markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
        style: { stroke: '#94a3b8', strokeWidth: 2 },
      });
    });
  });

  return { nodes, edges };
}

function hasCycle(nodes: Node[], edges: Edge[]): boolean {
  const adjacency = new Map<string, string[]>();
  nodes.forEach((n) => adjacency.set(n.id, []));
  edges.forEach((e) => {
    const list = adjacency.get(e.source);
    if (list) list.push(e.target);
  });

  const visited = new Set<string>();
  const recStack = new Set<string>();

  function dfs(nodeId: string): boolean {
    visited.add(nodeId);
    recStack.add(nodeId);
    const neighbors = adjacency.get(nodeId) || [];
    for (const neighbor of neighbors) {
      if (!visited.has(neighbor)) {
        if (dfs(neighbor)) return true;
      } else if (recStack.has(neighbor)) {
        return true;
      }
    }
    recStack.delete(nodeId);
    return false;
  }

  for (const node of nodes) {
    if (!visited.has(node.id)) {
      if (dfs(node.id)) return true;
    }
  }
  return false;
}

let nodeIdCounter = 1;

const DAGEditor: React.FC<DAGEditorProps> = ({ initialTasks = [], onSave, onValidationError }) => {
  const initial = tasksToNodesEdges(initialTasks);
  const [nodes, setNodes, onNodesChange] = useNodesState(initial.nodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initial.edges);
  const [selectedNode, setSelectedNode] = useState<Node<TaskNodeData> | null>(null);
  const [nodeConfig, setNodeConfig] = useState<Record<string, { name: string; config: Record<string, string>; timeout: number; retryMax: number }>>({});
  const reactFlowWrapper = useRef<HTMLDivElement>(null);

  // Initialize nodeConfig from initialTasks
  React.useEffect(() => {
    const config: typeof nodeConfig = {};
    initialTasks.forEach((t) => {
      config[t.id] = {
        name: t.name,
        config: Object.fromEntries(Object.entries(t.config).map(([k, v]) => [k, String(v)])),
        timeout: t.timeout,
        retryMax: t.retry.maxAttempts,
      };
    });
    setNodeConfig(config);
    nodeIdCounter = initialTasks.length + 1;
  }, [initialTasks]);

  const onConnect: OnConnect = useCallback(
    (connection: Connection) => {
      // Prevent self-connections
      if (connection.source === connection.target) return;

      // Check for cycles
      const newEdge: Edge = {
        id: `${connection.source}-${connection.target}`,
        source: connection.source!,
        target: connection.target!,
        type: 'smoothstep',
        markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
        style: { stroke: '#94a3b8', strokeWidth: 2 },
      };

      const testEdges = [...edges, newEdge];
      if (hasCycle(nodes, testEdges)) {
        onValidationError?.('Cannot create connection: would create a cycle');
        return;
      }

      setEdges((eds) => addEdge({ ...connection, type: 'smoothstep', markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 }, style: { stroke: '#94a3b8', strokeWidth: 2 } }, eds));
    },
    [edges, nodes, setEdges, onValidationError],
  );

  const onDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const onDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault();

      const taskType = event.dataTransfer.getData('application/reactflow') as TaskType;
      if (!taskType) return;

      const reactFlowBounds = reactFlowWrapper.current?.getBoundingClientRect();
      if (!reactFlowBounds) return;

      const position = {
        x: event.clientX - reactFlowBounds.left - 100,
        y: event.clientY - reactFlowBounds.top - 40,
      };

      const id = `task_${nodeIdCounter++}`;
      const label = `${taskType.charAt(0).toUpperCase() + taskType.slice(1)} Task`;

      const newNode: Node<TaskNodeData> = {
        id,
        type: 'taskNode',
        position,
        data: { label, taskType, isEditor: true },
      };

      setNodes((nds) => [...nds, newNode]);
      setNodeConfig((prev) => ({
        ...prev,
        [id]: { name: label, config: {}, timeout: 30, retryMax: 3 },
      }));
    },
    [setNodes],
  );

  const handleNodeClick = useCallback((_: React.MouseEvent, node: Node<TaskNodeData>) => {
    setSelectedNode(node);
  }, []);

  const handlePaneClick = useCallback(() => {
    setSelectedNode(null);
  }, []);

  const updateNodeConfig = useCallback(
    (field: string, value: string | number) => {
      if (!selectedNode) return;
      setNodeConfig((prev) => ({
        ...prev,
        [selectedNode.id]: {
          ...prev[selectedNode.id],
          [field]: value,
        },
      }));

      if (field === 'name') {
        setNodes((nds) =>
          nds.map((n) =>
            n.id === selectedNode.id
              ? { ...n, data: { ...n.data, label: value as string } }
              : n,
          ),
        );
      }
    },
    [selectedNode, setNodes],
  );

  const deleteSelectedNode = useCallback(() => {
    if (!selectedNode) return;
    setNodes((nds) => nds.filter((n) => n.id !== selectedNode.id));
    setEdges((eds) =>
      eds.filter((e) => e.source !== selectedNode.id && e.target !== selectedNode.id),
    );
    setNodeConfig((prev) => {
      const copy = { ...prev };
      delete copy[selectedNode.id];
      return copy;
    });
    setSelectedNode(null);
  }, [selectedNode, setNodes, setEdges]);

  const handleSave = useCallback(() => {
    if (hasCycle(nodes, edges)) {
      onValidationError?.('Cannot save: DAG contains a cycle');
      return;
    }

    const tasks: TaskDefinition[] = nodes.map((node) => {
      const config = nodeConfig[node.id] || { name: node.data.label, config: {}, timeout: 30, retryMax: 3 };
      const deps = edges.filter((e) => e.target === node.id).map((e) => e.source);

      return {
        id: node.id,
        name: config.name,
        type: node.data.taskType,
        config: config.config,
        dependsOn: deps,
        timeout: config.timeout,
        retry: {
          maxAttempts: config.retryMax,
          backoffMs: 1000,
          backoffMultiplier: 2,
        },
      };
    });

    onSave(tasks);
  }, [nodes, edges, nodeConfig, onSave, onValidationError]);

  const configFields = selectedNode
    ? getConfigFieldsForType(selectedNode.data.taskType)
    : [];

  return (
    <div className="flex h-full">
      {/* Left sidebar - node palette */}
      <div className="w-56 border-r border-slate-200 bg-white p-4">
        <h3 className="mb-3 text-sm font-semibold text-slate-900">Task Types</h3>
        <div className="space-y-2">
          {taskTypeOptions.map((option) => (
            <div
              key={option.type}
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData('application/reactflow', option.type);
                e.dataTransfer.effectAllowed = 'move';
              }}
              className={`cursor-grab rounded-lg border p-3 text-sm font-medium transition-shadow hover:shadow-md active:cursor-grabbing ${option.color}`}
            >
              {option.label}
            </div>
          ))}
        </div>
        <p className="mt-4 text-xs text-slate-400">
          Drag and drop task types onto the canvas to add them to your workflow.
        </p>
      </div>

      {/* Canvas */}
      <div ref={reactFlowWrapper} className="flex-1">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onDragOver={onDragOver}
          onDrop={onDrop}
          onNodeClick={handleNodeClick}
          onPaneClick={handlePaneClick}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.3 }}
          minZoom={0.2}
          maxZoom={2}
          snapToGrid
          snapGrid={[16, 16]}
        >
          <Background color="#e2e8f0" gap={20} size={1} />
          <Controls className="!border-slate-200 !bg-white !shadow-sm" />
          <Panel position="top-right">
            <button onClick={handleSave} className="btn-primary">
              <svg className="mr-2 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M9 3.75H6.912a2.25 2.25 0 00-2.15 1.588L2.35 13.177a2.25 2.25 0 00-.1.661V18a2.25 2.25 0 002.25 2.25h15A2.25 2.25 0 0021.75 18v-4.162c0-.224-.034-.447-.1-.661L19.24 5.338a2.25 2.25 0 00-2.15-1.588H15M2.25 13.5h3.86a2.25 2.25 0 012.012 1.244l.256.512a2.25 2.25 0 002.013 1.244h3.218a2.25 2.25 0 002.013-1.244l.256-.512a2.25 2.25 0 012.013-1.244h3.859" />
              </svg>
              Save Workflow
            </button>
          </Panel>
        </ReactFlow>
      </div>

      {/* Right panel - node configuration */}
      {selectedNode && (
        <div className="w-72 border-l border-slate-200 bg-white p-4 overflow-y-auto">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold text-slate-900">Configure Task</h3>
            <button
              onClick={deleteSelectedNode}
              className="rounded-lg p-1 text-slate-400 hover:bg-red-50 hover:text-red-600"
              title="Delete task"
            >
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
              </svg>
            </button>
          </div>

          <div className="mt-4 space-y-4">
            <div>
              <label className="block text-xs font-medium text-slate-600">Name</label>
              <input
                type="text"
                value={nodeConfig[selectedNode.id]?.name || ''}
                onChange={(e) => updateNodeConfig('name', e.target.value)}
                className="input mt-1"
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-slate-600">Type</label>
              <div className="mt-1 rounded-lg bg-slate-100 px-3 py-2 text-sm text-slate-700">
                {selectedNode.data.taskType.toUpperCase()}
              </div>
            </div>

            <div>
              <label className="block text-xs font-medium text-slate-600">Timeout (seconds)</label>
              <input
                type="number"
                value={nodeConfig[selectedNode.id]?.timeout || 30}
                onChange={(e) => updateNodeConfig('timeout', parseInt(e.target.value) || 30)}
                className="input mt-1"
                min={1}
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-slate-600">Max Retries</label>
              <input
                type="number"
                value={nodeConfig[selectedNode.id]?.retryMax || 3}
                onChange={(e) => updateNodeConfig('retryMax', parseInt(e.target.value) || 0)}
                className="input mt-1"
                min={0}
                max={10}
              />
            </div>

            {/* Type-specific config fields */}
            <div className="border-t border-slate-200 pt-4">
              <h4 className="mb-3 text-xs font-semibold uppercase tracking-wider text-slate-500">
                Configuration
              </h4>
              {configFields.map((field) => (
                <div key={field.key} className="mb-3">
                  <label className="block text-xs font-medium text-slate-600">{field.label}</label>
                  {field.type === 'textarea' ? (
                    <textarea
                      value={nodeConfig[selectedNode.id]?.config[field.key] || ''}
                      onChange={(e) => {
                        const config = { ...nodeConfig[selectedNode.id]?.config, [field.key]: e.target.value };
                        setNodeConfig((prev) => ({
                          ...prev,
                          [selectedNode.id]: { ...prev[selectedNode.id], config },
                        }));
                      }}
                      className="input mt-1 h-20 resize-none font-mono text-xs"
                      placeholder={field.placeholder}
                    />
                  ) : field.type === 'select' ? (
                    <select
                      value={nodeConfig[selectedNode.id]?.config[field.key] || ''}
                      onChange={(e) => {
                        const config = { ...nodeConfig[selectedNode.id]?.config, [field.key]: e.target.value };
                        setNodeConfig((prev) => ({
                          ...prev,
                          [selectedNode.id]: { ...prev[selectedNode.id], config },
                        }));
                      }}
                      className="input mt-1"
                    >
                      {field.options?.map((opt) => (
                        <option key={opt.value} value={opt.value}>
                          {opt.label}
                        </option>
                      ))}
                    </select>
                  ) : (
                    <input
                      type="text"
                      value={nodeConfig[selectedNode.id]?.config[field.key] || ''}
                      onChange={(e) => {
                        const config = { ...nodeConfig[selectedNode.id]?.config, [field.key]: e.target.value };
                        setNodeConfig((prev) => ({
                          ...prev,
                          [selectedNode.id]: { ...prev[selectedNode.id], config },
                        }));
                      }}
                      className="input mt-1"
                      placeholder={field.placeholder}
                    />
                  )}
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

interface ConfigField {
  key: string;
  label: string;
  type: 'text' | 'textarea' | 'select';
  placeholder?: string;
  options?: { value: string; label: string }[];
}

function getConfigFieldsForType(taskType: TaskType): ConfigField[] {
  switch (taskType) {
    case 'http':
      return [
        {
          key: 'method',
          label: 'Method',
          type: 'select',
          options: [
            { value: 'GET', label: 'GET' },
            { value: 'POST', label: 'POST' },
            { value: 'PUT', label: 'PUT' },
            { value: 'DELETE', label: 'DELETE' },
            { value: 'PATCH', label: 'PATCH' },
          ],
        },
        { key: 'url', label: 'URL', type: 'text', placeholder: 'https://api.example.com/data' },
        { key: 'headers', label: 'Headers (JSON)', type: 'textarea', placeholder: '{"Content-Type": "application/json"}' },
        { key: 'body', label: 'Request Body', type: 'textarea', placeholder: '{"key": "value"}' },
      ];
    case 'script':
      return [
        {
          key: 'language',
          label: 'Language',
          type: 'select',
          options: [
            { value: 'python', label: 'Python' },
            { value: 'bash', label: 'Bash' },
            { value: 'javascript', label: 'JavaScript' },
          ],
        },
        { key: 'code', label: 'Script Code', type: 'textarea', placeholder: 'print("Hello, world!")' },
      ];
    case 'condition':
      return [
        { key: 'expression', label: 'Expression', type: 'text', placeholder: '$.status == 200' },
        { key: 'trueBranch', label: 'True Branch Task', type: 'text', placeholder: 'task_id' },
        { key: 'falseBranch', label: 'False Branch Task', type: 'text', placeholder: 'task_id' },
      ];
    case 'parallel':
      return [
        { key: 'maxConcurrency', label: 'Max Concurrency', type: 'text', placeholder: '5' },
      ];
    case 'delay':
      return [
        { key: 'duration', label: 'Duration (seconds)', type: 'text', placeholder: '30' },
      ];
    default:
      return [];
  }
}

export default DAGEditor;
