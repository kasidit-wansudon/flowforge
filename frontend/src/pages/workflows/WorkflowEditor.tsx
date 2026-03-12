import React, { useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import DAGEditor from '../../components/dag/DAGEditor';
import Modal from '../../components/common/Modal';
import { useWorkflow } from '../../hooks/useWorkflows';
import { api } from '../../api/client';
import type { TaskDefinition, WorkflowDefinition } from '../../types';

const WorkflowEditor: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { workflow, loading } = useWorkflow(id);
  const isNew = !id;

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [tasks, setTasks] = useState<TaskDefinition[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [showYamlModal, setShowYamlModal] = useState(false);

  // Initialize form when workflow loads
  React.useEffect(() => {
    if (workflow) {
      setName(workflow.name);
      setDescription(workflow.description);
      if (workflow.definition?.tasks) {
        setTasks(workflow.definition.tasks);
      }
    }
  }, [workflow]);

  const handleSaveTasks = useCallback((newTasks: TaskDefinition[]) => {
    setTasks(newTasks);
    setValidationError(null);
  }, []);

  const handleValidationError = useCallback((err: string) => {
    setValidationError(err);
    setTimeout(() => setValidationError(null), 4000);
  }, []);

  const handleSave = useCallback(async () => {
    if (!name.trim()) {
      setError('Workflow name is required');
      return;
    }

    if (tasks.length === 0) {
      setError('At least one task is required');
      return;
    }

    setSaving(true);
    setError(null);

    const definition: WorkflowDefinition = {
      tasks,
      triggers: [],
      metadata: {},
    };

    try {
      if (isNew) {
        const result = await api.workflow.create({
          name: name.trim(),
          description: description.trim(),
          definition,
        });
        navigate(`/workflows/${result.id}`);
      } else {
        await api.workflow.update(id!, {
          name: name.trim(),
          description: description.trim(),
          definition,
        });
        navigate(`/workflows/${id}`);
      }
    } catch (err: unknown) {
      const message = err && typeof err === 'object' && 'message' in err
        ? (err as { message: string }).message
        : 'Failed to save workflow';
      setError(message);
    } finally {
      setSaving(false);
    }
  }, [name, description, tasks, isNew, id, navigate]);

  const generateYaml = useCallback((): string => {
    const lines: string[] = [
      `name: ${name || 'untitled'}`,
      `description: ${description || ''}`,
      `version: ${workflow?.version || 1}`,
      '',
      'tasks:',
    ];

    tasks.forEach((task) => {
      lines.push(`  - id: ${task.id}`);
      lines.push(`    name: ${task.name}`);
      lines.push(`    type: ${task.type}`);
      if (task.dependsOn.length > 0) {
        lines.push(`    dependsOn:`);
        task.dependsOn.forEach((dep) => lines.push(`      - ${dep}`));
      }
      lines.push(`    timeout: ${task.timeout}`);
      lines.push(`    retry:`);
      lines.push(`      maxAttempts: ${task.retry.maxAttempts}`);
      lines.push(`      backoffMs: ${task.retry.backoffMs}`);
      lines.push(`      backoffMultiplier: ${task.retry.backoffMultiplier}`);
      if (Object.keys(task.config).length > 0) {
        lines.push(`    config:`);
        Object.entries(task.config).forEach(([key, value]) => {
          lines.push(`      ${key}: ${typeof value === 'string' ? `"${value}"` : value}`);
        });
      }
      lines.push('');
    });

    return lines.join('\n');
  }, [name, description, tasks, workflow]);

  const handleExportYaml = useCallback(() => {
    const yaml = generateYaml();
    const blob = new Blob([yaml], { type: 'text/yaml' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${name || 'workflow'}.yaml`;
    a.click();
    URL.revokeObjectURL(url);
  }, [generateYaml, name]);

  if (!isNew && loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-sm text-slate-500">Loading workflow...</div>
      </div>
    );
  }

  return (
    <div className="flex h-[calc(100vh-7rem)] flex-col">
      {/* Top bar */}
      <div className="flex items-center justify-between border-b border-slate-200 bg-white px-4 py-3">
        <div className="flex items-center gap-4">
          <button onClick={() => navigate(-1)} className="btn-ghost btn-sm">
            <svg className="mr-1 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5L3 12m0 0l7.5-7.5M3 12h18" />
            </svg>
            Back
          </button>
          <div className="h-6 w-px bg-slate-200" />
          <div className="flex items-center gap-3">
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Workflow name"
              className="border-0 bg-transparent text-lg font-semibold text-slate-900 placeholder-slate-400 focus:outline-none focus:ring-0"
              style={{ width: Math.max(200, name.length * 10) }}
            />
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Add a description..."
              className="border-0 bg-transparent text-sm text-slate-500 placeholder-slate-400 focus:outline-none focus:ring-0"
              style={{ width: Math.max(180, description.length * 7) }}
            />
          </div>
        </div>

        <div className="flex items-center gap-2">
          {validationError && (
            <span className="mr-2 text-xs text-red-600">{validationError}</span>
          )}
          {error && (
            <span className="mr-2 text-xs text-red-600">{error}</span>
          )}
          <button onClick={() => setShowYamlModal(true)} className="btn-ghost btn-sm">
            <svg className="mr-1 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M17.25 6.75L22.5 12l-5.25 5.25m-10.5 0L1.5 12l5.25-5.25m7.5-3l-4.5 16.5" />
            </svg>
            YAML
          </button>
          <button onClick={handleExportYaml} className="btn-secondary btn-sm">
            <svg className="mr-1 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5M16.5 12L12 16.5m0 0L7.5 12m4.5 4.5V3" />
            </svg>
            Export
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            className="btn-primary btn-sm"
          >
            {saving ? 'Saving...' : isNew ? 'Create Workflow' : 'Save Changes'}
          </button>
        </div>
      </div>

      {/* Editor */}
      <div className="flex-1 overflow-hidden">
        <DAGEditor
          initialTasks={tasks}
          onSave={handleSaveTasks}
          onValidationError={handleValidationError}
        />
      </div>

      {/* YAML Preview Modal */}
      <Modal
        open={showYamlModal}
        onClose={() => setShowYamlModal(false)}
        title="Workflow YAML"
        size="lg"
        footer={
          <>
            <button onClick={() => setShowYamlModal(false)} className="btn-secondary">
              Close
            </button>
            <button onClick={handleExportYaml} className="btn-primary">
              Download YAML
            </button>
          </>
        }
      >
        <pre className="max-h-96 overflow-auto rounded-lg bg-slate-900 p-4 font-mono text-sm text-slate-300">
          {generateYaml()}
        </pre>
      </Modal>
    </div>
  );
};

export default WorkflowEditor;
