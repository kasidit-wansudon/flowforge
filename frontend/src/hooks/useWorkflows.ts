import { useState, useEffect, useCallback, useRef } from 'react';
import { api } from '../api/client';
import type { Workflow, WorkflowStatus } from '../types';

interface UseWorkflowsOptions {
  page?: number;
  pageSize?: number;
  search?: string;
  status?: WorkflowStatus | '';
}

interface UseWorkflowsReturn {
  workflows: Workflow[];
  total: number;
  loading: boolean;
  error: string | null;
  refetch: () => void;
}

export function useWorkflows(options: UseWorkflowsOptions = {}): UseWorkflowsReturn {
  const { page = 1, pageSize = 20, search = '', status = '' } = options;
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const fetchWorkflows = useCallback(async () => {
    abortRef.current?.abort();
    abortRef.current = new AbortController();

    setLoading(true);
    setError(null);

    try {
      const response = await api.workflow.list({
        page,
        pageSize,
        search,
        status,
      });
      setWorkflows(response.data);
      setTotal(response.total);
    } catch (err: unknown) {
      if (err instanceof DOMException && err.name === 'AbortError') return;
      const message = err && typeof err === 'object' && 'message' in err
        ? (err as { message: string }).message
        : 'Failed to fetch workflows';
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, search, status]);

  useEffect(() => {
    fetchWorkflows();
    return () => {
      abortRef.current?.abort();
    };
  }, [fetchWorkflows]);

  return { workflows, total, loading, error, refetch: fetchWorkflows };
}

export function useWorkflow(id: string | undefined) {
  const [workflow, setWorkflow] = useState<(Workflow & { definition: import('../types').WorkflowDefinition }) | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchWorkflow = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    setError(null);
    try {
      const data = await api.workflow.get(id);
      setWorkflow(data);
    } catch (err: unknown) {
      const message = err && typeof err === 'object' && 'message' in err
        ? (err as { message: string }).message
        : 'Failed to fetch workflow';
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    fetchWorkflow();
  }, [fetchWorkflow]);

  return { workflow, loading, error, refetch: fetchWorkflow };
}
