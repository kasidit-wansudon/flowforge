import { useState, useEffect, useCallback, useRef } from 'react';
import { api } from '../api/client';
import type { Run, RunStatus, LogEntry } from '../types';

interface UseRunsOptions {
  page?: number;
  pageSize?: number;
  workflowId?: string;
  status?: RunStatus | '';
  pollInterval?: number;
}

interface UseRunsReturn {
  runs: Run[];
  total: number;
  loading: boolean;
  error: string | null;
  refetch: () => void;
}

export function useRuns(options: UseRunsOptions = {}): UseRunsReturn {
  const { page = 1, pageSize = 20, workflowId, status = '', pollInterval = 0 } = options;
  const [runs, setRuns] = useState<Run[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchRuns = useCallback(async () => {
    try {
      const response = await api.run.list({
        page,
        pageSize,
        workflowId,
        status,
      });
      setRuns(response.data);
      setTotal(response.total);
      setError(null);
    } catch (err: unknown) {
      const message = err && typeof err === 'object' && 'message' in err
        ? (err as { message: string }).message
        : 'Failed to fetch runs';
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, workflowId, status]);

  useEffect(() => {
    setLoading(true);
    fetchRuns();

    if (pollInterval > 0) {
      intervalRef.current = setInterval(fetchRuns, pollInterval);
    }

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [fetchRuns, pollInterval]);

  return { runs, total, loading, error, refetch: fetchRuns };
}

export function useRun(id: string | undefined) {
  const [run, setRun] = useState<Run | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchRun = useCallback(async () => {
    if (!id) return;
    try {
      const data = await api.run.get(id);
      setRun(data);
      setError(null);

      // Stop polling when run is complete
      if (['success', 'failed', 'cancelled'].includes(data.status) && intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    } catch (err: unknown) {
      const message = err && typeof err === 'object' && 'message' in err
        ? (err as { message: string }).message
        : 'Failed to fetch run';
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    setLoading(true);
    fetchRun();
    // Poll every 2 seconds for active runs
    intervalRef.current = setInterval(fetchRun, 2000);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [fetchRun]);

  return { run, loading, error, refetch: fetchRun };
}

export function useRunLogs(runId: string | undefined, taskId?: string) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchLogs = useCallback(async () => {
    if (!runId) return;
    try {
      const data = await api.run.getLogs(runId, { taskId, limit: 500 });
      setLogs(data);
      setError(null);
    } catch (err: unknown) {
      const message = err && typeof err === 'object' && 'message' in err
        ? (err as { message: string }).message
        : 'Failed to fetch logs';
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [runId, taskId]);

  useEffect(() => {
    setLoading(true);
    fetchLogs();
    const interval = setInterval(fetchLogs, 3000);
    return () => clearInterval(interval);
  }, [fetchLogs]);

  return { logs, loading, error, refetch: fetchLogs };
}
