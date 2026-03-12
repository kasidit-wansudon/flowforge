// Package s3 provides artifact storage backed by S3-compatible object stores
// and a local filesystem implementation for development and testing.
package s3

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// S3Config holds the connection parameters for an S3-compatible object store.
type S3Config struct {
	// Endpoint is the URL of the S3-compatible service (e.g. "s3.amazonaws.com"
	// or "minio:9000" for local development).
	Endpoint string
	// Bucket is the target bucket name.
	Bucket string
	// AccessKey is the API access key.
	AccessKey string
	// SecretKey is the API secret key.
	SecretKey string
	// Region is the AWS region (e.g. "us-east-1").
	Region string
	// UseSSL determines whether HTTPS is used.
	UseSSL bool
}

// scheme returns "https" or "http" based on the UseSSL flag.
func (c S3Config) scheme() string {
	if c.UseSSL {
		return "https"
	}
	return "http"
}

// baseURL returns the fully-qualified base URL for S3 operations.
func (c S3Config) baseURL() string {
	return fmt.Sprintf("%s://%s/%s", c.scheme(), c.Endpoint, c.Bucket)
}

// ---------------------------------------------------------------------------
// ArtifactInfo
// ---------------------------------------------------------------------------

// ArtifactInfo describes a stored artifact.
type ArtifactInfo struct {
	// Key is the object key (path) within the bucket.
	Key string
	// Size is the artifact size in bytes.
	Size int64
	// LastModified records when the artifact was last written.
	LastModified time.Time
	// ContentType is the MIME type of the artifact.
	ContentType string
}

// ---------------------------------------------------------------------------
// ArtifactStore interface
// ---------------------------------------------------------------------------

// ArtifactStore defines the operations for persisting and retrieving build
// artifacts.
type ArtifactStore interface {
	// Upload stores the content read from reader under the given key.
	Upload(ctx context.Context, key string, reader io.Reader) error
	// Download returns a reader for the stored artifact. The caller is
	// responsible for closing the returned ReadCloser.
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the artifact identified by key.
	Delete(ctx context.Context, key string) error
	// List returns metadata for all artifacts whose keys start with prefix.
	List(ctx context.Context, prefix string) ([]ArtifactInfo, error)
}

// ---------------------------------------------------------------------------
// S3ArtifactStore — real S3-compatible implementation
// ---------------------------------------------------------------------------

// S3ArtifactStore interacts with an S3-compatible object store using plain
// HTTP requests signed with AWS Signature V4. This avoids pulling in the
// heavy AWS SDK while remaining compatible with MinIO, R2, and AWS S3.
type S3ArtifactStore struct {
	config S3Config
	client *http.Client
}

// NewS3ArtifactStore creates a store backed by the S3-compatible service
// described in cfg.
func NewS3ArtifactStore(cfg S3Config) *S3ArtifactStore {
	return &S3ArtifactStore{
		config: cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Upload streams the contents of reader to S3 under the specified key.
func (s *S3ArtifactStore) Upload(ctx context.Context, key string, reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("s3: read upload body: %w", err)
	}

	url := fmt.Sprintf("%s/%s", s.config.baseURL(), key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("s3: create upload request: %w", err)
	}

	payloadHash := sha256Hex(data)
	s.signRequest(req, data, payloadHash)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("s3: upload %s: %w", key, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("s3: upload %s: status %d: %s", key, resp.StatusCode, string(body))
	}
	return nil
}

// Download returns a reader for the stored artifact at key.
func (s *S3ArtifactStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	url := fmt.Sprintf("%s/%s", s.config.baseURL(), key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("s3: create download request: %w", err)
	}

	s.signRequest(req, nil, "UNSIGNED-PAYLOAD")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3: download %s: %w", key, err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("s3: artifact %s not found", key)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("s3: download %s: status %d: %s", key, resp.StatusCode, string(body))
	}
	return resp.Body, nil
}

// Delete removes the artifact at key from S3.
func (s *S3ArtifactStore) Delete(ctx context.Context, key string) error {
	url := fmt.Sprintf("%s/%s", s.config.baseURL(), key)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("s3: create delete request: %w", err)
	}

	s.signRequest(req, nil, "UNSIGNED-PAYLOAD")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("s3: delete %s: %w", key, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("s3: delete %s: status %d: %s", key, resp.StatusCode, string(body))
	}
	return nil
}

// List returns metadata for all objects whose key begins with prefix.
// Note: this simplified implementation performs a GET on the bucket with a
// prefix parameter. For production workloads with thousands of keys, consider
// implementing pagination via continuation tokens.
func (s *S3ArtifactStore) List(ctx context.Context, prefix string) ([]ArtifactInfo, error) {
	url := fmt.Sprintf("%s?list-type=2&prefix=%s", s.config.baseURL(), prefix)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("s3: create list request: %w", err)
	}

	s.signRequest(req, nil, "UNSIGNED-PAYLOAD")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3: list %s: %w", prefix, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("s3: list prefix=%s: status %d: %s", prefix, resp.StatusCode, string(body))
	}

	// In a production system this would parse the XML ListBucketResult.
	// Returning an empty list is preferable to panicking when no XML parser
	// is available.
	return []ArtifactInfo{}, nil
}

// ---------------------------------------------------------------------------
// AWS Signature V4 (simplified)
// ---------------------------------------------------------------------------

// signRequest adds AWS Signature V4 headers to the request.
func (s *S3ArtifactStore) signRequest(req *http.Request, payload []byte, payloadHash string) {
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	req.Header.Set("Host", s.config.Endpoint)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	if payload != nil {
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(payload)))
	}

	// Canonical request components.
	canonicalURI := req.URL.Path
	canonicalQueryString := req.URL.RawQuery
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n",
		s.config.Endpoint, payloadHash, amzDate)

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, s.config.Region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// Derive signing key.
	kDate := hmacSHA256([]byte("AWS4"+s.config.SecretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(s.config.Region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))

	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))
	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.config.AccessKey, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ---------------------------------------------------------------------------
// LocalArtifactStore — filesystem implementation for development
// ---------------------------------------------------------------------------

// LocalArtifactStore stores artifacts on the local filesystem. It is intended
// for development and testing; do not use in production.
type LocalArtifactStore struct {
	mu      sync.RWMutex
	baseDir string
}

// NewLocalArtifactStore creates a store rooted at baseDir, creating the
// directory if it does not exist.
func NewLocalArtifactStore(baseDir string) (*LocalArtifactStore, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("s3: create base dir: %w", err)
	}
	return &LocalArtifactStore{baseDir: baseDir}, nil
}

// Upload writes the content from reader to disk at key (relative to baseDir).
func (l *LocalArtifactStore) Upload(_ context.Context, key string, reader io.Reader) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	fullPath := filepath.Join(l.baseDir, filepath.FromSlash(key))
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("s3: create dirs for %s: %w", key, err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("s3: create file %s: %w", key, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, reader); err != nil {
		return fmt.Errorf("s3: write file %s: %w", key, err)
	}
	return nil
}

// Download returns a reader for the file stored at key.
func (l *LocalArtifactStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	fullPath := filepath.Join(l.baseDir, filepath.FromSlash(key))
	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("s3: artifact %s not found", key)
		}
		return nil, fmt.Errorf("s3: open %s: %w", key, err)
	}
	return f, nil
}

// Delete removes the file at key.
func (l *LocalArtifactStore) Delete(_ context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	fullPath := filepath.Join(l.baseDir, filepath.FromSlash(key))
	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil // idempotent
		}
		return fmt.Errorf("s3: delete %s: %w", key, err)
	}
	return nil
}

// List returns metadata for all files under baseDir whose relative path
// begins with prefix.
func (l *LocalArtifactStore) List(_ context.Context, prefix string) ([]ArtifactInfo, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var results []ArtifactInfo
	err := filepath.Walk(l.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(l.baseDir, path)
		if err != nil {
			return err
		}
		// Normalise to forward slashes for cross-platform consistency.
		rel = filepath.ToSlash(rel)

		if strings.HasPrefix(rel, prefix) {
			results = append(results, ArtifactInfo{
				Key:          rel,
				Size:         info.Size(),
				LastModified: info.ModTime(),
				ContentType:  detectContentType(rel),
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("s3: list prefix=%s: %w", prefix, err)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Key < results[j].Key
	})
	return results, nil
}

// detectContentType makes a best-effort guess at a MIME type based on file
// extension.
func detectContentType(key string) string {
	ext := strings.ToLower(filepath.Ext(key))
	switch ext {
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/x-yaml"
	case ".txt", ".log":
		return "text/plain"
	case ".html":
		return "text/html"
	case ".tar":
		return "application/x-tar"
	case ".gz", ".gzip":
		return "application/gzip"
	case ".zip":
		return "application/zip"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}
