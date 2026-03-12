// Package dag implements a Directed Acyclic Graph (DAG) data structure used to
// model workflow definitions. It provides construction, validation, cycle
// detection (via Kahn's algorithm), and topological sorting.
package dag

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

// RetryPolicy configures retry behaviour for a node.
type RetryPolicy struct {
	MaxRetries   int           `json:"max_retries" yaml:"max_retries"`
	InitialDelay time.Duration `json:"initial_delay" yaml:"initial_delay"`
	MaxDelay     time.Duration `json:"max_delay" yaml:"max_delay"`
	Multiplier   float64       `json:"multiplier" yaml:"multiplier"`
}

// Node represents a single step inside a workflow DAG.
type Node struct {
	ID          string            `json:"id" yaml:"id"`
	Name        string            `json:"name" yaml:"name"`
	Type        string            `json:"type" yaml:"type"`
	Config      map[string]any    `json:"config,omitempty" yaml:"config,omitempty"`
	DependsOn   []string          `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	RetryPolicy *RetryPolicy      `json:"retry_policy,omitempty" yaml:"retry_policy,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// Edge represents a directed connection between two nodes, optionally
// conditioned on a runtime expression.
type Edge struct {
	From      string `json:"from" yaml:"from"`
	To        string `json:"to" yaml:"to"`
	Condition string `json:"condition,omitempty" yaml:"condition,omitempty"`
}

// DAG is the top-level directed acyclic graph that represents a complete
// workflow definition.
type DAG struct {
	ID    string           `json:"id" yaml:"id"`
	Name  string           `json:"name" yaml:"name"`
	Nodes map[string]*Node `json:"nodes" yaml:"nodes"`
	Edges []Edge           `json:"edges" yaml:"edges"`
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrEmptyDAG        = errors.New("dag: DAG has no nodes")
	ErrDuplicateNode   = errors.New("dag: duplicate node ID")
	ErrDuplicateEdge   = errors.New("dag: duplicate edge")
	ErrNodeNotFound    = errors.New("dag: node not found")
	ErrSelfLoop        = errors.New("dag: self-loop detected")
	ErrCycleDetected   = errors.New("dag: cycle detected")
	ErrMissingDep      = errors.New("dag: missing dependency")
	ErrOrphanNode      = errors.New("dag: orphan node is neither a root nor reachable from a root")
	ErrEdgeNodeMissing = errors.New("dag: edge references a non-existent node")
)

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewDAG creates an empty DAG with the given ID and name.
func NewDAG(id, name string) *DAG {
	return &DAG{
		ID:    id,
		Name:  name,
		Nodes: make(map[string]*Node),
		Edges: make([]Edge, 0),
	}
}

// ---------------------------------------------------------------------------
// Mutation helpers
// ---------------------------------------------------------------------------

// AddNode adds a node to the DAG. It returns ErrDuplicateNode if a node with
// the same ID already exists.
func (d *DAG) AddNode(n *Node) error {
	if n == nil {
		return fmt.Errorf("dag: cannot add nil node")
	}
	if n.ID == "" {
		return fmt.Errorf("dag: node ID must not be empty")
	}
	if _, exists := d.Nodes[n.ID]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateNode, n.ID)
	}
	d.Nodes[n.ID] = n
	return nil
}

// AddEdge adds a directed edge between two existing nodes.
func (d *DAG) AddEdge(e Edge) error {
	if e.From == "" || e.To == "" {
		return fmt.Errorf("dag: edge From and To must not be empty")
	}
	if e.From == e.To {
		return fmt.Errorf("%w: %s", ErrSelfLoop, e.From)
	}
	if _, ok := d.Nodes[e.From]; !ok {
		return fmt.Errorf("%w: from=%s", ErrEdgeNodeMissing, e.From)
	}
	if _, ok := d.Nodes[e.To]; !ok {
		return fmt.Errorf("%w: to=%s", ErrEdgeNodeMissing, e.To)
	}
	// Reject duplicate edges.
	for _, existing := range d.Edges {
		if existing.From == e.From && existing.To == e.To {
			return fmt.Errorf("%w: %s -> %s", ErrDuplicateEdge, e.From, e.To)
		}
	}
	d.Edges = append(d.Edges, e)
	return nil
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// Validate performs structural validation of the DAG. It checks for:
//   - empty graph
//   - edges pointing to non-existent nodes
//   - nodes whose DependsOn references are missing
//   - orphan nodes (no edges in or out and not the only node)
//   - cycles
func (d *DAG) Validate() error {
	if len(d.Nodes) == 0 {
		return ErrEmptyDAG
	}

	// Check that every edge references known nodes.
	for _, e := range d.Edges {
		if _, ok := d.Nodes[e.From]; !ok {
			return fmt.Errorf("%w: from=%s", ErrEdgeNodeMissing, e.From)
		}
		if _, ok := d.Nodes[e.To]; !ok {
			return fmt.Errorf("%w: to=%s", ErrEdgeNodeMissing, e.To)
		}
	}

	// Check DependsOn references.
	for _, n := range d.Nodes {
		for _, dep := range n.DependsOn {
			if _, ok := d.Nodes[dep]; !ok {
				return fmt.Errorf("%w: node %s depends on %s", ErrMissingDep, n.ID, dep)
			}
		}
	}

	// Build adjacency sets from edges to detect orphan nodes. A node is
	// orphan if it participates in no edges AND there is more than one node
	// AND it has no DependsOn entries.
	if len(d.Nodes) > 1 {
		participating := make(map[string]bool)
		for _, e := range d.Edges {
			participating[e.From] = true
			participating[e.To] = true
		}
		// Also count DependsOn references.
		for _, n := range d.Nodes {
			if len(n.DependsOn) > 0 {
				participating[n.ID] = true
				for _, dep := range n.DependsOn {
					participating[dep] = true
				}
			}
		}
		for id := range d.Nodes {
			if !participating[id] {
				return fmt.Errorf("%w: %s", ErrOrphanNode, id)
			}
		}
	}

	// Cycle detection.
	if err := d.DetectCycles(); err != nil {
		return err
	}

	return nil
}

// ---------------------------------------------------------------------------
// Cycle detection — Kahn's algorithm
// ---------------------------------------------------------------------------

// buildAdjacency returns the full adjacency list and in-degree map derived
// from both Edges and Node.DependsOn fields.
func (d *DAG) buildAdjacency() (adj map[string][]string, inDegree map[string]int) {
	adj = make(map[string][]string, len(d.Nodes))
	inDegree = make(map[string]int, len(d.Nodes))

	for id := range d.Nodes {
		adj[id] = nil
		inDegree[id] = 0
	}

	// Track unique edges to avoid double-counting.
	seen := make(map[[2]string]bool)

	addEdge := func(from, to string) {
		key := [2]string{from, to}
		if seen[key] {
			return
		}
		seen[key] = true
		adj[from] = append(adj[from], to)
		inDegree[to]++
	}

	for _, e := range d.Edges {
		addEdge(e.From, e.To)
	}
	for _, n := range d.Nodes {
		for _, dep := range n.DependsOn {
			addEdge(dep, n.ID)
		}
	}
	return adj, inDegree
}

// DetectCycles returns ErrCycleDetected if the DAG contains a cycle. The
// implementation uses Kahn's algorithm (BFS-based topological sort).
func (d *DAG) DetectCycles() error {
	if len(d.Nodes) == 0 {
		return nil
	}

	adj, inDegree := d.buildAdjacency()

	queue := make([]string, 0, len(d.Nodes))
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++

		for _, neighbor := range adj[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if visited != len(d.Nodes) {
		return ErrCycleDetected
	}
	return nil
}

// ---------------------------------------------------------------------------
// Topological sort
// ---------------------------------------------------------------------------

// TopologicalSort returns the nodes in a valid execution order. It returns an
// error if the graph contains a cycle. Deterministic: ties are broken by
// lexicographic node ID.
func (d *DAG) TopologicalSort() ([]*Node, error) {
	if len(d.Nodes) == 0 {
		return nil, nil
	}

	adj, inDegree := d.buildAdjacency()

	// Seed the queue with zero in-degree nodes, sorted for determinism.
	queue := make([]string, 0, len(d.Nodes))
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	result := make([]*Node, 0, len(d.Nodes))
	for len(queue) > 0 {
		// Pop the lexicographically smallest node.
		node := queue[0]
		queue = queue[1:]
		result = append(result, d.Nodes[node])

		// Collect neighbors that become ready and sort them for determinism.
		var ready []string
		for _, neighbor := range adj[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				ready = append(ready, neighbor)
			}
		}
		sort.Strings(ready)
		queue = append(queue, ready...)
		// Re-sort the entire queue to maintain global deterministic ordering.
		sort.Strings(queue)
	}

	if len(result) != len(d.Nodes) {
		return nil, ErrCycleDetected
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

// GetDependencies returns the immediate upstream (dependency) nodes for the
// given nodeID. These are nodes that must complete before nodeID can run.
func (d *DAG) GetDependencies(nodeID string) ([]*Node, error) {
	node, ok := d.Nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
	}

	// Collect dependencies from DependsOn and from incoming edges.
	depSet := make(map[string]bool)
	for _, dep := range node.DependsOn {
		depSet[dep] = true
	}
	for _, e := range d.Edges {
		if e.To == nodeID {
			depSet[e.From] = true
		}
	}

	result := make([]*Node, 0, len(depSet))
	for depID := range depSet {
		if n, ok := d.Nodes[depID]; ok {
			result = append(result, n)
		}
	}

	// Sort for determinism.
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// GetDependents returns the immediate downstream nodes that depend on the
// given nodeID.
func (d *DAG) GetDependents(nodeID string) ([]*Node, error) {
	if _, ok := d.Nodes[nodeID]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
	}

	depSet := make(map[string]bool)
	for _, e := range d.Edges {
		if e.From == nodeID {
			depSet[e.To] = true
		}
	}
	for _, n := range d.Nodes {
		for _, dep := range n.DependsOn {
			if dep == nodeID {
				depSet[n.ID] = true
			}
		}
	}

	result := make([]*Node, 0, len(depSet))
	for depID := range depSet {
		if n, ok := d.Nodes[depID]; ok {
			result = append(result, n)
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// GetRootNodes returns all nodes that have no dependencies (in-degree 0).
func (d *DAG) GetRootNodes() []*Node {
	_, inDegree := d.buildAdjacency()

	roots := make([]*Node, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			roots = append(roots, d.Nodes[id])
		}
	}

	sort.Slice(roots, func(i, j int) bool { return roots[i].ID < roots[j].ID })
	return roots
}

// GetLeafNodes returns all nodes that have no dependents (out-degree 0).
func (d *DAG) GetLeafNodes() []*Node {
	outDegree := make(map[string]int, len(d.Nodes))
	for id := range d.Nodes {
		outDegree[id] = 0
	}

	seen := make(map[[2]string]bool)
	countEdge := func(from, to string) {
		key := [2]string{from, to}
		if seen[key] {
			return
		}
		seen[key] = true
		outDegree[from]++
	}

	for _, e := range d.Edges {
		countEdge(e.From, e.To)
	}
	for _, n := range d.Nodes {
		for _, dep := range n.DependsOn {
			countEdge(dep, n.ID)
		}
	}

	leaves := make([]*Node, 0)
	for id, deg := range outDegree {
		if deg == 0 {
			leaves = append(leaves, d.Nodes[id])
		}
	}

	sort.Slice(leaves, func(i, j int) bool { return leaves[i].ID < leaves[j].ID })
	return leaves
}
