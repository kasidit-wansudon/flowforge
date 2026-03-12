import React, { useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { format, formatDistanceToNow } from 'date-fns';
import DataTable, { type Column } from '../../components/common/DataTable';
import StatusBadge from '../../components/common/StatusBadge';
import { useRuns } from '../../hooks/useRuns';
import type { Run, RunStatus } from '../../types';

const RunList: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const initialWorkflowId = searchParams.get('workflowId') || '';
  const [status, setStatus] = useState<RunStatus | ''>('');
  const [workflowFilter, setWorkflowFilter] = useState(initialWorkflowId);
  const [page, setPage] = useState(1);

  const { runs, total, loading, error } = useRuns({
    page,
    pageSize: 20,
    status,
    workflowId: workflowFilter || undefined,
    pollInterval: 5000,
  });

  const formatDuration = (run: Run): string => {
    if (!run.startedAt) return '--';
    const start = new Date(run.startedAt).getTime();
    const end = run.completedAt ? new Date(run.completedAt).getTime() : Date.now();
    const durationMs = end - start;

    if (durationMs < 1000) return `${durationMs}ms`;
    if (durationMs < 60000) return `${(durationMs / 1000).toFixed(1)}s`;
    return `${Math.floor(durationMs / 60000)}m ${Math.floor((durationMs % 60000) / 1000)}s`;
  };

  const columns: Column<Run>[] = [
    {
      key: 'id',
      header: 'Run ID',
      width: '120px',
      render: (run) => (
        <span className="font-mono text-xs text-slate-600">{run.id.slice(0, 12)}</span>
      ),
    },
    {
      key: 'workflowName',
      header: 'Workflow',
      sortable: true,
      render: (run) => (
        <span className="font-medium text-slate-900">{run.workflowName}</span>
      ),
    },
    {
      key: 'status',
      header: 'Status',
      width: '120px',
      render: (run) => <StatusBadge status={run.status} />,
    },
    {
      key: 'triggerType',
      header: 'Trigger',
      width: '100px',
      render: (run) => (
        <span className="inline-flex items-center gap-1 text-xs text-slate-500">
          {run.triggerType === 'manual' && (
            <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 6a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0zM4.501 20.118a7.5 7.5 0 0114.998 0A17.933 17.933 0 0112 21.75c-2.676 0-5.216-.584-7.499-1.632z" />
            </svg>
          )}
          {run.triggerType === 'schedule' && (
            <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          )}
          {run.triggerType === 'webhook' && (
            <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m9.86-4.071a4.5 4.5 0 00-1.242-7.244l4.5-4.5a4.5 4.5 0 016.364 6.364l-1.757 1.757" />
            </svg>
          )}
          {run.triggerType.charAt(0).toUpperCase() + run.triggerType.slice(1)}
        </span>
      ),
    },
    {
      key: 'startedAt',
      header: 'Started',
      width: '160px',
      sortable: true,
      render: (run) =>
        run.startedAt ? (
          <span className="text-slate-500" title={format(new Date(run.startedAt), 'PPpp')}>
            {formatDistanceToNow(new Date(run.startedAt), { addSuffix: true })}
          </span>
        ) : (
          <span className="text-slate-400">--</span>
        ),
    },
    {
      key: 'duration',
      header: 'Duration',
      width: '100px',
      render: (run) => (
        <span className="font-mono text-xs text-slate-600">{formatDuration(run)}</span>
      ),
    },
    {
      key: 'tasks',
      header: 'Tasks',
      width: '100px',
      render: (run) => {
        const completed = run.taskStates?.filter((t) => t.status === 'success').length || 0;
        const total = run.taskStates?.length || 0;
        const pct = total > 0 ? (completed / total) * 100 : 0;
        return (
          <div className="flex items-center gap-2">
            <div className="h-1.5 w-16 rounded-full bg-slate-200">
              <div
                className={`h-1.5 rounded-full transition-all ${
                  run.status === 'failed' ? 'bg-red-500' :
                  run.status === 'success' ? 'bg-green-500' : 'bg-blue-500'
                }`}
                style={{ width: `${pct}%` }}
              />
            </div>
            <span className="text-xs text-slate-500">{completed}/{total}</span>
          </div>
        );
      },
    },
  ];

  return (
    <div>
      {/* Header */}
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-slate-900">Runs</h1>
        <p className="mt-1 text-sm text-slate-500">
          Monitor and manage workflow execution runs
        </p>
      </div>

      {/* Filters */}
      <div className="mb-4 flex items-center gap-3">
        <select
          value={status}
          onChange={(e) => {
            setStatus(e.target.value as RunStatus | '');
            setPage(1);
          }}
          className="input w-40"
        >
          <option value="">All Statuses</option>
          <option value="pending">Pending</option>
          <option value="running">Running</option>
          <option value="success">Success</option>
          <option value="failed">Failed</option>
          <option value="cancelled">Cancelled</option>
        </select>

        <input
          type="text"
          placeholder="Filter by workflow ID..."
          value={workflowFilter}
          onChange={(e) => {
            setWorkflowFilter(e.target.value);
            setPage(1);
          }}
          className="input w-64"
        />

        <div className="flex-1" />

        <div className="flex items-center gap-1 text-xs text-slate-400">
          <span className="h-2 w-2 animate-pulse rounded-full bg-green-400" />
          Auto-refreshing
        </div>
      </div>

      {error && (
        <div className="mb-4 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          {error}
        </div>
      )}

      <DataTable
        columns={columns}
        data={runs}
        loading={loading}
        onRowClick={(run) => navigate(`/runs/${run.id}`)}
        page={page}
        pageSize={20}
        total={total}
        onPageChange={setPage}
        keyExtractor={(r) => r.id}
        emptyMessage="No runs found"
        emptyDescription="Trigger a workflow to create your first run."
      />
    </div>
  );
};

export default RunList;
