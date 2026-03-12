package template_test

import (
	"testing"

	"github.com/kasidit-wansudon/flowforge/internal/workflow/definition"
	"github.com/kasidit-wansudon/flowforge/internal/workflow/template"
)

// --- NewTemplateRegistry includes built-ins ---

func TestNewTemplateRegistry_HasBuiltinTemplates(t *testing.T) {
	reg := template.NewTemplateRegistry()
	all := reg.List()

	builtins := map[string]bool{
		"ci-pipeline":    false,
		"data-etl":       false,
		"notification":   false,
		"approval-chain": false,
	}
	for _, tmpl := range all {
		builtins[tmpl.Name] = true
	}
	for name, found := range builtins {
		if !found {
			t.Errorf("built-in template %q not registered", name)
		}
	}
}

// --- Register ---

func TestTemplateRegistry_Register_Success(t *testing.T) {
	reg := template.NewTemplateRegistry()
	tmpl := &template.Template{
		Name:        "my-custom-template",
		Description: "A custom workflow",
		Category:    "test",
		Definition: &definition.WorkflowDefinition{
			ID:   "custom",
			Name: "Custom Workflow",
			Tasks: []definition.TaskDefinition{
				{ID: "t1", Name: "Task 1", Type: "script"},
			},
		},
	}
	if err := reg.Register(tmpl); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
}

func TestTemplateRegistry_Register_DuplicateReturnsError(t *testing.T) {
	reg := template.NewTemplateRegistry()
	tmpl := &template.Template{
		Name:     "duplicate",
		Category: "test",
		Definition: &definition.WorkflowDefinition{
			ID:    "dup",
			Name:  "Dup",
			Tasks: []definition.TaskDefinition{{ID: "t1", Name: "T1", Type: "script"}},
		},
	}
	if err := reg.Register(tmpl); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	if err := reg.Register(tmpl); err == nil {
		t.Error("expected error registering duplicate template")
	}
}

func TestTemplateRegistry_Register_NilReturnsError(t *testing.T) {
	reg := template.NewTemplateRegistry()
	if err := reg.Register(nil); err == nil {
		t.Error("expected error registering nil template")
	}
}

func TestTemplateRegistry_Register_MissingNameReturnsError(t *testing.T) {
	reg := template.NewTemplateRegistry()
	tmpl := &template.Template{
		Name:     "",
		Category: "test",
		Definition: &definition.WorkflowDefinition{
			ID:    "x",
			Name:  "X",
			Tasks: []definition.TaskDefinition{{ID: "t1", Name: "T1", Type: "script"}},
		},
	}
	if err := reg.Register(tmpl); err == nil {
		t.Error("expected error for empty template name")
	}
}

// --- Get ---

func TestTemplateRegistry_Get_BuiltinCIPipeline(t *testing.T) {
	reg := template.NewTemplateRegistry()
	tmpl, err := reg.Get("ci-pipeline")
	if err != nil {
		t.Fatalf("Get ci-pipeline failed: %v", err)
	}
	if tmpl.Name != "ci-pipeline" {
		t.Errorf("expected name %q, got %q", "ci-pipeline", tmpl.Name)
	}
	if tmpl.Definition == nil {
		t.Fatal("definition is nil")
	}
	if len(tmpl.Definition.Tasks) == 0 {
		t.Error("ci-pipeline template should have tasks")
	}
}

func TestTemplateRegistry_Get_NotFound(t *testing.T) {
	reg := template.NewTemplateRegistry()
	_, err := reg.Get("nonexistent-template")
	if err == nil {
		t.Error("expected error for unknown template")
	}
}

// --- List ---

func TestTemplateRegistry_List_SortedByName(t *testing.T) {
	reg := template.NewTemplateRegistry()
	all := reg.List()
	for i := 1; i < len(all); i++ {
		if all[i-1].Name >= all[i].Name {
			t.Errorf("list not sorted: %q >= %q at index %d", all[i-1].Name, all[i].Name, i)
		}
	}
}

// --- GetByCategory ---

func TestTemplateRegistry_GetByCategory_CICD(t *testing.T) {
	reg := template.NewTemplateRegistry()
	ciTemplates := reg.GetByCategory("ci-cd")
	if len(ciTemplates) == 0 {
		t.Error("expected at least one template in ci-cd category")
	}
	for _, tmpl := range ciTemplates {
		if tmpl.Category != "ci-cd" {
			t.Errorf("template %q has wrong category %q, expected ci-cd", tmpl.Name, tmpl.Category)
		}
	}
}

func TestTemplateRegistry_GetByCategory_CaseInsensitive(t *testing.T) {
	reg := template.NewTemplateRegistry()
	lower := reg.GetByCategory("ci-cd")
	upper := reg.GetByCategory("CI-CD")
	if len(lower) != len(upper) {
		t.Errorf("case insensitive category lookup mismatch: %d vs %d", len(lower), len(upper))
	}
}

func TestTemplateRegistry_GetByCategory_Unknown(t *testing.T) {
	reg := template.NewTemplateRegistry()
	result := reg.GetByCategory("totally-unknown-category")
	if len(result) != 0 {
		t.Errorf("expected empty slice for unknown category, got %d", len(result))
	}
}

// --- Built-in template content ---

func TestBuiltinTemplate_DataETL_HasCronTrigger(t *testing.T) {
	reg := template.NewTemplateRegistry()
	tmpl, err := reg.Get("data-etl")
	if err != nil {
		t.Fatalf("Get data-etl failed: %v", err)
	}
	if len(tmpl.Definition.Triggers) == 0 {
		t.Fatal("data-etl should have triggers")
	}
	found := false
	for _, tr := range tmpl.Definition.Triggers {
		if tr.Type == "cron" {
			found = true
			break
		}
	}
	if !found {
		t.Error("data-etl template should have a cron trigger")
	}
}

func TestBuiltinTemplate_ApprovalChain_HasMetadata(t *testing.T) {
	reg := template.NewTemplateRegistry()
	tmpl, err := reg.Get("approval-chain")
	if err != nil {
		t.Fatalf("Get approval-chain failed: %v", err)
	}
	if tmpl.Definition.Metadata == nil {
		t.Fatal("approval-chain metadata should not be nil")
	}
	if tmpl.Definition.Metadata["template"] != "approval-chain" {
		t.Errorf("expected metadata[template]=approval-chain, got %q", tmpl.Definition.Metadata["template"])
	}
}

func TestBuiltinTemplate_Notification_HasMultipleTriggers(t *testing.T) {
	reg := template.NewTemplateRegistry()
	tmpl, err := reg.Get("notification")
	if err != nil {
		t.Fatalf("Get notification failed: %v", err)
	}
	if len(tmpl.Definition.Triggers) < 2 {
		t.Errorf("notification template should have at least 2 triggers, got %d", len(tmpl.Definition.Triggers))
	}
}
