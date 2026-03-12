import React, { useState, useEffect, useCallback } from 'react';
import { format } from 'date-fns';
import { api } from '../../api/client';
import Modal from '../../components/common/Modal';
import type { ApiKey, HealthStatus } from '../../types';

const Settings: React.FC = () => {
  const [apiKeys, setApiKeys] = useState<ApiKey[]>([]);
  const [health, setHealth] = useState<HealthStatus | null>(null);
  const [versionInfo, setVersionInfo] = useState<{ version: string; commit: string; buildDate: string } | null>(null);
  const [loadingKeys, setLoadingKeys] = useState(true);
  const [loadingHealth, setLoadingHealth] = useState(true);
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [newKeyName, setNewKeyName] = useState('');
  const [createdKey, setCreatedKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [revokeKeyId, setRevokeKeyId] = useState<string | null>(null);

  // Load data
  useEffect(() => {
    async function load() {
      try {
        const keys = await api.apiKeys.list();
        setApiKeys(keys);
      } catch {
        // API might not be available
      } finally {
        setLoadingKeys(false);
      }
    }
    load();
  }, []);

  useEffect(() => {
    async function loadHealth() {
      try {
        const [h, v] = await Promise.all([api.system.health(), api.system.version()]);
        setHealth(h);
        setVersionInfo(v);
      } catch {
        // API might not be available
      } finally {
        setLoadingHealth(false);
      }
    }
    loadHealth();
    const interval = setInterval(loadHealth, 15000);
    return () => clearInterval(interval);
  }, []);

  const handleCreateKey = useCallback(async () => {
    if (!newKeyName.trim()) return;
    try {
      const result = await api.apiKeys.create(newKeyName.trim());
      setApiKeys((prev) => [...prev, result]);
      setCreatedKey(result.key);
      setNewKeyName('');
    } catch (err) {
      console.error('Failed to create API key:', err);
    }
  }, [newKeyName]);

  const handleRevokeKey = useCallback(async () => {
    if (!revokeKeyId) return;
    try {
      await api.apiKeys.revoke(revokeKeyId);
      setApiKeys((prev) => prev.filter((k) => k.id !== revokeKeyId));
      setRevokeKeyId(null);
    } catch (err) {
      console.error('Failed to revoke API key:', err);
    }
  }, [revokeKeyId]);

  const copyToClipboard = useCallback(async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback copy
      const textarea = document.createElement('textarea');
      textarea.value = text;
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand('copy');
      document.body.removeChild(textarea);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  }, []);

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'healthy': return 'text-green-600 bg-green-50';
      case 'degraded': return 'text-yellow-600 bg-yellow-50';
      case 'unhealthy': return 'text-red-600 bg-red-50';
      default: return 'text-slate-600 bg-slate-50';
    }
  };

  const getStatusDot = (status: string) => {
    switch (status) {
      case 'healthy': return 'bg-green-500';
      case 'degraded': return 'bg-yellow-500';
      case 'unhealthy': return 'bg-red-500';
      default: return 'bg-slate-400';
    }
  };

  return (
    <div className="mx-auto max-w-4xl space-y-8">
      {/* Page header */}
      <div>
        <h1 className="text-2xl font-bold text-slate-900">Settings</h1>
        <p className="mt-1 text-sm text-slate-500">
          Manage API keys, view system health, and configure your FlowForge instance
        </p>
      </div>

      {/* API Keys */}
      <section>
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold text-slate-900">API Keys</h2>
            <p className="mt-0.5 text-sm text-slate-500">
              Manage API keys for programmatic access to FlowForge
            </p>
          </div>
          <button onClick={() => setCreateModalOpen(true)} className="btn-primary btn-sm">
            <svg className="mr-1 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 4.5v15m7.5-7.5h-15" />
            </svg>
            Create Key
          </button>
        </div>

        <div className="card">
          {loadingKeys ? (
            <div className="space-y-3 p-4">
              {Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="skeleton h-12 w-full" />
              ))}
            </div>
          ) : apiKeys.length === 0 ? (
            <div className="flex flex-col items-center py-8">
              <svg className="h-10 w-10 text-slate-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z" />
              </svg>
              <p className="mt-3 text-sm text-slate-500">No API keys created yet</p>
            </div>
          ) : (
            <div className="divide-y divide-slate-100">
              {apiKeys.map((key) => (
                <div key={key.id} className="flex items-center justify-between px-4 py-3">
                  <div className="flex items-center gap-3">
                    <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-forge-50 text-forge-600">
                      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                        <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z" />
                      </svg>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-slate-900">{key.name}</p>
                      <div className="flex items-center gap-2 text-xs text-slate-400">
                        <span className="font-mono">{key.prefix}...</span>
                        <span>Created {format(new Date(key.createdAt), 'MMM d, yyyy')}</span>
                        {key.lastUsedAt && (
                          <span>Last used {format(new Date(key.lastUsedAt), 'MMM d, yyyy')}</span>
                        )}
                      </div>
                    </div>
                  </div>
                  <button
                    onClick={() => setRevokeKeyId(key.id)}
                    className="btn-ghost btn-sm text-red-600 hover:bg-red-50"
                  >
                    Revoke
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      </section>

      {/* System Health */}
      <section>
        <h2 className="mb-4 text-lg font-semibold text-slate-900">System Health</h2>
        <div className="card">
          {loadingHealth ? (
            <div className="space-y-3 p-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <div key={i} className="skeleton h-10 w-full" />
              ))}
            </div>
          ) : health ? (
            <>
              {/* Overall status */}
              <div className="flex items-center justify-between border-b border-slate-200 px-6 py-4">
                <div className="flex items-center gap-3">
                  <span className={`h-3 w-3 rounded-full ${getStatusDot(health.status)}`} />
                  <span className="text-sm font-medium text-slate-900">System Status</span>
                </div>
                <span className={`badge ${getStatusColor(health.status)}`}>
                  {health.status.charAt(0).toUpperCase() + health.status.slice(1)}
                </span>
              </div>

              {/* Component health */}
              <div className="divide-y divide-slate-100">
                {health.components.map((component) => (
                  <div key={component.name} className="flex items-center justify-between px-6 py-3">
                    <div className="flex items-center gap-3">
                      <span className={`h-2 w-2 rounded-full ${getStatusDot(component.status)}`} />
                      <span className="text-sm text-slate-700">{component.name}</span>
                    </div>
                    <div className="flex items-center gap-4">
                      <span className="font-mono text-xs text-slate-500">
                        {component.latencyMs}ms
                      </span>
                      <span className={`badge text-xs ${getStatusColor(component.status)}`}>
                        {component.status}
                      </span>
                    </div>
                  </div>
                ))}
              </div>

              {/* Version info */}
              {versionInfo && (
                <div className="border-t border-slate-200 bg-slate-50 px-6 py-3">
                  <div className="flex items-center gap-6 text-xs text-slate-500">
                    <span>Version: <strong className="text-slate-700">{versionInfo.version}</strong></span>
                    <span>Commit: <span className="font-mono">{versionInfo.commit.slice(0, 8)}</span></span>
                    <span>Build: {versionInfo.buildDate}</span>
                    <span>Uptime: {Math.floor(health.uptime / 3600)}h {Math.floor((health.uptime % 3600) / 60)}m</span>
                  </div>
                </div>
              )}
            </>
          ) : (
            <div className="flex items-center justify-center py-8">
              <div className="text-center">
                <div className="flex justify-center">
                  <span className="h-3 w-3 animate-pulse rounded-full bg-red-500" />
                </div>
                <p className="mt-3 text-sm text-slate-500">Unable to connect to server</p>
                <p className="mt-1 text-xs text-slate-400">
                  Check that the FlowForge server is running on port 8080
                </p>
              </div>
            </div>
          )}
        </div>
      </section>

      {/* Server Configuration */}
      <section>
        <h2 className="mb-4 text-lg font-semibold text-slate-900">Server Configuration</h2>
        <div className="card divide-y divide-slate-100">
          {[
            { label: 'API URL', value: window.location.origin + '/api/v1' },
            { label: 'WebSocket URL', value: `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws` },
            { label: 'Environment', value: 'Production' },
            { label: 'Max Concurrent Runs', value: '50' },
            { label: 'Task Timeout Default', value: '300s' },
            { label: 'Log Retention', value: '30 days' },
          ].map((item) => (
            <div key={item.label} className="flex items-center justify-between px-6 py-3">
              <span className="text-sm text-slate-600">{item.label}</span>
              <span className="font-mono text-sm text-slate-900">{item.value}</span>
            </div>
          ))}
        </div>
      </section>

      {/* Create API Key Modal */}
      <Modal
        open={createModalOpen}
        onClose={() => {
          setCreateModalOpen(false);
          setCreatedKey(null);
          setNewKeyName('');
        }}
        title={createdKey ? 'API Key Created' : 'Create API Key'}
        footer={
          createdKey ? (
            <button
              onClick={() => {
                setCreateModalOpen(false);
                setCreatedKey(null);
              }}
              className="btn-primary"
            >
              Done
            </button>
          ) : (
            <>
              <button onClick={() => setCreateModalOpen(false)} className="btn-secondary">
                Cancel
              </button>
              <button onClick={handleCreateKey} disabled={!newKeyName.trim()} className="btn-primary">
                Create
              </button>
            </>
          )
        }
      >
        {createdKey ? (
          <div>
            <div className="rounded-lg border border-amber-200 bg-amber-50 p-3">
              <p className="text-xs font-medium text-amber-700">
                Copy this key now. You will not be able to see it again.
              </p>
            </div>
            <div className="mt-3 flex items-center gap-2">
              <input
                type="text"
                value={createdKey}
                readOnly
                className="input flex-1 font-mono text-sm"
              />
              <button
                onClick={() => copyToClipboard(createdKey)}
                className="btn-secondary btn-sm"
              >
                {copied ? 'Copied!' : 'Copy'}
              </button>
            </div>
          </div>
        ) : (
          <div>
            <label className="block text-sm font-medium text-slate-700">Key Name</label>
            <input
              type="text"
              value={newKeyName}
              onChange={(e) => setNewKeyName(e.target.value)}
              placeholder="e.g., CI/CD Pipeline"
              className="input mt-1"
              autoFocus
            />
            <p className="mt-2 text-xs text-slate-500">
              Give your key a descriptive name to identify its purpose.
            </p>
          </div>
        )}
      </Modal>

      {/* Revoke Key Modal */}
      <Modal
        open={!!revokeKeyId}
        onClose={() => setRevokeKeyId(null)}
        title="Revoke API Key"
        footer={
          <>
            <button onClick={() => setRevokeKeyId(null)} className="btn-secondary">
              Cancel
            </button>
            <button onClick={handleRevokeKey} className="btn-danger">
              Revoke Key
            </button>
          </>
        }
      >
        <p className="text-sm text-slate-600">
          Are you sure you want to revoke this API key? Any integrations using this key will
          immediately lose access. This action cannot be undone.
        </p>
      </Modal>
    </div>
  );
};

export default Settings;
