package http

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer creates a simple test server that returns the configured response.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func TestNewHTTPTaskHandler(t *testing.T) {
	h := NewHTTPTaskHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.client == nil {
		t.Fatal("expected non-nil http client")
	}
}

func TestNewHTTPTaskHandlerWithClient(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}
	h := NewHTTPTaskHandlerWithClient(custom)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.client != custom {
		t.Fatal("expected handler to use the provided custom client")
	}
}

func TestExecuteGET(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL + "/test",
		Method: "GET",
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true, got false; error: %s", result.Error)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}
	if !strings.Contains(result.Body, "ok") {
		t.Errorf("expected body to contain 'ok', got %q", result.Body)
	}
}

func TestExecutePOST(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "name") {
			http.Error(w, "missing body", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"created":true}`)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL + "/create",
		Method: "POST",
		Body:   `{"name":"test"}`,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		ValidStatusCodes: []int{201},
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true, got false; error: %s", result.Error)
	}
	if result.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", result.StatusCode)
	}
}

func TestExecutePUT(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL + "/update",
		Method: "PUT",
		Body:   `{"value":"updated"}`,
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestExecuteDELETE(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:              srv.URL + "/delete/1",
		Method:           "DELETE",
		ValidStatusCodes: []int{204},
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", result.StatusCode)
	}
}

func TestExecuteBearerAuth(t *testing.T) {
	const expectedToken = "my-secret-token"

	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		expected := "Bearer " + expectedToken
		if authHeader != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"auth":"ok"}`)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL + "/secure",
		Method: "GET",
		Auth: &AuthConfig{
			Type: "bearer",
			Credentials: map[string]string{
				"token": expectedToken,
			},
		},
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success with bearer auth, got error: %s", result.Error)
	}
}

func TestExecuteBasicAuth(t *testing.T) {
	const user = "admin"
	const pass = "secret"

	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		expected := "Basic " + encoded
		if authHeader != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"auth":"basic_ok"}`)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL + "/secure",
		Method: "GET",
		Auth: &AuthConfig{
			Type: "basic",
			Credentials: map[string]string{
				"username": user,
				"password": pass,
			},
		},
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success with basic auth, got error: %s", result.Error)
	}
}

func TestExecuteAPIKeyAuth(t *testing.T) {
	const headerName = "X-API-Key"
	const apiKey = "abc123"

	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerName) != apiKey {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"auth":"apikey_ok"}`)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL + "/api",
		Method: "GET",
		Auth: &AuthConfig{
			Type: "apikey",
			Credentials: map[string]string{
				"header": headerName,
				"value":  apiKey,
			},
		},
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success with API key auth, got error: %s", result.Error)
	}
}

func TestExecuteInvalidStatusCode(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL + "/missing",
		Method: "GET",
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default valid codes are 200, 201, 202, 204 — 404 should fail.
	if result.Success {
		t.Error("expected success=false for 404 response")
	}
	if result.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", result.StatusCode)
	}
	if result.Error == "" {
		t.Error("expected non-empty error message for invalid status")
	}
}

func TestExecuteCustomValidStatusCodes(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"queued":true}`)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:              srv.URL + "/queue",
		Method:           "POST",
		ValidStatusCodes: []int{202},
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success for 202 with custom valid codes, got error: %s", result.Error)
	}
}

func TestExecuteMissingURL(t *testing.T) {
	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    "",
		Method: "GET",
	}
	_, err := h.Execute(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
	if !strings.Contains(err.Error(), "URL") {
		t.Errorf("expected error about URL, got: %v", err)
	}
}

func TestExecuteMissingMethod(t *testing.T) {
	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    "http://example.com",
		Method: "",
	}
	_, err := h.Execute(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for missing method")
	}
}

func TestExecuteInvalidMethod(t *testing.T) {
	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    "http://example.com",
		Method: "INVALID",
	}
	_, err := h.Execute(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for invalid HTTP method")
	}
	if !strings.Contains(err.Error(), "unsupported HTTP method") {
		t.Errorf("expected 'unsupported HTTP method' error, got: %v", err)
	}
}

func TestExecuteInvalidAuthType(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL,
		Method: "GET",
		Auth: &AuthConfig{
			Type: "oauth2",
		},
	}
	_, err := h.Execute(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for invalid auth type")
	}
}

func TestExecuteRequestHeaders(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			http.Error(w, "missing header", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"headers":"ok"}`)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL + "/headers",
		Method: "GET",
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success with custom headers, got error: %s", result.Error)
	}
}

func TestExecuteResponseHeaders(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Response-ID", "resp-123")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL,
		Method: "GET",
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Headers["X-Response-Id"] == "" && result.Headers["X-Response-ID"] == "" {
		t.Error("expected X-Response-ID response header to be captured")
	}
}

func TestExecuteTimeout(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the timeout.
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:     srv.URL + "/slow",
		Method:  "GET",
		Timeout: 50 * time.Millisecond,
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The request should fail because of timeout.
	if result.Success {
		t.Error("expected success=false for timed-out request")
	}
	if result.Error == "" {
		t.Error("expected non-empty error for timed-out request")
	}
}

func TestExecuteDuration(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "pong")
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:    srv.URL,
		Method: "GET",
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Duration <= 0 {
		t.Error("expected Duration > 0")
	}
}

func TestParseConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		raw := map[string]any{
			"url":    "http://example.com",
			"method": "GET",
		}
		cfg, err := ParseConfig(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.URL != "http://example.com" {
			t.Errorf("expected URL http://example.com, got %q", cfg.URL)
		}
		if cfg.Method != "GET" {
			t.Errorf("expected method GET, got %q", cfg.Method)
		}
	})

	t.Run("with headers", func(t *testing.T) {
		raw := map[string]any{
			"url":    "http://example.com",
			"method": "POST",
			"headers": map[string]interface{}{
				"Content-Type": "application/json",
			},
		}
		cfg, err := ParseConfig(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Headers["Content-Type"] != "application/json" {
			t.Errorf("expected Content-Type header, got %v", cfg.Headers)
		}
	})
}

func TestBearerAuthMissingToken(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	auth := &AuthConfig{
		Type:        "bearer",
		Credentials: map[string]string{},
	}
	err := applyAuth(req, auth)
	if err == nil {
		t.Fatal("expected error when bearer token is missing")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("expected error about token, got: %v", err)
	}
}

func TestBasicAuthMissingUsername(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	auth := &AuthConfig{
		Type:        "basic",
		Credentials: map[string]string{"password": "secret"},
	}
	err := applyAuth(req, auth)
	if err == nil {
		t.Fatal("expected error when basic auth username is missing")
	}
	if !strings.Contains(err.Error(), "username") {
		t.Errorf("expected error about username, got: %v", err)
	}
}

func TestAPIKeyAuthMissingHeader(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	auth := &AuthConfig{
		Type:        "apikey",
		Credentials: map[string]string{"value": "mykey"},
	}
	err := applyAuth(req, auth)
	if err == nil {
		t.Fatal("expected error when API key header is missing")
	}
}

func TestExecuteMaxResponseSize(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write more than the max response size.
		fmt.Fprint(w, strings.Repeat("a", 200))
	})
	defer srv.Close()

	h := NewHTTPTaskHandler()
	cfg := HTTPTaskConfig{
		URL:             srv.URL,
		Method:          "GET",
		MaxResponseSize: 10, // Only read 10 bytes.
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Body should be truncated to MaxResponseSize.
	if len(result.Body) > 10 {
		t.Errorf("expected body to be truncated to 10 bytes, got %d bytes", len(result.Body))
	}
}
