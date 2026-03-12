// Package auth provides API key and JWT-based authentication for the
// FlowForge HTTP API, including middleware, token generation/validation,
// and an in-memory API key store.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// AuthConfig holds authentication configuration parameters.
type AuthConfig struct {
	// JWTSecret is the HMAC-SHA256 signing key for JWT tokens.
	JWTSecret string
	// TokenExpiry controls how long generated tokens remain valid.
	TokenExpiry time.Duration
	// APIKeyHeader is the HTTP header name used for API key authentication.
	// Defaults to "X-API-Key".
	APIKeyHeader string
}

// DefaultAuthConfig returns an AuthConfig with sensible defaults. Callers
// MUST override JWTSecret before production use.
func DefaultAuthConfig() AuthConfig {
	return AuthConfig{
		JWTSecret:    "",
		TokenExpiry:  24 * time.Hour,
		APIKeyHeader: "X-API-Key",
	}
}

// ---------------------------------------------------------------------------
// Claims — JWT-like payload
// ---------------------------------------------------------------------------

// Claims carries the authenticated principal's identity and role.
type Claims struct {
	// UserID is the unique identifier of the authenticated user.
	UserID string `json:"sub"`
	// Role is the authorisation role (e.g. "admin", "operator", "viewer").
	Role string `json:"role"`
	// IssuedAt records when the token was created (Unix seconds).
	IssuedAt int64 `json:"iat"`
	// ExpiresAt records when the token expires (Unix seconds).
	ExpiresAt int64 `json:"exp"`
}

// Valid returns an error when the claims have expired.
func (c *Claims) Valid() error {
	if time.Now().Unix() > c.ExpiresAt {
		return errors.New("auth: token has expired")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Token generation / validation (HMAC-SHA256 JWT)
// ---------------------------------------------------------------------------

// base64URLEncode is a minimal base64url encoder (RFC 4648) without padding.
func base64URLEncode(data []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	result := make([]byte, 0, (len(data)*4+2)/3)
	for i := 0; i < len(data); i += 3 {
		val := uint(data[i]) << 16
		if i+1 < len(data) {
			val |= uint(data[i+1]) << 8
		}
		if i+2 < len(data) {
			val |= uint(data[i+2])
		}

		remaining := len(data) - i
		result = append(result, alphabet[(val>>18)&0x3F])
		result = append(result, alphabet[(val>>12)&0x3F])
		if remaining > 1 {
			result = append(result, alphabet[(val>>6)&0x3F])
		}
		if remaining > 2 {
			result = append(result, alphabet[val&0x3F])
		}
	}
	return string(result)
}

// base64URLDecode decodes a base64url-encoded string (no padding).
func base64URLDecode(s string) ([]byte, error) {
	const decodeMap = "" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\x3e\xff\xff" +
		"\x34\x35\x36\x37\x38\x39\x3a\x3b\x3c\x3d\xff\xff\xff\xff\xff\xff" +
		"\xff\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e" +
		"\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\xff\xff\xff\xff\x3f" +
		"\xff\x1a\x1b\x1c\x1d\x1e\x1f\x20\x21\x22\x23\x24\x25\x26\x27\x28" +
		"\x29\x2a\x2b\x2c\x2d\x2e\x2f\x30\x31\x32\x33\xff\xff\xff\xff\xff"

	// Add padding if necessary.
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}

	result := make([]byte, 0, len(s)*3/4)
	var buf uint
	var bits int
	for _, c := range []byte(s) {
		if c == '=' {
			break
		}
		if int(c) >= len(decodeMap) || decodeMap[c] == 0xff {
			return nil, fmt.Errorf("auth: invalid base64url character: %c", c)
		}
		buf = (buf << 6) | uint(decodeMap[c])
		bits += 6
		if bits >= 8 {
			bits -= 8
			result = append(result, byte(buf>>uint(bits)))
			buf &= (1 << uint(bits)) - 1
		}
	}
	return result, nil
}

// signHMAC computes an HMAC-SHA256 signature.
func signHMAC(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return base64URLEncode(mac.Sum(nil))
}

// GenerateToken creates a signed JWT-like token for the given user and role
// using HMAC-SHA256.
func GenerateToken(cfg AuthConfig, userID, role string) (string, error) {
	if cfg.JWTSecret == "" {
		return "", errors.New("auth: JWT secret must not be empty")
	}

	now := time.Now().UTC()
	claims := Claims{
		UserID:    userID,
		Role:      role,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(cfg.TokenExpiry).Unix(),
	}

	headerJSON := []byte(`{"alg":"HS256","typ":"JWT"}`)
	header := base64URLEncode(headerJSON)

	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: marshal claims: %w", err)
	}
	payload := base64URLEncode(payloadJSON)

	signingInput := header + "." + payload
	signature := signHMAC(cfg.JWTSecret, signingInput)

	return signingInput + "." + signature, nil
}

// ValidateToken parses and verifies a JWT-like token, returning the embedded
// claims on success.
func ValidateToken(cfg AuthConfig, tokenString string) (*Claims, error) {
	if cfg.JWTSecret == "" {
		return nil, errors.New("auth: JWT secret must not be empty")
	}

	parts := strings.SplitN(tokenString, ".", 3)
	if len(parts) != 3 {
		return nil, errors.New("auth: malformed token")
	}

	signingInput := parts[0] + "." + parts[1]
	expectedSig := signHMAC(cfg.JWTSecret, signingInput)

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, errors.New("auth: invalid token signature")
	}

	payloadJSON, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("auth: decode payload: %w", err)
	}

	claims := &Claims{}
	if err := json.Unmarshal(payloadJSON, claims); err != nil {
		return nil, fmt.Errorf("auth: unmarshal claims: %w", err)
	}

	if err := claims.Valid(); err != nil {
		return nil, err
	}
	return claims, nil
}

// ---------------------------------------------------------------------------
// API Key
// ---------------------------------------------------------------------------

// APIKey represents a stored API key.
type APIKey struct {
	// ID is the unique identifier for the API key.
	ID string `json:"id"`
	// Name is a human-readable label.
	Name string `json:"name"`
	// KeyHash is the bcrypt hash of the raw key.
	KeyHash string `json:"-"`
	// KeyPrefix stores the first 8 characters for display purposes.
	KeyPrefix string `json:"key_prefix"`
	// UserID is the owning user.
	UserID string `json:"user_id"`
	// Role is the authorisation role granted by this key.
	Role string `json:"role"`
	// CreatedAt records when the key was created.
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt is the optional expiry timestamp. Zero means no expiry.
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	// Revoked indicates whether the key has been explicitly revoked.
	Revoked bool `json:"revoked"`
}

// APIKeyStore defines the persistence operations for API keys.
type APIKeyStore interface {
	// Get retrieves an API key by its ID.
	Get(ctx context.Context, id string) (*APIKey, error)
	// Create stores a new API key and returns the raw key string that must
	// be shown to the user exactly once.
	Create(ctx context.Context, name, userID, role string, expiresAt time.Time) (rawKey string, key *APIKey, err error)
	// Revoke marks the key identified by id as revoked.
	Revoke(ctx context.Context, id string) error
	// List returns all non-revoked keys for the given user.
	List(ctx context.Context, userID string) ([]*APIKey, error)
}

// ---------------------------------------------------------------------------
// InMemoryAPIKeyStore
// ---------------------------------------------------------------------------

// InMemoryAPIKeyStore is an in-memory implementation of APIKeyStore suitable
// for testing and single-instance development deployments.
type InMemoryAPIKeyStore struct {
	mu   sync.RWMutex
	keys map[string]*APIKey // id -> key
}

// NewInMemoryAPIKeyStore creates a ready-to-use in-memory store.
func NewInMemoryAPIKeyStore() *InMemoryAPIKeyStore {
	return &InMemoryAPIKeyStore{
		keys: make(map[string]*APIKey),
	}
}

// generateRawKey produces a cryptographically random 32-byte hex-encoded key.
func generateRawKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate key: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Create generates a new API key, hashes it, stores it, and returns the raw
// key string.
func (s *InMemoryAPIKeyStore) Create(_ context.Context, name, userID, role string, expiresAt time.Time) (string, *APIKey, error) {
	rawKey, err := generateRawKey()
	if err != nil {
		return "", nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, fmt.Errorf("auth: hash key: %w", err)
	}

	prefix := rawKey
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}

	id, err := generateRawKey()
	if err != nil {
		return "", nil, err
	}
	id = id[:16] // shorter ID

	key := &APIKey{
		ID:        id,
		Name:      name,
		KeyHash:   string(hash),
		KeyPrefix: prefix,
		UserID:    userID,
		Role:      role,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: expiresAt,
		Revoked:   false,
	}

	s.mu.Lock()
	s.keys[id] = key
	s.mu.Unlock()

	return rawKey, key, nil
}

// Get retrieves the key by ID.
func (s *InMemoryAPIKeyStore) Get(_ context.Context, id string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.keys[id]
	if !ok {
		return nil, fmt.Errorf("auth: api key %s not found", id)
	}
	return key, nil
}

// Revoke marks the key as revoked.
func (s *InMemoryAPIKeyStore) Revoke(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, ok := s.keys[id]
	if !ok {
		return fmt.Errorf("auth: api key %s not found", id)
	}
	key.Revoked = true
	return nil
}

// List returns all non-revoked keys belonging to userID.
func (s *InMemoryAPIKeyStore) List(_ context.Context, userID string) ([]*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*APIKey
	for _, key := range s.keys {
		if key.UserID == userID && !key.Revoked {
			result = append(result, key)
		}
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// APIKeyAuth — validates raw API keys against the store
// ---------------------------------------------------------------------------

// APIKeyAuth validates raw API key strings.
type APIKeyAuth struct {
	store APIKeyStore
}

// NewAPIKeyAuth creates an APIKeyAuth backed by the given store.
func NewAPIKeyAuth(store APIKeyStore) *APIKeyAuth {
	return &APIKeyAuth{store: store}
}

// ValidateAPIKey checks the raw key against all stored (non-revoked) keys and
// returns Claims on success.
func (a *APIKeyAuth) ValidateAPIKey(ctx context.Context, rawKey string) (*Claims, error) {
	// We need to iterate over keys to find a matching hash. In a production
	// system this would use an indexed lookup by key prefix.
	store, ok := a.store.(*InMemoryAPIKeyStore)
	if !ok {
		return nil, errors.New("auth: unsupported key store type for validation")
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	for _, key := range store.keys {
		if key.Revoked {
			continue
		}
		if !key.ExpiresAt.IsZero() && time.Now().After(key.ExpiresAt) {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(key.KeyHash), []byte(rawKey)); err == nil {
			return &Claims{
				UserID:    key.UserID,
				Role:      key.Role,
				ExpiresAt: key.ExpiresAt.Unix(),
			}, nil
		}
	}
	return nil, errors.New("auth: invalid api key")
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

type contextKey string

const claimsKey contextKey = "auth_claims"

// ContextWithClaims returns a new context carrying the given claims.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFromContext extracts claims from the context, returning nil when not
// present.
func ClaimsFromContext(ctx context.Context) *Claims {
	claims, _ := ctx.Value(claimsKey).(*Claims)
	return claims
}

// ---------------------------------------------------------------------------
// HTTP Middleware
// ---------------------------------------------------------------------------

// AuthMiddleware returns an http.Handler that authenticates incoming requests
// using either a Bearer JWT token or an API key header. Authenticated claims
// are stored in the request context.
func AuthMiddleware(cfg AuthConfig, apiKeyAuth *APIKeyAuth) func(http.Handler) http.Handler {
	if cfg.APIKeyHeader == "" {
		cfg.APIKeyHeader = "X-API-Key"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var claims *Claims
			var err error

			// Try Bearer token first.
			if authHeader := r.Header.Get("Authorization"); authHeader != "" {
				if strings.HasPrefix(authHeader, "Bearer ") {
					token := strings.TrimPrefix(authHeader, "Bearer ")
					claims, err = ValidateToken(cfg, token)
					if err != nil {
						writeUnauthorized(w, "invalid bearer token: "+err.Error())
						return
					}
				}
			}

			// Fall back to API key.
			if claims == nil {
				if apiKey := r.Header.Get(cfg.APIKeyHeader); apiKey != "" && apiKeyAuth != nil {
					claims, err = apiKeyAuth.ValidateAPIKey(r.Context(), apiKey)
					if err != nil {
						writeUnauthorized(w, "invalid api key")
						return
					}
				}
			}

			if claims == nil {
				writeUnauthorized(w, "missing authentication credentials")
				return
			}

			ctx := ContextWithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeUnauthorized sends a 401 JSON response.
func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	resp := map[string]string{"error": message}
	_ = json.NewEncoder(w).Encode(resp)
}
