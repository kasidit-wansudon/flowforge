// Package redis provides a Redis-backed cache, distributed locking, and
// workflow state serialisation for FlowForge.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------

// Cache wraps a go-redis client with convenience methods for common caching
// operations. All methods accept a context.Context for cancellation and
// deadline propagation.
type Cache struct {
	client *redis.Client
	prefix string
}

// CacheOption configures optional Cache parameters.
type CacheOption func(*Cache)

// WithPrefix sets a key prefix applied to all cache operations so that
// multiple applications can share a single Redis instance.
func WithPrefix(prefix string) CacheOption {
	return func(c *Cache) { c.prefix = prefix }
}

// NewCache creates a Redis cache connected to the given address.
// addr is in "host:port" format. Pass an empty password for unauthenticated
// servers.
func NewCache(addr, password string, db int, opts ...CacheOption) (*Cache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
		MinIdleConns: 5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis: ping: %w", err)
	}

	c := &Cache{
		client: client,
		prefix: "flowforge:",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Close gracefully closes the underlying Redis connection.
func (c *Cache) Close() error {
	return c.client.Close()
}

// Client returns the raw go-redis client for advanced use cases.
func (c *Cache) Client() *redis.Client {
	return c.client
}

// key prepends the configured prefix to the provided key.
func (c *Cache) key(k string) string {
	return c.prefix + k
}

// Get retrieves the value for key. Returns ("", nil) when the key does not exist.
func (c *Cache) Get(ctx context.Context, key string) (string, error) {
	val, err := c.client.Get(ctx, c.key(key)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("redis: get %s: %w", key, err)
	}
	return val, nil
}

// Set stores a value with no expiration.
func (c *Cache) Set(ctx context.Context, key, value string) error {
	if err := c.client.Set(ctx, c.key(key), value, 0).Err(); err != nil {
		return fmt.Errorf("redis: set %s: %w", key, err)
	}
	return nil
}

// SetWithTTL stores a value that expires after ttl.
func (c *Cache) SetWithTTL(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := c.client.Set(ctx, c.key(key), value, ttl).Err(); err != nil {
		return fmt.Errorf("redis: set %s (ttl=%s): %w", key, ttl, err)
	}
	return nil
}

// Delete removes one or more keys.
func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	prefixed := make([]string, len(keys))
	for i, k := range keys {
		prefixed[i] = c.key(k)
	}
	if err := c.client.Del(ctx, prefixed...).Err(); err != nil {
		return fmt.Errorf("redis: delete: %w", err)
	}
	return nil
}

// Exists returns true when the key exists.
func (c *Cache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.client.Exists(ctx, c.key(key)).Result()
	if err != nil {
		return false, fmt.Errorf("redis: exists %s: %w", key, err)
	}
	return n > 0, nil
}

// ---------------------------------------------------------------------------
// WorkflowState caching
// ---------------------------------------------------------------------------

// WorkflowState captures the runtime state of a workflow run for fast access.
type WorkflowState struct {
	RunID        string            `json:"run_id"`
	WorkflowID   string            `json:"workflow_id"`
	Status       string            `json:"status"`
	CurrentTasks []string          `json:"current_tasks"`
	Variables    map[string]string `json:"variables"`
	StartedAt    time.Time         `json:"started_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

const workflowStatePrefix = "wfstate:"

// CacheWorkflowState serialises and stores the given state in Redis with a TTL.
func (c *Cache) CacheWorkflowState(ctx context.Context, state *WorkflowState, ttl time.Duration) error {
	if state == nil {
		return errors.New("redis: cannot cache nil workflow state")
	}
	state.UpdatedAt = time.Now().UTC()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("redis: marshal workflow state: %w", err)
	}

	key := workflowStatePrefix + state.RunID
	return c.SetWithTTL(ctx, key, string(data), ttl)
}

// GetWorkflowState retrieves and deserialises a cached workflow state.
// Returns (nil, nil) when the key does not exist.
func (c *Cache) GetWorkflowState(ctx context.Context, runID string) (*WorkflowState, error) {
	key := workflowStatePrefix + runID
	val, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if val == "" {
		return nil, nil
	}

	state := &WorkflowState{}
	if err := json.Unmarshal([]byte(val), state); err != nil {
		return nil, fmt.Errorf("redis: unmarshal workflow state: %w", err)
	}
	return state, nil
}

// DeleteWorkflowState removes the cached state for the given run.
func (c *Cache) DeleteWorkflowState(ctx context.Context, runID string) error {
	key := workflowStatePrefix + runID
	return c.Delete(ctx, key)
}

// ---------------------------------------------------------------------------
// DistributedLock
// ---------------------------------------------------------------------------

// DistributedLock provides a simple Redis-based distributed lock using SET NX.
// It is suitable for leader election and coordination across multiple
// FlowForge instances.
type DistributedLock struct {
	client *redis.Client
	prefix string
	// value is a unique token used to ensure only the lock holder can release.
	value string
}

// NewDistributedLock creates a lock handle backed by the given Cache.
func NewDistributedLock(cache *Cache) *DistributedLock {
	return &DistributedLock{
		client: cache.client,
		prefix: cache.prefix + "lock:",
		value:  uuid.New().String(),
	}
}

// Acquire attempts to acquire the named lock with the given TTL. Returns
// (true, nil) when the lock is successfully obtained and (false, nil) when
// the lock is already held by another process.
func (l *DistributedLock) Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ok, err := l.client.SetNX(ctx, l.prefix+key, l.value, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("redis: acquire lock %s: %w", key, err)
	}
	return ok, nil
}

// Release releases the named lock, but only if the current process is the
// holder (compare-and-delete via Lua script for atomicity).
func (l *DistributedLock) Release(ctx context.Context, key string) error {
	// Lua script: delete key only if the stored value matches our token.
	const script = `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		end
		return 0
	`
	result, err := l.client.Eval(ctx, script, []string{l.prefix + key}, l.value).Int64()
	if err != nil {
		return fmt.Errorf("redis: release lock %s: %w", key, err)
	}
	if result == 0 {
		return fmt.Errorf("redis: lock %s not held or already expired", key)
	}
	return nil
}

// Extend resets the TTL of a held lock. Fails if the lock is not held by
// this process.
func (l *DistributedLock) Extend(ctx context.Context, key string, ttl time.Duration) error {
	const script = `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		end
		return 0
	`
	result, err := l.client.Eval(ctx, script, []string{l.prefix + key}, l.value, ttl.Milliseconds()).Int64()
	if err != nil {
		return fmt.Errorf("redis: extend lock %s: %w", key, err)
	}
	if result == 0 {
		return fmt.Errorf("redis: lock %s not held or already expired", key)
	}
	return nil
}
