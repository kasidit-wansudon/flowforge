package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/auth"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testConfig() auth.AuthConfig {
	return auth.AuthConfig{
		JWTSecret:    "test-secret-key-for-unit-tests",
		TokenExpiry:  time.Hour,
		APIKeyHeader: "X-API-Key",
	}
}

// ---------------------------------------------------------------------------
// JWT generation
// ---------------------------------------------------------------------------

func TestGenerateToken_Success(t *testing.T) {
	cfg := testConfig()
	token, err := auth.GenerateToken(cfg, "user-1", "admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	// A JWT-like token has 3 dot-separated parts.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("token parts = %d, want 3", len(parts))
	}
}

func TestGenerateToken_EmptySecretFails(t *testing.T) {
	cfg := testConfig()
	cfg.JWTSecret = ""
	_, err := auth.GenerateToken(cfg, "user-1", "admin")
	if err == nil {
		t.Fatal("expected error for empty secret, got nil")
	}
}

func TestGenerateToken_DifferentSecretsProduceDifferentTokens(t *testing.T) {
	cfg1 := testConfig()
	cfg2 := testConfig()
	cfg2.JWTSecret = "different-secret"

	tok1, err1 := auth.GenerateToken(cfg1, "user-1", "admin")
	tok2, err2 := auth.GenerateToken(cfg2, "user-1", "admin")
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if tok1 == tok2 {
		t.Error("tokens from different secrets should differ")
	}
}

// ---------------------------------------------------------------------------
// JWT validation
// ---------------------------------------------------------------------------

func TestValidateToken_Success(t *testing.T) {
	cfg := testConfig()
	token, err := auth.GenerateToken(cfg, "user-42", "operator")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	claims, err := auth.ValidateToken(cfg, token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.UserID != "user-42" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-42")
	}
	if claims.Role != "operator" {
		t.Errorf("Role = %q, want %q", claims.Role, "operator")
	}
}

func TestValidateToken_EmptySecretFails(t *testing.T) {
	cfg := testConfig()
	token, _ := auth.GenerateToken(cfg, "u", "r")
	cfg.JWTSecret = ""
	_, err := auth.ValidateToken(cfg, token)
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestValidateToken_TamperedSignatureFails(t *testing.T) {
	cfg := testConfig()
	token, _ := auth.GenerateToken(cfg, "u", "r")

	// Replace the last character of the signature.
	last := token[len(token)-1]
	var tampered string
	if last == 'A' {
		tampered = token[:len(token)-1] + "B"
	} else {
		tampered = token[:len(token)-1] + "A"
	}

	_, err := auth.ValidateToken(cfg, tampered)
	if err == nil {
		t.Fatal("expected error for tampered signature")
	}
}

func TestValidateToken_MalformedTokenFails(t *testing.T) {
	cfg := testConfig()
	_, err := auth.ValidateToken(cfg, "not.a.valid.jwt.at.all")
	if err == nil {
		t.Fatal("expected error for malformed token with wrong segment count")
	}
	_, err2 := auth.ValidateToken(cfg, "onlyone")
	if err2 == nil {
		t.Fatal("expected error for single-segment token")
	}
}

func TestValidateToken_ExpiredTokenFails(t *testing.T) {
	cfg := testConfig()
	cfg.TokenExpiry = -time.Hour // already expired
	token, err := auth.GenerateToken(cfg, "u", "r")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	_, err = auth.ValidateToken(cfg, token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error message %q should contain 'expired'", err.Error())
	}
}

func TestValidateToken_WrongSecretFails(t *testing.T) {
	cfg := testConfig()
	token, _ := auth.GenerateToken(cfg, "u", "r")

	wrongCfg := testConfig()
	wrongCfg.JWTSecret = "wrong-secret"
	_, err := auth.ValidateToken(wrongCfg, token)
	if err == nil {
		t.Fatal("expected error when validating with wrong secret")
	}
}

// ---------------------------------------------------------------------------
// Claims.Valid
// ---------------------------------------------------------------------------

func TestClaimsValid_NotExpired(t *testing.T) {
	c := &auth.Claims{
		UserID:    "u",
		Role:      "r",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if err := c.Valid(); err != nil {
		t.Errorf("unexpected error for non-expired claims: %v", err)
	}
}

func TestClaimsValid_Expired(t *testing.T) {
	c := &auth.Claims{
		UserID:    "u",
		Role:      "r",
		IssuedAt:  time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	}
	if err := c.Valid(); err == nil {
		t.Error("expected error for expired claims")
	}
}

// ---------------------------------------------------------------------------
// InMemoryAPIKeyStore
// ---------------------------------------------------------------------------

func TestAPIKeyStore_CreateAndGet(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	rawKey, key, err := store.Create(ctx, "ci-key", "user-1", "admin", time.Time{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rawKey == "" {
		t.Fatal("expected non-empty raw key")
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
	if key.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", key.UserID, "user-1")
	}
	if key.Role != "admin" {
		t.Errorf("Role = %q, want %q", key.Role, "admin")
	}
	if key.Revoked {
		t.Error("new key should not be revoked")
	}

	// Retrieve it by ID.
	got, err := store.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != key.ID {
		t.Errorf("ID = %q, want %q", got.ID, key.ID)
	}
}

func TestAPIKeyStore_GetNotFound(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	_, err := store.Get(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestAPIKeyStore_Revoke(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	_, key, _ := store.Create(ctx, "k", "u", "r", time.Time{})
	if err := store.Revoke(ctx, key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, _ := store.Get(ctx, key.ID)
	if !got.Revoked {
		t.Error("key should be revoked after Revoke()")
	}
}

func TestAPIKeyStore_RevokeNotFound(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	err := store.Revoke(context.Background(), "no-such-id")
	if err == nil {
		t.Fatal("expected error when revoking missing key")
	}
}

func TestAPIKeyStore_List(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	// Create two keys for user-A and one for user-B.
	_, ka1, _ := store.Create(ctx, "a1", "user-A", "viewer", time.Time{})
	_, ka2, _ := store.Create(ctx, "a2", "user-A", "viewer", time.Time{})
	_, _, _ = store.Create(ctx, "b1", "user-B", "viewer", time.Time{})

	// Revoke one of user-A's keys.
	_ = store.Revoke(ctx, ka1.ID)
	_ = ka2 // keep ka2 active

	keys, err := store.List(ctx, "user-A")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("keys for user-A = %d, want 1 (revoked excluded)", len(keys))
	}
	if keys[0].UserID != "user-A" {
		t.Errorf("UserID = %q, want %q", keys[0].UserID, "user-A")
	}
}

// ---------------------------------------------------------------------------
// APIKeyAuth.ValidateAPIKey
// ---------------------------------------------------------------------------

func TestValidateAPIKey_Success(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	rawKey, _, err := store.Create(ctx, "k", "user-X", "viewer", time.Time{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	apiKeyAuth := auth.NewAPIKeyAuth(store)
	claims, err := apiKeyAuth.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.UserID != "user-X" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-X")
	}
}

func TestValidateAPIKey_InvalidKeyFails(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	_, _, _ = store.Create(ctx, "k", "u", "r", time.Time{})

	apiKeyAuth := auth.NewAPIKeyAuth(store)
	_, err := apiKeyAuth.ValidateAPIKey(ctx, "completely-wrong-key")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestValidateAPIKey_RevokedKeyFails(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	rawKey, key, _ := store.Create(ctx, "k", "u", "r", time.Time{})
	_ = store.Revoke(ctx, key.ID)

	apiKeyAuth := auth.NewAPIKeyAuth(store)
	_, err := apiKeyAuth.ValidateAPIKey(ctx, rawKey)
	if err == nil {
		t.Fatal("expected error for revoked key")
	}
}

func TestValidateAPIKey_ExpiredKeyFails(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	// Expire one second in the past.
	rawKey, _, _ := store.Create(ctx, "k", "u", "r", time.Now().Add(-time.Second))

	apiKeyAuth := auth.NewAPIKeyAuth(store)
	_, err := apiKeyAuth.ValidateAPIKey(ctx, rawKey)
	if err == nil {
		t.Fatal("expected error for expired API key")
	}
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

func TestContextWithClaims_RoundTrip(t *testing.T) {
	original := &auth.Claims{UserID: "u", Role: "r"}
	ctx := auth.ContextWithClaims(context.Background(), original)
	got := auth.ClaimsFromContext(ctx)
	if got == nil {
		t.Fatal("expected claims in context, got nil")
	}
	if got.UserID != original.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, original.UserID)
	}
}

func TestClaimsFromContext_MissingReturnsNil(t *testing.T) {
	got := auth.ClaimsFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil for empty context, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// AuthMiddleware
// ---------------------------------------------------------------------------

func TestAuthMiddleware_BearerTokenAllows(t *testing.T) {
	cfg := testConfig()
	token, _ := auth.GenerateToken(cfg, "user-1", "admin")

	middleware := auth.AuthMiddleware(cfg, nil)
	called := false
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		claims := auth.ClaimsFromContext(r.Context())
		if claims == nil {
			t.Error("expected claims in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler was not called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_InvalidBearerRejects(t *testing.T) {
	cfg := testConfig()
	middleware := auth.AuthMiddleware(cfg, nil)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for invalid bearer token")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_APIKeyAllows(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	rawKey, _, _ := store.Create(context.Background(), "k", "user-2", "viewer", time.Time{})
	apiKeyAuth := auth.NewAPIKeyAuth(store)

	cfg := testConfig()
	middleware := auth.AuthMiddleware(cfg, apiKeyAuth)

	called := false
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", rawKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler was not called for valid API key")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_NoCredentialsRejects(t *testing.T) {
	cfg := testConfig()
	middleware := auth.AuthMiddleware(cfg, nil)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without credentials")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_InvalidAPIKeyRejects(t *testing.T) {
	store := auth.NewInMemoryAPIKeyStore()
	apiKeyAuth := auth.NewAPIKeyAuth(store)

	cfg := testConfig()
	middleware := auth.AuthMiddleware(cfg, apiKeyAuth)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for invalid API key")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_BearerTakesPrecedenceOverAPIKey(t *testing.T) {
	cfg := testConfig()
	// A valid JWT should be accepted even if an API key header is also present.
	token, _ := auth.GenerateToken(cfg, "bearer-user", "admin")

	store := auth.NewInMemoryAPIKeyStore()
	apiKeyAuth := auth.NewAPIKeyAuth(store)

	middleware := auth.AuthMiddleware(cfg, apiKeyAuth)

	var seenUserID string
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c := auth.ClaimsFromContext(r.Context()); c != nil {
			seenUserID = c.UserID
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-API-Key", "any-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if seenUserID != "bearer-user" {
		t.Errorf("seenUserID = %q, want %q", seenUserID, "bearer-user")
	}
}

func TestAuthMiddleware_DefaultAPIKeyHeaderFallback(t *testing.T) {
	// When APIKeyHeader is empty it should default to "X-API-Key".
	cfg := auth.AuthConfig{
		JWTSecret:   "secret",
		TokenExpiry: time.Hour,
		// APIKeyHeader intentionally left empty
	}

	store := auth.NewInMemoryAPIKeyStore()
	rawKey, _, _ := store.Create(context.Background(), "k", "u", "r", time.Time{})
	apiKeyAuth := auth.NewAPIKeyAuth(store)

	middleware := auth.AuthMiddleware(cfg, apiKeyAuth)
	called := false
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", rawKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler was not called; default header 'X-API-Key' may not be working")
	}
}
