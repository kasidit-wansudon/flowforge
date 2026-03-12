// Package condition provides a conditional branching task handler for workflow execution.
package condition

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ConditionConfig defines the configuration for a condition task.
type ConditionConfig struct {
	Expression string `json:"expression" yaml:"expression"`
	OnTrue     string `json:"on_true" yaml:"on_true"`
	OnFalse    string `json:"on_false" yaml:"on_false"`
}

// ConditionResult represents the outcome of a condition evaluation.
type ConditionResult struct {
	Expression string `json:"expression"`
	Result     bool   `json:"result"`
	Branch     string `json:"branch"`
}

// ConditionHandler evaluates conditional expressions and determines branching.
type ConditionHandler struct{}

// NewConditionHandler creates a new ConditionHandler.
func NewConditionHandler() *ConditionHandler {
	return &ConditionHandler{}
}

// Evaluate evaluates the condition expression against the provided inputs
// and returns which branch to take.
func (h *ConditionHandler) Evaluate(_ context.Context, config ConditionConfig, inputs map[string]interface{}) (string, error) {
	if config.Expression == "" {
		return "", fmt.Errorf("expression is required")
	}

	result, err := evaluateExpression(config.Expression, inputs)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate expression %q: %w", config.Expression, err)
	}

	if result {
		return config.OnTrue, nil
	}
	return config.OnFalse, nil
}

// EvaluateWithResult evaluates and returns a full ConditionResult.
func (h *ConditionHandler) EvaluateWithResult(_ context.Context, config ConditionConfig, inputs map[string]interface{}) (*ConditionResult, error) {
	if config.Expression == "" {
		return nil, fmt.Errorf("expression is required")
	}

	result, err := evaluateExpression(config.Expression, inputs)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate expression %q: %w", config.Expression, err)
	}

	branch := config.OnFalse
	if result {
		branch = config.OnTrue
	}

	return &ConditionResult{
		Expression: config.Expression,
		Result:     result,
		Branch:     branch,
	}, nil
}

// evaluateExpression evaluates a simple expression against the provided inputs.
// Supported operators: ==, !=, >, <, >=, <=, contains, isEmpty
func evaluateExpression(expr string, inputs map[string]interface{}) (bool, error) {
	expr = strings.TrimSpace(expr)

	// Check for "isEmpty" operator: "fieldName isEmpty"
	if strings.HasSuffix(expr, " isEmpty") || strings.HasSuffix(expr, ".isEmpty") {
		fieldName := strings.TrimSuffix(strings.TrimSuffix(expr, " isEmpty"), ".isEmpty")
		fieldName = strings.TrimSpace(fieldName)
		return evalIsEmpty(fieldName, inputs), nil
	}

	// Check for "!isEmpty" operator.
	if strings.HasSuffix(expr, " !isEmpty") || strings.HasSuffix(expr, ".!isEmpty") {
		fieldName := strings.TrimSuffix(strings.TrimSuffix(expr, " !isEmpty"), ".!isEmpty")
		fieldName = strings.TrimSpace(fieldName)
		return !evalIsEmpty(fieldName, inputs), nil
	}

	// Parse binary operators in order of specificity.
	operators := []struct {
		op   string
		eval func(left, right string, inputs map[string]interface{}) (bool, error)
	}{
		{"!=", evalNotEquals},
		{">=", evalGreaterEquals},
		{"<=", evalLessEquals},
		{"==", evalEquals},
		{">", evalGreaterThan},
		{"<", evalLessThan},
		{" contains ", evalContains},
	}

	for _, op := range operators {
		parts := strings.SplitN(expr, op.op, 2)
		if len(parts) == 2 {
			left := strings.TrimSpace(parts[0])
			right := strings.TrimSpace(parts[1])
			return op.eval(left, right, inputs)
		}
	}

	// If no operator found, treat as a truthy check on a variable.
	val := resolveValue(expr, inputs)
	return isTruthy(val), nil
}

// resolveValue resolves a value from the inputs or returns it as a literal.
func resolveValue(token string, inputs map[string]interface{}) interface{} {
	token = strings.TrimSpace(token)

	// Check for string literals (quoted).
	if (strings.HasPrefix(token, "\"") && strings.HasSuffix(token, "\"")) ||
		(strings.HasPrefix(token, "'") && strings.HasSuffix(token, "'")) {
		return token[1 : len(token)-1]
	}

	// Check for boolean literals.
	if strings.ToLower(token) == "true" {
		return true
	}
	if strings.ToLower(token) == "false" {
		return false
	}

	// Check for nil/null.
	if strings.ToLower(token) == "nil" || strings.ToLower(token) == "null" {
		return nil
	}

	// Check for numeric literals.
	if i, err := strconv.ParseInt(token, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(token, 64); err == nil {
		return f
	}

	// Look up in inputs, supporting dot-notation.
	return lookupNested(token, inputs)
}

// lookupNested resolves a dot-notation path in a nested map.
func lookupNested(path string, inputs map[string]interface{}) interface{} {
	parts := strings.Split(path, ".")
	var current interface{} = inputs

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			val, ok := v[part]
			if !ok {
				return path // Return as string literal if not found.
			}
			current = val
		default:
			return path
		}
	}

	return current
}

// toString converts a value to its string representation.
func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// toFloat64 attempts to convert a value to float64 for numeric comparisons.
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// isTruthy returns whether a value is truthy.
func isTruthy(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != "" && strings.ToLower(val) != "false" && val != "0"
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	default:
		return true
	}
}

// evalEquals evaluates the == operator.
func evalEquals(left, right string, inputs map[string]interface{}) (bool, error) {
	lv := resolveValue(left, inputs)
	rv := resolveValue(right, inputs)

	// Try numeric comparison first.
	lf, lok := toFloat64(lv)
	rf, rok := toFloat64(rv)
	if lok && rok {
		return lf == rf, nil
	}

	return toString(lv) == toString(rv), nil
}

// evalNotEquals evaluates the != operator.
func evalNotEquals(left, right string, inputs map[string]interface{}) (bool, error) {
	result, err := evalEquals(left, right, inputs)
	if err != nil {
		return false, err
	}
	return !result, nil
}

// evalGreaterThan evaluates the > operator.
func evalGreaterThan(left, right string, inputs map[string]interface{}) (bool, error) {
	lv := resolveValue(left, inputs)
	rv := resolveValue(right, inputs)

	lf, lok := toFloat64(lv)
	rf, rok := toFloat64(rv)
	if lok && rok {
		return lf > rf, nil
	}

	return toString(lv) > toString(rv), nil
}

// evalLessThan evaluates the < operator.
func evalLessThan(left, right string, inputs map[string]interface{}) (bool, error) {
	lv := resolveValue(left, inputs)
	rv := resolveValue(right, inputs)

	lf, lok := toFloat64(lv)
	rf, rok := toFloat64(rv)
	if lok && rok {
		return lf < rf, nil
	}

	return toString(lv) < toString(rv), nil
}

// evalGreaterEquals evaluates the >= operator.
func evalGreaterEquals(left, right string, inputs map[string]interface{}) (bool, error) {
	lv := resolveValue(left, inputs)
	rv := resolveValue(right, inputs)

	lf, lok := toFloat64(lv)
	rf, rok := toFloat64(rv)
	if lok && rok {
		return lf >= rf, nil
	}

	return toString(lv) >= toString(rv), nil
}

// evalLessEquals evaluates the <= operator.
func evalLessEquals(left, right string, inputs map[string]interface{}) (bool, error) {
	lv := resolveValue(left, inputs)
	rv := resolveValue(right, inputs)

	lf, lok := toFloat64(lv)
	rf, rok := toFloat64(rv)
	if lok && rok {
		return lf <= rf, nil
	}

	return toString(lv) <= toString(rv), nil
}

// evalContains evaluates the contains operator.
func evalContains(left, right string, inputs map[string]interface{}) (bool, error) {
	lv := resolveValue(left, inputs)
	rv := resolveValue(right, inputs)

	ls := toString(lv)
	rs := toString(rv)

	return strings.Contains(ls, rs), nil
}

// evalIsEmpty checks if a field value is empty.
func evalIsEmpty(fieldName string, inputs map[string]interface{}) bool {
	val := resolveValue(fieldName, inputs)
	if val == nil {
		return true
	}

	switch v := val.(type) {
	case string:
		return v == "" || v == fieldName // v == fieldName means not found in inputs
	case []interface{}:
		return len(v) == 0
	case map[string]interface{}:
		return len(v) == 0
	default:
		return false
	}
}

// ParseConfig parses a generic config map into a ConditionConfig.
func ParseConfig(config map[string]any) (ConditionConfig, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return ConditionConfig{}, fmt.Errorf("failed to marshal config: %w", err)
	}

	var cfg ConditionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ConditionConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}
