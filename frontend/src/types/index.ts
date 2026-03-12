export type WorkflowStatus = 'active' | 'inactive' | 'draft' | 'archived';
export type RunStatus = 'pending' | 'running' | 'success' | 'failed' | 'cancelled';
export type TaskStatus = 'pending' | 'running' | 'success' | 'failed' | 'skipped' | 'cancelled';
export type TriggerType = 'manual' | 'schedule' | 'webhook' | 'event';
export type TaskType = 'http' | 'script' | 'condition' | 'parallel' | 'delay';
export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

export interface Workflow {
  id: string;
  name: string;
  description: string;
  version: number;
  status: WorkflowStatus;
  taskCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface WorkflowDefinition {
  tasks: TaskDefinition[];
  triggers: TriggerDefinition[];
  metadata: Record<string, string>;
}

export interface TaskDefinition {
  id: string;
  name: string;
  type: TaskType;
  config: Record<string, unknown>;
  dependsOn: string[];
  timeout: number;
  retry: RetryConfig;
}

export interface RetryConfig {
  maxAttempts: number;
  backoffMs: number;
  backoffMultiplier: number;
}

export interface TriggerDefinition {
  type: TriggerType;
  config: Record<string, unknown>;
}

export interface Run {
  id: string;
  workflowId: string;
  workflowName: string;
  status: RunStatus;
  triggerType: TriggerType;
  startedAt: string;
  completedAt?: string;
  taskStates: TaskState[];
  error?: string;
}

export interface TaskState {
  taskId: string;
  status: TaskStatus;
  startedAt?: string;
  completedAt?: string;
  attempt: number;
  output?: Record<string, unknown>;
  error?: string;
}

export interface DashboardStats {
  totalWorkflows: number;
  activeRuns: number;
  successRate: number;
  avgDuration: number;
  recentRuns: Run[];
}

export interface LogEntry {
  timestamp: string;
  level: LogLevel;
  message: string;
  taskId?: string;
  metadata?: Record<string, unknown>;
}

export interface ApiError {
  status: number;
  message: string;
  details?: string;
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  pageSize: number;
}

export interface HealthStatus {
  status: 'healthy' | 'degraded' | 'unhealthy';
  components: ComponentHealth[];
  version: string;
  uptime: number;
}

export interface ComponentHealth {
  name: string;
  status: 'healthy' | 'degraded' | 'unhealthy';
  latencyMs: number;
  message?: string;
}

export interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  createdAt: string;
  lastUsedAt?: string;
  expiresAt?: string;
}

export interface SystemMetrics {
  cpu: number;
  memory: number;
  goroutines: number;
  openConnections: number;
  requestsPerSecond: number;
}
