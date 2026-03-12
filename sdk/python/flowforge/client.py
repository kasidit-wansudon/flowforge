"""FlowForge Python SDK client implementation."""

from __future__ import annotations

import json
import time as _time
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Any, Callable, Dict, List, Optional, Union

import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry


# =============================================================================
# Error classes
# =============================================================================


class FlowForgeError(Exception):
    """Base exception for all FlowForge SDK errors."""

    def __init__(self, message: str, status_code: int = 0, code: str = "") -> None:
        super().__init__(message)
        self.status_code = status_code
        self.code = code


class NotFoundError(FlowForgeError):
    """Raised when a requested resource does not exist."""

    pass


class UnauthorizedError(FlowForgeError):
    """Raised when authentication fails."""

    pass


class TimeoutError(FlowForgeError):
    """Raised when an operation exceeds its deadline."""

    pass


class BadRequestError(FlowForgeError):
    """Raised for invalid input."""

    pass


class ConflictError(FlowForgeError):
    """Raised when a resource conflict occurs."""

    pass


class ServerError(FlowForgeError):
    """Raised for unexpected server-side errors."""

    pass


# =============================================================================
# Enums
# =============================================================================


class WorkflowStatus(str, Enum):
    ACTIVE = "active"
    INACTIVE = "inactive"
    ARCHIVED = "archived"
    DRAFT = "draft"


class RunStatus(str, Enum):
    PENDING = "pending"
    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"
    TIMED_OUT = "timed_out"


class TaskStatus(str, Enum):
    PENDING = "pending"
    QUEUED = "queued"
    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"
    SKIPPED = "skipped"
    RETRYING = "retrying"
    TIMED_OUT = "timed_out"


class Priority(str, Enum):
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


# =============================================================================
# Data classes
# =============================================================================


@dataclass
class RetryPolicy:
    """Retry configuration for a task."""

    max_retries: int = 3
    initial_delay: str = "1s"
    max_delay: str = "60s"
    multiplier: float = 2.0
    strategy: str = "exponential"  # "fixed", "exponential", "linear"

    def to_dict(self) -> Dict[str, Any]:
        return {
            "max_retries": self.max_retries,
            "initial_delay": self.initial_delay,
            "max_delay": self.max_delay,
            "multiplier": self.multiplier,
            "strategy": self.strategy,
        }


@dataclass
class TriggerConfig:
    """Trigger configuration for a workflow."""

    type: str
    config: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return {"type": self.type, "config": self.config}


@dataclass
class TaskDefinition:
    """Definition of a single task within a workflow."""

    id: str
    name: str
    type: str
    config: Dict[str, Any] = field(default_factory=dict)
    depends_on: List[str] = field(default_factory=list)
    timeout: str = ""
    retry: Optional[RetryPolicy] = None
    condition: str = ""
    metadata: Dict[str, str] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        result: Dict[str, Any] = {
            "id": self.id,
            "name": self.name,
            "type": self.type,
        }
        if self.config:
            result["config"] = self.config
        if self.depends_on:
            result["depends_on"] = self.depends_on
        if self.timeout:
            result["timeout"] = self.timeout
        if self.retry:
            result["retry"] = self.retry.to_dict()
        if self.condition:
            result["condition"] = self.condition
        if self.metadata:
            result["metadata"] = self.metadata
        return result


@dataclass
class TaskResult:
    """Outcome of a single task execution."""

    task_id: str
    status: str
    output: Any = None
    error: str = ""
    duration: str = ""
    retry_count: int = 0
    metadata: Dict[str, str] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "TaskResult":
        return cls(
            task_id=data.get("task_id", ""),
            status=data.get("status", ""),
            output=data.get("output"),
            error=data.get("error", ""),
            duration=data.get("duration", ""),
            retry_count=data.get("retry_count", 0),
            metadata=data.get("metadata", {}),
        )


@dataclass
class LogEntry:
    """A single log line from a workflow run."""

    id: str
    run_id: str
    task_id: str
    level: str
    message: str
    timestamp: str
    fields: Dict[str, str] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "LogEntry":
        return cls(
            id=data.get("id", ""),
            run_id=data.get("run_id", ""),
            task_id=data.get("task_id", ""),
            level=data.get("level", ""),
            message=data.get("message", ""),
            timestamp=data.get("timestamp", ""),
            fields=data.get("fields", {}),
        )


@dataclass
class Workflow:
    """Represents a workflow definition returned by the server."""

    id: str
    name: str
    description: str = ""
    version: int = 1
    status: str = "active"
    triggers: List[Dict[str, Any]] = field(default_factory=list)
    tasks: List[Dict[str, Any]] = field(default_factory=list)
    timeout: str = ""
    metadata: Dict[str, str] = field(default_factory=dict)
    created_at: str = ""
    updated_at: str = ""

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "Workflow":
        return cls(
            id=data.get("id", ""),
            name=data.get("name", ""),
            description=data.get("description", ""),
            version=data.get("version", 1),
            status=data.get("status", "active"),
            triggers=data.get("triggers", []),
            tasks=data.get("tasks", []),
            timeout=data.get("timeout", ""),
            metadata=data.get("metadata", {}),
            created_at=data.get("created_at", ""),
            updated_at=data.get("updated_at", ""),
        )


@dataclass
class Run:
    """Represents a workflow execution instance."""

    id: str
    workflow_id: str
    version: int = 1
    status: str = "pending"
    trigger: str = ""
    params: Dict[str, Any] = field(default_factory=dict)
    results: List[TaskResult] = field(default_factory=list)
    created_at: str = ""
    started_at: str = ""
    completed_at: str = ""
    error: str = ""
    metadata: Dict[str, str] = field(default_factory=dict)

    @property
    def is_terminal(self) -> bool:
        """Return True if the run has reached a terminal state."""
        return self.status in (
            RunStatus.COMPLETED.value,
            RunStatus.FAILED.value,
            RunStatus.CANCELLED.value,
            RunStatus.TIMED_OUT.value,
        )

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "Run":
        results = [TaskResult.from_dict(r) for r in data.get("results", [])]
        return cls(
            id=data.get("id", ""),
            workflow_id=data.get("workflow_id", ""),
            version=data.get("version", 1),
            status=data.get("status", "pending"),
            trigger=data.get("trigger", ""),
            params=data.get("params", {}),
            results=results,
            created_at=data.get("created_at", ""),
            started_at=data.get("started_at", ""),
            completed_at=data.get("completed_at", ""),
            error=data.get("error", ""),
            metadata=data.get("metadata", {}),
        )


# =============================================================================
# Workflow builder
# =============================================================================


class WorkflowBuilder:
    """Fluent builder for constructing workflow definitions.

    Usage::

        definition = (
            WorkflowBuilder("my-pipeline")
            .description("A CI pipeline")
            .add_task(TaskDefinition(id="lint", name="Lint", type="script",
                      config={"command": "golangci-lint", "args": ["run"]}))
            .add_task(TaskDefinition(id="test", name="Test", type="script",
                      config={"command": "go", "args": ["test", "./..."]}))
            .add_dependency("test", "lint")
            .build()
        )
    """

    def __init__(self, name: str) -> None:
        self._name = name
        self._description = ""
        self._triggers: List[TriggerConfig] = []
        self._tasks: List[TaskDefinition] = []
        self._timeout = ""
        self._metadata: Dict[str, str] = {}
        self._task_index: Dict[str, int] = {}

    def description(self, desc: str) -> "WorkflowBuilder":
        self._description = desc
        return self

    def timeout(self, t: str) -> "WorkflowBuilder":
        self._timeout = t
        return self

    def with_metadata(self, key: str, value: str) -> "WorkflowBuilder":
        self._metadata[key] = value
        return self

    def add_trigger(
        self, trigger_type: str, config: Optional[Dict[str, Any]] = None
    ) -> "WorkflowBuilder":
        self._triggers.append(TriggerConfig(type=trigger_type, config=config or {}))
        return self

    def add_task(self, task: TaskDefinition) -> "WorkflowBuilder":
        if not task.id:
            raise ValueError("Task ID is required")
        if task.id in self._task_index:
            raise ValueError(f"Duplicate task ID: {task.id}")
        self._task_index[task.id] = len(self._tasks)
        self._tasks.append(task)
        return self

    def add_dependency(self, task_id: str, depends_on_id: str) -> "WorkflowBuilder":
        if task_id not in self._task_index:
            raise ValueError(f"Task {task_id!r} not found")
        if depends_on_id not in self._task_index:
            raise ValueError(f"Dependency {depends_on_id!r} not found")
        idx = self._task_index[task_id]
        if depends_on_id not in self._tasks[idx].depends_on:
            self._tasks[idx].depends_on.append(depends_on_id)
        return self

    def build(self) -> Dict[str, Any]:
        if not self._name:
            raise ValueError("Workflow name is required")
        if not self._tasks:
            raise ValueError("Workflow must have at least one task")

        # Validate dependency references.
        for task in self._tasks:
            for dep in task.depends_on:
                if dep not in self._task_index:
                    raise ValueError(
                        f"Task {task.id!r} depends on unknown task {dep!r}"
                    )

        result: Dict[str, Any] = {
            "name": self._name,
            "tasks": [t.to_dict() for t in self._tasks],
        }
        if self._description:
            result["description"] = self._description
        if self._triggers:
            result["triggers"] = [t.to_dict() for t in self._triggers]
        if self._timeout:
            result["timeout"] = self._timeout
        if self._metadata:
            result["metadata"] = self._metadata
        return result


# =============================================================================
# Task builder helpers
# =============================================================================


def http_task(
    id: str,
    name: str,
    method: str,
    url: str,
    headers: Optional[Dict[str, str]] = None,
    body: Any = None,
) -> TaskDefinition:
    """Create an HTTP request task."""
    config: Dict[str, Any] = {"method": method, "url": url}
    if headers:
        config["headers"] = headers
    if body is not None:
        config["body"] = body
    return TaskDefinition(id=id, name=name, type="http", config=config)


def script_task(
    id: str, name: str, command: str, args: Optional[List[str]] = None
) -> TaskDefinition:
    """Create a script execution task."""
    config: Dict[str, Any] = {"command": command}
    if args:
        config["args"] = args
    return TaskDefinition(id=id, name=name, type="script", config=config)


def condition_task(id: str, name: str, expression: str) -> TaskDefinition:
    """Create a conditional branching task."""
    return TaskDefinition(
        id=id, name=name, type="condition", config={"expression": expression}
    )


def delay_task(id: str, name: str, duration: str) -> TaskDefinition:
    """Create a delay task."""
    return TaskDefinition(
        id=id, name=name, type="delay", config={"duration": duration}
    )


def notify_task(
    id: str, name: str, channel: str, config: Optional[Dict[str, Any]] = None
) -> TaskDefinition:
    """Create a notification task."""
    cfg = dict(config or {})
    cfg["channel"] = channel
    return TaskDefinition(id=id, name=name, type="notify", config=cfg)


def approval_task(
    id: str,
    name: str,
    approvers: List[str],
    timeout: str = "24h",
) -> TaskDefinition:
    """Create a human approval task."""
    return TaskDefinition(
        id=id,
        name=name,
        type="approval",
        config={"approvers": approvers},
        timeout=timeout,
    )


# =============================================================================
# FlowForgeClient
# =============================================================================


class FlowForgeClient:
    """Client for the FlowForge workflow orchestration API.

    Usage::

        client = FlowForgeClient(
            server_url="https://flowforge.example.com",
            api_key="my-api-key",
        )
        workflow = client.create_workflow(definition)
        run = client.trigger_workflow(workflow.id, params={"env": "staging"})
        result = client.wait_for_completion(run.id)
    """

    def __init__(
        self,
        server_url: str,
        api_key: str,
        timeout: float = 30.0,
        max_retries: int = 3,
    ) -> None:
        self.server_url = server_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

        self._session = requests.Session()
        self._session.headers.update(
            {
                "Content-Type": "application/json",
                "Accept": "application/json",
                "User-Agent": "flowforge-python-sdk/1.0",
                "Authorization": f"Bearer {api_key}",
            }
        )

        # Automatic retries for transient errors.
        retry_strategy = Retry(
            total=max_retries,
            backoff_factor=0.5,
            status_forcelist=[502, 503, 504],
            allowed_methods=["GET", "HEAD", "OPTIONS"],
        )
        adapter = HTTPAdapter(max_retries=retry_strategy)
        self._session.mount("https://", adapter)
        self._session.mount("http://", adapter)

    def close(self) -> None:
        """Close the underlying HTTP session."""
        self._session.close()

    def __enter__(self) -> "FlowForgeClient":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    # -------------------------------------------------------------------------
    # Workflow CRUD
    # -------------------------------------------------------------------------

    def create_workflow(self, definition: Dict[str, Any]) -> Workflow:
        """Create a new workflow from a definition dict.

        Args:
            definition: Workflow definition (as produced by WorkflowBuilder.build()).

        Returns:
            The created Workflow.
        """
        data = self._request("POST", "/api/v1/workflows", json_body=definition)
        return Workflow.from_dict(data)

    def get_workflow(self, workflow_id: str) -> Workflow:
        """Retrieve a workflow by ID."""
        data = self._request("GET", f"/api/v1/workflows/{workflow_id}")
        return Workflow.from_dict(data)

    def list_workflows(
        self,
        status: Optional[str] = None,
        page_size: int = 20,
        page_token: str = "",
    ) -> Dict[str, Any]:
        """List workflows with optional filtering.

        Returns:
            Dict with keys 'workflows' (list), 'next_page_token', 'total_count'.
        """
        params: Dict[str, Any] = {"page_size": page_size}
        if status:
            params["status"] = status
        if page_token:
            params["page_token"] = page_token
        data = self._request("GET", "/api/v1/workflows", params=params)
        if isinstance(data, dict):
            data["workflows"] = [
                Workflow.from_dict(w) for w in data.get("workflows", [])
            ]
        return data

    def update_workflow(
        self, workflow_id: str, updates: Dict[str, Any]
    ) -> Workflow:
        """Update an existing workflow."""
        data = self._request(
            "PUT", f"/api/v1/workflows/{workflow_id}", json_body=updates
        )
        return Workflow.from_dict(data)

    def delete_workflow(self, workflow_id: str) -> None:
        """Delete a workflow by ID."""
        self._request("DELETE", f"/api/v1/workflows/{workflow_id}")

    # -------------------------------------------------------------------------
    # Trigger & Run
    # -------------------------------------------------------------------------

    def trigger_workflow(
        self,
        workflow_id: str,
        params: Optional[Dict[str, Any]] = None,
        priority: str = "medium",
        metadata: Optional[Dict[str, str]] = None,
    ) -> Run:
        """Trigger a workflow run.

        Args:
            workflow_id: ID of the workflow to trigger.
            params: Runtime parameters passed to the workflow.
            priority: Execution priority (low, medium, high, critical).
            metadata: Additional metadata for the run.

        Returns:
            The created Run.
        """
        body: Dict[str, Any] = {"priority": priority}
        if params:
            body["params"] = params
        if metadata:
            body["metadata"] = metadata
        data = self._request(
            "POST", f"/api/v1/workflows/{workflow_id}/trigger", json_body=body
        )
        return Run.from_dict(data)

    def get_run(self, run_id: str) -> Run:
        """Retrieve a workflow run by ID."""
        data = self._request("GET", f"/api/v1/runs/{run_id}")
        return Run.from_dict(data)

    def cancel_run(self, run_id: str) -> Run:
        """Cancel a running workflow execution."""
        data = self._request("POST", f"/api/v1/runs/{run_id}/cancel")
        return Run.from_dict(data)

    def wait_for_completion(
        self,
        run_id: str,
        poll_interval: float = 2.0,
        timeout: float = 0,
    ) -> Run:
        """Poll a run until it reaches a terminal state.

        Args:
            run_id: ID of the run to wait on.
            poll_interval: Seconds between polls.
            timeout: Maximum seconds to wait (0 = no limit).

        Returns:
            The terminal Run.

        Raises:
            TimeoutError: If the timeout is exceeded.
        """
        deadline = _time.monotonic() + timeout if timeout > 0 else None

        while True:
            run = self.get_run(run_id)
            if run.is_terminal:
                return run

            if deadline is not None and _time.monotonic() >= deadline:
                raise TimeoutError(
                    f"Timed out waiting for run {run_id} after {timeout}s"
                )

            _time.sleep(poll_interval)

    def stream_logs(
        self,
        run_id: str,
        callback: Callable[[LogEntry], None],
        timeout: float = 0,
    ) -> None:
        """Stream log entries for a run via server-sent events.

        Args:
            run_id: ID of the run.
            callback: Invoked for each received LogEntry.
            timeout: Connection timeout in seconds (0 = use default).
        """
        url = f"{self.server_url}/api/v1/runs/{run_id}/logs/stream"
        stream_timeout = timeout if timeout > 0 else self.timeout

        resp = self._session.get(
            url,
            headers={"Accept": "text/event-stream"},
            stream=True,
            timeout=stream_timeout,
        )
        self._check_response(resp)

        try:
            buffer = ""
            for chunk in resp.iter_content(chunk_size=None, decode_unicode=True):
                if chunk:
                    buffer += chunk
                    while "\n" in buffer:
                        line, buffer = buffer.split("\n", 1)
                        line = line.strip()
                        if not line:
                            continue
                        # SSE data lines.
                        if line.startswith("data: "):
                            line = line[6:]
                        try:
                            entry_data = json.loads(line)
                            callback(LogEntry.from_dict(entry_data))
                        except json.JSONDecodeError:
                            continue
        except requests.exceptions.ChunkedEncodingError:
            # Stream ended (normal for SSE when the run completes).
            pass

    # -------------------------------------------------------------------------
    # Internal helpers
    # -------------------------------------------------------------------------

    def _request(
        self,
        method: str,
        path: str,
        json_body: Any = None,
        params: Optional[Dict[str, Any]] = None,
    ) -> Any:
        """Execute an HTTP request and return parsed JSON."""
        url = f"{self.server_url}{path}"

        resp = self._session.request(
            method,
            url,
            json=json_body,
            params=params,
            timeout=self.timeout,
        )

        self._check_response(resp)

        if resp.status_code == 204 or not resp.content:
            return {}

        return resp.json()

    @staticmethod
    def _check_response(resp: requests.Response) -> None:
        """Raise the appropriate FlowForgeError for non-2xx responses."""
        if resp.ok:
            return

        message = ""
        code = ""
        try:
            body = resp.json()
            message = body.get("message", "")
            code = body.get("code", "")
        except (ValueError, KeyError):
            message = resp.text or resp.reason or "Unknown error"

        if not message:
            message = resp.reason or "Unknown error"

        status = resp.status_code
        error_map = {
            401: UnauthorizedError,
            403: UnauthorizedError,
            404: NotFoundError,
            400: BadRequestError,
            422: BadRequestError,
            408: TimeoutError,
            409: ConflictError,
            504: TimeoutError,
        }

        error_cls = error_map.get(status, ServerError if status >= 500 else FlowForgeError)
        raise error_cls(message, status_code=status, code=code)
