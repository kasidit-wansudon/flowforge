import React, { useState, useEffect, useCallback } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { format } from 'date-fns';
import DataTable, { type Column } from '../../components/common/DataTable';
import StatusBadge from '../../components/common/StatusBadge';
import { useWorkflows } from '../../hooks/useWorkflows';
import type { Workflow, WorkflowStatus } from '../../types';

const WorkflowList: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [search, setSearch] = useState(searchParams.get('search') || '');
  const [debouncedSearch, setDebouncedSearch] = useState(search);
  const [status, setStatus] = useState<WorkflowStatus | ''>(
    (searchParams.get('status') as WorkflowStatus) || '',
  );
  const [page, setPage] = useState(1);

  // Debounce search input
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(search);
      setPage(1);
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);

  // Sync search params
  useEffect(() => {
    const params: Record<string, string> = {};
    if (debouncedSearch) params.search = debouncedSearch;
    if (status) params.status = status;
    setSearchParams(params, { replace: true });
  }, [debouncedSearch, status, setSearchParams]);

  const { workflows, total, loading, error } = useWorkflows({
    page,
    pageSize: 20,
    search: debouncedSearch,
    status,
  });

  const handleRowClick = useCallback(
    (workflow: Workflow) => {
      navigate(`/workflows/${workflow.id}`);
    },
    [navigate],
  );

  const columns: Column<Workflow>[] = [
    {
      key: 'name',
      header: 'Name',
      sortable: true,
      render: (w) => (
        <div>
          <div className="font-medium text-slate-900">{w.name}</div>
          {w.description && (
            <div className="mt-0.5 truncate text-xs text-slate-500" style={{ maxWidth: 300 }}>
              {w.description}
            </div>
          )}
        </div>
      ),
    },
    {
      key: 'version',
      header: 'Version',
      width: '80px',
      render: (w) => <span className="font-mono text-xs text-slate-600">v{w.version}</span>,
    },
    {
      key: 'status',
      header: 'Status',
      width: '120px',
      render: (w) => <StatusBadge status={w.status} />,
    },
    {
      key: 'taskCount',
      header: 'Tasks',
      width: '80px',
      sortable: true,
      render: (w) => <span className="text-slate-600">{w.taskCount}</span>,
    },
    {
      key: 'updatedAt',
      header: 'Last Updated',
      width: '160px',
      sortable: true,
      render: (w) => (
        <span className="text-slate-500">
          {format(new Date(w.updatedAt), 'MMM d, yyyy HH:mm')}
        </span>
      ),
    },
    {
      key: 'createdAt',
      header: 'Created',
      width: '140px',
      sortable: true,
      render: (w) => (
        <span className="text-slate-500">
          {format(new Date(w.createdAt), 'MMM d, yyyy')}
        </span>
      ),
    },
  ];

  return (
    <div>
      {/* Page header */}
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-900">Workflows</h1>
          <p className="mt-1 text-sm text-slate-500">
            Manage and monitor your workflow definitions
          </p>
        </div>
        <button
          onClick={() => navigate('/workflows/new')}
          className="btn-primary"
        >
          <svg className="mr-2 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 4.5v15m7.5-7.5h-15" />
          </svg>
          New Workflow
        </button>
      </div>

      {/* Filters */}
      <div className="mb-4 flex items-center gap-3">
        <div className="relative flex-1 max-w-md">
          <svg
            className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            strokeWidth={1.5}
          >
            <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-5.197-5.197m0 0A7.5 7.5 0 105.196 5.196a7.5 7.5 0 0010.607 10.607z" />
          </svg>
          <input
            type="text"
            placeholder="Search workflows..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="input pl-10"
          />
        </div>

        <select
          value={status}
          onChange={(e) => {
            setStatus(e.target.value as WorkflowStatus | '');
            setPage(1);
          }}
          className="input w-40"
        >
          <option value="">All Statuses</option>
          <option value="active">Active</option>
          <option value="inactive">Inactive</option>
          <option value="draft">Draft</option>
          <option value="archived">Archived</option>
        </select>
      </div>

      {/* Error state */}
      {error && (
        <div className="mb-4 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          {error}
        </div>
      )}

      {/* Table */}
      <DataTable
        columns={columns}
        data={workflows}
        loading={loading}
        onRowClick={handleRowClick}
        page={page}
        pageSize={20}
        total={total}
        onPageChange={setPage}
        keyExtractor={(w) => w.id}
        emptyMessage="No workflows found"
        emptyDescription="Create your first workflow to get started."
      />
    </div>
  );
};

export default WorkflowList;
