import React from 'react';
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  Legend,
  BarChart,
  Bar,
} from 'recharts';

// Runs over time chart
interface RunsOverTimeDataPoint {
  time: string;
  success: number;
  failed: number;
  total: number;
}

interface RunsOverTimeChartProps {
  data: RunsOverTimeDataPoint[];
}

export const RunsOverTimeChart: React.FC<RunsOverTimeChartProps> = ({ data }) => (
  <div className="card card-body">
    <h3 className="mb-4 text-sm font-semibold text-slate-900">Runs Over Time (24h)</h3>
    <ResponsiveContainer width="100%" height={280}>
      <LineChart data={data} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" />
        <XAxis
          dataKey="time"
          tick={{ fill: '#64748b', fontSize: 12 }}
          axisLine={{ stroke: '#e2e8f0' }}
        />
        <YAxis
          tick={{ fill: '#64748b', fontSize: 12 }}
          axisLine={{ stroke: '#e2e8f0' }}
        />
        <Tooltip
          contentStyle={{
            backgroundColor: '#fff',
            border: '1px solid #e2e8f0',
            borderRadius: '8px',
            boxShadow: '0 4px 6px -1px rgb(0 0 0 / 0.1)',
          }}
          labelStyle={{ color: '#0f172a', fontWeight: 600, marginBottom: 4 }}
        />
        <Line
          type="monotone"
          dataKey="total"
          stroke="#3b82f6"
          strokeWidth={2}
          dot={false}
          activeDot={{ r: 4, fill: '#3b82f6' }}
          name="Total"
        />
        <Line
          type="monotone"
          dataKey="success"
          stroke="#22c55e"
          strokeWidth={2}
          dot={false}
          activeDot={{ r: 4, fill: '#22c55e' }}
          name="Success"
        />
        <Line
          type="monotone"
          dataKey="failed"
          stroke="#ef4444"
          strokeWidth={2}
          dot={false}
          activeDot={{ r: 4, fill: '#ef4444' }}
          name="Failed"
        />
      </LineChart>
    </ResponsiveContainer>
  </div>
);

// Status distribution pie chart
interface StatusDistributionDataPoint {
  name: string;
  value: number;
  color: string;
}

interface StatusDistributionChartProps {
  data: StatusDistributionDataPoint[];
}

const RADIAN = Math.PI / 180;
const renderCustomLabel = ({
  cx,
  cy,
  midAngle,
  innerRadius,
  outerRadius,
  percent,
}: {
  cx: number;
  cy: number;
  midAngle: number;
  innerRadius: number;
  outerRadius: number;
  percent: number;
}) => {
  const radius = innerRadius + (outerRadius - innerRadius) * 0.5;
  const x = cx + radius * Math.cos(-midAngle * RADIAN);
  const y = cy + radius * Math.sin(-midAngle * RADIAN);

  if (percent < 0.05) return null;

  return (
    <text x={x} y={y} fill="white" textAnchor="middle" dominantBaseline="central" fontSize={12} fontWeight={600}>
      {`${(percent * 100).toFixed(0)}%`}
    </text>
  );
};

export const StatusDistributionChart: React.FC<StatusDistributionChartProps> = ({ data }) => (
  <div className="card card-body">
    <h3 className="mb-4 text-sm font-semibold text-slate-900">Status Distribution</h3>
    <ResponsiveContainer width="100%" height={280}>
      <PieChart>
        <Pie
          data={data}
          cx="50%"
          cy="50%"
          labelLine={false}
          label={renderCustomLabel}
          outerRadius={100}
          innerRadius={50}
          fill="#8884d8"
          dataKey="value"
          strokeWidth={2}
          stroke="#fff"
        >
          {data.map((entry, index) => (
            <Cell key={`cell-${index}`} fill={entry.color} />
          ))}
        </Pie>
        <Legend
          verticalAlign="bottom"
          height={36}
          iconType="circle"
          iconSize={8}
          formatter={(value: string) => (
            <span className="text-xs text-slate-600">{value}</span>
          )}
        />
        <Tooltip
          contentStyle={{
            backgroundColor: '#fff',
            border: '1px solid #e2e8f0',
            borderRadius: '8px',
            boxShadow: '0 4px 6px -1px rgb(0 0 0 / 0.1)',
          }}
        />
      </PieChart>
    </ResponsiveContainer>
  </div>
);

// Task duration bar chart
interface TaskDurationDataPoint {
  name: string;
  duration: number;
  color?: string;
}

interface TaskDurationChartProps {
  data: TaskDurationDataPoint[];
}

export const TaskDurationChart: React.FC<TaskDurationChartProps> = ({ data }) => (
  <div className="card card-body">
    <h3 className="mb-4 text-sm font-semibold text-slate-900">Task Execution Times (ms)</h3>
    <ResponsiveContainer width="100%" height={280}>
      <BarChart data={data} margin={{ top: 5, right: 20, left: 0, bottom: 5 }} layout="vertical">
        <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" horizontal={false} />
        <XAxis
          type="number"
          tick={{ fill: '#64748b', fontSize: 12 }}
          axisLine={{ stroke: '#e2e8f0' }}
        />
        <YAxis
          type="category"
          dataKey="name"
          tick={{ fill: '#64748b', fontSize: 12 }}
          axisLine={{ stroke: '#e2e8f0' }}
          width={120}
        />
        <Tooltip
          contentStyle={{
            backgroundColor: '#fff',
            border: '1px solid #e2e8f0',
            borderRadius: '8px',
            boxShadow: '0 4px 6px -1px rgb(0 0 0 / 0.1)',
          }}
          formatter={(value: number) => [`${value}ms`, 'Duration']}
        />
        <Bar
          dataKey="duration"
          fill="#3b82f6"
          radius={[0, 4, 4, 0]}
          barSize={24}
        >
          {data.map((entry, index) => (
            <Cell key={`cell-${index}`} fill={entry.color || '#3b82f6'} />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  </div>
);
