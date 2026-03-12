# FlowForge Workflow Definition Specification

## Overview

FlowForge workflows are defined in YAML or JSON format. Each workflow describes a directed acyclic graph (DAG) of tasks with dependencies, triggers, and execution policies.

## Schema

```yaml
# Workflow metadata
id: string                    # Unique identifier (auto-generated if omitted)
name: string                  # Human-readable name (required)
description: string           # Optional description
version: int                  # Auto-incremented on update

# Trigger configuration
triggers:
  - type: cron|webhook|event|manual
    config:
      schedule: string        # Cron expression (for cron trigger)
      path: string            # Webhook path (for webhook trigger)
      event: string           # Event pattern (for event trigger)

# Global settings
timeout: duration             # Maximum workflow execution time (e.g., "30m")
on_failure: string            # Failure handling: "fail", "continue", "pause"
metadata:                     # Custom key-value metadata
  key: value

# Task definitions
tasks:
  - id: string                # Unique task identifier (required)
    name: string              # Human-readable name
    type: string              # Task type: http, script, condition, parallel, delay
    depends_on:               # List of upstream task IDs
      - task_id
    condition: string         # Execution condition expression
    timeout: duration         # Task-level timeout
    retry:                    # Retry policy
      max_retries: int
      delay: duration
      max_delay: duration
      multiplier: float
      strategy: exponential|linear|constant
    config:                   # Type-specific configuration
      ...
```

## Task Types

### HTTP Task

Executes an HTTP request.

```yaml
tasks:
  - id: fetch-data
    name: Fetch Data from API
    type: http
    config:
      url: https://api.example.com/data
      method: GET
      headers:
        Authorization: "Bearer ${secrets.API_TOKEN}"
        Content-Type: application/json
      body: |
        {"query": "SELECT * FROM users"}
      timeout: 30s
      valid_status_codes: [200, 201]
      auth:
        type: bearer
        token: "${secrets.API_TOKEN}"
    retry:
      max_retries: 3
      delay: 5s
      strategy: exponential
```

### Script Task

Executes a script in a sandboxed environment.

```yaml
tasks:
  - id: process-data
    name: Process Data
    type: script
    config:
      language: python3       # bash, python3, node
      script: |
        import json
        data = json.loads('${tasks.fetch-data.output}')
        result = [item for item in data if item['active']]
        print(json.dumps(result))
      env:
        DATA_DIR: /tmp/data
      timeout: 60s
```

### Condition Task

Conditional branching based on upstream results.

```yaml
tasks:
  - id: check-result
    name: Check Processing Result
    type: condition
    depends_on: [process-data]
    config:
      expression: "${tasks.process-data.output.count} > 0"
      on_true: notify-success
      on_false: notify-empty
```

### Parallel Task

Fan-out execution of multiple tasks concurrently.

```yaml
tasks:
  - id: parallel-tests
    name: Run Tests in Parallel
    type: parallel
    config:
      tasks:
        - run-unit-tests
        - run-integration-tests
        - run-e2e-tests
      max_concurrency: 3
      fail_fast: true
```

### Delay Task

Wait for a specified duration before continuing.

```yaml
tasks:
  - id: cooldown
    name: Wait for Deployment
    type: delay
    depends_on: [deploy]
    config:
      duration: 30s
      # OR
      until: "2024-01-15T10:00:00Z"
```

## Variable Interpolation

FlowForge supports variable interpolation in task configurations:

| Pattern | Description |
|---------|-------------|
| `${tasks.<id>.output}` | Output from a completed task |
| `${tasks.<id>.status}` | Status of a task |
| `${secrets.<name>}` | Secret from secure store |
| `${env.<name>}` | Environment variable |
| `${workflow.id}` | Current workflow ID |
| `${workflow.name}` | Current workflow name |
| `${run.id}` | Current run ID |
| `${trigger.type}` | Trigger type that started this run |
| `${trigger.payload}` | Trigger payload (for webhooks/events) |

## Complete Example

```yaml
id: data-pipeline
name: Daily Data Pipeline
description: Extract, transform, and load data from external API
version: 1

triggers:
  - type: cron
    config:
      schedule: "0 6 * * *"    # Daily at 6 AM

timeout: 1h
on_failure: fail

metadata:
  team: data-engineering
  priority: high

tasks:
  - id: extract
    name: Extract Data
    type: http
    config:
      url: https://api.example.com/export
      method: POST
      headers:
        Authorization: "Bearer ${secrets.API_TOKEN}"
      timeout: 120s
    retry:
      max_retries: 3
      delay: 10s
      strategy: exponential

  - id: validate
    name: Validate Data
    type: script
    depends_on: [extract]
    config:
      language: python3
      script: |
        import json, sys
        data = json.loads('${tasks.extract.output}')
        if len(data) == 0:
            print("ERROR: No data received")
            sys.exit(1)
        print(f"Validated {len(data)} records")

  - id: transform
    name: Transform Data
    type: script
    depends_on: [validate]
    config:
      language: python3
      script: |
        import json
        data = json.loads('${tasks.extract.output}')
        transformed = [{"id": r["id"], "value": r["amount"] * 100} for r in data]
        print(json.dumps(transformed))
      timeout: 300s

  - id: load
    name: Load to Database
    type: http
    depends_on: [transform]
    config:
      url: https://internal.example.com/api/import
      method: POST
      headers:
        Content-Type: application/json
      body: "${tasks.transform.output}"
    retry:
      max_retries: 2
      delay: 30s
      strategy: constant

  - id: notify
    name: Send Notification
    type: http
    depends_on: [load]
    config:
      url: https://hooks.slack.com/services/xxx
      method: POST
      body: |
        {"text": "Data pipeline completed: ${tasks.transform.output.count} records loaded"}
```
