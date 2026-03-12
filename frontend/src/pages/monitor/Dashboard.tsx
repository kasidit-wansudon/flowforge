import React from 'react';
import { useNavigate } from 'react-router-dom';
import { formatDistanceToNow } from 'date-fns';
import { useDashboard } from '../../hooks/useDashboard';
import StatCard from '../../components/metrics/StatCard';
import {
  RunsOverTimeChart,
  StatusDistributionChart,
  TaskDurationChart,
} from '../../components/metrics/Charts';
import StatusBadge from '../../components/common/StatusBadge';

// Mock chart data generators (will be replaced by API data)
function generateRunsOverTimeData() {
  const data = [];
  const now = Date.now();
  for (let i = 23; i >= 0; i--) {
    const hour = new Date(now - i * 3600000);
    const total = Math.floor(Math.random() * 20) + 5;
    const failed = Math.floor(Math.random() * 3);
    data.push({
      time: `${hour.getHours().toString().padStart(2, '0')}:00`,
      total,
      success: total - failed,
      failed,
    });
  }
  return data;
}

function generateStatusDistribution() {
  return [
    { name: 'Success', value: 68, color: '#22c55e' },
    { name: 'Failed', value: 12, color: '#ef4444' },
    { name: 'Running', value: 8, color: '#3b82f6' },
    { name: 'Pending', value: 7, color: '#f59e0b' },
    { name: 'Cancelled', value: 5, color: '#6b7280' },
  ];
}

function generateTaskDurations() {
  return [
    { name: 'Fetch User Data', duration: 1250, color: '#3b82f6' },
    { name: 'Process Payment', duration: 890, color: '#22c55e' },
    { name: 'Send Notification', duration: 450, color: '#8b5cf6' },
    { name: 'Update Database', duration: 320, color: '#f59e0b' },
    { name: 'Generate Report', duration: 2100, color: '#ef4444' },
    { name: 'Validate Input', duration: 150, color: '#06b6d4' },
  ];
}

const Dashboard: React.FC = () => {
  const navigate = useNavigate();
  const { stats, loading } = useDashboard();

  const runsOverTimeData = React.useMemo(() => generateRunsOverTimeData(), []);
  const statusDistData = React.useMemo(() => generateStatusDistribution(), []);
  const taskDurationData = React.useMemo(() => generateTaskDurations(), []);

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div>
        <h1 className="text-2xl font-bold text-slate-900">Dashboard</h1>
        <p className="mt-1 text-sm text-slate-500">
          Overview of your workflow orchestration system
        </p>
      </div>

      {/* Stat cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label="Total Workflows"
          value={loading ? '--' : stats?.totalWorkflows ?? 0}
          color="blue"
          change={{ value: 12, type: 'increase' }}
          icon={
            <svg className="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M7.5 21L3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" />
            </svg>
          }
        />
        <StatCard
          label="Active Runs"
          value={loading ? '--' : stats?.activeRuns ?? 0}
          color="green"
          change={{ value: 3, type: 'increase' }}
          icon={
            <svg className="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M5.25 5.653c0-.856.917-1.398 1.667-.986l11.54 6.348a1.125 1.125 0 010 1.971l-11.54 6.347a1.125 1.125 0 01-1.667-.985V5.653z" />
            </svg>
          }
        />
        <StatCard
          label="Success Rate"
          value={loading ? '--' : `${(stats?.successRate ?? 0).toFixed(1)}%`}
          color="amber"
          change={{ value: 2.1, type: 'increase' }}
          icon={
            <svg className="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          }
        />
        <StatCard
          label="Avg Duration"
          value={loading ? '--' : `${((stats?.avgDuration ?? 0) / 1000).toFixed(1)}s`}
          color="purple"
          change={{ value: -5.3, type: 'decrease' }}
          icon={
            <svg className="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          }
        />
      </div>

      {/* Charts row */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <RunsOverTimeChart data={runsOverTimeData} />
        <StatusDistributionChart data={statusDistData} />
      </div>

      {/* Task duration chart */}
      <TaskDurationChart data={taskDurationData} />

      {/* Bottom row: Recent activity + Active runs */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* Recent Activity */}
        <div className="card">
          <div className="border-b border-slate-200 px-6 py-4">
            <h3 className="text-sm font-semibold text-slate-900">Recent Activity</h3>
          </div>
          <div className="divide-y divide-slate-100">
            {loading ? (
              Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="px-6 py-3">
                  <div className="skeleton h-4 w-3/4" />
                </div>
              ))
            ) : stats?.recentRuns && stats.recentRuns.length > 0 ? (
              stats.recentRuns.slice(0, 8).map((run) => (
                <button
                  key={run.id}
                  onClick={() => navigate(`/runs/${run.id}`)}
                  className="flex w-full items-center gap-3 px-6 py-3 text-left transition-colors hover:bg-slate-50"
                >
                  <StatusBadge status={run.status} size="sm" />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-slate-900">
                      {run.workflowName}
                    </p>
                    <p className="text-xs text-slate-400">
                      {run.triggerType} trigger
                    </p>
                  </div>
                  <span className="shrink-0 text-xs text-slate-400">
                    {run.startedAt
                      ? formatDistanceToNow(new Date(run.startedAt), { addSuffix: true })
                      : '--'}
                  </span>
                </button>
              ))
            ) : (
              <div className="flex items-center justify-center py-8">
                <p className="text-sm text-slate-500">No recent activity</p>
              </div>
            )}
          </div>
        </div>

        {/* Active Runs */}
        <div className="card">
          <div className="border-b border-slate-200 px-6 py-4">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold text-slate-900">Active Runs</h3>
              <button
                onClick={() => navigate('/runs?status=running')}
                className="text-xs text-forge-600 hover:text-forge-700"
              >
                View all
              </button>
            </div>
          </div>
          <div className="divide-y divide-slate-100">
            {loading ? (
              Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="px-6 py-3">
                  <div className="skeleton h-10 w-full" />
                </div>
              ))
            ) : stats?.recentRuns ? (
              (() => {
                const activeRuns = stats.recentRuns.filter(
                  (r) => r.status === 'running' || r.status === 'pending',
                );
                if (activeRuns.length === 0) {
                  return (
                    <div className="flex items-center justify-center py-8">
                      <p className="text-sm text-slate-500">No active runs</p>
                    </div>
                  );
                }
                return activeRuns.map((run) => {
                  const completed = run.taskStates?.filter((t) => t.status === 'success').length || 0;
                  const total = run.taskStates?.length || 0;
                  const pct = total > 0 ? (completed / total) * 100 : 0;
                  return (
                    <button
                      key={run.id}
                      onClick={() => navigate(`/runs/${run.id}`)}
                      className="w-full px-6 py-3 text-left transition-colors hover:bg-slate-50"
                    >
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2">
                          <StatusBadge status={run.status} size="sm" />
                          <span className="text-sm font-medium text-slate-900">{run.workflowName}</span>
                        </div>
                        <span className="text-xs text-slate-400">
                          {completed}/{total} tasks
                        </span>
                      </div>
                      <div className="mt-2 h-1.5 w-full rounded-full bg-slate-200">
                        <div
                          className="h-1.5 rounded-full bg-forge-500 transition-all"
                          style={{ width: `${pct}%` }}
                        />
                      </div>
                    </button>
                  );
                });
              })()
            ) : (
              <div className="flex items-center justify-center py-8">
                <p className="text-sm text-slate-500">No active runs</p>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default Dashboard;
