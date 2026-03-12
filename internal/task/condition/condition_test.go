package condition

import (
	"context"
	"testing"
)

func TestNewConditionHandler(t *testing.T) {
	h := NewConditionHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestEvaluateEqualsOperator(t *testing.T) {
	h := NewConditionHandler()
	tests := []struct {
		name       string
		expr       string
		inputs     map[string]interface{}
		wantResult bool
	}{
		{
			name:       "equal strings",
			expr:       `status == "active"`,
			inputs:     map[string]interface{}{"status": "active"},
			wantResult: true,
		},
		{
			name:       "not equal strings",
			expr:       `status == "inactive"`,
			inputs:     map[string]interface{}{"status": "active"},
			wantResult: false,
		},
		{
			name:       "equal numbers",
			expr:       "count == 5",
			inputs:     map[string]interface{}{"count": int64(5)},
			wantResult: true,
		},
		{
			name:       "equal literal strings",
			expr:       `"hello" == "hello"`,
			inputs:     map[string]interface{}{},
			wantResult: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ConditionConfig{Expression: tc.expr, OnTrue: "yes", OnFalse: "no"}
			branch, err := h.Evaluate(context.Background(), cfg, tc.inputs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			wantBranch := "no"
			if tc.wantResult {
				wantBranch = "yes"
			}
			if branch != wantBranch {
				t.Errorf("expression %q: expected branch %q, got %q", tc.expr, wantBranch, branch)
			}
		})
	}
}

func TestEvaluateNotEqualsOperator(t *testing.T) {
	h := NewConditionHandler()

	t.Run("values differ", func(t *testing.T) {
		cfg := ConditionConfig{
			Expression: `status != "inactive"`,
			OnTrue:     "active-branch",
			OnFalse:    "inactive-branch",
		}
		branch, err := h.Evaluate(context.Background(), cfg, map[string]interface{}{"status": "active"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "active-branch" {
			t.Errorf("expected 'active-branch', got %q", branch)
		}
	})

	t.Run("values same", func(t *testing.T) {
		cfg := ConditionConfig{
			Expression: `status != "active"`,
			OnTrue:     "true-branch",
			OnFalse:    "false-branch",
		}
		branch, err := h.Evaluate(context.Background(), cfg, map[string]interface{}{"status": "active"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "false-branch" {
			t.Errorf("expected 'false-branch', got %q", branch)
		}
	})
}

func TestEvaluateGreaterThan(t *testing.T) {
	h := NewConditionHandler()

	tests := []struct {
		name   string
		expr   string
		inputs map[string]interface{}
		want   bool
	}{
		{"10 > 5 is true", "score > 5", map[string]interface{}{"score": int64(10)}, true},
		{"3 > 5 is false", "score > 5", map[string]interface{}{"score": int64(3)}, false},
		{"5 > 5 is false", "score > 5", map[string]interface{}{"score": int64(5)}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ConditionConfig{Expression: tc.expr, OnTrue: "yes", OnFalse: "no"}
			branch, err := h.Evaluate(context.Background(), cfg, tc.inputs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected := "no"
			if tc.want {
				expected = "yes"
			}
			if branch != expected {
				t.Errorf("%q: expected %q, got %q", tc.expr, expected, branch)
			}
		})
	}
}

func TestEvaluateLessThan(t *testing.T) {
	h := NewConditionHandler()

	tests := []struct {
		name   string
		expr   string
		inputs map[string]interface{}
		want   bool
	}{
		{"3 < 5 is true", "score < 5", map[string]interface{}{"score": int64(3)}, true},
		{"10 < 5 is false", "score < 5", map[string]interface{}{"score": int64(10)}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ConditionConfig{Expression: tc.expr, OnTrue: "yes", OnFalse: "no"}
			branch, err := h.Evaluate(context.Background(), cfg, tc.inputs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected := "no"
			if tc.want {
				expected = "yes"
			}
			if branch != expected {
				t.Errorf("%q: expected %q, got %q", tc.expr, expected, branch)
			}
		})
	}
}

func TestEvaluateGreaterEquals(t *testing.T) {
	h := NewConditionHandler()

	tests := []struct {
		name   string
		expr   string
		inputs map[string]interface{}
		want   bool
	}{
		{"10 >= 5 is true", "score >= 5", map[string]interface{}{"score": int64(10)}, true},
		{"5 >= 5 is true", "score >= 5", map[string]interface{}{"score": int64(5)}, true},
		{"3 >= 5 is false", "score >= 5", map[string]interface{}{"score": int64(3)}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ConditionConfig{Expression: tc.expr, OnTrue: "yes", OnFalse: "no"}
			branch, err := h.Evaluate(context.Background(), cfg, tc.inputs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected := "no"
			if tc.want {
				expected = "yes"
			}
			if branch != expected {
				t.Errorf("%q: expected %q, got %q", tc.expr, expected, branch)
			}
		})
	}
}

func TestEvaluateLessEquals(t *testing.T) {
	h := NewConditionHandler()

	tests := []struct {
		name   string
		expr   string
		inputs map[string]interface{}
		want   bool
	}{
		{"3 <= 5 is true", "score <= 5", map[string]interface{}{"score": int64(3)}, true},
		{"5 <= 5 is true", "score <= 5", map[string]interface{}{"score": int64(5)}, true},
		{"10 <= 5 is false", "score <= 5", map[string]interface{}{"score": int64(10)}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ConditionConfig{Expression: tc.expr, OnTrue: "yes", OnFalse: "no"}
			branch, err := h.Evaluate(context.Background(), cfg, tc.inputs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected := "no"
			if tc.want {
				expected = "yes"
			}
			if branch != expected {
				t.Errorf("%q: expected %q, got %q", tc.expr, expected, branch)
			}
		})
	}
}

func TestEvaluateContains(t *testing.T) {
	h := NewConditionHandler()

	t.Run("string contains substring", func(t *testing.T) {
		cfg := ConditionConfig{
			Expression: `message contains "error"`,
			OnTrue:     "error-branch",
			OnFalse:    "ok-branch",
		}
		branch, err := h.Evaluate(context.Background(), cfg, map[string]interface{}{
			"message": "an error occurred",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "error-branch" {
			t.Errorf("expected 'error-branch', got %q", branch)
		}
	})

	t.Run("string does not contain substring", func(t *testing.T) {
		cfg := ConditionConfig{
			Expression: `message contains "error"`,
			OnTrue:     "error-branch",
			OnFalse:    "ok-branch",
		}
		branch, err := h.Evaluate(context.Background(), cfg, map[string]interface{}{
			"message": "everything is fine",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "ok-branch" {
			t.Errorf("expected 'ok-branch', got %q", branch)
		}
	})
}

func TestEvaluateIsEmpty(t *testing.T) {
	h := NewConditionHandler()

	tests := []struct {
		name   string
		expr   string
		inputs map[string]interface{}
		want   bool
	}{
		{
			name:   "empty string is empty",
			expr:   "name isEmpty",
			inputs: map[string]interface{}{"name": ""},
			want:   true,
		},
		{
			name:   "non-empty string is not empty",
			expr:   "name isEmpty",
			inputs: map[string]interface{}{"name": "John"},
			want:   false,
		},
		{
			name:   "nil value is empty",
			expr:   "field isEmpty",
			inputs: map[string]interface{}{"field": nil},
			want:   true,
		},
		{
			name:   "missing key is empty",
			expr:   "missing isEmpty",
			inputs: map[string]interface{}{},
			want:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ConditionConfig{Expression: tc.expr, OnTrue: "empty", OnFalse: "not-empty"}
			branch, err := h.Evaluate(context.Background(), cfg, tc.inputs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected := "not-empty"
			if tc.want {
				expected = "empty"
			}
			if branch != expected {
				t.Errorf("%q: expected %q, got %q", tc.expr, expected, branch)
			}
		})
	}
}

func TestEvaluateDotNotation(t *testing.T) {
	h := NewConditionHandler()

	inputs := map[string]interface{}{
		"user": map[string]interface{}{
			"role":  "admin",
			"score": int64(100),
		},
	}

	t.Run("nested field equals", func(t *testing.T) {
		cfg := ConditionConfig{
			Expression: `user.role == "admin"`,
			OnTrue:     "admin-branch",
			OnFalse:    "user-branch",
		}
		branch, err := h.Evaluate(context.Background(), cfg, inputs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "admin-branch" {
			t.Errorf("expected 'admin-branch', got %q", branch)
		}
	})

	t.Run("nested numeric comparison", func(t *testing.T) {
		cfg := ConditionConfig{
			Expression: "user.score > 50",
			OnTrue:     "high-branch",
			OnFalse:    "low-branch",
		}
		branch, err := h.Evaluate(context.Background(), cfg, inputs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "high-branch" {
			t.Errorf("expected 'high-branch', got %q", branch)
		}
	})
}

func TestEvaluateWithResult(t *testing.T) {
	h := NewConditionHandler()
	cfg := ConditionConfig{
		Expression: "value == 42",
		OnTrue:     "match",
		OnFalse:    "no-match",
	}

	t.Run("true branch", func(t *testing.T) {
		result, err := h.EvaluateWithResult(context.Background(), cfg, map[string]interface{}{"value": int64(42)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Result {
			t.Error("expected Result=true")
		}
		if result.Branch != "match" {
			t.Errorf("expected Branch='match', got %q", result.Branch)
		}
		if result.Expression != cfg.Expression {
			t.Errorf("expected Expression to be preserved, got %q", result.Expression)
		}
	})

	t.Run("false branch", func(t *testing.T) {
		result, err := h.EvaluateWithResult(context.Background(), cfg, map[string]interface{}{"value": int64(1)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Result {
			t.Error("expected Result=false")
		}
		if result.Branch != "no-match" {
			t.Errorf("expected Branch='no-match', got %q", result.Branch)
		}
	})
}

func TestEvaluateEmptyExpression(t *testing.T) {
	h := NewConditionHandler()
	cfg := ConditionConfig{Expression: "", OnTrue: "yes", OnFalse: "no"}
	_, err := h.Evaluate(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error for empty expression")
	}
}

func TestEvaluateTruthyCheck(t *testing.T) {
	h := NewConditionHandler()

	tests := []struct {
		name   string
		expr   string
		inputs map[string]interface{}
		want   bool
	}{
		{"truthy bool true", "enabled", map[string]interface{}{"enabled": true}, true},
		{"falsy bool false", "enabled", map[string]interface{}{"enabled": false}, false},
		{"truthy non-empty string", "name", map[string]interface{}{"name": "hello"}, true},
		{"falsy empty string", "name", map[string]interface{}{"name": ""}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ConditionConfig{Expression: tc.expr, OnTrue: "yes", OnFalse: "no"}
			branch, err := h.Evaluate(context.Background(), cfg, tc.inputs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected := "no"
			if tc.want {
				expected = "yes"
			}
			if branch != expected {
				t.Errorf("%q: expected %q, got %q", tc.expr, expected, branch)
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	raw := map[string]any{
		"expression": "count > 0",
		"on_true":    "proceed",
		"on_false":   "stop",
	}
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Expression != "count > 0" {
		t.Errorf("expected expression 'count > 0', got %q", cfg.Expression)
	}
	if cfg.OnTrue != "proceed" {
		t.Errorf("expected on_true 'proceed', got %q", cfg.OnTrue)
	}
	if cfg.OnFalse != "stop" {
		t.Errorf("expected on_false 'stop', got %q", cfg.OnFalse)
	}
}

func TestEvaluateTypeCoercion(t *testing.T) {
	h := NewConditionHandler()

	t.Run("string number equals integer", func(t *testing.T) {
		cfg := ConditionConfig{
			Expression: `count == "5"`,
			OnTrue:     "match",
			OnFalse:    "no-match",
		}
		// count is an int64, right side is string "5" — should coerce to numeric comparison.
		branch, err := h.Evaluate(context.Background(), cfg, map[string]interface{}{"count": int64(5)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "match" {
			t.Errorf("expected numeric coercion to match, got %q", branch)
		}
	})

	t.Run("float comparison", func(t *testing.T) {
		cfg := ConditionConfig{
			Expression: "price > 9.99",
			OnTrue:     "expensive",
			OnFalse:    "cheap",
		}
		branch, err := h.Evaluate(context.Background(), cfg, map[string]interface{}{"price": float64(10.5)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "expensive" {
			t.Errorf("expected 'expensive', got %q", branch)
		}
	})
}
