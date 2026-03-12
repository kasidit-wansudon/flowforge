import React, { useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { format, formatDistanceToNow } from 'date-fns';
import { useRun, useRunLogs } from '../../hooks/useRuns';
import DAGViewer from '../../components/dag/DAGViewer';
import LogViewer from '../../components/log/LogViewer';
import StatusBadge from '../../components/common/StatusBadge';
import Modal from '../../components/common/Modal';
import { api } from '../../api/client';
import type { TaskState, TaskDefinition } from '../../types';

const RunDetail: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { run, loading, error } = useRun(id);
  const { logs, loading: logsLoading } = useRunLogs(id);
  const [selectedTask, setSelectedTask] = useState<TaskState | null>(null);
  const [cancelModalOpen, setCancelModalOpen] = useState(false);
  const [retrying, setRetrying] = useState(false);

  const handleCancel = useCallback(async () => {
    if (!id) return;
    try {
      await api.run.cancel(id);
      setCancelModalOpen(false);
    } catch (err) {
      console.error('Failed to cancel run:', err);
    }
  }, [id]);

  const handleRetry = useCallback(async () => {
    if (!id) return;
    setRetrying(true);
    try {
      const result = await api.run.retry(id);
      navigate(`/runs/${result.runId}`);
    } catch (err) {
      console.error('Failed to retry run:', err);
    } finally {
      setRetrying(false);
    }
  }, [id, navigate]);

  const formatDuration = (startedAt?: string, completedAt?: string): string => {
    if (!startedAt) return '--';
    const start = new Date(startedAt).getTime();
    const end = completedAt ? new Date(completedAt).getTime() : Date.now();
    const ms = end - start;
    if (ms < 1000) return `${ms}ms`;
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
    return `${Math.floor(ms / 60000)}m ${Math.floor((ms % 60000) / 1000)}s`;
  };

  // Build synthetic tasks from task states for DAG visualization
  const syntheticTasks: TaskDefinition[] = (run?.taskStates || []).map((ts) => ({
    id: ts.taskId,
    name: ts.taskId,
    type: 'script' as const,
    config: {},
    dependsOn: [],
    timeout: 30,
    retry: { maxAttempts: 3, backoffMs: 1000, backoffMultiplier: 2 },
  }));

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="skeleton h-8 w-64" />
        <div className="card card-body">
          <div className="skeleton h-[300px] w-full" />
        </div>
      </div>
    );
  }

  if (error || !run) {
    return (
      <div className="rounded-lg border border-red-200 bg-red-50 p-8 text-center">
        <p className="text-red-700">{error || 'Run not found'}</p>
        <button onClick={() => navigate('/runs')} className="btn-secondary mt-4">
          Back to Runs
        </button>
      </div>
    );
  }

  const isActive = run.status === 'running' || run.status === 'pending';
  const completedTasks = run.taskStates?.filter((t) => t.status === 'success').length || 0;
  const totalTasks = run.taskStates?.length || 0;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <div className="flex items-center gap-3">
            <button onClick={() => navigate('/runs')} className="text-slate-400 hover:text-slate-600">
              <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5L3 12m0 0l7.5-7.5M3 12h18" />
              </svg>
            </button>
            <h1 className="text-2xl font-bold text-slate-900">Run {run.id.slice(0, 12)}</h1>
            <StatusBadge status={run.status} />
          </div>
          <div className="mt-2 flex items-center gap-6 text-sm text-slate-500">
            <span>
              Workflow:{' '}
              <button
                onClick={() => navigate(`/workflows/${run.workflowId}`)}
                className="font-medium text-forge-600 hover:text-forge-700"
              >
                {run.workflowName}
              </button>
            </span>
            <span>Trigger: {run.triggerType}</span>
            <span>Duration: {formatDuration(run.startedAt, run.completedAt)}</span>
            {run.startedAt && (
              <span>
                Started {formatDistanceToNow(new Date(run.startedAt), { addSuffix: true })}
              </span>
            )}
          </div>
        </div>

        <div className="flex items-center gap-2">
          {isActive && (
            <button onClick={() => setCancelModalOpen(true)} className="btn-danger btn-sm">
              <svg className="mr-1 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M5.25 7.5A2.25 2.25 0 017.5 5.25h9a2.25 2.25 0 012.25 2.25v9a2.25 2.25 0 01-2.25 2.25h-9a2.25 2.25 0 01-2.25-2.25v-9z" />
              </svg>
              Cancel
            </button>
          )}
          {(run.status === 'failed' || run.status === 'cancelled') && (
            <button
              onClick={handleRetry}
              disabled={retrying}
              className="btn-primary btn-sm"
            >
              <svg className="mr-1 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182" />
              </svg>
              {retrying ? 'Retrying...' : 'Retry'}
            </button>
          )}
        </div>
      </div>

      {/* Progress bar */}
      {isActive && totalTasks > 0 && (
        <div className="card card-body">
          <div className="flex items-center justify-between text-sm">
            <span className="font-medium text-slate-700">Progress</span>
            <span className="text-slate-500">{completedTasks} / {totalTasks} tasks complete</span>
          </div>
          <div className="mt-2 h-2 w-full rounded-full bg-slate-200">
            <div
              className="h-2 rounded-full bg-forge-500 transition-all duration-500"
              style={{ width: `${(completedTasks / totalTasks) * 100}%` }}
            />
          </div>
        </div>
      )}

      {/* Error message */}
      {run.error && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4">
          <div className="flex items-start gap-2">
            <svg className="mt-0.5 h-5 w-5 shrink-0 text-red-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
            </svg>
            <div>
              <p className="text-sm font-medium text-red-700">Run Failed</p>
              <p className="mt-1 text-sm text-red-600">{run.error}</p>
            </div>
          </div>
        </div>
      )}

      {/* DAG Visualization */}
      {syntheticTasks.length > 0 && (
        <div>
          <h2 className="mb-3 text-lg font-semibold text-slate-900">Task Graph</h2>
          <DAGViewer tasks={syntheticTasks} taskStates={run.taskStates} />
        </div>
      )}

      {/* Task Timeline */}
      <div>
        <h2 className="mb-3 text-lg font-semibold text-slate-900">Task Execution</h2>
        <div className="card divide-y divide-slate-100">
          {run.taskStates && run.taskStates.length > 0 ? (
            run.taskStates.map((task) => (
              <div key={task.taskId}>
                <button
                  onClick={() =>
                    setSelectedTask(
                      selectedTask?.taskId === task.taskId ? null : task,
                    )
                  }
                  className="flex w-full items-center justify-between px-4 py-3 text-left transition-colors hover:bg-slate-50"
                >
                  <div className="flex items-center gap-3">
                    <StatusBadge status={task.status} size="sm" />
                    <span className="font-medium text-slate-900">{task.taskId}</span>
                    <span className="text-xs text-slate-400">
                      Attempt {task.attempt}
                    </span>
                  </div>
                  <div className="flex items-center gap-4">
                    <span className="font-mono text-xs text-slate-500">
                      {formatDuration(task.startedAt, task.completedAt)}
                    </span>
                    <svg
                      className={`h-4 w-4 text-slate-400 transition-transform ${
                        selectedTask?.taskId === task.taskId ? 'rotate-180' : ''
                      }`}
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      strokeWidth={1.5}
                    >
                      <path strokeLinecap="round" strokeLinejoin="round" d="M19.5 8.25l-7.5 7.5-7.5-7.5" />
                    </svg>
                  </div>
                </button>

                {/* Expanded task details */}
                {selectedTask?.taskId === task.taskId && (
                  <div className="border-t border-slate-100 bg-slate-50 px-4 py-4">
                    <div className="grid grid-cols-2 gap-4 text-sm">
                      <div>
                        <span className="text-xs font-medium text-slate-500">Started At</span>
                        <p className="mt-0.5 text-slate-700">
                          {task.startedAt ? format(new Date(task.startedAt), 'PPpp') : '--'}
                        </p>
                      </div>
                      <div>
                        <span className="text-xs font-medium text-slate-500">Completed At</span>
                        <p className="mt-0.5 text-slate-700">
                          {task.completedAt ? format(new Date(task.completedAt), 'PPpp') : '--'}
                        </p>
                      </div>
                      <div>
                        <span className="text-xs font-medium text-slate-500">Duration</span>
                        <p className="mt-0.5 font-mono text-slate-700">
                          {formatDuration(task.startedAt, task.completedAt)}
                        </p>
                      </div>
                      <div>
                        <span className="text-xs font-medium text-slate-500">Attempt</span>
                        <p className="mt-0.5 text-slate-700">{task.attempt}</p>
                      </div>
                    </div>

                    {task.error && (
                      <div className="mt-4 rounded-lg border border-red-200 bg-red-50 p-3">
                        <p className="text-xs font-medium text-red-700">Error</p>
                        <p className="mt-1 font-mono text-xs text-red-600">{task.error}</p>
                      </div>
                    )}

                    {task.output && Object.keys(task.output).length > 0 && (
                      <div className="mt-4">
                        <p className="text-xs font-medium text-slate-500">Output</p>
                        <pre className="mt-1 max-h-48 overflow-auto rounded-lg bg-slate-900 p-3 font-mono text-xs text-slate-300">
                          {JSON.stringify(task.output, null, 2)}
                        </pre>
                      </div>
                    )}
                  </div>
                )}
              </div>
            ))
          ) : (
            <div className="flex items-center justify-center py-8">
              <p className="text-sm text-slate-500">No task states available</p>
            </div>
          )}
        </div>
      </div>

      {/* Logs */}
      <div>
        <h2 className="mb-3 text-lg font-semibold text-slate-900">Logs</h2>
        <LogViewer logs={logs} loading={logsLoading} maxHeight="500px" />
      </div>

      {/* Cancel Modal */}
      <Modal
        open={cancelModalOpen}
        onClose={() => setCancelModalOpen(false)}
        title="Cancel Run"
        footer={
          <>
            <button onClick={() => setCancelModalOpen(false)} className="btn-secondary">
              Keep Running
            </button>
            <button onClick={handleCancel} className="btn-danger">
              Cancel Run
            </button>
          </>
        }
      >
        <p className="text-sm text-slate-600">
          Are you sure you want to cancel this run? Any currently executing tasks will be
          interrupted.
        </p>
      </Modal>
    </div>
  );
};

export default RunDetail;
