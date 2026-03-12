// Package http provides an HTTP request task handler for workflow execution.
package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Result represents the outcome of an HTTP task execution.
type Result struct {
	StatusCode int                    `json:"status_code"`
	Headers    map[string]string      `json:"headers"`
	Body       string                 `json:"body"`
	Duration   time.Duration          `json:"duration"`
	Success    bool                   `json:"success"`
	Error      string                 `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// AuthConfig defines authentication configuration for HTTP requests.
type AuthConfig struct {
	Type        string            `json:"type" yaml:"type"` // "basic", "bearer", "apikey"
	Credentials map[string]string `json:"credentials" yaml:"credentials"`
}

// HTTPTaskConfig defines the configuration for an HTTP task.
type HTTPTaskConfig struct {
	URL              string            `json:"url" yaml:"url"`
	Method           string            `json:"method" yaml:"method"`
	Headers          map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body             string            `json:"body,omitempty" yaml:"body,omitempty"`
	Auth             *AuthConfig       `json:"auth,omitempty" yaml:"auth,omitempty"`
	Timeout          time.Duration     `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	FollowRedirects  bool              `json:"follow_redirects" yaml:"follow_redirects"`
	ValidStatusCodes []int             `json:"valid_status_codes,omitempty" yaml:"valid_status_codes,omitempty"`
	MaxResponseSize  int64             `json:"max_response_size,omitempty" yaml:"max_response_size,omitempty"`
}

// HTTPTaskHandler executes HTTP request tasks.
type HTTPTaskHandler struct {
	client *http.Client
}

// NewHTTPTaskHandler creates a new HTTPTaskHandler with a default HTTP client.
func NewHTTPTaskHandler() *HTTPTaskHandler {
	return &HTTPTaskHandler{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewHTTPTaskHandlerWithClient creates a new HTTPTaskHandler with a custom HTTP client.
func NewHTTPTaskHandlerWithClient(client *http.Client) *HTTPTaskHandler {
	return &HTTPTaskHandler{
		client: client,
	}
}

// Execute performs the HTTP request described by the config.
func (h *HTTPTaskHandler) Execute(ctx context.Context, config HTTPTaskConfig) (*Result, error) {
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid HTTP task config: %w", err)
	}

	// Apply defaults.
	applyDefaults(&config)

	// Create a client with the appropriate settings.
	client := h.buildClient(config)

	// Build the request.
	req, err := buildRequest(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}

	// Apply authentication.
	if config.Auth != nil {
		if err := applyAuth(req, config.Auth); err != nil {
			return nil, fmt.Errorf("failed to apply authentication: %w", err)
		}
	}

	// Execute the request.
	start := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(start)

	if err != nil {
		return &Result{
			Duration: duration,
			Success:  false,
			Error:    err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	// Read the response body.
	maxSize := config.MaxResponseSize
	if maxSize <= 0 {
		maxSize = 10 << 20 // 10MB default
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return &Result{
			StatusCode: resp.StatusCode,
			Duration:   duration,
			Success:    false,
			Error:      fmt.Sprintf("failed to read response body: %s", err),
		}, nil
	}

	// Extract response headers.
	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		headers[k] = strings.Join(v, ", ")
	}

	result := &Result{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       string(body),
		Duration:   duration,
		Metadata: map[string]interface{}{
			"content_length": resp.ContentLength,
			"proto":          resp.Proto,
		},
	}

	// Validate status code.
	result.Success = isValidStatus(resp.StatusCode, config.ValidStatusCodes)
	if !result.Success {
		result.Error = fmt.Sprintf("unexpected status code %d", resp.StatusCode)
	}

	return result, nil
}

// validateConfig validates the HTTP task configuration.
func validateConfig(config *HTTPTaskConfig) error {
	if config.URL == "" {
		return fmt.Errorf("URL is required")
	}
	if config.Method == "" {
		return fmt.Errorf("method is required")
	}

	validMethods := map[string]bool{
		http.MethodGet:    true,
		http.MethodPost:   true,
		http.MethodPut:    true,
		http.MethodDelete: true,
		http.MethodPatch:  true,
		http.MethodHead:   true,
	}
	method := strings.ToUpper(config.Method)
	if !validMethods[method] {
		return fmt.Errorf("unsupported HTTP method: %s", config.Method)
	}

	if config.Auth != nil {
		validAuthTypes := map[string]bool{
			"basic":  true,
			"bearer": true,
			"apikey": true,
		}
		if !validAuthTypes[strings.ToLower(config.Auth.Type)] {
			return fmt.Errorf("unsupported auth type: %s", config.Auth.Type)
		}
	}

	return nil
}

// applyDefaults sets default values for unset config fields.
func applyDefaults(config *HTTPTaskConfig) {
	config.Method = strings.ToUpper(config.Method)
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if len(config.ValidStatusCodes) == 0 {
		config.ValidStatusCodes = []int{200, 201, 202, 204}
	}
}

// buildClient creates an HTTP client with the appropriate settings.
func (h *HTTPTaskHandler) buildClient(config HTTPTaskConfig) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	client := &http.Client{
		Timeout:   config.Timeout,
		Transport: transport,
	}

	if !config.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	return client
}

// buildRequest constructs the HTTP request from the config.
func buildRequest(ctx context.Context, config HTTPTaskConfig) (*http.Request, error) {
	var bodyReader io.Reader
	if config.Body != "" {
		bodyReader = bytes.NewBufferString(config.Body)
	}

	req, err := http.NewRequestWithContext(ctx, config.Method, config.URL, bodyReader)
	if err != nil {
		return nil, err
	}

	// Apply headers.
	for k, v := range config.Headers {
		req.Header.Set(k, v)
	}

	// Set default Content-Type for methods that typically have a body.
	if config.Body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

// applyAuth applies authentication to the HTTP request.
func applyAuth(req *http.Request, auth *AuthConfig) error {
	switch strings.ToLower(auth.Type) {
	case "basic":
		username := auth.Credentials["username"]
		password := auth.Credentials["password"]
		if username == "" {
			return fmt.Errorf("basic auth requires 'username' in credentials")
		}
		encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		req.Header.Set("Authorization", "Basic "+encoded)

	case "bearer":
		token := auth.Credentials["token"]
		if token == "" {
			return fmt.Errorf("bearer auth requires 'token' in credentials")
		}
		req.Header.Set("Authorization", "Bearer "+token)

	case "apikey":
		key := auth.Credentials["key"]
		value := auth.Credentials["value"]
		header := auth.Credentials["header"]
		if key == "" && header == "" {
			return fmt.Errorf("apikey auth requires 'header' or 'key' in credentials")
		}
		if header != "" {
			req.Header.Set(header, value)
		} else {
			req.Header.Set(key, value)
		}

	default:
		return fmt.Errorf("unsupported auth type: %s", auth.Type)
	}

	return nil
}

// isValidStatus checks if the status code is in the list of valid status codes.
func isValidStatus(statusCode int, validCodes []int) bool {
	for _, code := range validCodes {
		if statusCode == code {
			return true
		}
	}
	return false
}

// ParseConfig parses a generic config map into an HTTPTaskConfig.
func ParseConfig(config map[string]any) (HTTPTaskConfig, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return HTTPTaskConfig{}, fmt.Errorf("failed to marshal config: %w", err)
	}

	var cfg HTTPTaskConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return HTTPTaskConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}
