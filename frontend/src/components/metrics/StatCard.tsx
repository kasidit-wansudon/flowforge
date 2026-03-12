import React from 'react';

interface StatCardProps {
  label: string;
  value: string | number;
  icon: React.ReactNode;
  change?: {
    value: number;
    type: 'increase' | 'decrease' | 'neutral';
  };
  color?: 'blue' | 'green' | 'amber' | 'red' | 'purple';
}

const colorConfig = {
  blue: { bg: 'bg-blue-50', icon: 'text-blue-600' },
  green: { bg: 'bg-green-50', icon: 'text-green-600' },
  amber: { bg: 'bg-amber-50', icon: 'text-amber-600' },
  red: { bg: 'bg-red-50', icon: 'text-red-600' },
  purple: { bg: 'bg-purple-50', icon: 'text-purple-600' },
};

const StatCard: React.FC<StatCardProps> = ({
  label,
  value,
  icon,
  change,
  color = 'blue',
}) => {
  const colors = colorConfig[color];

  return (
    <div className="card card-body">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-sm font-medium text-slate-500">{label}</p>
          <p className="mt-1 text-2xl font-bold text-slate-900">{value}</p>
          {change && (
            <div className="mt-2 flex items-center gap-1">
              {change.type === 'increase' ? (
                <svg className="h-4 w-4 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M7 11l5-5m0 0l5 5m-5-5v12" />
                </svg>
              ) : change.type === 'decrease' ? (
                <svg className="h-4 w-4 text-red-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M17 13l-5 5m0 0l-5-5m5 5V6" />
                </svg>
              ) : null}
              <span
                className={`text-xs font-medium ${
                  change.type === 'increase' ? 'text-green-600' :
                  change.type === 'decrease' ? 'text-red-600' :
                  'text-slate-500'
                }`}
              >
                {change.value > 0 ? '+' : ''}{change.value}%
              </span>
              <span className="text-xs text-slate-400">vs last week</span>
            </div>
          )}
        </div>
        <div className={`rounded-xl p-3 ${colors.bg}`}>
          <div className={colors.icon}>{icon}</div>
        </div>
      </div>
    </div>
  );
};

export default StatCard;
