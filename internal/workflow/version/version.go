// Package version provides workflow versioning with content-addressable storage.
package version

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/workflow/definition"
)

// WorkflowVersion represents a specific version of a workflow definition.
type WorkflowVersion struct {
	ID         string                       `json:"id"`
	WorkflowID string                       `json:"workflow_id"`
	Version    int                          `json:"version"`
	Definition *definition.WorkflowDefinition `json:"definition"`
	CreatedAt  time.Time                    `json:"created_at"`
	Hash       string                       `json:"hash"`
}

// VersionStore defines the interface for persisting workflow versions.
type VersionStore interface {
	// Save persists a new workflow version.
	Save(version *WorkflowVersion) error
	// Get retrieves a specific version by workflow ID and version number.
	Get(workflowID string, version int) (*WorkflowVersion, error)
	// List returns all versions of a workflow, ordered by version number descending.
	List(workflowID string) ([]*WorkflowVersion, error)
	// GetLatest returns the most recent version of a workflow.
	GetLatest(workflowID string) (*WorkflowVersion, error)
	// GetByHash retrieves a version by its content hash.
	GetByHash(hash string) (*WorkflowVersion, error)
}

// InMemoryVersionStore is a thread-safe in-memory implementation of VersionStore.
type InMemoryVersionStore struct {
	mu       sync.RWMutex
	versions map[string][]*WorkflowVersion // workflowID -> versions
	byHash   map[string]*WorkflowVersion   // hash -> version
	byID     map[string]*WorkflowVersion   // version ID -> version
}

// NewInMemoryVersionStore creates a new InMemoryVersionStore.
func NewInMemoryVersionStore() *InMemoryVersionStore {
	return &InMemoryVersionStore{
		versions: make(map[string][]*WorkflowVersion),
		byHash:   make(map[string]*WorkflowVersion),
		byID:     make(map[string]*WorkflowVersion),
	}
}

// Save persists a new workflow version.
func (s *InMemoryVersionStore) Save(version *WorkflowVersion) error {
	if version == nil {
		return fmt.Errorf("version is nil")
	}
	if version.ID == "" {
		return fmt.Errorf("version ID is required")
	}
	if version.WorkflowID == "" {
		return fmt.Errorf("workflow ID is required")
	}
	if version.Definition == nil {
		return fmt.Errorf("definition is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate version ID.
	if _, exists := s.byID[version.ID]; exists {
		return fmt.Errorf("version with ID %q already exists", version.ID)
	}

	// Check for duplicate version number for the same workflow.
	for _, v := range s.versions[version.WorkflowID] {
		if v.Version == version.Version {
			return fmt.Errorf("version %d already exists for workflow %q", version.Version, version.WorkflowID)
		}
	}

	// Compute hash if not set.
	if version.Hash == "" {
		hash, err := ComputeHash(version.Definition)
		if err != nil {
			return fmt.Errorf("failed to compute hash: %w", err)
		}
		version.Hash = hash
	}

	// Set created time if not set.
	if version.CreatedAt.IsZero() {
		version.CreatedAt = time.Now().UTC()
	}

	s.versions[version.WorkflowID] = append(s.versions[version.WorkflowID], version)
	s.byHash[version.Hash] = version
	s.byID[version.ID] = version

	return nil
}

// Get retrieves a specific version by workflow ID and version number.
func (s *InMemoryVersionStore) Get(workflowID string, versionNum int) (*WorkflowVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions, ok := s.versions[workflowID]
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", workflowID)
	}

	for _, v := range versions {
		if v.Version == versionNum {
			return v, nil
		}
	}

	return nil, fmt.Errorf("version %d not found for workflow %q", versionNum, workflowID)
}

// List returns all versions of a workflow, ordered by version number descending.
func (s *InMemoryVersionStore) List(workflowID string) ([]*WorkflowVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions, ok := s.versions[workflowID]
	if !ok {
		return nil, nil
	}

	// Return a copy sorted by version descending.
	result := make([]*WorkflowVersion, len(versions))
	copy(result, versions)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Version > result[j].Version
	})

	return result, nil
}

// GetLatest returns the most recent version of a workflow.
func (s *InMemoryVersionStore) GetLatest(workflowID string) (*WorkflowVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions, ok := s.versions[workflowID]
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", workflowID)
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("no versions found for workflow %q", workflowID)
	}

	var latest *WorkflowVersion
	for _, v := range versions {
		if latest == nil || v.Version > latest.Version {
			latest = v
		}
	}

	return latest, nil
}

// GetByHash retrieves a version by its content hash.
func (s *InMemoryVersionStore) GetByHash(hash string) (*WorkflowVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.byHash[hash]
	if !ok {
		return nil, fmt.Errorf("version with hash %q not found", hash)
	}

	return v, nil
}

// ComputeHash computes the SHA256 hash of a workflow definition.
// The definition is serialized to canonical JSON to ensure consistent hashing.
func ComputeHash(def *definition.WorkflowDefinition) (string, error) {
	if def == nil {
		return "", fmt.Errorf("definition is nil")
	}

	data, err := json.Marshal(def)
	if err != nil {
		return "", fmt.Errorf("failed to marshal definition: %w", err)
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// DiffResult represents the difference between two workflow versions.
type DiffResult struct {
	Version1    int      `json:"version1"`
	Version2    int      `json:"version2"`
	Hash1       string   `json:"hash1"`
	Hash2       string   `json:"hash2"`
	Identical   bool     `json:"identical"`
	Differences []string `json:"differences"`
}

// Diff compares two workflow versions and returns a simple text diff.
func Diff(v1, v2 *WorkflowVersion) (*DiffResult, error) {
	if v1 == nil || v2 == nil {
		return nil, fmt.Errorf("both versions must be non-nil")
	}

	result := &DiffResult{
		Version1: v1.Version,
		Version2: v2.Version,
		Hash1:    v1.Hash,
		Hash2:    v2.Hash,
	}

	if v1.Hash == v2.Hash && v1.Hash != "" {
		result.Identical = true
		return result, nil
	}

	var diffs []string

	d1 := v1.Definition
	d2 := v2.Definition

	if d1 == nil || d2 == nil {
		return nil, fmt.Errorf("both version definitions must be non-nil")
	}

	// Compare top-level fields.
	if d1.Name != d2.Name {
		diffs = append(diffs, fmt.Sprintf("name changed: %q -> %q", d1.Name, d2.Name))
	}
	if d1.Description != d2.Description {
		diffs = append(diffs, fmt.Sprintf("description changed: %q -> %q", d1.Description, d2.Description))
	}
	if d1.Timeout.Duration != d2.Timeout.Duration {
		diffs = append(diffs, fmt.Sprintf("timeout changed: %s -> %s", d1.Timeout.Duration, d2.Timeout.Duration))
	}

	// Compare triggers.
	if len(d1.Triggers) != len(d2.Triggers) {
		diffs = append(diffs, fmt.Sprintf("trigger count changed: %d -> %d", len(d1.Triggers), len(d2.Triggers)))
	} else {
		for i := range d1.Triggers {
			if d1.Triggers[i].Type != d2.Triggers[i].Type {
				diffs = append(diffs, fmt.Sprintf("trigger[%d] type changed: %q -> %q", i, d1.Triggers[i].Type, d2.Triggers[i].Type))
			}
		}
	}

	// Compare tasks.
	tasks1 := buildTaskMap(d1.Tasks)
	tasks2 := buildTaskMap(d2.Tasks)

	// Find added tasks.
	for id := range tasks2 {
		if _, exists := tasks1[id]; !exists {
			diffs = append(diffs, fmt.Sprintf("task added: %q", id))
		}
	}

	// Find removed tasks.
	for id := range tasks1 {
		if _, exists := tasks2[id]; !exists {
			diffs = append(diffs, fmt.Sprintf("task removed: %q", id))
		}
	}

	// Find modified tasks.
	for id, t1 := range tasks1 {
		t2, exists := tasks2[id]
		if !exists {
			continue
		}
		taskDiffs := diffTasks(id, t1, t2)
		diffs = append(diffs, taskDiffs...)
	}

	// Compare metadata.
	for key := range d1.Metadata {
		if _, exists := d2.Metadata[key]; !exists {
			diffs = append(diffs, fmt.Sprintf("metadata key removed: %q", key))
		} else if d1.Metadata[key] != d2.Metadata[key] {
			diffs = append(diffs, fmt.Sprintf("metadata %q changed: %q -> %q", key, d1.Metadata[key], d2.Metadata[key]))
		}
	}
	for key := range d2.Metadata {
		if _, exists := d1.Metadata[key]; !exists {
			diffs = append(diffs, fmt.Sprintf("metadata key added: %q", key))
		}
	}

	result.Identical = len(diffs) == 0
	result.Differences = diffs
	return result, nil
}

// buildTaskMap creates a map of task ID to TaskDefinition for comparison.
func buildTaskMap(tasks []definition.TaskDefinition) map[string]definition.TaskDefinition {
	m := make(map[string]definition.TaskDefinition, len(tasks))
	for _, t := range tasks {
		m[t.ID] = t
	}
	return m
}

// diffTasks compares two task definitions and returns differences.
func diffTasks(id string, t1, t2 definition.TaskDefinition) []string {
	var diffs []string
	prefix := fmt.Sprintf("task %q", id)

	if t1.Name != t2.Name {
		diffs = append(diffs, fmt.Sprintf("%s name changed: %q -> %q", prefix, t1.Name, t2.Name))
	}
	if t1.Type != t2.Type {
		diffs = append(diffs, fmt.Sprintf("%s type changed: %q -> %q", prefix, t1.Type, t2.Type))
	}
	if t1.Condition != t2.Condition {
		diffs = append(diffs, fmt.Sprintf("%s condition changed: %q -> %q", prefix, t1.Condition, t2.Condition))
	}
	if t1.Timeout.Duration != t2.Timeout.Duration {
		diffs = append(diffs, fmt.Sprintf("%s timeout changed: %s -> %s", prefix, t1.Timeout.Duration, t2.Timeout.Duration))
	}

	// Compare dependencies.
	deps1 := strings.Join(sorted(t1.DependsOn), ",")
	deps2 := strings.Join(sorted(t2.DependsOn), ",")
	if deps1 != deps2 {
		diffs = append(diffs, fmt.Sprintf("%s dependencies changed: [%s] -> [%s]", prefix, deps1, deps2))
	}

	// Compare config (shallow).
	cfg1, _ := json.Marshal(t1.Config)
	cfg2, _ := json.Marshal(t2.Config)
	if string(cfg1) != string(cfg2) {
		diffs = append(diffs, fmt.Sprintf("%s config changed", prefix))
	}

	return diffs
}

// sorted returns a sorted copy of a string slice.
func sorted(s []string) []string {
	cp := make([]string, len(s))
	copy(cp, s)
	sort.Strings(cp)
	return cp
}
