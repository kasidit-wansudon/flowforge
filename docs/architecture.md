# FlowForge Architecture

## Overview

FlowForge is a distributed workflow orchestration engine designed for reliability, scalability, and observability. It enables teams to define, execute, and monitor complex multi-step workflows using a visual DAG editor or YAML/JSON definitions.

## System Architecture

```
                                    ┌─────────────────────┐
                                    │    React Frontend    │
                                    │  (DAG Editor, Dash)  │
                                    └──────────┬──────────┘
                                               │ HTTP/WS
                                    ┌──────────▼──────────┐
                                    │    API Gateway       │
                                    │  REST + WebSocket    │
                                    └──────────┬──────────┘
                                               │
                    ┌──────────────────────────┼──────────────────────────┐
                    │                          │                          │
         ┌──────────▼──────────┐   ┌──────────▼──────────┐   ┌──────────▼──────────┐
         │   Workflow Engine   │   │     Scheduler        │   │   Event Router      │
         │  (DAG, State, Exec) │   │  (Priority Queue)    │   │  (NATS JetStream)   │
         └──────────┬──────────┘   └──────────┬──────────┘   └──────────┬──────────┘
                    │                          │                          │
                    └──────────────────────────┼──────────────────────────┘
                                               │ gRPC
                    ┌──────────────────────────┼──────────────────────────┐
                    │                          │                          │
         ┌──────────▼──────────┐   ┌──────────▼──────────┐   ┌──────────▼──────────┐
         │     Worker 1        │   │     Worker 2         │   │     Worker N        │
         │  (Task Executor)    │   │  (Task Executor)     │   │  (Task Executor)    │
         └─────────────────────┘   └──────────────────────┘   └─────────────────────┘
                    │                          │                          │
                    └──────────────────────────┼──────────────────────────┘
                                               │
              ┌────────────────┬───────────────┼───────────────┬────────────────┐
              │                │               │               │                │
    ┌─────────▼────┐  ┌───────▼─────┐  ┌──────▼──────┐  ┌────▼──────┐  ┌──────▼──────┐
    │  PostgreSQL  │  │    Redis    │  │    NATS     │  │     S3    │  │  Prometheus │
    │  (State)     │  │  (Cache/    │  │ (JetStream) │  │(Artifacts)│  │  (Metrics)  │
    │              │  │   Locks)    │  │             │  │           │  │             │
    └──────────────┘  └─────────────┘  └─────────────┘  └───────────┘  └─────────────┘
```

## Core Components

### 1. Workflow Engine

The engine is the heart of FlowForge, responsible for parsing, validating, and orchestrating workflow execution.

**DAG Parser & Validator**
- Parses workflow definitions from YAML/JSON into directed acyclic graphs
- Validates graph structure: no cycles, no orphan nodes, valid references
- Performs topological sorting for execution ordering
- Cycle detection using Kahn's algorithm

**State Machine**
- Manages workflow and task lifecycle states
- Enforces valid state transitions (e.g., Running → Completed, not Completed → Running)
- States: Pending, Queued, Running, Paused, Completed, Failed, Cancelled, TimedOut

**Executor**
- Executes individual tasks with timeout and cancellation support
- Registry of task type handlers (HTTP, Script, Condition, Parallel, Delay)
- Wraps execution with retry policies and circuit breakers

### 2. Scheduler

Priority-based task scheduler that determines execution order.

- Four priority levels: Critical, High, Normal, Low
- Dependency resolution: tasks only become ready when all upstream dependencies complete
- Fair scheduling across workflows to prevent starvation
- Integration with distributed locks for worker coordination

### 3. Task Types

| Type | Description |
|------|-------------|
| HTTP | HTTP requests with configurable method, headers, auth, and retry |
| Script | Execute bash/python/node scripts in sandboxed environments |
| Condition | Conditional branching (if/else, switch) based on upstream outputs |
| Parallel | Fan-out execution with configurable concurrency and fail-fast |
| Delay | Timer/delay with relative or absolute scheduling |
| Plugin | Custom task types via plugin interface |

### 4. Event Processing

Built on NATS JetStream for reliable message delivery.

- **Producer/Consumer**: Publish and subscribe to event streams
- **Event Router**: Pattern-based routing with wildcard support
- **Dead Letter Queue**: Failed messages are captured with retry metadata

### 5. Worker Fleet

Horizontally scalable workers that claim and execute tasks.

- Workers register with the server via gRPC
- Tasks are distributed via NATS JetStream
- Each worker runs a configurable number of concurrent task executors
- Heartbeat mechanism for health monitoring
- Graceful shutdown: drain in-flight tasks before stopping

### 6. Storage Layer

| Store | Purpose |
|-------|---------|
| PostgreSQL | Workflow definitions, run history, task states, API keys |
| Redis | Caching, distributed locks, rate limiting, workflow state cache |
| S3 | Build artifacts, task outputs, log archives |

### 7. API Layer

- **REST API**: Full CRUD for workflows, runs, and system management
- **gRPC**: High-performance worker communication (task claiming, completion)
- **WebSocket**: Real-time execution updates, log streaming

## Data Flow

### Workflow Execution

1. Client submits workflow trigger (REST API, webhook, cron, or event)
2. Engine parses the workflow definition and creates a DAG
3. Scheduler performs topological sort and queues root tasks
4. Workers claim tasks via NATS and execute them
5. On task completion, scheduler resolves dependencies and queues next tasks
6. State machine tracks progress; WebSocket broadcasts updates
7. When all tasks complete (or one fails in fail-fast mode), workflow run finishes

### Retry & Recovery

1. Task failure triggers retry policy evaluation
2. Retry policies: exponential backoff, linear, constant, or custom
3. Circuit breaker prevents cascading failures to external services
4. After max retries, task is marked failed
5. Workflow-level failure handlers execute (notification, cleanup)

## Security

- **Authentication**: JWT tokens for UI, API keys for programmatic access
- **Authorization**: Role-based (admin, operator, viewer)
- **API Key Management**: Create, revoke, expire API keys
- **Input Validation**: All workflow definitions validated before storage

## Observability

- **Prometheus Metrics**: Workflow counts, task durations, error rates, queue depth
- **Structured Logging**: JSON-formatted logs with correlation IDs
- **Distributed Tracing**: OpenTelemetry integration points
- **Real-time Dashboard**: Live execution monitoring via WebSocket

## Scalability

- **Horizontal Worker Scaling**: Add workers on demand; Kubernetes HPA based on queue depth
- **Database Connection Pooling**: pgx pool with configurable limits
- **Redis Caching**: Reduce database load for frequently accessed data
- **Event-Driven**: NATS JetStream handles backpressure naturally
