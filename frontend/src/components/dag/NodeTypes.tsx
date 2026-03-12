import React, { memo } from 'react';
import { Handle, Position, type NodeProps } from 'reactflow';
import type { TaskType, TaskStatus } from '../../types';

export interface TaskNodeData {
  label: string;
  taskType: TaskType;
  status?: TaskStatus;
  isEditor?: boolean;
}

const typeConfig: Record<TaskType, { color: string; bg: string; border: string; icon: React.ReactNode }> = {
  http: {
    color: 'text-blue-700',
    bg: 'bg-blue-50',
    border: 'border-blue-300',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
        <path strokeLinecap="round" strokeLinejoin="round" d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418" />
      </svg>
    ),
  },
  script: {
    color: 'text-green-700',
    bg: 'bg-green-50',
    border: 'border-green-300',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
        <path strokeLinecap="round" strokeLinejoin="round" d="M17.25 6.75L22.5 12l-5.25 5.25m-10.5 0L1.5 12l5.25-5.25m7.5-3l-4.5 16.5" />
      </svg>
    ),
  },
  condition: {
    color: 'text-yellow-700',
    bg: 'bg-yellow-50',
    border: 'border-yellow-300',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
        <path strokeLinecap="round" strokeLinejoin="round" d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9 5.25h.008v.008H12v-.008z" />
      </svg>
    ),
  },
  parallel: {
    color: 'text-purple-700',
    bg: 'bg-purple-50',
    border: 'border-purple-300',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
        <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6.75h16.5M3.75 12h16.5m-16.5 5.25h16.5" />
      </svg>
    ),
  },
  delay: {
    color: 'text-slate-600',
    bg: 'bg-slate-50',
    border: 'border-slate-300',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
        <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
    ),
  },
};

const statusBorderColors: Record<TaskStatus, string> = {
  pending: 'border-amber-400',
  running: 'border-blue-500 shadow-blue-100 shadow-lg',
  success: 'border-green-500',
  failed: 'border-red-500',
  skipped: 'border-slate-300',
  cancelled: 'border-slate-400',
};

const TaskNode: React.FC<NodeProps<TaskNodeData>> = memo(({ data, selected }) => {
  const config = typeConfig[data.taskType] || typeConfig.http;
  const statusBorder = data.status ? statusBorderColors[data.status] : '';
  const isRunning = data.status === 'running';

  return (
    <div
      className={`relative min-w-[160px] rounded-lg border-2 bg-white px-4 py-3 transition-shadow ${
        statusBorder || config.border
      } ${selected ? 'ring-2 ring-forge-500 ring-offset-1' : ''} ${
        isRunning ? 'animate-pulse-slow' : ''
      }`}
    >
      <Handle
        type="target"
        position={Position.Top}
        className="!h-2.5 !w-2.5 !border-2 !border-white !bg-slate-400"
      />

      <div className="flex items-center gap-2">
        <div className={`flex h-7 w-7 items-center justify-center rounded-md ${config.bg} ${config.color}`}>
          {config.icon}
        </div>
        <div className="flex-1 min-w-0">
          <div className="truncate text-sm font-medium text-slate-900">{data.label}</div>
          <div className={`text-xs ${config.color} font-medium`}>
            {data.taskType.toUpperCase()}
          </div>
        </div>
      </div>

      {data.status && (
        <div className="mt-2 flex items-center gap-1">
          <span
            className={`inline-block h-1.5 w-1.5 rounded-full ${
              data.status === 'success' ? 'bg-green-500' :
              data.status === 'running' ? 'bg-blue-500' :
              data.status === 'failed' ? 'bg-red-500' :
              data.status === 'pending' ? 'bg-amber-500' :
              'bg-slate-400'
            } ${isRunning ? 'animate-pulse' : ''}`}
          />
          <span className="text-xs text-slate-500">
            {data.status.charAt(0).toUpperCase() + data.status.slice(1)}
          </span>
        </div>
      )}

      <Handle
        type="source"
        position={Position.Bottom}
        className="!h-2.5 !w-2.5 !border-2 !border-white !bg-slate-400"
      />
    </div>
  );
});

TaskNode.displayName = 'TaskNode';

export const nodeTypes = {
  taskNode: TaskNode,
};

export default TaskNode;
