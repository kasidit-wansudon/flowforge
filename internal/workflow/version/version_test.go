package version_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/workflow/definition"
	"github.com/kasidit-wansudon/flowforge/internal/workflow/version"
)

// --- helpers ---

func makeDefinition(name string) *definition.WorkflowDefinition {
	return &definition.WorkflowDefinition{
		ID:          "wf-" + name,
		Name:        name,
		Description: "test workflow " + name,
		Version:     1,
		Tasks: []definition.TaskDefinition{
			{
				ID:   "task-1",
				Name: "First Task",
				Type: "script",
				Config: map[string]any{
					"script": "echo hello",
				},
			},
		},
	}
}

func makeVersion(workflowID string, vNum int, def *definition.WorkflowDefinition) *version.WorkflowVersion {
	return &version.WorkflowVersion{
		ID:         fmt.Sprintf("%s-v%d", workflowID, vNum),
		WorkflowID: workflowID,
		Version:    vNum,
		Definition: def,
	}
}

// --- ComputeHash ---

func TestComputeHash_NilDefinition(t *testing.T) {
	_, err := version.ComputeHash(nil)
	if err == nil {
		t.Fatal("expected error for nil definition, got nil")
	}
}

func TestComputeHash_SameInputProducesSameHash(t *testing.T) {
	def := makeDefinition("alpha")
	h1, err := version.ComputeHash(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h2, err := version.ComputeHash(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h1 != h2 {
		t.Errorf("identical definitions produced different hashes: %q vs %q", h1, h2)
	}
}

func TestComputeHash_DifferentDefinitionsProduceDifferentHashes(t *testing.T) {
	def1 := makeDefinition("alpha")
	def2 := makeDefinition("beta")

	h1, err := version.ComputeHash(def1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h2, err := version.ComputeHash(def2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h1 == h2 {
		t.Error("different definitions produced the same hash")
	}
}

func TestComputeHash_IsHexString(t *testing.T) {
	def := makeDefinition("alpha")
	h, err := version.ComputeHash(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// SHA256 produces 64 hex chars
	if len(h) != 64 {
		t.Errorf("expected 64-char hex string, got length %d: %q", len(h), h)
	}
}

// --- InMemoryVersionStore: Save ---

func TestInMemoryVersionStore_Save_NilVersion(t *testing.T) {
	store := version.NewInMemoryVersionStore()
	if err := store.Save(nil); err == nil {
		t.Fatal("expected error saving nil version")
	}
}

func TestInMemoryVersionStore_Save_MissingFields(t *testing.T) {
	store := version.NewInMemoryVersionStore()

	t.Run("missing ID", func(t *testing.T) {
		v := makeVersion("wf-1", 1, makeDefinition("x"))
		v.ID = ""
		if err := store.Save(v); err == nil {
			t.Error("expected error for missing ID")
		}
	})

	t.Run("missing WorkflowID", func(t *testing.T) {
		v := makeVersion("wf-1", 1, makeDefinition("x"))
		v.WorkflowID = ""
		if err := store.Save(v); err == nil {
			t.Error("expected error for missing WorkflowID")
		}
	})

	t.Run("missing Definition", func(t *testing.T) {
		v := makeVersion("wf-1", 1, makeDefinition("x"))
		v.Definition = nil
		if err := store.Save(v); err == nil {
			t.Error("expected error for missing Definition")
		}
	})
}

func TestInMemoryVersionStore_Save_SetsHashAndTime(t *testing.T) {
	store := version.NewInMemoryVersionStore()
	v := makeVersion("wf-1", 1, makeDefinition("x"))
	before := time.Now()

	if err := store.Save(v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v.Hash == "" {
		t.Error("Hash should have been set by Save")
	}
	if v.CreatedAt.IsZero() {
		t.Error("CreatedAt should have been set by Save")
	}
	if v.CreatedAt.Before(before) {
		t.Error("CreatedAt is before the test started")
	}
}

func TestInMemoryVersionStore_Save_DuplicateIDRejected(t *testing.T) {
	store := version.NewInMemoryVersionStore()
	v := makeVersion("wf-1", 1, makeDefinition("x"))
	if err := store.Save(v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Try to save with same ID but different version number.
	v2 := makeVersion("wf-1", 2, makeDefinition("y"))
	v2.ID = v.ID
	if err := store.Save(v2); err == nil {
		t.Error("expected error for duplicate ID")
	}
}

func TestInMemoryVersionStore_Save_DuplicateVersionNumberRejected(t *testing.T) {
	store := version.NewInMemoryVersionStore()
	v1 := makeVersion("wf-1", 1, makeDefinition("x"))
	if err := store.Save(v1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v2 := makeVersion("wf-1", 1, makeDefinition("x"))
	v2.ID = "wf-1-v1-dup"
	if err := store.Save(v2); err == nil {
		t.Error("expected error for duplicate version number")
	}
}

// --- InMemoryVersionStore: Get ---

func TestInMemoryVersionStore_Get_Success(t *testing.T) {
	store := version.NewInMemoryVersionStore()
	def := makeDefinition("x")
	v := makeVersion("wf-1", 1, def)
	if err := store.Save(v); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := store.Get("wf-1", 1)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != v.ID {
		t.Errorf("got ID %q, want %q", got.ID, v.ID)
	}
}

func TestInMemoryVersionStore_Get_NotFound(t *testing.T) {
	store := version.NewInMemoryVersionStore()
	_, err := store.Get("nonexistent", 1)
	if err == nil {
		t.Fatal("expected error for unknown workflow")
	}
}

// --- InMemoryVersionStore: List ---

func TestInMemoryVersionStore_List_EmptyWorkflow(t *testing.T) {
	store := version.NewInMemoryVersionStore()
	versions, err := store.List("no-such-workflow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("expected 0 versions, got %d", len(versions))
	}
}

func TestInMemoryVersionStore_List_SortedDescending(t *testing.T) {
	store := version.NewInMemoryVersionStore()

	for i := 1; i <= 4; i++ {
		v := makeVersion("wf-list", i, makeDefinition(fmt.Sprintf("v%d", i)))
		if err := store.Save(v); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}

	versions, err := store.List("wf-list")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(versions) != 4 {
		t.Fatalf("expected 4 versions, got %d", len(versions))
	}
	for i := 1; i < len(versions); i++ {
		if versions[i-1].Version <= versions[i].Version {
			t.Errorf("versions not sorted descending at index %d: %d, %d", i, versions[i-1].Version, versions[i].Version)
		}
	}
}

// --- InMemoryVersionStore: GetLatest ---

func TestInMemoryVersionStore_GetLatest_Success(t *testing.T) {
	store := version.NewInMemoryVersionStore()

	for i := 1; i <= 3; i++ {
		v := makeVersion("wf-latest", i, makeDefinition(fmt.Sprintf("v%d", i)))
		if err := store.Save(v); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}

	latest, err := store.GetLatest("wf-latest")
	if err != nil {
		t.Fatalf("GetLatest failed: %v", err)
	}
	if latest.Version != 3 {
		t.Errorf("expected latest version 3, got %d", latest.Version)
	}
}

func TestInMemoryVersionStore_GetLatest_NotFound(t *testing.T) {
	store := version.NewInMemoryVersionStore()
	_, err := store.GetLatest("unknown-workflow")
	if err == nil {
		t.Fatal("expected error for unknown workflow")
	}
}

// --- InMemoryVersionStore: GetByHash ---

func TestInMemoryVersionStore_GetByHash_Success(t *testing.T) {
	store := version.NewInMemoryVersionStore()
	def := makeDefinition("hashtest")
	v := makeVersion("wf-hash", 1, def)
	if err := store.Save(v); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := store.GetByHash(v.Hash)
	if err != nil {
		t.Fatalf("GetByHash failed: %v", err)
	}
	if got.ID != v.ID {
		t.Errorf("got ID %q, want %q", got.ID, v.ID)
	}
}

func TestInMemoryVersionStore_GetByHash_NotFound(t *testing.T) {
	store := version.NewInMemoryVersionStore()
	_, err := store.GetByHash("deadbeef")
	if err == nil {
		t.Fatal("expected error for unknown hash")
	}
}

// --- Diff ---

func TestDiff_IdenticalVersions(t *testing.T) {
	def := makeDefinition("same")
	hash, _ := version.ComputeHash(def)

	v1 := &version.WorkflowVersion{Version: 1, Hash: hash, Definition: def}
	v2 := &version.WorkflowVersion{Version: 2, Hash: hash, Definition: def}

	result, err := version.Diff(v1, v2)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if !result.Identical {
		t.Error("expected identical result for same hash")
	}
	if len(result.Differences) != 0 {
		t.Errorf("expected no differences, got: %v", result.Differences)
	}
}

func TestDiff_NameChange(t *testing.T) {
	def1 := makeDefinition("OriginalName")
	def2 := makeDefinition("UpdatedName")

	v1 := &version.WorkflowVersion{Version: 1, Hash: "h1", Definition: def1}
	v2 := &version.WorkflowVersion{Version: 2, Hash: "h2", Definition: def2}

	result, err := version.Diff(v1, v2)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if result.Identical {
		t.Error("expected non-identical result")
	}
	found := false
	for _, d := range result.Differences {
		if len(d) > 0 && containsSubstring(d, "name changed") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'name changed' difference, got: %v", result.Differences)
	}
}

func TestDiff_TaskAdded(t *testing.T) {
	def1 := makeDefinition("base")
	def2 := makeDefinition("base")
	def2.Name = def1.Name // same name to isolate task diff
	def2.Tasks = append(def2.Tasks, definition.TaskDefinition{
		ID:   "task-2",
		Name: "Second Task",
		Type: "http",
	})

	v1 := &version.WorkflowVersion{Version: 1, Hash: "h1", Definition: def1}
	v2 := &version.WorkflowVersion{Version: 2, Hash: "h2", Definition: def2}

	result, err := version.Diff(v1, v2)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if result.Identical {
		t.Error("expected non-identical result")
	}
	found := false
	for _, d := range result.Differences {
		if containsSubstring(d, "task added") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'task added' difference, got: %v", result.Differences)
	}
}

func TestDiff_NilVersionsReturnsError(t *testing.T) {
	def := makeDefinition("x")
	v1 := &version.WorkflowVersion{Version: 1, Hash: "h1", Definition: def}

	_, err := version.Diff(v1, nil)
	if err == nil {
		t.Error("expected error when v2 is nil")
	}
	_, err = version.Diff(nil, v1)
	if err == nil {
		t.Error("expected error when v1 is nil")
	}
}

func TestDiff_VersionNumbers(t *testing.T) {
	def1 := makeDefinition("x")
	def2 := makeDefinition("y")

	v1 := &version.WorkflowVersion{Version: 3, Hash: "h1", Definition: def1}
	v2 := &version.WorkflowVersion{Version: 7, Hash: "h2", Definition: def2}

	result, err := version.Diff(v1, v2)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if result.Version1 != 3 {
		t.Errorf("expected Version1=3, got %d", result.Version1)
	}
	if result.Version2 != 7 {
		t.Errorf("expected Version2=7, got %d", result.Version2)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
