import type {
  Workflow,
  WorkflowDefinition,
  Run,
  LogEntry,
  DashboardStats,
  HealthStatus,
  SystemMetrics,
  ApiKey,
  PaginatedResponse,
  ApiError,
} from '../types';

const API_BASE = '/api/v1';

class ApiClient {
  private async request<T>(
    path: string,
    options: RequestInit = {},
  ): Promise<T> {
    const url = `${API_BASE}${path}`;
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...((options.headers as Record<string, string>) || {}),
    };

    const apiKey = localStorage.getItem('flowforge_api_key');
    if (apiKey) {
      headers['Authorization'] = `Bearer ${apiKey}`;
    }

    const response = await fetch(url, {
      ...options,
      headers,
    });

    if (!response.ok) {
      let errorBody: { message?: string; details?: string } = {};
      try {
        errorBody = await response.json();
      } catch {
        // response body is not JSON
      }
      const error: ApiError = {
        status: response.status,
        message: errorBody.message || response.statusText,
        details: errorBody.details,
      };
      throw error;
    }

    if (response.status === 204) {
      return undefined as T;
    }

    return response.json();
  }

  private buildQuery(params: Record<string, unknown>): string {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== null && value !== '') {
        query.set(key, String(value));
      }
    }
    const str = query.toString();
    return str ? `?${str}` : '';
  }

  // Workflow endpoints
  workflow = {
    list: (params?: {
      page?: number;
      pageSize?: number;
      search?: string;
      status?: string;
    }): Promise<PaginatedResponse<Workflow>> =>
      this.request(`/workflows${this.buildQuery(params || {})}`),

    get: (id: string): Promise<Workflow & { definition: WorkflowDefinition }> =>
      this.request(`/workflows/${id}`),

    create: (
      data: Partial<Workflow> & { definition: WorkflowDefinition },
    ): Promise<Workflow> =>
      this.request('/workflows', {
        method: 'POST',
        body: JSON.stringify(data),
      }),

    update: (
      id: string,
      data: Partial<Workflow> & { definition?: WorkflowDefinition },
    ): Promise<Workflow> =>
      this.request(`/workflows/${id}`, {
        method: 'PUT',
        body: JSON.stringify(data),
      }),

    delete: (id: string): Promise<void> =>
      this.request(`/workflows/${id}`, { method: 'DELETE' }),

    trigger: (
      id: string,
      params?: Record<string, unknown>,
    ): Promise<{ runId: string }> =>
      this.request(`/workflows/${id}/trigger`, {
        method: 'POST',
        body: JSON.stringify({ params }),
      }),
  };

  // Run endpoints
  run = {
    list: (params?: {
      page?: number;
      pageSize?: number;
      workflowId?: string;
      status?: string;
    }): Promise<PaginatedResponse<Run>> =>
      this.request(`/runs${this.buildQuery(params || {})}`),

    get: (id: string): Promise<Run> => this.request(`/runs/${id}`),

    cancel: (id: string): Promise<void> =>
      this.request(`/runs/${id}/cancel`, { method: 'POST' }),

    retry: (id: string): Promise<{ runId: string }> =>
      this.request(`/runs/${id}/retry`, { method: 'POST' }),

    getLogs: (
      id: string,
      params?: { taskId?: string; level?: string; limit?: number },
    ): Promise<LogEntry[]> =>
      this.request(`/runs/${id}/logs${this.buildQuery(params || {})}`),
  };

  // System endpoints
  system = {
    health: (): Promise<HealthStatus> => this.request('/system/health'),

    metrics: (): Promise<SystemMetrics> => this.request('/system/metrics'),

    version: (): Promise<{ version: string; commit: string; buildDate: string }> =>
      this.request('/system/version'),
  };

  // Dashboard endpoints
  dashboard = {
    stats: (): Promise<DashboardStats> => this.request('/dashboard/stats'),
  };

  // API Key endpoints
  apiKeys = {
    list: (): Promise<ApiKey[]> => this.request('/api-keys'),

    create: (name: string, expiresAt?: string): Promise<ApiKey & { key: string }> =>
      this.request('/api-keys', {
        method: 'POST',
        body: JSON.stringify({ name, expiresAt }),
      }),

    revoke: (id: string): Promise<void> =>
      this.request(`/api-keys/${id}`, { method: 'DELETE' }),
  };
}

export const api = new ApiClient();
export default api;
