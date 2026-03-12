// Package definition provides YAML/JSON workflow definition parsing and validation.
package definition

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/engine/dag"
	"gopkg.in/yaml.v3"
)

// WorkflowDefinition represents a complete workflow specification.
type WorkflowDefinition struct {
	ID          string            `json:"id" yaml:"id"`
	Name        string            `json:"name" yaml:"name"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Version     int               `json:"version" yaml:"version"`
	Triggers    []TriggerDefinition `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	Tasks       []TaskDefinition  `json:"tasks" yaml:"tasks"`
	OnFailure   *FailureAction    `json:"on_failure,omitempty" yaml:"on_failure,omitempty"`
	Timeout     Duration          `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// TaskDefinition represents a single task within a workflow.
type TaskDefinition struct {
	ID        string         `json:"id" yaml:"id"`
	Name      string         `json:"name" yaml:"name"`
	Type      string         `json:"type" yaml:"type"`
	Config    map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	DependsOn []string       `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Timeout   Duration       `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retry     *RetryConfig   `json:"retry,omitempty" yaml:"retry,omitempty"`
	Condition string         `json:"condition,omitempty" yaml:"condition,omitempty"`
}

// TriggerDefinition specifies how a workflow is triggered.
type TriggerDefinition struct {
	Type   string         `json:"type" yaml:"type"`
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// RetryConfig specifies retry behavior for a task.
type RetryConfig struct {
	MaxRetries   int      `json:"max_retries" yaml:"max_retries"`
	InitialDelay Duration `json:"initial_delay,omitempty" yaml:"initial_delay,omitempty"`
	MaxDelay     Duration `json:"max_delay,omitempty" yaml:"max_delay,omitempty"`
	Multiplier   float64  `json:"multiplier,omitempty" yaml:"multiplier,omitempty"`
	Strategy     string   `json:"strategy,omitempty" yaml:"strategy,omitempty"`
}

// FailureAction defines what happens when a workflow fails.
type FailureAction struct {
	Action      string         `json:"action" yaml:"action"`
	NotifyEmail string         `json:"notify_email,omitempty" yaml:"notify_email,omitempty"`
	RetryAll    bool           `json:"retry_all,omitempty" yaml:"retry_all,omitempty"`
	Webhook     string         `json:"webhook,omitempty" yaml:"webhook,omitempty"`
	Config      map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// Duration is a wrapper around time.Duration that supports YAML/JSON marshaling
// from human-readable strings like "30s", "5m", "1h".
type Duration struct {
	time.Duration
}

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch val := v.(type) {
	case float64:
		d.Duration = time.Duration(int64(val))
		return nil
	case string:
		if val == "" {
			d.Duration = 0
			return nil
		}
		dur, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", val, err)
		}
		d.Duration = dur
		return nil
	default:
		return fmt.Errorf("invalid duration type %T", v)
	}
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (interface{}, error) {
	if d.Duration == 0 {
		return "", nil
	}
	return d.Duration.String(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		// Try as integer (nanoseconds).
		var n int64
		if err2 := value.Decode(&n); err2 != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		d.Duration = time.Duration(n)
		return nil
	}
	if s == "" {
		d.Duration = 0
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

// Parse parses a workflow definition from the given data in the specified format.
// Supported formats: "yaml", "yml", "json".
func Parse(data []byte, format string) (*WorkflowDefinition, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty workflow definition data")
	}

	def := &WorkflowDefinition{}

	switch strings.ToLower(format) {
	case "yaml", "yml":
		if err := yaml.Unmarshal(data, def); err != nil {
			return nil, fmt.Errorf("failed to parse YAML workflow definition: %w", err)
		}
	case "json":
		if err := json.Unmarshal(data, def); err != nil {
			return nil, fmt.Errorf("failed to parse JSON workflow definition: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported format %q: expected yaml, yml, or json", format)
	}

	return def, nil
}

// validTriggerTypes lists all supported trigger types.
var validTriggerTypes = map[string]bool{
	"cron":    true,
	"webhook": true,
	"event":   true,
	"manual":  true,
}

// validTaskTypes lists all supported task types.
var validTaskTypes = map[string]bool{
	"http":      true,
	"script":    true,
	"condition": true,
	"parallel":  true,
	"delay":     true,
	"plugin":    true,
	"approval":  true,
	"notify":    true,
	"transform": true,
	"subflow":   true,
}

// Validate validates a workflow definition for correctness.
func Validate(def *WorkflowDefinition) error {
	if def == nil {
		return fmt.Errorf("workflow definition is nil")
	}

	var errs []string

	// Validate required fields.
	if def.ID == "" {
		errs = append(errs, "workflow ID is required")
	}
	if def.Name == "" {
		errs = append(errs, "workflow name is required")
	}
	if def.Version < 1 {
		errs = append(errs, "workflow version must be >= 1")
	}
	if len(def.Tasks) == 0 {
		errs = append(errs, "workflow must have at least one task")
	}

	// Validate triggers.
	for i, trigger := range def.Triggers {
		if trigger.Type == "" {
			errs = append(errs, fmt.Sprintf("trigger[%d]: type is required", i))
		} else if !validTriggerTypes[trigger.Type] {
			errs = append(errs, fmt.Sprintf("trigger[%d]: unsupported type %q", i, trigger.Type))
		}

		// Validate trigger-specific config.
		switch trigger.Type {
		case "cron":
			if _, ok := trigger.Config["schedule"]; !ok {
				errs = append(errs, fmt.Sprintf("trigger[%d]: cron trigger requires 'schedule' in config", i))
			}
		case "webhook":
			if _, ok := trigger.Config["path"]; !ok {
				errs = append(errs, fmt.Sprintf("trigger[%d]: webhook trigger requires 'path' in config", i))
			}
		case "event":
			if _, ok := trigger.Config["pattern"]; !ok {
				errs = append(errs, fmt.Sprintf("trigger[%d]: event trigger requires 'pattern' in config", i))
			}
		}
	}

	// Build a set of task IDs for reference checking.
	taskIDs := make(map[string]bool)
	for _, task := range def.Tasks {
		if task.ID == "" {
			errs = append(errs, "task ID is required for all tasks")
			continue
		}
		if taskIDs[task.ID] {
			errs = append(errs, fmt.Sprintf("duplicate task ID %q", task.ID))
		}
		taskIDs[task.ID] = true
	}

	// Validate each task.
	for i, task := range def.Tasks {
		prefix := fmt.Sprintf("task[%d] (%s)", i, task.ID)

		if task.Name == "" {
			errs = append(errs, fmt.Sprintf("%s: name is required", prefix))
		}
		if task.Type == "" {
			errs = append(errs, fmt.Sprintf("%s: type is required", prefix))
		} else if !validTaskTypes[task.Type] {
			errs = append(errs, fmt.Sprintf("%s: unsupported type %q", prefix, task.Type))
		}

		// Validate dependency references.
		for _, dep := range task.DependsOn {
			if !taskIDs[dep] {
				errs = append(errs, fmt.Sprintf("%s: depends on non-existent task %q", prefix, dep))
			}
			if dep == task.ID {
				errs = append(errs, fmt.Sprintf("%s: task cannot depend on itself", prefix))
			}
		}

		// Validate retry config.
		if task.Retry != nil {
			if task.Retry.MaxRetries < 0 {
				errs = append(errs, fmt.Sprintf("%s: max_retries must be >= 0", prefix))
			}
			if task.Retry.Multiplier < 0 {
				errs = append(errs, fmt.Sprintf("%s: retry multiplier must be >= 0", prefix))
			}
			if task.Retry.Strategy != "" {
				validStrategies := map[string]bool{"fixed": true, "exponential": true, "linear": true}
				if !validStrategies[task.Retry.Strategy] {
					errs = append(errs, fmt.Sprintf("%s: unsupported retry strategy %q", prefix, task.Retry.Strategy))
				}
			}
		}
	}

	// Check for circular dependencies using a simple DFS.
	if len(errs) == 0 {
		if err := checkCycles(def.Tasks); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("workflow validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// checkCycles detects circular dependencies among tasks.
func checkCycles(tasks []TaskDefinition) error {
	const (
		white = 0
		gray  = 1
		black = 2
	)

	adj := make(map[string][]string)
	colors := make(map[string]int)

	for _, task := range tasks {
		colors[task.ID] = white
		for _, dep := range task.DependsOn {
			// Edge from dependency to dependent (dep -> task.ID),
			// meaning task.ID runs after dep.
			adj[dep] = append(adj[dep], task.ID)
		}
	}

	var dfs func(string) error
	dfs = func(nodeID string) error {
		colors[nodeID] = gray
		for _, neighbor := range adj[nodeID] {
			if colors[neighbor] == gray {
				return fmt.Errorf("circular dependency detected involving tasks %q and %q", nodeID, neighbor)
			}
			if colors[neighbor] == white {
				if err := dfs(neighbor); err != nil {
					return err
				}
			}
		}
		colors[nodeID] = black
		return nil
	}

	for _, task := range tasks {
		if colors[task.ID] == white {
			if err := dfs(task.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

// ToDAG converts a WorkflowDefinition to a DAG for execution.
func ToDAG(def *WorkflowDefinition) (*dag.DAG, error) {
	if def == nil {
		return nil, fmt.Errorf("workflow definition is nil")
	}

	if err := Validate(def); err != nil {
		return nil, fmt.Errorf("invalid workflow definition: %w", err)
	}

	d := dag.NewDAG(def.ID, def.Name)

	// Add all tasks as nodes.
	for _, task := range def.Tasks {
		node := &dag.Node{
			ID:        task.ID,
			Name:      task.Name,
			Type:      task.Type,
			Config:    task.Config,
			DependsOn: task.DependsOn,
			Timeout:   task.Timeout.Duration,
		}

		if task.Retry != nil {
			node.RetryPolicy = &dag.RetryPolicy{
				MaxRetries:   task.Retry.MaxRetries,
				InitialDelay: task.Retry.InitialDelay.Duration,
				MaxDelay:     task.Retry.MaxDelay.Duration,
				Multiplier:   task.Retry.Multiplier,
			}
		}

		if err := d.AddNode(node); err != nil {
			return nil, fmt.Errorf("failed to add node %s: %w", task.ID, err)
		}
	}

	// Add edges based on dependencies.
	for _, task := range def.Tasks {
		for _, dep := range task.DependsOn {
			edge := dag.Edge{
				From:      dep,
				To:        task.ID,
				Condition: task.Condition,
			}
			if err := d.AddEdge(edge); err != nil {
				return nil, fmt.Errorf("failed to add edge %s -> %s: %w", dep, task.ID, err)
			}
		}
	}

	// Validate the resulting DAG.
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("DAG validation failed: %w", err)
	}

	return d, nil
}

// ParseAndValidate is a convenience function that parses and validates in one call.
func ParseAndValidate(data []byte, format string) (*WorkflowDefinition, error) {
	def, err := Parse(data, format)
	if err != nil {
		return nil, err
	}
	if err := Validate(def); err != nil {
		return nil, err
	}
	return def, nil
}

// ToJSON serializes a WorkflowDefinition to JSON.
func ToJSON(def *WorkflowDefinition) ([]byte, error) {
	return json.MarshalIndent(def, "", "  ")
}

// ToYAML serializes a WorkflowDefinition to YAML.
func ToYAML(def *WorkflowDefinition) ([]byte, error) {
	return yaml.Marshal(def)
}
