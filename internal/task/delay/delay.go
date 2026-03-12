// Package delay provides a delay/timer task handler for workflow execution.
package delay

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Result represents the outcome of a delay task execution.
type Result struct {
	RequestedDelay time.Duration `json:"requested_delay"`
	ActualDelay    time.Duration `json:"actual_delay"`
	Completed      bool          `json:"completed"`
	Cancelled      bool          `json:"cancelled"`
	Reason         string        `json:"reason,omitempty"`
}

// DelayConfig defines the configuration for a delay task.
type DelayConfig struct {
	// Duration specifies a relative delay (e.g., "5m", "1h30m").
	Duration time.Duration `json:"duration,omitempty" yaml:"duration,omitempty"`

	// Until specifies an absolute timestamp to wait until.
	// If both Duration and Until are set, Until takes precedence.
	Until *time.Time `json:"until,omitempty" yaml:"until,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling to handle duration strings.
func (c *DelayConfig) UnmarshalJSON(data []byte) error {
	type rawConfig struct {
		Duration string  `json:"duration,omitempty"`
		Until    *string `json:"until,omitempty"`
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if raw.Duration != "" {
		d, err := time.ParseDuration(raw.Duration)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", raw.Duration, err)
		}
		c.Duration = d
	}

	if raw.Until != nil && *raw.Until != "" {
		// Try multiple timestamp formats.
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		var parsed bool
		for _, format := range formats {
			t, err := time.Parse(format, *raw.Until)
			if err == nil {
				c.Until = &t
				parsed = true
				break
			}
		}
		if !parsed {
			return fmt.Errorf("invalid timestamp %q: expected RFC3339 or common format", *raw.Until)
		}
	}

	return nil
}

// MarshalJSON implements custom JSON marshaling.
func (c DelayConfig) MarshalJSON() ([]byte, error) {
	type rawConfig struct {
		Duration string  `json:"duration,omitempty"`
		Until    *string `json:"until,omitempty"`
	}

	raw := rawConfig{}
	if c.Duration > 0 {
		raw.Duration = c.Duration.String()
	}
	if c.Until != nil {
		s := c.Until.Format(time.RFC3339)
		raw.Until = &s
	}

	return json.Marshal(raw)
}

// DelayHandler executes delay tasks.
type DelayHandler struct{}

// NewDelayHandler creates a new DelayHandler.
func NewDelayHandler() *DelayHandler {
	return &DelayHandler{}
}

// Execute waits for the specified duration or until the specified timestamp.
// It respects context cancellation for graceful shutdown.
func (h *DelayHandler) Execute(ctx context.Context, config DelayConfig) (*Result, error) {
	if err := validate(&config); err != nil {
		return nil, fmt.Errorf("invalid delay config: %w", err)
	}

	var waitDuration time.Duration

	if config.Until != nil {
		// Absolute timestamp: wait until the specified time.
		waitDuration = time.Until(*config.Until)
		if waitDuration <= 0 {
			// The target time has already passed.
			return &Result{
				RequestedDelay: 0,
				ActualDelay:    0,
				Completed:      true,
				Reason:         "target time already passed",
			}, nil
		}
	} else {
		// Relative duration.
		waitDuration = config.Duration
	}

	start := time.Now()
	timer := time.NewTimer(waitDuration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return &Result{
			RequestedDelay: waitDuration,
			ActualDelay:    time.Since(start),
			Completed:      true,
		}, nil
	case <-ctx.Done():
		return &Result{
			RequestedDelay: waitDuration,
			ActualDelay:    time.Since(start),
			Completed:      false,
			Cancelled:      true,
			Reason:         ctx.Err().Error(),
		}, nil
	}
}

// validate checks the delay task configuration.
func validate(config *DelayConfig) error {
	if config.Duration <= 0 && config.Until == nil {
		return fmt.Errorf("either duration or until must be specified")
	}
	if config.Duration < 0 {
		return fmt.Errorf("duration must be non-negative")
	}
	return nil
}

// ParseConfig parses a generic config map into a DelayConfig.
func ParseConfig(config map[string]any) (DelayConfig, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return DelayConfig{}, fmt.Errorf("failed to marshal config: %w", err)
	}

	var cfg DelayConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DelayConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}
