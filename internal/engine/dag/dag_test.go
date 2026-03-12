package dag

import (
	"testing"
	"time"
)

func TestNewDAG(t *testing.T) {
	d := NewDAG("wf-1", "test workflow")
	if d.ID != "wf-1" {
		t.Errorf("expected ID wf-1, got %s", d.ID)
	}
	if d.Name != "test workflow" {
		t.Errorf("expected Name 'test workflow', got %s", d.Name)
	}
	if d.Nodes == nil {
		t.Fatal("Nodes map should be initialised")
	}
	if len(d.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(d.Nodes))
	}
	if d.Edges == nil {
		t.Fatal("Edges slice should be initialised")
	}
}

func TestAddNode(t *testing.T) {
	d := NewDAG("wf-1", "test")
	err := d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := d.Nodes["a"]; !ok {
		t.Error("node 'a' should be in the DAG")
	}
}

func TestAddNodeDuplicate(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	err := d.AddNode(&Node{ID: "a", Name: "A2", Type: "http"})
	if err == nil {
		t.Fatal("expected ErrDuplicateNode error")
	}
	if !containsErr(err, ErrDuplicateNode) {
		t.Errorf("expected ErrDuplicateNode, got %v", err)
	}
}

func TestAddNodeNil(t *testing.T) {
	d := NewDAG("wf-1", "test")
	err := d.AddNode(nil)
	if err == nil {
		t.Fatal("expected error when adding nil node")
	}
}

func TestAddNodeEmptyID(t *testing.T) {
	d := NewDAG("wf-1", "test")
	err := d.AddNode(&Node{ID: "", Name: "A", Type: "http"})
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestAddEdge(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})

	err := d.AddEdge(Edge{From: "a", To: "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(d.Edges))
	}
}

func TestAddEdgeSelfLoop(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})

	err := d.AddEdge(Edge{From: "a", To: "a"})
	if err == nil {
		t.Fatal("expected ErrSelfLoop")
	}
	if !containsErr(err, ErrSelfLoop) {
		t.Errorf("expected ErrSelfLoop, got %v", err)
	}
}

func TestAddEdgeDuplicate(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})
	_ = d.AddEdge(Edge{From: "a", To: "b"})

	err := d.AddEdge(Edge{From: "a", To: "b"})
	if err == nil {
		t.Fatal("expected ErrDuplicateEdge")
	}
	if !containsErr(err, ErrDuplicateEdge) {
		t.Errorf("expected ErrDuplicateEdge, got %v", err)
	}
}

func TestAddEdgeMissingFromNode(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})

	err := d.AddEdge(Edge{From: "a", To: "b"})
	if err == nil {
		t.Fatal("expected ErrEdgeNodeMissing")
	}
	if !containsErr(err, ErrEdgeNodeMissing) {
		t.Errorf("expected ErrEdgeNodeMissing, got %v", err)
	}
}

func TestAddEdgeMissingToNode(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})

	err := d.AddEdge(Edge{From: "a", To: "b"})
	if err == nil {
		t.Fatal("expected ErrEdgeNodeMissing")
	}
}

func TestValidateEmptyDAG(t *testing.T) {
	d := NewDAG("wf-1", "test")
	err := d.Validate()
	if err != ErrEmptyDAG {
		t.Errorf("expected ErrEmptyDAG, got %v", err)
	}
}

func TestValidateSingleNode(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})

	err := d.Validate()
	if err != nil {
		t.Fatalf("single node DAG should be valid, got %v", err)
	}
}

func TestValidateLinearChain(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})
	_ = d.AddNode(&Node{ID: "c", Name: "C", Type: "http"})
	_ = d.AddEdge(Edge{From: "a", To: "b"})
	_ = d.AddEdge(Edge{From: "b", To: "c"})

	if err := d.Validate(); err != nil {
		t.Fatalf("linear chain should be valid: %v", err)
	}
}

func TestDetectCyclesNoCycle(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})
	_ = d.AddEdge(Edge{From: "a", To: "b"})

	err := d.DetectCycles()
	if err != nil {
		t.Fatalf("expected no cycle, got %v", err)
	}
}

func TestDetectCyclesWithCycle(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http", DependsOn: []string{"b"}})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http", DependsOn: []string{"a"}})

	err := d.DetectCycles()
	if err != ErrCycleDetected {
		t.Errorf("expected ErrCycleDetected, got %v", err)
	}
}

func TestTopologicalSortSimple(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})
	_ = d.AddNode(&Node{ID: "c", Name: "C", Type: "http"})
	_ = d.AddEdge(Edge{From: "a", To: "b"})
	_ = d.AddEdge(Edge{From: "b", To: "c"})

	sorted, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(sorted))
	}
	// a must come before b, b before c
	ids := make([]string, len(sorted))
	for i, n := range sorted {
		ids[i] = n.ID
	}
	if ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Errorf("expected [a b c], got %v", ids)
	}
}

func TestTopologicalSortDeterministic(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "c", Name: "C", Type: "http"})
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})

	// No edges, so all are roots. Should sort lexicographically.
	// But we need edges or dependsOn to avoid orphan validation issues.
	// Actually TopologicalSort doesn't validate, it just sorts.
	sorted, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := make([]string, len(sorted))
	for i, n := range sorted {
		ids[i] = n.ID
	}
	if ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Errorf("expected lexicographic order [a b c], got %v", ids)
	}
}

func TestTopologicalSortWithCycle(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http", DependsOn: []string{"b"}})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http", DependsOn: []string{"a"}})

	_, err := d.TopologicalSort()
	if err != ErrCycleDetected {
		t.Errorf("expected ErrCycleDetected, got %v", err)
	}
}

func TestGetRootNodes(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})
	_ = d.AddNode(&Node{ID: "c", Name: "C", Type: "http"})
	_ = d.AddEdge(Edge{From: "a", To: "b"})
	_ = d.AddEdge(Edge{From: "a", To: "c"})

	roots := d.GetRootNodes()
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if roots[0].ID != "a" {
		t.Errorf("expected root 'a', got %s", roots[0].ID)
	}
}

func TestGetLeafNodes(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})
	_ = d.AddNode(&Node{ID: "c", Name: "C", Type: "http"})
	_ = d.AddEdge(Edge{From: "a", To: "b"})
	_ = d.AddEdge(Edge{From: "a", To: "c"})

	leaves := d.GetLeafNodes()
	if len(leaves) != 2 {
		t.Fatalf("expected 2 leaves, got %d", len(leaves))
	}
	if leaves[0].ID != "b" || leaves[1].ID != "c" {
		t.Errorf("expected leaves [b c], got [%s %s]", leaves[0].ID, leaves[1].ID)
	}
}

func TestGetDependencies(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})
	_ = d.AddNode(&Node{ID: "c", Name: "C", Type: "http"})
	_ = d.AddEdge(Edge{From: "a", To: "c"})
	_ = d.AddEdge(Edge{From: "b", To: "c"})

	deps, err := d.GetDependencies("c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(deps))
	}
	if deps[0].ID != "a" || deps[1].ID != "b" {
		t.Errorf("expected deps [a b], got [%s %s]", deps[0].ID, deps[1].ID)
	}
}

func TestGetDependenciesNotFound(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_, err := d.GetDependencies("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent node")
	}
	if !containsErr(err, ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestGetDependents(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})
	_ = d.AddNode(&Node{ID: "c", Name: "C", Type: "http"})
	_ = d.AddEdge(Edge{From: "a", To: "b"})
	_ = d.AddEdge(Edge{From: "a", To: "c"})

	dependents, err := d.GetDependents("a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dependents) != 2 {
		t.Fatalf("expected 2 dependents, got %d", len(dependents))
	}
}

func TestGetDependentsNotFound(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_, err := d.GetDependents("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent node")
	}
}

func TestValidateOrphanNode(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})
	_ = d.AddNode(&Node{ID: "orphan", Name: "Orphan", Type: "http"})
	_ = d.AddEdge(Edge{From: "a", To: "b"})

	err := d.Validate()
	if err == nil {
		t.Fatal("expected ErrOrphanNode")
	}
	if !containsErr(err, ErrOrphanNode) {
		t.Errorf("expected ErrOrphanNode, got %v", err)
	}
}

func TestValidateMissingDependency(t *testing.T) {
	d := NewDAG("wf-1", "test")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http", DependsOn: []string{"missing"}})

	err := d.Validate()
	if err == nil {
		t.Fatal("expected ErrMissingDep")
	}
	if !containsErr(err, ErrMissingDep) {
		t.Errorf("expected ErrMissingDep, got %v", err)
	}
}

func TestNodeMetadata(t *testing.T) {
	d := NewDAG("wf-1", "test")
	node := &Node{
		ID:   "a",
		Name: "A",
		Type: "http",
		Config: map[string]any{
			"url": "https://example.com",
		},
		Timeout: 30 * time.Second,
		RetryPolicy: &RetryPolicy{
			MaxRetries:   3,
			InitialDelay: 1 * time.Second,
			MaxDelay:     10 * time.Second,
			Multiplier:   2.0,
		},
		Metadata: map[string]string{
			"owner": "team-a",
		},
	}
	err := d.AddNode(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := d.Nodes["a"]
	if got.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", got.Timeout)
	}
	if got.RetryPolicy.MaxRetries != 3 {
		t.Errorf("expected max retries 3, got %d", got.RetryPolicy.MaxRetries)
	}
	if got.Metadata["owner"] != "team-a" {
		t.Errorf("expected owner team-a, got %s", got.Metadata["owner"])
	}
}

func TestDiamondDAG(t *testing.T) {
	// Diamond: a -> b, a -> c, b -> d, c -> d
	d := NewDAG("wf-1", "diamond")
	_ = d.AddNode(&Node{ID: "a", Name: "A", Type: "http"})
	_ = d.AddNode(&Node{ID: "b", Name: "B", Type: "http"})
	_ = d.AddNode(&Node{ID: "c", Name: "C", Type: "http"})
	_ = d.AddNode(&Node{ID: "d", Name: "D", Type: "http"})
	_ = d.AddEdge(Edge{From: "a", To: "b"})
	_ = d.AddEdge(Edge{From: "a", To: "c"})
	_ = d.AddEdge(Edge{From: "b", To: "d"})
	_ = d.AddEdge(Edge{From: "c", To: "d"})

	if err := d.Validate(); err != nil {
		t.Fatalf("diamond DAG should be valid: %v", err)
	}

	sorted, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(sorted))
	}
	// a must be first, d must be last
	if sorted[0].ID != "a" {
		t.Errorf("expected 'a' first, got %s", sorted[0].ID)
	}
	if sorted[3].ID != "d" {
		t.Errorf("expected 'd' last, got %s", sorted[3].ID)
	}

	roots := d.GetRootNodes()
	if len(roots) != 1 || roots[0].ID != "a" {
		t.Errorf("expected single root 'a', got %v", roots)
	}

	leaves := d.GetLeafNodes()
	if len(leaves) != 1 || leaves[0].ID != "d" {
		t.Errorf("expected single leaf 'd', got %v", leaves)
	}
}

// helper to unwrap errors
func containsErr(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		if unwrap, ok := err.(interface{ Unwrap() error }); ok {
			err = unwrap.Unwrap()
		} else {
			break
		}
	}
	return false
}
