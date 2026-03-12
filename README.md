# FlowForge

**High-performance distributed workflow orchestration engine with visual DAG editor and real-time monitoring.**

[![CI](https://github.com/kasidit-wansudon/flowforge/actions/workflows/ci.yml/badge.svg)](https://github.com/kasidit-wansudon/flowforge/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/kasidit-wansudon/flowforge)](https://goreportcard.com/report/github.com/kasidit-wansudon/flowforge)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

FlowForge lets you define, execute, and monitor complex multi-step workflows as directed acyclic graphs (DAGs). Define pipelines in YAML, build them visually with the drag-and-drop editor, or construct them programmatically with the Go/Python SDKs.

```
                         ┌──────────┐
                         │  Extract  │
                         └─────┬─────┘
                               │
                         ┌─────▼─────┐
                         │  Validate  │
                         └─────┬─────┘
                               │
                    ┌──────────┼──────────┐
                    │                     │
              ┌─────▼─────┐        ┌─────▼─────┐
              │ Transform │        │  Archive   │
              └─────┬─────┘        └─────┬─────┘
                    │                     │
                    └──────────┬──────────┘
                               │
                         ┌─────▼─────┐
                         │   Load    │
                         └─────┬─────┘
                               │
                         ┌─────▼─────┐
                         │  Notify   │
                         └───────────┘
```

## Features

### Core Engine
- **DAG Execution** — Parse, validate, and execute directed acyclic graphs with cycle detection, topological sorting, and parallel fan-out/fan-in
- **Priority Scheduler** — Four-tier priority scheduling (Critical, High, Normal, Low) with dependency resolution
- **Configurable Retry** — Exponential backoff, linear, constant, and custom retry strategies with jitter
- **State Machine** — Strict workflow/task lifecycle management with validated transitions
- **Circuit Breaker** — Prevent cascading failures to external services

### Task Types
- **HTTP** — REST API calls with auth, headers, retry, and response validation
- **Script** — Execute bash, Python, or Node.js scripts in sandboxed environments
- **Condition** — If/else branching based on upstream task outputs
- **Parallel** — Concurrent fan-out execution with configurable concurrency limits
- **Delay** — Relative or absolute time delays
- **Plugin** — Extensible plugin system for custom task types

### Visual DAG Editor
- Drag-and-drop workflow builder powered by React Flow
- Real-time DAG validation (cycle detection, dependency checking)
- Type-specific node configuration panels
- Export workflows to YAML/JSON

### Distributed Workers
- Horizontally scalable worker fleet with Kubernetes HPA
- NATS JetStream-based task distribution with at-least-once delivery
- Distributed locking for task claiming
- Graceful shutdown with in-flight task draining
- Heartbeat-based health monitoring

### Monitoring & Observability
- Real-time execution tracking via WebSocket
- Prometheus metrics (workflow counts, task durations, error rates, queue depth)
- Structured JSON logging with correlation IDs
- Terminal-like log viewer with filtering and search

### Triggers
- **Cron** — Schedule workflows with cron expressions
- **Webhook** — Trigger via HTTP POST with payload forwarding
- **Event** — React to NATS events with pattern matching
- **Manual** — On-demand execution via API or UI

## Architecture

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│  React UI    │────▶│  API Server  │────▶│  PostgreSQL  │
│  (DAG Editor)│     │  (REST/WS)   │     │  (State)     │
└──────────────┘     └──────┬───────┘     └──────────────┘
                            │
                     ┌──────▼───────┐     ┌──────────────┐
                     │  Scheduler   │────▶│    Redis      │
                     │  (Priority)  │     │  (Cache/Lock) │
                     └──────┬───────┘     └──────────────┘
                            │ NATS
              ┌─────────────┼─────────────┐
              │             │             │
        ┌─────▼────┐  ┌────▼─────┐  ┌────▼─────┐
        │ Worker 1 │  │ Worker 2 │  │ Worker N │
        └──────────┘  └──────────┘  └──────────┘
```

## Quick Start

### Docker Compose (Recommended)

```bash
git clone https://github.com/kasidit-wansudon/flowforge.git
cd flowforge
docker-compose up -d
```

Services:
| Service | URL |
|---------|-----|
| FlowForge UI | http://localhost:8080 |
| REST API | http://localhost:8080/api/v1 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3001 |

### From Source

```bash
# Build
make build

# Start infrastructure
docker-compose up -d postgres redis nats

# Run migrations
./bin/migrate up

# Start server
./bin/server

# Start worker (in another terminal)
./bin/worker
```

### CLI

```bash
# Create a workflow
flowforge workflow create examples/ci-pipeline.yaml

# List workflows
flowforge workflow list

# Trigger execution
flowforge workflow run <workflow-id>

# Check status
flowforge run status <run-id>

# Stream logs
flowforge run logs <run-id>
```

## Workflow Definition

Define workflows in YAML:

```yaml
name: CI Pipeline
description: Build, test, and deploy application

triggers:
  - type: webhook
    config:
      path: /hooks/ci

tasks:
  - id: checkout
    name: Checkout Code
    type: script
    config:
      language: bash
      script: git clone ${trigger.payload.repo_url} /workspace

  - id: lint
    name: Run Linter
    type: script
    depends_on: [checkout]
    config:
      language: bash
      script: cd /workspace && golangci-lint run ./...

  - id: test
    name: Run Tests
    type: parallel
    depends_on: [lint]
    config:
      tasks: [unit-tests, integration-tests]
      fail_fast: true

  - id: unit-tests
    name: Unit Tests
    type: script
    config:
      language: bash
      script: cd /workspace && go test ./...

  - id: integration-tests
    name: Integration Tests
    type: script
    config:
      language: bash
      script: cd /workspace && go test -tags=integration ./...

  - id: build
    name: Build Binary
    type: script
    depends_on: [test]
    config:
      language: bash
      script: cd /workspace && go build -o app ./cmd/server

  - id: deploy
    name: Deploy
    type: http
    depends_on: [build]
    config:
      url: https://deploy.example.com/api/deploy
      method: POST
      body: '{"version": "${run.id}"}'
    retry:
      max_retries: 2
      delay: 30s
      strategy: exponential
```

## SDKs

### Go SDK

```go
package main

import (
    "context"
    flowforge "github.com/kasidit-wansudon/flowforge/sdk/go"
)

func main() {
    client := flowforge.NewClient("http://localhost:8080", "your-api-key")

    workflow := flowforge.NewWorkflow("data-pipeline").
        AddTask(flowforge.NewHTTPTask("fetch", "https://api.example.com/data").
            WithMethod("GET").
            WithRetry(3, "exponential")).
        AddTask(flowforge.NewScriptTask("process", "python3", "transform.py").
            DependsOn("fetch")).
        Build()

    ctx := context.Background()
    run, _ := client.CreateAndTrigger(ctx, workflow)
    client.WaitForCompletion(ctx, run.ID, 0)
}
```

### Python SDK

```python
from flowforge import FlowForgeClient

client = FlowForgeClient("http://localhost:8080", api_key="your-api-key")

workflow = client.create_workflow({
    "name": "Data ETL",
    "tasks": [
        {"id": "extract", "type": "http", "config": {"url": "https://api.example.com/data"}},
        {"id": "transform", "type": "script", "depends_on": ["extract"],
         "config": {"language": "python3", "script": "process_data.py"}},
        {"id": "load", "type": "http", "depends_on": ["transform"],
         "config": {"url": "https://db.example.com/import", "method": "POST"}}
    ]
})

run = client.trigger_workflow(workflow["id"])
client.wait_for_completion(run["id"])
```

## API Reference

### Workflows
| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/workflows` | Create workflow |
| `GET` | `/api/v1/workflows` | List workflows |
| `GET` | `/api/v1/workflows/:id` | Get workflow |
| `PUT` | `/api/v1/workflows/:id` | Update workflow |
| `DELETE` | `/api/v1/workflows/:id` | Delete workflow |
| `POST` | `/api/v1/workflows/:id/trigger` | Trigger execution |

### Runs
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/runs` | List runs |
| `GET` | `/api/v1/runs/:id` | Get run details |
| `POST` | `/api/v1/runs/:id/cancel` | Cancel run |
| `POST` | `/api/v1/runs/:id/retry` | Retry failed run |
| `GET` | `/api/v1/runs/:id/logs` | Get run logs |

### System
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/health` | Health check |
| `GET` | `/api/v1/metrics` | Prometheus metrics |
| `GET` | `/api/v1/version` | Version info |

## Project Structure

```
flowforge/
├── cmd/                    # Application entrypoints
│   ├── server/             # API server + scheduler
│   ├── worker/             # Task execution worker
│   ├── cli/                # CLI management tool
│   └── migrate/            # Database migrations
├── internal/               # Private application code
│   ├── engine/             # Core workflow engine
│   │   ├── dag/            # DAG parser & validator
│   │   ├── scheduler/      # Priority task scheduler
│   │   ├── executor/       # Task execution runtime
│   │   ├── retry/          # Retry policies
│   │   └── state/          # Workflow state machine
│   ├── workflow/           # Workflow management
│   ├── task/               # Task type implementations
│   ├── event/              # Event processing (NATS)
│   ├── api/                # REST, gRPC, WebSocket
│   ├── storage/            # PostgreSQL, Redis, S3
│   ├── auth/               # Authentication
│   ├── metrics/            # Prometheus metrics
│   └── pkg/                # Shared utilities
├── frontend/               # React 18 + TypeScript UI
├── proto/                  # Protocol Buffer definitions
├── sdk/                    # Go and Python SDKs
├── examples/               # Example workflow definitions
├── deploy/                 # Kubernetes & Helm charts
├── docs/                   # Documentation
└── tests/                  # Integration & load tests
```

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Core Engine | Go 1.22+ (zero-framework) |
| API | gorilla/mux (REST), gRPC, gorilla/websocket |
| Frontend | React 18, TypeScript, React Flow, Tailwind CSS, Recharts |
| Database | PostgreSQL 15+ (pgx) |
| Cache | Redis 7+ (go-redis) |
| Messaging | NATS JetStream |
| Metrics | Prometheus + Grafana |
| Infrastructure | Docker, Kubernetes, Helm |
| CI/CD | GitHub Actions |

## Benchmarks

Measured on a 3-node cluster (8 vCPU, 16GB RAM each):

| Metric | Value |
|--------|-------|
| Workflow throughput | ~500 workflows/min |
| Task execution latency (p50) | 12ms |
| Task execution latency (p99) | 85ms |
| Concurrent workflows | 1,000+ |
| Workers per cluster | 50+ |
| Event throughput (NATS) | 100K msgs/sec |

## Development

```bash
# Run tests
make test

# Run linter
make lint

# Format code
make fmt

# Generate protobuf code
make proto-gen

# Build Docker images
make docker-build
```

## Deployment

See the [Deployment Guide](docs/deployment.md) for detailed instructions on deploying to Docker Compose or Kubernetes.

### Kubernetes with Helm

```bash
helm install flowforge deploy/helm/flowforge \
  --namespace flowforge \
  --create-namespace \
  --set worker.autoscaling.maxReplicas=20
```

## Documentation

- [Architecture Overview](docs/architecture.md)
- [Workflow Definition Spec](docs/pipeline-spec.md)
- [Deployment Guide](docs/deployment.md)

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/my-feature`)
3. Commit your changes (`git commit -am 'Add my feature'`)
4. Push to the branch (`git push origin feature/my-feature`)
5. Open a Pull Request

## License

MIT License — see [LICENSE](LICENSE) for details.

## Author

**Kasidit Wansudon (Oak)**
- GitHub: [@kasidit-wansudon](https://github.com/kasidit-wansudon)
- Email: kasidit.wans@gmail.com
