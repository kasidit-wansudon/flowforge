package definition

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func validWorkflowDef() *WorkflowDefinition {
	return &WorkflowDefinition{
		ID:      "wf-1",
		Name:    "Test Workflow",
		Version: 1,
		Tasks: []TaskDefinition{
			{
				ID:   "task-1",
				Name: "First Task",
				Type: "http",
			},
		},
	}
}

func TestParseYAML(t *testing.T) {
	yamlData := []byte(`
id: wf-1
name: Test Workflow
version: 1
tasks:
  - id: task-1
    name: First Task
    type: http
    config:
      url: "https://example.com"
`)
	def, err := Parse(yamlData, "yaml")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if def.ID != "wf-1" {
		t.Errorf("ID = %q, want %q", def.ID, "wf-1")
	}
	if def.Name != "Test Workflow" {
		t.Errorf("Name = %q, want %q", def.Name, "Test Workflow")
	}
	if len(def.Tasks) != 1 {
		t.Fatalf("len(Tasks) = %d, want 1", len(def.Tasks))
	}
	if def.Tasks[0].Type != "http" {
		t.Errorf("Tasks[0].Type = %q, want %q", def.Tasks[0].Type, "http")
	}
}

func TestParseYMLFormat(t *testing.T) {
	yamlData := []byte(`
id: wf-1
name: Test
version: 1
tasks:
  - id: t1
    name: T1
    type: script
`)
	def, err := Parse(yamlData, "yml")
	if err != nil {
		t.Fatalf("Parse(yml) error: %v", err)
	}
	if def.ID != "wf-1" {
		t.Errorf("ID = %q, want %q", def.ID, "wf-1")
	}
}

func TestParseJSON(t *testing.T) {
	jsonData := []byte(`{
		"id": "wf-2",
		"name": "JSON Workflow",
		"version": 1,
		"tasks": [
			{"id": "task-1", "name": "Task One", "type": "http"}
		]
	}`)
	def, err := Parse(jsonData, "json")
	if err != nil {
		t.Fatalf("Parse(json) error: %v", err)
	}
	if def.ID != "wf-2" {
		t.Errorf("ID = %q, want %q", def.ID, "wf-2")
	}
}

func TestParseUnsupportedFormat(t *testing.T) {
	_, err := Parse([]byte("data"), "xml")
	if err == nil {
		t.Fatal("Parse(xml) should return error")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("error = %q, want to contain 'unsupported format'", err.Error())
	}
}

func TestParseEmptyData(t *testing.T) {
	_, err := Parse([]byte{}, "yaml")
	if err == nil {
		t.Fatal("Parse(empty) should return error")
	}
}

func TestParseInvalidYAML(t *testing.T) {
	_, err := Parse([]byte(":::invalid"), "yaml")
	if err == nil {
		t.Fatal("Parse(invalid yaml) should return error")
	}
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := Parse([]byte("{invalid}"), "json")
	if err == nil {
		t.Fatal("Parse(invalid json) should return error")
	}
}

func TestValidateValid(t *testing.T) {
	def := validWorkflowDef()
	if err := Validate(def); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
}

func TestValidateNilDef(t *testing.T) {
	err := Validate(nil)
	if err == nil {
		t.Fatal("Validate(nil) should return error")
	}
}

func TestValidateMissingID(t *testing.T) {
	def := validWorkflowDef()
	def.ID = ""
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail when ID is empty")
	}
	if !strings.Contains(err.Error(), "workflow ID is required") {
		t.Errorf("error = %q, want to mention workflow ID", err.Error())
	}
}

func TestValidateMissingName(t *testing.T) {
	def := validWorkflowDef()
	def.Name = ""
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail when Name is empty")
	}
	if !strings.Contains(err.Error(), "workflow name is required") {
		t.Errorf("error = %q, want to mention workflow name", err.Error())
	}
}

func TestValidateInvalidVersion(t *testing.T) {
	def := validWorkflowDef()
	def.Version = 0
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail when Version < 1")
	}
}

func TestValidateNoTasks(t *testing.T) {
	def := validWorkflowDef()
	def.Tasks = nil
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail when no tasks")
	}
	if !strings.Contains(err.Error(), "at least one task") {
		t.Errorf("error = %q, want to mention at least one task", err.Error())
	}
}

func TestValidateDuplicateTaskID(t *testing.T) {
	def := &WorkflowDefinition{
		ID:      "wf-1",
		Name:    "Test",
		Version: 1,
		Tasks: []TaskDefinition{
			{ID: "t1", Name: "Task 1", Type: "http"},
			{ID: "t1", Name: "Task 2", Type: "http"},
		},
	}
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail with duplicate task IDs")
	}
	if !strings.Contains(err.Error(), "duplicate task ID") {
		t.Errorf("error = %q, want to mention duplicate task ID", err.Error())
	}
}

func TestValidateInvalidTaskType(t *testing.T) {
	def := &WorkflowDefinition{
		ID:      "wf-1",
		Name:    "Test",
		Version: 1,
		Tasks: []TaskDefinition{
			{ID: "t1", Name: "Task 1", Type: "invalid_type"},
		},
	}
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail with invalid task type")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("error = %q, want to mention unsupported type", err.Error())
	}
}

func TestValidateInvalidDependencyRef(t *testing.T) {
	def := &WorkflowDefinition{
		ID:      "wf-1",
		Name:    "Test",
		Version: 1,
		Tasks: []TaskDefinition{
			{ID: "t1", Name: "Task 1", Type: "http", DependsOn: []string{"non-existent"}},
		},
	}
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail with invalid dependency ref")
	}
	if !strings.Contains(err.Error(), "non-existent") {
		t.Errorf("error = %q, want to mention the missing dependency", err.Error())
	}
}

func TestValidateSelfDependency(t *testing.T) {
	def := &WorkflowDefinition{
		ID:      "wf-1",
		Name:    "Test",
		Version: 1,
		Tasks: []TaskDefinition{
			{ID: "t1", Name: "Task 1", Type: "http", DependsOn: []string{"t1"}},
		},
	}
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail with self-dependency")
	}
	if !strings.Contains(err.Error(), "depend on itself") {
		t.Errorf("error = %q, want to mention self-dependency", err.Error())
	}
}

func TestValidateCircularDependency(t *testing.T) {
	def := &WorkflowDefinition{
		ID:      "wf-1",
		Name:    "Test",
		Version: 1,
		Tasks: []TaskDefinition{
			{ID: "t1", Name: "Task 1", Type: "http", DependsOn: []string{"t2"}},
			{ID: "t2", Name: "Task 2", Type: "http", DependsOn: []string{"t1"}},
		},
	}
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail with circular dependency")
	}
	if !strings.Contains(err.Error(), "circular dependency") {
		t.Errorf("error = %q, want to mention circular dependency", err.Error())
	}
}

func TestValidateRetryConfig(t *testing.T) {
	def := &WorkflowDefinition{
		ID:      "wf-1",
		Name:    "Test",
		Version: 1,
		Tasks: []TaskDefinition{
			{
				ID:   "t1",
				Name: "Task 1",
				Type: "http",
				Retry: &RetryConfig{
					MaxRetries: -1,
				},
			},
		},
	}
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail with negative max_retries")
	}
	if !strings.Contains(err.Error(), "max_retries") {
		t.Errorf("error = %q, want to mention max_retries", err.Error())
	}
}

func TestValidateRetryStrategy(t *testing.T) {
	def := &WorkflowDefinition{
		ID:      "wf-1",
		Name:    "Test",
		Version: 1,
		Tasks: []TaskDefinition{
			{
				ID:   "t1",
				Name: "Task 1",
				Type: "http",
				Retry: &RetryConfig{
					MaxRetries: 3,
					Strategy:   "invalid_strategy",
				},
			},
		},
	}
	err := Validate(def)
	if err == nil {
		t.Fatal("Validate() should fail with invalid retry strategy")
	}
	if !strings.Contains(err.Error(), "unsupported retry strategy") {
		t.Errorf("error = %q, want to mention unsupported retry strategy", err.Error())
	}
}

func TestValidateTriggerTypes(t *testing.T) {
	tests := []struct {
		name    string
		trigger TriggerDefinition
		wantErr bool
	}{
		{"valid cron", TriggerDefinition{Type: "cron", Config: map[string]any{"schedule": "* * * * *"}}, false},
		{"valid webhook", TriggerDefinition{Type: "webhook", Config: map[string]any{"path": "/hook"}}, false},
		{"valid event", TriggerDefinition{Type: "event", Config: map[string]any{"pattern": "user.*"}}, false},
		{"valid manual", TriggerDefinition{Type: "manual"}, false},
		{"invalid type", TriggerDefinition{Type: "invalid"}, true},
		{"cron missing schedule", TriggerDefinition{Type: "cron", Config: map[string]any{}}, true},
		{"webhook missing path", TriggerDefinition{Type: "webhook", Config: map[string]any{}}, true},
		{"event missing pattern", TriggerDefinition{Type: "event", Config: map[string]any{}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := validWorkflowDef()
			def.Triggers = []TriggerDefinition{tt.trigger}
			err := Validate(def)
			if tt.wantErr && err == nil {
				t.Error("Validate() should have returned error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate() error: %v", err)
			}
		})
	}
}

func TestToDAG(t *testing.T) {
	def := &WorkflowDefinition{
		ID:      "wf-dag",
		Name:    "DAG Test",
		Version: 1,
		Tasks: []TaskDefinition{
			{ID: "t1", Name: "Task 1", Type: "http"},
			{ID: "t2", Name: "Task 2", Type: "script", DependsOn: []string{"t1"}},
			{ID: "t3", Name: "Task 3", Type: "http", DependsOn: []string{"t1"}},
		},
	}

	d, err := ToDAG(def)
	if err != nil {
		t.Fatalf("ToDAG() error: %v", err)
	}
	if d.ID != "wf-dag" {
		t.Errorf("DAG ID = %q, want %q", d.ID, "wf-dag")
	}
	if len(d.Nodes) != 3 {
		t.Errorf("len(Nodes) = %d, want 3", len(d.Nodes))
	}
	if len(d.Edges) != 2 {
		t.Errorf("len(Edges) = %d, want 2", len(d.Edges))
	}
}

func TestToDAGWithRetry(t *testing.T) {
	def := &WorkflowDefinition{
		ID:      "wf-retry",
		Name:    "Retry DAG",
		Version: 1,
		Tasks: []TaskDefinition{
			{
				ID:   "t1",
				Name: "Task 1",
				Type: "http",
				Retry: &RetryConfig{
					MaxRetries:   3,
					InitialDelay: Duration{5 * time.Second},
					MaxDelay:     Duration{30 * time.Second},
					Multiplier:   2.0,
				},
			},
		},
	}

	d, err := ToDAG(def)
	if err != nil {
		t.Fatalf("ToDAG() error: %v", err)
	}
	node := d.Nodes["t1"]
	if node.RetryPolicy == nil {
		t.Fatal("node.RetryPolicy is nil")
	}
	if node.RetryPolicy.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", node.RetryPolicy.MaxRetries)
	}
}

func TestToDAGNilDef(t *testing.T) {
	_, err := ToDAG(nil)
	if err == nil {
		t.Fatal("ToDAG(nil) should return error")
	}
}

func TestToDAGInvalidDef(t *testing.T) {
	def := &WorkflowDefinition{} // missing required fields
	_, err := ToDAG(def)
	if err == nil {
		t.Fatal("ToDAG(invalid) should return error")
	}
}

func TestParseAndValidate(t *testing.T) {
	yamlData := []byte(`
id: wf-1
name: Test Workflow
version: 1
tasks:
  - id: task-1
    name: First Task
    type: http
`)
	def, err := ParseAndValidate(yamlData, "yaml")
	if err != nil {
		t.Fatalf("ParseAndValidate() error: %v", err)
	}
	if def.ID != "wf-1" {
		t.Errorf("ID = %q, want %q", def.ID, "wf-1")
	}
}

func TestParseAndValidateInvalid(t *testing.T) {
	yamlData := []byte(`
id: ""
name: ""
version: 0
tasks: []
`)
	_, err := ParseAndValidate(yamlData, "yaml")
	if err == nil {
		t.Fatal("ParseAndValidate() should fail for invalid definition")
	}
}

func TestToJSON(t *testing.T) {
	def := validWorkflowDef()
	data, err := ToJSON(def)
	if err != nil {
		t.Fatalf("ToJSON() error: %v", err)
	}

	var parsed WorkflowDefinition
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if parsed.ID != def.ID {
		t.Errorf("round-trip ID = %q, want %q", parsed.ID, def.ID)
	}
}

func TestToYAML(t *testing.T) {
	def := validWorkflowDef()
	data, err := ToYAML(def)
	if err != nil {
		t.Fatalf("ToYAML() error: %v", err)
	}

	var parsed WorkflowDefinition
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal() error: %v", err)
	}
	if parsed.ID != def.ID {
		t.Errorf("round-trip ID = %q, want %q", parsed.ID, def.ID)
	}
}

func TestDurationJSONMarshal(t *testing.T) {
	d := Duration{30 * time.Second}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	if string(data) != `"30s"` {
		t.Errorf("json.Marshal(Duration{30s}) = %s, want %q", string(data), "30s")
	}
}

func TestDurationJSONUnmarshalString(t *testing.T) {
	var d Duration
	err := json.Unmarshal([]byte(`"5m"`), &d)
	if err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if d.Duration != 5*time.Minute {
		t.Errorf("Duration = %v, want 5m", d.Duration)
	}
}

func TestDurationJSONUnmarshalNumber(t *testing.T) {
	var d Duration
	err := json.Unmarshal([]byte(`1000000000`), &d) // 1 second in nanoseconds
	if err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if d.Duration != time.Second {
		t.Errorf("Duration = %v, want 1s", d.Duration)
	}
}

func TestDurationJSONUnmarshalEmpty(t *testing.T) {
	var d Duration
	err := json.Unmarshal([]byte(`""`), &d)
	if err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if d.Duration != 0 {
		t.Errorf("Duration = %v, want 0", d.Duration)
	}
}

func TestDurationJSONUnmarshalInvalid(t *testing.T) {
	var d Duration
	err := json.Unmarshal([]byte(`"not-a-duration"`), &d)
	if err == nil {
		t.Fatal("json.Unmarshal(invalid) should return error")
	}
}

func TestDurationYAMLRoundTrip(t *testing.T) {
	d := Duration{1 * time.Hour}
	data, err := yaml.Marshal(d)
	if err != nil {
		t.Fatalf("yaml.Marshal() error: %v", err)
	}

	var parsed Duration
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal() error: %v", err)
	}
	if parsed.Duration != d.Duration {
		t.Errorf("round-trip Duration = %v, want %v", parsed.Duration, d.Duration)
	}
}

func TestValidWorkflowWithDependencies(t *testing.T) {
	def := &WorkflowDefinition{
		ID:      "wf-deps",
		Name:    "Dependency Workflow",
		Version: 1,
		Tasks: []TaskDefinition{
			{ID: "fetch", Name: "Fetch Data", Type: "http"},
			{ID: "transform", Name: "Transform", Type: "transform", DependsOn: []string{"fetch"}},
			{ID: "notify", Name: "Notify", Type: "notify", DependsOn: []string{"transform"}},
		},
	}

	if err := Validate(def); err != nil {
		t.Fatalf("Validate() error for valid workflow with dependencies: %v", err)
	}
}

func TestAllValidTaskTypes(t *testing.T) {
	types := []string{"http", "script", "condition", "parallel", "delay", "plugin", "approval", "notify", "transform", "subflow"}
	for _, typ := range types {
		def := &WorkflowDefinition{
			ID:      "wf-" + typ,
			Name:    "Test " + typ,
			Version: 1,
			Tasks: []TaskDefinition{
				{ID: "t1", Name: "Task", Type: typ},
			},
		}
		if err := Validate(def); err != nil {
			t.Errorf("Validate() failed for valid task type %q: %v", typ, err)
		}
	}
}
