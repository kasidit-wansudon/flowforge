import React, { useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { format, formatDistanceToNow } from 'date-fns';
import { useWorkflow } from '../../hooks/useWorkflows';
import { useRuns } from '../../hooks/useRuns';
import DAGViewer from '../../components/dag/DAGViewer';
import StatusBadge from '../../components/common/StatusBadge';
import Modal from '../../components/common/Modal';
import { api } from '../../api/client';
import type { Run } from '../../types';

const WorkflowDetail: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { workflow, loading, error, refetch } = useWorkflow(id);
  const { runs, loading: runsLoading } = useRuns({ workflowId: id, pageSize: 10 });
  const [triggerModalOpen, setTriggerModalOpen] = useState(false);
  const [triggering, setTriggering] = useState(false);
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);

  const handleTrigger = useCallback(async () => {
    if (!id) return;
    setTriggering(true);
    try {
      const result = await api.workflow.trigger(id);
      setTriggerModalOpen(false);
      navigate(`/runs/${result.runId}`);
    } catch (err) {
      console.error('Failed to trigger workflow:', err);
    } finally {
      setTriggering(false);
    }
  }, [id, navigate]);

  const handleDelete = useCallback(async () => {
    if (!id) return;
    try {
      await api.workflow.delete(id);
      navigate('/workflows');
    } catch (err) {
      console.error('Failed to delete workflow:', err);
    }
  }, [id, navigate]);

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="skeleton h-8 w-64" />
        <div className="skeleton h-4 w-96" />
        <div className="card card-body">
          <div className="skeleton h-[400px] w-full" />
        </div>
      </div>
    );
  }

  if (error || !workflow) {
    return (
      <div className="rounded-lg border border-red-200 bg-red-50 p-8 text-center">
        <p className="text-red-700">{error || 'Workflow not found'}</p>
        <button onClick={() => navigate('/workflows')} className="btn-secondary mt-4">
          Back to Workflows
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold text-slate-900">{workflow.name}</h1>
            <StatusBadge status={workflow.status} />
            <span className="rounded-full bg-slate-100 px-2.5 py-0.5 text-xs font-medium text-slate-600">
              v{workflow.version}
            </span>
          </div>
          {workflow.description && (
            <p className="mt-2 text-sm text-slate-500">{workflow.description}</p>
          )}
          <div className="mt-2 flex items-center gap-4 text-xs text-slate-400">
            <span>Created {format(new Date(workflow.createdAt), 'MMM d, yyyy')}</span>
            <span>Updated {formatDistanceToNow(new Date(workflow.updatedAt), { addSuffix: true })}</span>
            <span>{workflow.taskCount} tasks</span>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <button
            onClick={() => setTriggerModalOpen(true)}
            className="btn-primary"
          >
            <svg className="mr-2 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M5.25 5.653c0-.856.917-1.398 1.667-.986l11.54 6.348a1.125 1.125 0 010 1.971l-11.54 6.347a1.125 1.125 0 01-1.667-.985V5.653z" />
            </svg>
            Trigger Run
          </button>
          <button
            onClick={() => navigate(`/workflows/${id}/edit`)}
            className="btn-secondary"
          >
            <svg className="mr-2 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931zm0 0L19.5 7.125M18 14v4.75A2.25 2.25 0 0115.75 21H5.25A2.25 2.25 0 013 18.75V8.25A2.25 2.25 0 015.25 6H10" />
            </svg>
            Edit
          </button>
          <button
            onClick={() => setDeleteModalOpen(true)}
            className="btn-ghost text-red-600 hover:bg-red-50"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
            </svg>
          </button>
        </div>
      </div>

      {/* DAG Viewer */}
      <div>
        <h2 className="mb-3 text-lg font-semibold text-slate-900">Workflow Graph</h2>
        {workflow.definition?.tasks && workflow.definition.tasks.length > 0 ? (
          <DAGViewer tasks={workflow.definition.tasks} />
        ) : (
          <div className="card flex items-center justify-center py-16">
            <p className="text-sm text-slate-500">No tasks defined in this workflow.</p>
          </div>
        )}
      </div>

      {/* Version History */}
      <div>
        <h2 className="mb-3 text-lg font-semibold text-slate-900">Version History</h2>
        <div className="card divide-y divide-slate-100">
          <div className="flex items-center justify-between px-4 py-3">
            <div className="flex items-center gap-3">
              <span className="rounded-full bg-forge-100 px-2 py-0.5 text-xs font-medium text-forge-700">
                v{workflow.version}
              </span>
              <span className="text-sm text-slate-700">Current version</span>
            </div>
            <span className="text-xs text-slate-400">
              {format(new Date(workflow.updatedAt), 'MMM d, yyyy HH:mm')}
            </span>
          </div>
          {workflow.version > 1 &&
            Array.from({ length: Math.min(workflow.version - 1, 4) }, (_, i) => (
              <div key={i} className="flex items-center justify-between px-4 py-3">
                <div className="flex items-center gap-3">
                  <span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-600">
                    v{workflow.version - i - 1}
                  </span>
                  <span className="text-sm text-slate-500">Previous version</span>
                </div>
                <span className="text-xs text-slate-400">--</span>
              </div>
            ))}
        </div>
      </div>

      {/* Recent Runs */}
      <div>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-slate-900">Recent Runs</h2>
          <button
            onClick={() => navigate(`/runs?workflowId=${id}`)}
            className="text-sm text-forge-600 hover:text-forge-700"
          >
            View all runs
          </button>
        </div>
        {runsLoading ? (
          <div className="card card-body">
            <div className="space-y-3">
              {Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="skeleton h-12 w-full" />
              ))}
            </div>
          </div>
        ) : runs.length === 0 ? (
          <div className="card flex items-center justify-center py-8">
            <p className="text-sm text-slate-500">No runs yet. Trigger a run to get started.</p>
          </div>
        ) : (
          <div className="card divide-y divide-slate-100">
            {runs.map((run: Run) => (
              <button
                key={run.id}
                onClick={() => navigate(`/runs/${run.id}`)}
                className="flex w-full items-center justify-between px-4 py-3 text-left transition-colors hover:bg-slate-50"
              >
                <div className="flex items-center gap-3">
                  <StatusBadge status={run.status} size="sm" />
                  <span className="font-mono text-xs text-slate-500">{run.id.slice(0, 8)}</span>
                  <span className="text-xs text-slate-400">{run.triggerType}</span>
                </div>
                <div className="flex items-center gap-4">
                  {run.completedAt && run.startedAt && (
                    <span className="text-xs text-slate-400">
                      {((new Date(run.completedAt).getTime() - new Date(run.startedAt).getTime()) / 1000).toFixed(1)}s
                    </span>
                  )}
                  <span className="text-xs text-slate-400">
                    {formatDistanceToNow(new Date(run.startedAt), { addSuffix: true })}
                  </span>
                </div>
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Trigger Modal */}
      <Modal
        open={triggerModalOpen}
        onClose={() => setTriggerModalOpen(false)}
        title="Trigger Workflow Run"
        footer={
          <>
            <button onClick={() => setTriggerModalOpen(false)} className="btn-secondary">
              Cancel
            </button>
            <button onClick={handleTrigger} disabled={triggering} className="btn-primary">
              {triggering ? 'Triggering...' : 'Trigger Run'}
            </button>
          </>
        }
      >
        <p className="text-sm text-slate-600">
          This will start a new run of <strong>{workflow.name}</strong> (v{workflow.version}).
          The run will execute all {workflow.taskCount} tasks in the defined order.
        </p>
      </Modal>

      {/* Delete Modal */}
      <Modal
        open={deleteModalOpen}
        onClose={() => setDeleteModalOpen(false)}
        title="Delete Workflow"
        footer={
          <>
            <button onClick={() => setDeleteModalOpen(false)} className="btn-secondary">
              Cancel
            </button>
            <button onClick={handleDelete} className="btn-danger">
              Delete Workflow
            </button>
          </>
        }
      >
        <p className="text-sm text-slate-600">
          Are you sure you want to delete <strong>{workflow.name}</strong>?
          This action cannot be undone. All associated run history will be preserved.
        </p>
      </Modal>
    </div>
  );
};

export default WorkflowDetail;
