# FlowForge Python SDK

Python client library for the [FlowForge](https://github.com/kasidit-wansudon/flowforge) distributed workflow orchestration engine.

## Installation

```bash
pip install flowforge
```

Or install from source:

```bash
cd sdk/python
pip install -e .
```

## Quick Start

```python
from flowforge import FlowForgeClient, WorkflowBuilder, TaskDefinition, RetryPolicy

# Connect to the FlowForge server
client = FlowForgeClient(
    server_url="https://flowforge.example.com",
    api_key="your-api-key",
)

# Build a workflow using the fluent builder
definition = (
    WorkflowBuilder("data-pipeline")
    .description("ETL pipeline for daily data processing")
    .timeout("30m")
    .add_task(TaskDefinition(
        id="extract",
        name="Extract Data",
        type="http",
        config={"method": "GET", "url": "https://api.example.com/data"},
        retry=RetryPolicy(max_retries=3, strategy="exponential"),
    ))
    .add_task(TaskDefinition(
        id="transform",
        name="Transform Data",
        type="script",
        config={"command": "python", "args": ["transform.py"]},
    ))
    .add_task(TaskDefinition(
        id="load",
        name="Load Data",
        type="http",
        config={"method": "POST", "url": "https://warehouse.example.com/ingest"},
    ))
    .add_dependency("transform", "extract")
    .add_dependency("load", "transform")
    .build()
)

# Create the workflow
workflow = client.create_workflow(definition)
print(f"Created workflow: {workflow.id}")

# Trigger a run
run = client.trigger_workflow(workflow.id, params={"date": "2024-01-15"})
print(f"Started run: {run.id}")

# Wait for completion
result = client.wait_for_completion(run.id, poll_interval=5.0, timeout=1800)
print(f"Run completed with status: {result.status}")
```

## Task Helpers

The SDK provides helper functions for common task types:

```python
from flowforge.client import http_task, script_task, condition_task, notify_task, approval_task

# HTTP request task
extract = http_task("extract", "Fetch API", "GET", "https://api.example.com/data")

# Script execution task
transform = script_task("transform", "Run Script", "python", ["transform.py"])

# Conditional branching
check = condition_task("check", "Validate Data", "result.count > 0")

# Notification
alert = notify_task("alert", "Send Alert", "slack", {"webhook": "https://hooks.slack.com/..."})

# Human approval with timeout
approve = approval_task("approve", "Manager Approval", ["manager@co.com"], timeout="24h")
```

## Streaming Logs

```python
def on_log(entry):
    print(f"[{entry.level}] {entry.task_id}: {entry.message}")

client.stream_logs(run.id, callback=on_log)
```

## Error Handling

```python
from flowforge import (
    FlowForgeError,
    NotFoundError,
    UnauthorizedError,
    FlowForgeTimeoutError,
)

try:
    workflow = client.get_workflow("nonexistent-id")
except NotFoundError:
    print("Workflow not found")
except UnauthorizedError:
    print("Invalid API key")
except FlowForgeTimeoutError:
    print("Request timed out")
except FlowForgeError as e:
    print(f"Error: {e} (status={e.status_code})")
```

## Context Manager

```python
with FlowForgeClient("https://flowforge.example.com", "key") as client:
    workflows = client.list_workflows()
```

## License

MIT
