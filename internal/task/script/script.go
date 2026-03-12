// Package script provides a script execution task handler for workflow execution.
package script

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Result represents the outcome of a script task execution.
type Result struct {
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration"`
	Success  bool          `json:"success"`
	Error    string        `json:"error,omitempty"`
}

// ScriptTaskConfig defines the configuration for a script task.
type ScriptTaskConfig struct {
	Language string            `json:"language" yaml:"language"` // "bash", "python3", "node"
	Script   string            `json:"script" yaml:"script"`
	Timeout  time.Duration     `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Env      map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	WorkDir  string            `json:"work_dir,omitempty" yaml:"work_dir,omitempty"`
}

// supportedLanguages maps language names to their interpreter commands.
var supportedLanguages = map[string]languageConfig{
	"bash": {
		Command: "bash",
		Flag:    "-c",
		UseTempFile: false,
	},
	"sh": {
		Command: "sh",
		Flag:    "-c",
		UseTempFile: false,
	},
	"python3": {
		Command:     "python3",
		Flag:        "-c",
		UseTempFile: false,
	},
	"python": {
		Command:     "python3",
		Flag:        "-c",
		UseTempFile: false,
	},
	"node": {
		Command:     "node",
		Flag:        "-e",
		UseTempFile: false,
	},
	"nodejs": {
		Command:     "node",
		Flag:        "-e",
		UseTempFile: false,
	},
}

// languageConfig defines how to run a specific language.
type languageConfig struct {
	Command     string
	Flag        string
	UseTempFile bool
	Extension   string
}

// ScriptTaskHandler executes script tasks.
type ScriptTaskHandler struct {
	// AllowedLanguages restricts which languages can be executed.
	// If empty, all supported languages are allowed.
	AllowedLanguages []string

	// MaxOutputSize limits the size of captured stdout/stderr in bytes.
	// Default is 1MB.
	MaxOutputSize int
}

// NewScriptTaskHandler creates a new ScriptTaskHandler.
func NewScriptTaskHandler() *ScriptTaskHandler {
	return &ScriptTaskHandler{
		MaxOutputSize: 1 << 20, // 1MB
	}
}

// Execute runs the script specified by the config.
func (h *ScriptTaskHandler) Execute(ctx context.Context, config ScriptTaskConfig) (*Result, error) {
	if err := h.validate(&config); err != nil {
		return nil, fmt.Errorf("invalid script task config: %w", err)
	}

	// Apply defaults.
	h.applyDefaults(&config)

	langCfg, ok := supportedLanguages[strings.ToLower(config.Language)]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", config.Language)
	}

	// Create a context with timeout.
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	// Build the command.
	var cmd *exec.Cmd
	if langCfg.UseTempFile {
		tmpFile, err := h.writeTempScript(config.Script, langCfg.Extension)
		if err != nil {
			return nil, fmt.Errorf("failed to create temp script file: %w", err)
		}
		defer os.Remove(tmpFile)

		cmd = exec.CommandContext(ctx, langCfg.Command, tmpFile)
	} else {
		cmd = exec.CommandContext(ctx, langCfg.Command, langCfg.Flag, config.Script)
	}

	// Set working directory.
	if config.WorkDir != "" {
		cmd.Dir = config.WorkDir
	}

	// Set environment variables.
	if len(config.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range config.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Capture stdout and stderr.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute.
	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &Result{
		Stdout:   h.truncateOutput(stdout.String()),
		Stderr:   h.truncateOutput(stderr.String()),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			result.Success = false
			result.Error = fmt.Sprintf("script exited with code %d", result.ExitCode)
		} else if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = -1
			result.Success = false
			result.Error = fmt.Sprintf("script timed out after %s", config.Timeout)
		} else if ctx.Err() == context.Canceled {
			result.ExitCode = -1
			result.Success = false
			result.Error = "script execution was cancelled"
		} else {
			result.ExitCode = -1
			result.Success = false
			result.Error = err.Error()
		}
	} else {
		result.ExitCode = 0
		result.Success = true
	}

	return result, nil
}

// validate checks the script task configuration.
func (h *ScriptTaskHandler) validate(config *ScriptTaskConfig) error {
	if config.Script == "" {
		return fmt.Errorf("script is required")
	}
	if config.Language == "" {
		return fmt.Errorf("language is required")
	}

	lang := strings.ToLower(config.Language)
	if _, ok := supportedLanguages[lang]; !ok {
		return fmt.Errorf("unsupported language %q; supported: bash, sh, python3, python, node, nodejs", config.Language)
	}

	// Check allowed languages.
	if len(h.AllowedLanguages) > 0 {
		allowed := false
		for _, l := range h.AllowedLanguages {
			if strings.ToLower(l) == lang {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("language %q is not in the allowed list", config.Language)
		}
	}

	// Validate working directory if specified.
	if config.WorkDir != "" {
		info, err := os.Stat(config.WorkDir)
		if err != nil {
			return fmt.Errorf("work_dir %q: %w", config.WorkDir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("work_dir %q is not a directory", config.WorkDir)
		}
	}

	return nil
}

// applyDefaults sets default values for the config.
func (h *ScriptTaskHandler) applyDefaults(config *ScriptTaskConfig) {
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Minute
	}
}

// writeTempScript writes the script to a temporary file and returns the path.
func (h *ScriptTaskHandler) writeTempScript(script, extension string) (string, error) {
	if extension == "" {
		extension = ".sh"
	}

	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("flowforge-script-*%s", extension))

	f, err := os.CreateTemp(tmpDir, filepath.Base(tmpFile))
	if err != nil {
		return "", err
	}

	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}

	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}

	// Make the file executable.
	if err := os.Chmod(f.Name(), 0700); err != nil {
		os.Remove(f.Name())
		return "", err
	}

	return f.Name(), nil
}

// truncateOutput truncates output to the configured maximum size.
func (h *ScriptTaskHandler) truncateOutput(s string) string {
	maxSize := h.MaxOutputSize
	if maxSize <= 0 {
		maxSize = 1 << 20 // 1MB default
	}
	if len(s) > maxSize {
		return s[:maxSize] + "\n... (output truncated)"
	}
	return s
}

// ParseConfig parses a generic config map into a ScriptTaskConfig.
func ParseConfig(config map[string]any) (ScriptTaskConfig, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return ScriptTaskConfig{}, fmt.Errorf("failed to marshal config: %w", err)
	}

	var cfg ScriptTaskConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ScriptTaskConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}
