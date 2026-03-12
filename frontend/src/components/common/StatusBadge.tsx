import React from 'react';
import type { WorkflowStatus, RunStatus, TaskStatus } from '../../types';

type Status = WorkflowStatus | RunStatus | TaskStatus;

interface StatusBadgeProps {
  status: Status;
  size?: 'sm' | 'md';
  pulse?: boolean;
}

const statusConfig: Record<string, { bg: string; text: string; dot: string }> = {
  // Run / Task statuses
  success: { bg: 'bg-green-50', text: 'text-green-700', dot: 'bg-green-500' },
  running: { bg: 'bg-blue-50', text: 'text-blue-700', dot: 'bg-blue-500' },
  pending: { bg: 'bg-amber-50', text: 'text-amber-700', dot: 'bg-amber-500' },
  failed: { bg: 'bg-red-50', text: 'text-red-700', dot: 'bg-red-500' },
  cancelled: { bg: 'bg-slate-50', text: 'text-slate-600', dot: 'bg-slate-400' },
  skipped: { bg: 'bg-slate-50', text: 'text-slate-500', dot: 'bg-slate-300' },
  // Workflow statuses
  active: { bg: 'bg-green-50', text: 'text-green-700', dot: 'bg-green-500' },
  inactive: { bg: 'bg-slate-50', text: 'text-slate-600', dot: 'bg-slate-400' },
  draft: { bg: 'bg-amber-50', text: 'text-amber-700', dot: 'bg-amber-500' },
  archived: { bg: 'bg-slate-50', text: 'text-slate-500', dot: 'bg-slate-300' },
};

const StatusBadge: React.FC<StatusBadgeProps> = ({ status, size = 'md', pulse = false }) => {
  const config = statusConfig[status] || statusConfig.pending;
  const shouldPulse = pulse || status === 'running';

  return (
    <span
      className={`badge ${config.bg} ${config.text} ${
        size === 'sm' ? 'px-2 py-0.5 text-xs' : 'px-2.5 py-1 text-xs'
      }`}
    >
      <span
        className={`mr-1.5 inline-block h-1.5 w-1.5 rounded-full ${config.dot} ${
          shouldPulse ? 'animate-pulse' : ''
        }`}
      />
      {status.charAt(0).toUpperCase() + status.slice(1)}
    </span>
  );
};

export default StatusBadge;
