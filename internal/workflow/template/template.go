// Package template provides built-in workflow templates for common patterns.
package template

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/kasidit-wansudon/flowforge/internal/workflow/definition"
)

// Template represents a reusable workflow template.
type Template struct {
	Name        string                           `json:"name"`
	Description string                           `json:"description"`
	Category    string                           `json:"category"`
	Definition  *definition.WorkflowDefinition   `json:"definition"`
}

// TemplateRegistry manages a collection of workflow templates.
type TemplateRegistry struct {
	mu        sync.RWMutex
	templates map[string]*Template
}

// NewTemplateRegistry creates a new TemplateRegistry with built-in templates pre-registered.
func NewTemplateRegistry() *TemplateRegistry {
	r := &TemplateRegistry{
		templates: make(map[string]*Template),
	}
	r.registerBuiltins()
	return r
}

// Register adds a template to the registry. Returns an error if a template
// with the same name already exists.
func (r *TemplateRegistry) Register(t *Template) error {
	if t == nil {
		return fmt.Errorf("template is nil")
	}
	if t.Name == "" {
		return fmt.Errorf("template name is required")
	}
	if t.Definition == nil {
		return fmt.Errorf("template definition is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.templates[t.Name]; exists {
		return fmt.Errorf("template %q already registered", t.Name)
	}

	r.templates[t.Name] = t
	return nil
}

// Get retrieves a template by name.
func (r *TemplateRegistry) Get(name string) (*Template, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.templates[name]
	if !ok {
		return nil, fmt.Errorf("template %q not found", name)
	}
	return t, nil
}

// List returns all registered templates sorted by name.
func (r *TemplateRegistry) List() []*Template {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Template, 0, len(r.templates))
	for _, t := range r.templates {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// GetByCategory returns all templates in the given category.
func (r *TemplateRegistry) GetByCategory(category string) []*Template {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*Template
	lowerCat := strings.ToLower(category)
	for _, t := range r.templates {
		if strings.ToLower(t.Category) == lowerCat {
			result = append(result, t)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// registerBuiltins registers all built-in workflow templates.
func (r *TemplateRegistry) registerBuiltins() {
	builtins := []*Template{
		ciPipelineTemplate(),
		dataETLTemplate(),
		notificationTemplate(),
		approvalChainTemplate(),
	}

	for _, t := range builtins {
		r.templates[t.Name] = t
	}
}

// ciPipelineTemplate creates a CI pipeline workflow template.
func ciPipelineTemplate() *Template {
	return &Template{
		Name:        "ci-pipeline",
		Description: "Continuous integration pipeline with build, test, lint, and deploy stages",
		Category:    "ci-cd",
		Definition: &definition.WorkflowDefinition{
			ID:          "ci-pipeline",
			Name:        "CI Pipeline",
			Description: "Standard CI pipeline: checkout, build, test, lint, and deploy",
			Version:     1,
			Triggers: []definition.TriggerDefinition{
				{
					Type: "webhook",
					Config: map[string]any{
						"path":   "/hooks/ci",
						"method": "POST",
						"secret": "${CI_WEBHOOK_SECRET}",
					},
				},
			},
			Tasks: []definition.TaskDefinition{
				{
					ID:   "checkout",
					Name: "Checkout Code",
					Type: "script",
					Config: map[string]any{
						"language": "bash",
						"script":   "git clone ${REPO_URL} workspace && cd workspace && git checkout ${COMMIT_SHA}",
					},
					Timeout: definition.Duration{},
				},
				{
					ID:   "install-deps",
					Name: "Install Dependencies",
					Type: "script",
					Config: map[string]any{
						"language": "bash",
						"script":   "cd workspace && make deps",
					},
					DependsOn: []string{"checkout"},
				},
				{
					ID:   "lint",
					Name: "Run Linter",
					Type: "script",
					Config: map[string]any{
						"language": "bash",
						"script":   "cd workspace && make lint",
					},
					DependsOn: []string{"install-deps"},
					Retry: &definition.RetryConfig{
						MaxRetries: 1,
						Strategy:   "fixed",
					},
				},
				{
					ID:   "test",
					Name: "Run Tests",
					Type: "script",
					Config: map[string]any{
						"language": "bash",
						"script":   "cd workspace && make test",
					},
					DependsOn: []string{"install-deps"},
					Retry: &definition.RetryConfig{
						MaxRetries: 2,
						Strategy:   "fixed",
					},
				},
				{
					ID:   "build",
					Name: "Build Artifacts",
					Type: "script",
					Config: map[string]any{
						"language": "bash",
						"script":   "cd workspace && make build",
					},
					DependsOn: []string{"lint", "test"},
				},
				{
					ID:   "deploy-check",
					Name: "Check Deploy Conditions",
					Type: "condition",
					Config: map[string]any{
						"expression": "branch == main",
						"on_true":    "deploy",
						"on_false":   "",
					},
					DependsOn: []string{"build"},
				},
				{
					ID:   "deploy",
					Name: "Deploy to Staging",
					Type: "script",
					Config: map[string]any{
						"language": "bash",
						"script":   "cd workspace && make deploy-staging",
					},
					DependsOn: []string{"deploy-check"},
					Condition: "branch == main",
				},
				{
					ID:   "notify-result",
					Name: "Notify Build Result",
					Type: "http",
					Config: map[string]any{
						"url":    "${SLACK_WEBHOOK_URL}",
						"method": "POST",
						"headers": map[string]any{
							"Content-Type": "application/json",
						},
						"body": `{"text": "CI Pipeline completed for ${COMMIT_SHA}"}`,
					},
					DependsOn: []string{"build"},
				},
			},
			OnFailure: &definition.FailureAction{
				Action:  "notify",
				Webhook: "${SLACK_WEBHOOK_URL}",
			},
			Metadata: map[string]string{
				"template": "ci-pipeline",
				"author":   "flowforge",
			},
		},
	}
}

// dataETLTemplate creates an ETL workflow template.
func dataETLTemplate() *Template {
	return &Template{
		Name:        "data-etl",
		Description: "Extract, transform, and load data pipeline with validation and error handling",
		Category:    "data",
		Definition: &definition.WorkflowDefinition{
			ID:          "data-etl",
			Name:        "Data ETL Pipeline",
			Description: "Extract data from source, transform, validate, and load to destination",
			Version:     1,
			Triggers: []definition.TriggerDefinition{
				{
					Type: "cron",
					Config: map[string]any{
						"schedule": "0 2 * * *",
						"timezone": "UTC",
					},
				},
			},
			Tasks: []definition.TaskDefinition{
				{
					ID:   "extract-source-a",
					Name: "Extract from Source A",
					Type: "http",
					Config: map[string]any{
						"url":    "${SOURCE_A_URL}",
						"method": "GET",
						"headers": map[string]any{
							"Authorization": "Bearer ${SOURCE_A_TOKEN}",
						},
						"timeout": "120s",
					},
					Retry: &definition.RetryConfig{
						MaxRetries: 3,
						Strategy:   "exponential",
						Multiplier: 2.0,
					},
				},
				{
					ID:   "extract-source-b",
					Name: "Extract from Source B",
					Type: "script",
					Config: map[string]any{
						"language": "python3",
						"script":   "import json, urllib.request; data = urllib.request.urlopen('${SOURCE_B_URL}').read(); print(data.decode())",
					},
					Retry: &definition.RetryConfig{
						MaxRetries: 3,
						Strategy:   "exponential",
						Multiplier: 2.0,
					},
				},
				{
					ID:   "validate-data",
					Name: "Validate Extracted Data",
					Type: "script",
					Config: map[string]any{
						"language": "python3",
						"script":   "import json, sys; data_a = json.loads('''${extract-source-a.output}'''); data_b = json.loads('''${extract-source-b.output}'''); assert len(data_a) > 0 and len(data_b) > 0, 'Empty data'; print('valid')",
					},
					DependsOn: []string{"extract-source-a", "extract-source-b"},
				},
				{
					ID:   "transform",
					Name: "Transform Data",
					Type: "script",
					Config: map[string]any{
						"language": "python3",
						"script":   "import json; data_a = json.loads('''${extract-source-a.output}'''); data_b = json.loads('''${extract-source-b.output}'''); merged = {**data_a, **data_b} if isinstance(data_a, dict) else data_a + data_b; print(json.dumps(merged))",
					},
					DependsOn: []string{"validate-data"},
				},
				{
					ID:   "load",
					Name: "Load to Destination",
					Type: "http",
					Config: map[string]any{
						"url":    "${DESTINATION_URL}",
						"method": "POST",
						"headers": map[string]any{
							"Content-Type":  "application/json",
							"Authorization": "Bearer ${DESTINATION_TOKEN}",
						},
						"body": "${transform.output}",
					},
					DependsOn: []string{"transform"},
					Retry: &definition.RetryConfig{
						MaxRetries: 2,
						Strategy:   "exponential",
						Multiplier: 2.0,
					},
				},
				{
					ID:   "notify-completion",
					Name: "Send Completion Notification",
					Type: "http",
					Config: map[string]any{
						"url":    "${NOTIFICATION_WEBHOOK}",
						"method": "POST",
						"body":   `{"status": "completed", "pipeline": "data-etl", "records_processed": "${load.output}"}`,
					},
					DependsOn: []string{"load"},
				},
			},
			OnFailure: &definition.FailureAction{
				Action:      "notify",
				NotifyEmail: "${ALERT_EMAIL}",
			},
			Metadata: map[string]string{
				"template": "data-etl",
				"author":   "flowforge",
			},
		},
	}
}

// notificationTemplate creates a multi-channel notification workflow template.
func notificationTemplate() *Template {
	return &Template{
		Name:        "notification",
		Description: "Multi-channel notification workflow with conditional routing and templating",
		Category:    "communication",
		Definition: &definition.WorkflowDefinition{
			ID:          "notification",
			Name:        "Multi-Channel Notification",
			Description: "Send notifications across multiple channels based on severity and preferences",
			Version:     1,
			Triggers: []definition.TriggerDefinition{
				{
					Type: "event",
					Config: map[string]any{
						"pattern": "notification.*",
					},
				},
				{
					Type: "manual",
					Config: map[string]any{
						"description": "Manually trigger a notification",
					},
				},
			},
			Tasks: []definition.TaskDefinition{
				{
					ID:   "classify",
					Name: "Classify Notification",
					Type: "condition",
					Config: map[string]any{
						"expression": "severity == critical",
						"on_true":    "urgent-path",
						"on_false":   "normal-path",
					},
				},
				{
					ID:   "send-slack",
					Name: "Send Slack Notification",
					Type: "http",
					Config: map[string]any{
						"url":    "${SLACK_WEBHOOK_URL}",
						"method": "POST",
						"headers": map[string]any{
							"Content-Type": "application/json",
						},
						"body": `{"channel": "${SLACK_CHANNEL}", "text": "${message}", "username": "FlowForge"}`,
					},
					DependsOn: []string{"classify"},
				},
				{
					ID:   "send-email",
					Name: "Send Email Notification",
					Type: "http",
					Config: map[string]any{
						"url":    "${EMAIL_API_URL}",
						"method": "POST",
						"headers": map[string]any{
							"Content-Type":  "application/json",
							"Authorization": "Bearer ${EMAIL_API_KEY}",
						},
						"body": `{"to": "${recipients}", "subject": "${subject}", "body": "${message}"}`,
					},
					DependsOn: []string{"classify"},
				},
				{
					ID:   "send-pagerduty",
					Name: "Send PagerDuty Alert",
					Type: "http",
					Config: map[string]any{
						"url":    "https://events.pagerduty.com/v2/enqueue",
						"method": "POST",
						"headers": map[string]any{
							"Content-Type": "application/json",
						},
						"body": `{"routing_key": "${PAGERDUTY_KEY}", "event_action": "trigger", "payload": {"summary": "${message}", "severity": "${severity}", "source": "flowforge"}}`,
					},
					DependsOn: []string{"classify"},
					Condition: "severity == critical",
				},
				{
					ID:   "log-notification",
					Name: "Log Notification",
					Type: "script",
					Config: map[string]any{
						"language": "bash",
						"script":   "echo '{\"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"type\": \"${type}\", \"severity\": \"${severity}\", \"message\": \"${message}\"}' >> /var/log/notifications.log",
					},
					DependsOn: []string{"send-slack", "send-email"},
				},
			},
			Metadata: map[string]string{
				"template": "notification",
				"author":   "flowforge",
			},
		},
	}
}

// approvalChainTemplate creates an approval chain workflow template.
func approvalChainTemplate() *Template {
	return &Template{
		Name:        "approval-chain",
		Description: "Multi-level approval chain with escalation and timeout handling",
		Category:    "governance",
		Definition: &definition.WorkflowDefinition{
			ID:          "approval-chain",
			Name:        "Approval Chain",
			Description: "Multi-level approval workflow with escalation, timeouts, and notifications",
			Version:     1,
			Triggers: []definition.TriggerDefinition{
				{
					Type: "event",
					Config: map[string]any{
						"pattern": "approval.requested.*",
					},
				},
				{
					Type: "manual",
					Config: map[string]any{
						"description": "Manually start an approval request",
					},
				},
			},
			Tasks: []definition.TaskDefinition{
				{
					ID:   "prepare-request",
					Name: "Prepare Approval Request",
					Type: "script",
					Config: map[string]any{
						"language": "bash",
						"script":   "echo '{\"request_id\": \"${request_id}\", \"requester\": \"${requester}\", \"type\": \"${approval_type}\", \"details\": \"${details}\"}'",
					},
				},
				{
					ID:   "notify-approver-l1",
					Name: "Notify L1 Approver",
					Type: "http",
					Config: map[string]any{
						"url":    "${APPROVAL_API_URL}/notify",
						"method": "POST",
						"headers": map[string]any{
							"Content-Type":  "application/json",
							"Authorization": "Bearer ${APPROVAL_API_TOKEN}",
						},
						"body": `{"approver": "${L1_APPROVER}", "request_id": "${request_id}", "level": 1}`,
					},
					DependsOn: []string{"prepare-request"},
				},
				{
					ID:   "wait-l1-approval",
					Name: "Wait for L1 Approval",
					Type: "delay",
					Config: map[string]any{
						"duration": "24h",
					},
					DependsOn: []string{"notify-approver-l1"},
				},
				{
					ID:   "check-l1-result",
					Name: "Check L1 Approval Result",
					Type: "condition",
					Config: map[string]any{
						"expression": "l1_approved == true",
						"on_true":    "notify-approver-l2",
						"on_false":   "notify-rejection",
					},
					DependsOn: []string{"wait-l1-approval"},
				},
				{
					ID:   "notify-approver-l2",
					Name: "Notify L2 Approver",
					Type: "http",
					Config: map[string]any{
						"url":    "${APPROVAL_API_URL}/notify",
						"method": "POST",
						"headers": map[string]any{
							"Content-Type":  "application/json",
							"Authorization": "Bearer ${APPROVAL_API_TOKEN}",
						},
						"body": `{"approver": "${L2_APPROVER}", "request_id": "${request_id}", "level": 2}`,
					},
					DependsOn: []string{"check-l1-result"},
					Condition: "l1_approved == true",
				},
				{
					ID:   "wait-l2-approval",
					Name: "Wait for L2 Approval",
					Type: "delay",
					Config: map[string]any{
						"duration": "48h",
					},
					DependsOn: []string{"notify-approver-l2"},
				},
				{
					ID:   "check-l2-result",
					Name: "Check L2 Approval Result",
					Type: "condition",
					Config: map[string]any{
						"expression": "l2_approved == true",
						"on_true":    "execute-action",
						"on_false":   "notify-rejection",
					},
					DependsOn: []string{"wait-l2-approval"},
				},
				{
					ID:   "execute-action",
					Name: "Execute Approved Action",
					Type: "http",
					Config: map[string]any{
						"url":    "${ACTION_API_URL}/execute",
						"method": "POST",
						"headers": map[string]any{
							"Content-Type":  "application/json",
							"Authorization": "Bearer ${ACTION_API_TOKEN}",
						},
						"body": `{"request_id": "${request_id}", "action": "${action}", "approved_by": ["${L1_APPROVER}", "${L2_APPROVER}"]}`,
					},
					DependsOn: []string{"check-l2-result"},
					Condition: "l2_approved == true",
				},
				{
					ID:   "notify-rejection",
					Name: "Notify Rejection",
					Type: "http",
					Config: map[string]any{
						"url":    "${NOTIFICATION_WEBHOOK}",
						"method": "POST",
						"body":   `{"text": "Approval request ${request_id} was rejected", "channel": "${NOTIFICATION_CHANNEL}"}`,
					},
					DependsOn: []string{"check-l1-result"},
					Condition: "l1_approved != true",
				},
				{
					ID:   "audit-log",
					Name: "Write Audit Log",
					Type: "script",
					Config: map[string]any{
						"language": "bash",
						"script":   "echo '{\"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"request_id\": \"${request_id}\", \"result\": \"${result}\", \"approvers\": [\"${L1_APPROVER}\", \"${L2_APPROVER}\"]}' >> /var/log/approvals.log",
					},
					DependsOn: []string{"execute-action"},
				},
			},
			OnFailure: &definition.FailureAction{
				Action:      "notify",
				NotifyEmail: "${ADMIN_EMAIL}",
				Webhook:     "${NOTIFICATION_WEBHOOK}",
			},
			Metadata: map[string]string{
				"template": "approval-chain",
				"author":   "flowforge",
			},
		},
	}
}
