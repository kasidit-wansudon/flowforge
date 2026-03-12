import React, { useState, useEffect, useRef, useMemo } from 'react';
import { format } from 'date-fns';
import type { LogEntry, LogLevel } from '../../types';

interface LogViewerProps {
  logs: LogEntry[];
  loading?: boolean;
  maxHeight?: string;
}

const levelStyles: Record<LogLevel, string> = {
  debug: 'log-debug',
  info: 'log-info',
  warn: 'log-warn',
  error: 'log-error',
};

const levelBadgeStyles: Record<LogLevel, string> = {
  debug: 'bg-slate-700 text-slate-300',
  info: 'bg-green-900/50 text-green-400',
  warn: 'bg-yellow-900/50 text-yellow-400',
  error: 'bg-red-900/50 text-red-400',
};

const LogViewer: React.FC<LogViewerProps> = ({ logs, loading = false, maxHeight = '400px' }) => {
  const [filter, setFilter] = useState<LogLevel | 'all'>('all');
  const [search, setSearch] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);
  const containerRef = useRef<HTMLDivElement>(null);
  const prevLogCountRef = useRef(0);

  const filteredLogs = useMemo(() => {
    return logs.filter((log) => {
      if (filter !== 'all' && log.level !== filter) return false;
      if (search && !log.message.toLowerCase().includes(search.toLowerCase())) return false;
      return true;
    });
  }, [logs, filter, search]);

  useEffect(() => {
    if (autoScroll && containerRef.current && logs.length > prevLogCountRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
    prevLogCountRef.current = logs.length;
  }, [logs, autoScroll]);

  const handleScroll = () => {
    if (!containerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current;
    const isAtBottom = scrollHeight - scrollTop - clientHeight < 50;
    setAutoScroll(isAtBottom);
  };

  return (
    <div className="card overflow-hidden">
      {/* Toolbar */}
      <div className="flex items-center gap-3 border-b border-slate-700 bg-slate-900 px-4 py-2">
        <div className="flex items-center gap-1">
          {(['all', 'debug', 'info', 'warn', 'error'] as const).map((level) => (
            <button
              key={level}
              onClick={() => setFilter(level)}
              className={`rounded px-2 py-1 text-xs font-medium transition-colors ${
                filter === level
                  ? 'bg-slate-700 text-white'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              {level.toUpperCase()}
            </button>
          ))}
        </div>

        <div className="flex-1" />

        <div className="relative">
          <svg
            className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-slate-500"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            strokeWidth={1.5}
          >
            <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-5.197-5.197m0 0A7.5 7.5 0 105.196 5.196a7.5 7.5 0 0010.607 10.607z" />
          </svg>
          <input
            type="text"
            placeholder="Search logs..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="rounded border border-slate-700 bg-slate-800 py-1 pl-7 pr-3 text-xs text-slate-300 placeholder-slate-500 focus:border-forge-500 focus:outline-none"
          />
        </div>

        <button
          onClick={() => {
            setAutoScroll(true);
            if (containerRef.current) {
              containerRef.current.scrollTop = containerRef.current.scrollHeight;
            }
          }}
          className={`rounded px-2 py-1 text-xs font-medium transition-colors ${
            autoScroll ? 'bg-forge-600 text-white' : 'text-slate-400 hover:text-slate-200'
          }`}
          title="Auto-scroll to bottom"
        >
          <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M19 14l-7 7m0 0l-7-7m7 7V3" />
          </svg>
        </button>

        <span className="text-xs text-slate-500">
          {filteredLogs.length} / {logs.length} entries
        </span>
      </div>

      {/* Log content */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="custom-scrollbar overflow-y-auto bg-slate-950 p-4"
        style={{ maxHeight }}
      >
        {loading && logs.length === 0 ? (
          <div className="flex items-center justify-center py-8">
            <div className="text-sm text-slate-500">Loading logs...</div>
          </div>
        ) : filteredLogs.length === 0 ? (
          <div className="flex items-center justify-center py-8">
            <div className="text-sm text-slate-500">No log entries found</div>
          </div>
        ) : (
          <div className="space-y-0.5">
            {filteredLogs.map((log, index) => (
              <div key={index} className="log-line flex items-start gap-2 py-0.5">
                <span className="shrink-0 text-slate-600">
                  {format(new Date(log.timestamp), 'HH:mm:ss.SSS')}
                </span>
                <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-bold uppercase ${levelBadgeStyles[log.level]}`}>
                  {log.level}
                </span>
                {log.taskId && (
                  <span className="shrink-0 rounded bg-slate-800 px-1.5 py-0.5 text-[10px] text-slate-400">
                    {log.taskId}
                  </span>
                )}
                <span className={levelStyles[log.level]}>{log.message}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

export default LogViewer;
