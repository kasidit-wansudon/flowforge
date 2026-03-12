-- FlowForge: Initial Database Schema
-- Migration: 001_initial
-- Description: Creates all core tables for the workflow orchestration engine.
--
-- This migration establishes:
--   - workflows and workflow_versions (definition storage with versioning)
--   - tasks (individual units of work within a workflow)
--   - task_dependencies (DAG edges between tasks)
--   - triggers (cron, webhook, event, manual triggers)
--   - runs (workflow execution instances)
--   - task_runs (individual task execution within a run)
--   - task_run_logs (structured log entries for task runs)
--   - workers (registered worker pool)
--   - worker_heartbeats (worker health tracking)
--   - approval_requests (human-in-the-loop approvals)
--   - webhook_endpoints (registered webhook receivers)
--   - audit_log (system-wide audit trail)
--   - secrets (encrypted secret references)

-- ============================================================================
-- Extensions
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================================
-- Enums
-- ============================================================================

CREATE TYPE workflow_status AS ENUM (
    'draft',
    'active',
    'paused',
    'archived'
);

CREATE TYPE task_type AS ENUM (
    'http',
    'script',
    'condition',
    'parallel',
    'delay',
    'approval',
    'notify',
    'sub_workflow'
);

CREATE TYPE trigger_type AS ENUM (
    'cron',
    'webhook',
    'event',
    'manual'
);

CREATE TYPE run_status AS ENUM (
    'pending',
    'queued',
    'running',
    'paused',
    'completed',
    'failed',
    'cancelled',
    'timed_out'
);

CREATE TYPE task_run_status AS ENUM (
    'pending',
    'queued',
    'running',
    'completed',
    'failed',
    'skipped',
    'cancelled',
    'waiting_approval',
    'timed_out',
    'retrying'
);

CREATE TYPE worker_status AS ENUM (
    'online',
    'busy',
    'draining',
    'offline'
);

CREATE TYPE priority_level AS ENUM (
    'low',
    'normal',
    'high',
    'critical'
);

CREATE TYPE log_level AS ENUM (
    'debug',
    'info',
    'warn',
    'error'
);

CREATE TYPE approval_status AS ENUM (
    'pending',
    'approved',
    'rejected',
    'timed_out'
);

CREATE TYPE audit_action AS ENUM (
    'workflow.created',
    'workflow.updated',
    'workflow.deleted',
    'workflow.activated',
    'workflow.paused',
    'workflow.archived',
    'run.started',
    'run.completed',
    'run.failed',
    'run.cancelled',
    'task.started',
    'task.completed',
    'task.failed',
    'task.retried',
    'approval.requested',
    'approval.approved',
    'approval.rejected',
    'worker.registered',
    'worker.deregistered',
    'secret.created',
    'secret.updated',
    'secret.deleted'
);

-- ============================================================================
-- Workflows
-- ============================================================================

CREATE TABLE workflows (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name            VARCHAR(255) NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    status          workflow_status NOT NULL DEFAULT 'draft',
    current_version INTEGER NOT NULL DEFAULT 1,
    max_concurrent  INTEGER NOT NULL DEFAULT 1 CHECK (max_concurrent > 0),
    timeout         INTERVAL,
    priority        priority_level NOT NULL DEFAULT 'normal',
    tags            TEXT[] NOT NULL DEFAULT '{}',
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_by      VARCHAR(255) NOT NULL DEFAULT 'system',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_workflows_status ON workflows (status);
CREATE INDEX idx_workflows_name ON workflows (name);
CREATE INDEX idx_workflows_tags ON workflows USING GIN (tags);
CREATE INDEX idx_workflows_metadata ON workflows USING GIN (metadata);
CREATE INDEX idx_workflows_created_at ON workflows (created_at DESC);

-- Unique constraint on name for active/draft workflows
CREATE UNIQUE INDEX idx_workflows_name_active ON workflows (name)
    WHERE status IN ('draft', 'active');

-- ============================================================================
-- Workflow Versions
-- ============================================================================

CREATE TABLE workflow_versions (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    version         INTEGER NOT NULL CHECK (version > 0),
    definition      JSONB NOT NULL,
    hash            VARCHAR(64) NOT NULL,
    changelog       TEXT NOT NULL DEFAULT '',
    created_by      VARCHAR(255) NOT NULL DEFAULT 'system',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (workflow_id, version)
);

CREATE INDEX idx_workflow_versions_workflow ON workflow_versions (workflow_id, version DESC);
CREATE INDEX idx_workflow_versions_hash ON workflow_versions (hash);

-- ============================================================================
-- Tasks (definitions within a workflow version)
-- ============================================================================

CREATE TABLE tasks (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id         UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    workflow_version    INTEGER NOT NULL,
    name                VARCHAR(255) NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    type                task_type NOT NULL,
    config              JSONB NOT NULL DEFAULT '{}',
    timeout             INTERVAL NOT NULL DEFAULT '5 minutes',
    retry_max_attempts  INTEGER NOT NULL DEFAULT 0 CHECK (retry_max_attempts >= 0),
    retry_initial_delay INTERVAL NOT NULL DEFAULT '1 second',
    retry_max_delay     INTERVAL NOT NULL DEFAULT '5 minutes',
    retry_multiplier    DOUBLE PRECISION NOT NULL DEFAULT 2.0 CHECK (retry_multiplier > 0),
    condition_expr      TEXT,
    priority            priority_level NOT NULL DEFAULT 'normal',
    metadata            JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (workflow_id, workflow_version, name)
);

CREATE INDEX idx_tasks_workflow ON tasks (workflow_id, workflow_version);
CREATE INDEX idx_tasks_type ON tasks (type);

-- ============================================================================
-- Task Dependencies (DAG edges)
-- ============================================================================

CREATE TABLE task_dependencies (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    workflow_version INTEGER NOT NULL,
    task_id         UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_id   UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    condition_expr  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Prevent duplicate edges
    UNIQUE (task_id, depends_on_id),
    -- Prevent self-referencing edges
    CHECK (task_id != depends_on_id)
);

CREATE INDEX idx_task_deps_task ON task_dependencies (task_id);
CREATE INDEX idx_task_deps_depends_on ON task_dependencies (depends_on_id);
CREATE INDEX idx_task_deps_workflow ON task_dependencies (workflow_id, workflow_version);

-- ============================================================================
-- Triggers
-- ============================================================================

CREATE TABLE triggers (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    name            VARCHAR(255) NOT NULL,
    type            trigger_type NOT NULL,
    config          JSONB NOT NULL DEFAULT '{}',
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    last_fired_at   TIMESTAMPTZ,
    fire_count      BIGINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (workflow_id, name)
);

CREATE INDEX idx_triggers_workflow ON triggers (workflow_id);
CREATE INDEX idx_triggers_type ON triggers (type);
CREATE INDEX idx_triggers_enabled ON triggers (enabled) WHERE enabled = TRUE;

-- ============================================================================
-- Runs (workflow execution instances)
-- ============================================================================

CREATE TABLE runs (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    workflow_version INTEGER NOT NULL,
    trigger_id      UUID REFERENCES triggers(id) ON DELETE SET NULL,
    status          run_status NOT NULL DEFAULT 'pending',
    priority        priority_level NOT NULL DEFAULT 'normal',
    input           JSONB NOT NULL DEFAULT '{}',
    output          JSONB,
    error           TEXT,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    timeout_at      TIMESTAMPTZ,
    duration_ms     BIGINT,
    retry_count     INTEGER NOT NULL DEFAULT 0,
    parent_run_id   UUID REFERENCES runs(id) ON DELETE SET NULL,
    parent_task_id  UUID,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_runs_workflow ON runs (workflow_id, created_at DESC);
CREATE INDEX idx_runs_status ON runs (status);
CREATE INDEX idx_runs_created ON runs (created_at DESC);
CREATE INDEX idx_runs_started ON runs (started_at DESC) WHERE started_at IS NOT NULL;
CREATE INDEX idx_runs_parent ON runs (parent_run_id) WHERE parent_run_id IS NOT NULL;
CREATE INDEX idx_runs_active ON runs (workflow_id, status)
    WHERE status IN ('pending', 'queued', 'running');
CREATE INDEX idx_runs_trigger ON runs (trigger_id) WHERE trigger_id IS NOT NULL;

-- Partial index for cleanup of old completed runs
CREATE INDEX idx_runs_retention ON runs (completed_at)
    WHERE status IN ('completed', 'failed', 'cancelled', 'timed_out')
    AND completed_at IS NOT NULL;

-- ============================================================================
-- Task Runs (individual task execution within a run)
-- ============================================================================

CREATE TABLE task_runs (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    run_id          UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    task_id         UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    worker_id       UUID,
    status          task_run_status NOT NULL DEFAULT 'pending',
    attempt         INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
    input           JSONB NOT NULL DEFAULT '{}',
    output          JSONB,
    error           TEXT,
    error_code      VARCHAR(100),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    duration_ms     BIGINT,
    scheduled_at    TIMESTAMPTZ,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_task_runs_run ON task_runs (run_id);
CREATE INDEX idx_task_runs_task ON task_runs (task_id);
CREATE INDEX idx_task_runs_worker ON task_runs (worker_id) WHERE worker_id IS NOT NULL;
CREATE INDEX idx_task_runs_status ON task_runs (status);
CREATE INDEX idx_task_runs_run_status ON task_runs (run_id, status);
CREATE INDEX idx_task_runs_active ON task_runs (worker_id, status)
    WHERE status IN ('running', 'queued');

-- Unique constraint: one active execution per task per run (allow retries as separate rows)
CREATE UNIQUE INDEX idx_task_runs_unique_attempt ON task_runs (run_id, task_id, attempt);

-- ============================================================================
-- Task Run Logs
-- ============================================================================

CREATE TABLE task_run_logs (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_run_id     UUID NOT NULL REFERENCES task_runs(id) ON DELETE CASCADE,
    run_id          UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    level           log_level NOT NULL DEFAULT 'info',
    message         TEXT NOT NULL,
    fields          JSONB NOT NULL DEFAULT '{}',
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Ordered by timestamp for streaming
CREATE INDEX idx_task_run_logs_task_run ON task_run_logs (task_run_id, timestamp);
CREATE INDEX idx_task_run_logs_run ON task_run_logs (run_id, timestamp);
CREATE INDEX idx_task_run_logs_level ON task_run_logs (level) WHERE level IN ('warn', 'error');

-- Retention index for log cleanup
CREATE INDEX idx_task_run_logs_retention ON task_run_logs (timestamp);

-- ============================================================================
-- Workers
-- ============================================================================

CREATE TABLE workers (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name            VARCHAR(255) NOT NULL,
    hostname        VARCHAR(255) NOT NULL,
    ip_address      INET,
    status          worker_status NOT NULL DEFAULT 'online',
    version         VARCHAR(50) NOT NULL DEFAULT '',
    capabilities    TEXT[] NOT NULL DEFAULT '{}',
    max_concurrent  INTEGER NOT NULL DEFAULT 10 CHECK (max_concurrent > 0),
    current_tasks   INTEGER NOT NULL DEFAULT 0 CHECK (current_tasks >= 0),
    labels          JSONB NOT NULL DEFAULT '{}',
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_heartbeat  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deregistered_at TIMESTAMPTZ,

    UNIQUE (name)
);

CREATE INDEX idx_workers_status ON workers (status);
CREATE INDEX idx_workers_heartbeat ON workers (last_heartbeat DESC);
CREATE INDEX idx_workers_available ON workers (status, current_tasks, max_concurrent)
    WHERE status IN ('online', 'busy');
CREATE INDEX idx_workers_capabilities ON workers USING GIN (capabilities);

-- ============================================================================
-- Worker Heartbeats (time-series health data)
-- ============================================================================

CREATE TABLE worker_heartbeats (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    worker_id       UUID NOT NULL REFERENCES workers(id) ON DELETE CASCADE,
    cpu_percent     DOUBLE PRECISION CHECK (cpu_percent >= 0 AND cpu_percent <= 100),
    memory_percent  DOUBLE PRECISION CHECK (memory_percent >= 0 AND memory_percent <= 100),
    disk_percent    DOUBLE PRECISION CHECK (disk_percent >= 0 AND disk_percent <= 100),
    active_tasks    INTEGER NOT NULL DEFAULT 0,
    goroutines      INTEGER,
    metadata        JSONB NOT NULL DEFAULT '{}',
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_worker_heartbeats_worker ON worker_heartbeats (worker_id, timestamp DESC);

-- Retention: keep only recent heartbeats
CREATE INDEX idx_worker_heartbeats_retention ON worker_heartbeats (timestamp);

-- ============================================================================
-- Approval Requests (human-in-the-loop)
-- ============================================================================

CREATE TABLE approval_requests (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_run_id     UUID NOT NULL REFERENCES task_runs(id) ON DELETE CASCADE,
    run_id          UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    status          approval_status NOT NULL DEFAULT 'pending',
    title           VARCHAR(500) NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    approvers       TEXT[] NOT NULL DEFAULT '{}',
    approved_by     VARCHAR(255),
    rejected_by     VARCHAR(255),
    comment         TEXT,
    timeout_at      TIMESTAMPTZ,
    decided_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_approval_requests_status ON approval_requests (status) WHERE status = 'pending';
CREATE INDEX idx_approval_requests_run ON approval_requests (run_id);
CREATE INDEX idx_approval_requests_workflow ON approval_requests (workflow_id);
CREATE INDEX idx_approval_requests_timeout ON approval_requests (timeout_at)
    WHERE status = 'pending' AND timeout_at IS NOT NULL;

-- ============================================================================
-- Webhook Endpoints
-- ============================================================================

CREATE TABLE webhook_endpoints (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    trigger_id      UUID NOT NULL REFERENCES triggers(id) ON DELETE CASCADE,
    path            VARCHAR(500) NOT NULL,
    secret          VARCHAR(255),
    allowed_ips     INET[] NOT NULL DEFAULT '{}',
    rate_limit      INTEGER NOT NULL DEFAULT 60 CHECK (rate_limit > 0),
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    last_called_at  TIMESTAMPTZ,
    call_count      BIGINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (path)
);

CREATE INDEX idx_webhook_endpoints_workflow ON webhook_endpoints (workflow_id);
CREATE INDEX idx_webhook_endpoints_path ON webhook_endpoints (path) WHERE enabled = TRUE;

-- ============================================================================
-- Audit Log
-- ============================================================================

CREATE TABLE audit_log (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    action          audit_action NOT NULL,
    actor           VARCHAR(255) NOT NULL DEFAULT 'system',
    resource_type   VARCHAR(100) NOT NULL,
    resource_id     UUID NOT NULL,
    resource_name   VARCHAR(255),
    details         JSONB NOT NULL DEFAULT '{}',
    ip_address      INET,
    user_agent      TEXT,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_log_action ON audit_log (action);
CREATE INDEX idx_audit_log_resource ON audit_log (resource_type, resource_id);
CREATE INDEX idx_audit_log_actor ON audit_log (actor);
CREATE INDEX idx_audit_log_timestamp ON audit_log (timestamp DESC);

-- Composite index for common queries
CREATE INDEX idx_audit_log_resource_time ON audit_log (resource_type, resource_id, timestamp DESC);

-- Retention index
CREATE INDEX idx_audit_log_retention ON audit_log (timestamp);

-- ============================================================================
-- Secrets (encrypted references, not plaintext)
-- ============================================================================

CREATE TABLE secrets (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name            VARCHAR(255) NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    encrypted_value BYTEA NOT NULL,
    scope           VARCHAR(100) NOT NULL DEFAULT 'global',
    scope_id        UUID,
    version         INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by      VARCHAR(255) NOT NULL DEFAULT 'system',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ,

    UNIQUE (name, scope, scope_id)
);

CREATE INDEX idx_secrets_name ON secrets (name);
CREATE INDEX idx_secrets_scope ON secrets (scope, scope_id);
CREATE INDEX idx_secrets_expires ON secrets (expires_at) WHERE expires_at IS NOT NULL;

-- ============================================================================
-- Scheduled Jobs (internal scheduler tracking)
-- ============================================================================

CREATE TABLE scheduled_jobs (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    trigger_id      UUID NOT NULL REFERENCES triggers(id) ON DELETE CASCADE,
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    cron_expr       VARCHAR(255) NOT NULL,
    timezone        VARCHAR(100) NOT NULL DEFAULT 'UTC',
    next_run_at     TIMESTAMPTZ NOT NULL,
    last_run_at     TIMESTAMPTZ,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    locked_by       VARCHAR(255),
    locked_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_scheduled_jobs_next_run ON scheduled_jobs (next_run_at)
    WHERE enabled = TRUE;
CREATE INDEX idx_scheduled_jobs_locked ON scheduled_jobs (locked_by, locked_at)
    WHERE locked_by IS NOT NULL;
CREATE INDEX idx_scheduled_jobs_workflow ON scheduled_jobs (workflow_id);

-- ============================================================================
-- Functions
-- ============================================================================

-- Auto-update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Calculate run duration on completion
CREATE OR REPLACE FUNCTION calculate_run_duration()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.completed_at IS NOT NULL AND NEW.started_at IS NOT NULL THEN
        NEW.duration_ms = EXTRACT(EPOCH FROM (NEW.completed_at - NEW.started_at)) * 1000;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Calculate task run duration on completion
CREATE OR REPLACE FUNCTION calculate_task_run_duration()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.completed_at IS NOT NULL AND NEW.started_at IS NOT NULL THEN
        NEW.duration_ms = EXTRACT(EPOCH FROM (NEW.completed_at - NEW.started_at)) * 1000;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Increment trigger fire count
CREATE OR REPLACE FUNCTION increment_trigger_fire_count()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.trigger_id IS NOT NULL THEN
        UPDATE triggers
        SET fire_count = fire_count + 1,
            last_fired_at = NOW(),
            updated_at = NOW()
        WHERE id = NEW.trigger_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Update worker current_tasks count
CREATE OR REPLACE FUNCTION update_worker_task_count()
RETURNS TRIGGER AS $$
BEGIN
    -- On insert/update to running, increment
    IF TG_OP = 'INSERT' AND NEW.status = 'running' AND NEW.worker_id IS NOT NULL THEN
        UPDATE workers SET current_tasks = current_tasks + 1 WHERE id = NEW.worker_id;
    ELSIF TG_OP = 'UPDATE' THEN
        -- Task started running
        IF OLD.status != 'running' AND NEW.status = 'running' AND NEW.worker_id IS NOT NULL THEN
            UPDATE workers SET current_tasks = current_tasks + 1 WHERE id = NEW.worker_id;
        -- Task stopped running
        ELSIF OLD.status = 'running' AND NEW.status != 'running' AND OLD.worker_id IS NOT NULL THEN
            UPDATE workers SET current_tasks = GREATEST(current_tasks - 1, 0) WHERE id = OLD.worker_id;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Increment webhook call count
CREATE OR REPLACE FUNCTION increment_webhook_call_count()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.trigger_id IS NOT NULL THEN
        UPDATE webhook_endpoints
        SET call_count = call_count + 1,
            last_called_at = NOW(),
            updated_at = NOW()
        WHERE trigger_id = NEW.trigger_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- Triggers (database triggers, not workflow triggers)
-- ============================================================================

-- Auto-update updated_at
CREATE TRIGGER trg_workflows_updated_at
    BEFORE UPDATE ON workflows
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_tasks_updated_at
    BEFORE UPDATE ON tasks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_triggers_updated_at
    BEFORE UPDATE ON triggers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_runs_updated_at
    BEFORE UPDATE ON runs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_task_runs_updated_at
    BEFORE UPDATE ON task_runs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_approval_requests_updated_at
    BEFORE UPDATE ON approval_requests
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_webhook_endpoints_updated_at
    BEFORE UPDATE ON webhook_endpoints
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_secrets_updated_at
    BEFORE UPDATE ON secrets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_scheduled_jobs_updated_at
    BEFORE UPDATE ON scheduled_jobs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Duration calculation
CREATE TRIGGER trg_runs_duration
    BEFORE INSERT OR UPDATE ON runs
    FOR EACH ROW EXECUTE FUNCTION calculate_run_duration();

CREATE TRIGGER trg_task_runs_duration
    BEFORE INSERT OR UPDATE ON task_runs
    FOR EACH ROW EXECUTE FUNCTION calculate_task_run_duration();

-- Trigger fire count
CREATE TRIGGER trg_runs_trigger_fire_count
    AFTER INSERT ON runs
    FOR EACH ROW EXECUTE FUNCTION increment_trigger_fire_count();

-- Worker task count tracking
CREATE TRIGGER trg_task_runs_worker_count
    AFTER INSERT OR UPDATE ON task_runs
    FOR EACH ROW EXECUTE FUNCTION update_worker_task_count();

-- ============================================================================
-- Views
-- ============================================================================

-- Active runs with workflow info
CREATE VIEW v_active_runs AS
SELECT
    r.id AS run_id,
    r.status AS run_status,
    r.priority,
    r.started_at,
    r.timeout_at,
    r.created_at,
    w.id AS workflow_id,
    w.name AS workflow_name,
    w.status AS workflow_status,
    r.workflow_version,
    (SELECT COUNT(*) FROM task_runs tr WHERE tr.run_id = r.id) AS total_tasks,
    (SELECT COUNT(*) FROM task_runs tr WHERE tr.run_id = r.id AND tr.status = 'completed') AS completed_tasks,
    (SELECT COUNT(*) FROM task_runs tr WHERE tr.run_id = r.id AND tr.status = 'running') AS running_tasks,
    (SELECT COUNT(*) FROM task_runs tr WHERE tr.run_id = r.id AND tr.status = 'failed') AS failed_tasks
FROM runs r
JOIN workflows w ON w.id = r.workflow_id
WHERE r.status IN ('pending', 'queued', 'running', 'paused');

-- Worker overview
CREATE VIEW v_worker_overview AS
SELECT
    w.id,
    w.name,
    w.hostname,
    w.status,
    w.version,
    w.max_concurrent,
    w.current_tasks,
    w.capabilities,
    w.last_heartbeat,
    EXTRACT(EPOCH FROM (NOW() - w.last_heartbeat)) AS seconds_since_heartbeat,
    (SELECT COUNT(*) FROM task_runs tr WHERE tr.worker_id = w.id AND tr.status = 'running') AS actual_running_tasks
FROM workers w
WHERE w.deregistered_at IS NULL;

-- Run statistics (for dashboard)
CREATE VIEW v_run_statistics AS
SELECT
    w.id AS workflow_id,
    w.name AS workflow_name,
    COUNT(r.id) AS total_runs,
    COUNT(r.id) FILTER (WHERE r.status = 'completed') AS completed_runs,
    COUNT(r.id) FILTER (WHERE r.status = 'failed') AS failed_runs,
    COUNT(r.id) FILTER (WHERE r.status IN ('running', 'pending', 'queued')) AS active_runs,
    AVG(r.duration_ms) FILTER (WHERE r.duration_ms IS NOT NULL) AS avg_duration_ms,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY r.duration_ms)
        FILTER (WHERE r.duration_ms IS NOT NULL) AS p95_duration_ms,
    MAX(r.completed_at) AS last_completed_at,
    MIN(r.created_at) AS first_run_at
FROM workflows w
LEFT JOIN runs r ON r.workflow_id = w.id
GROUP BY w.id, w.name;

-- Pending approvals
CREATE VIEW v_pending_approvals AS
SELECT
    ar.id AS approval_id,
    ar.title,
    ar.description,
    ar.approvers,
    ar.timeout_at,
    ar.created_at,
    w.name AS workflow_name,
    r.id AS run_id,
    t.name AS task_name,
    EXTRACT(EPOCH FROM (COALESCE(ar.timeout_at, NOW() + INTERVAL '999 years') - NOW())) AS seconds_until_timeout
FROM approval_requests ar
JOIN runs r ON r.id = ar.run_id
JOIN workflows w ON w.id = ar.workflow_id
JOIN task_runs tr ON tr.id = ar.task_run_id
JOIN tasks t ON t.id = tr.task_id
WHERE ar.status = 'pending'
ORDER BY ar.created_at;

-- ============================================================================
-- Row-Level Security (prepared for multi-tenancy)
-- ============================================================================

-- Note: Enable RLS when multi-tenancy is needed:
-- ALTER TABLE workflows ENABLE ROW LEVEL SECURITY;
-- CREATE POLICY workflow_tenant_isolation ON workflows
--     USING (metadata->>'tenant_id' = current_setting('app.tenant_id'));

-- ============================================================================
-- Comments
-- ============================================================================

COMMENT ON TABLE workflows IS 'Workflow definitions with versioning and lifecycle management';
COMMENT ON TABLE workflow_versions IS 'Immutable snapshots of workflow definitions';
COMMENT ON TABLE tasks IS 'Task definitions within a workflow version (DAG nodes)';
COMMENT ON TABLE task_dependencies IS 'Dependencies between tasks (DAG edges)';
COMMENT ON TABLE triggers IS 'Trigger configurations for starting workflows';
COMMENT ON TABLE runs IS 'Workflow execution instances';
COMMENT ON TABLE task_runs IS 'Individual task execution within a workflow run';
COMMENT ON TABLE task_run_logs IS 'Structured log entries for task run observability';
COMMENT ON TABLE workers IS 'Registered worker pool for task execution';
COMMENT ON TABLE worker_heartbeats IS 'Time-series health and resource data from workers';
COMMENT ON TABLE approval_requests IS 'Human-in-the-loop approval workflow support';
COMMENT ON TABLE webhook_endpoints IS 'Registered webhook receivers for trigger integration';
COMMENT ON TABLE audit_log IS 'System-wide audit trail for compliance and debugging';
COMMENT ON TABLE secrets IS 'Encrypted secret storage with scoping and expiration';
COMMENT ON TABLE scheduled_jobs IS 'Internal scheduler state for cron-triggered workflows';

COMMENT ON VIEW v_active_runs IS 'Currently active workflow runs with task progress';
COMMENT ON VIEW v_worker_overview IS 'Live worker status and capacity overview';
COMMENT ON VIEW v_run_statistics IS 'Aggregate run statistics per workflow for dashboards';
COMMENT ON VIEW v_pending_approvals IS 'Pending approval requests requiring human action';
