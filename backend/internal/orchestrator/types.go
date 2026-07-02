// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package orchestrator implements the DAG runtime that drives every
// strategy execution. It is intentionally deterministic: given the
// same DAG + inputs + clock, it produces the same NodeResult stream
// (see design.md D4.5).
//
// The package also owns the strategy CRUD service and the HTTP
// handlers that expose it. A7-BE2 (scheduler + SSE) consumes the
// Executor and EventBus exported here.
package orchestrator

import (
	"encoding/json"
	"fmt"
	"time"
)

// =============================================================================
// DAG types
// =============================================================================

// NodeDef describes a single node in a strategy DAG. The ID is unique
// within a DAG. Type identifies the registered node implementation
// (e.g. "data_source"); Subtype picks a variant where applicable
// (e.g. "ma" under "indicator"). Params is a raw JSON blob; the
// owning Node implementation is responsible for unmarshalling it.
type NodeDef struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Subtype string          `json:"subtype,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Position *Position      `json:"position,omitempty"`
}

// Position is the ReactFlow canvas coordinate. It is preserved on
// round-trip but not used by the executor.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Edge connects two nodes by ID. SourceHandle / TargetHandle are
// ReactFlow's typed-connection fields; they are preserved on
// round-trip and exposed for UI filtering.
type Edge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	SourceHandle string `json:"sourceHandle,omitempty"`
	Target       string `json:"target"`
	TargetHandle string `json:"targetHandle,omitempty"`
}

// DAG is the in-memory representation of a strategy's flow graph.
type DAG struct {
	Nodes []NodeDef `json:"nodes"`
	Edges []Edge    `json:"edges"`
}

// Validate returns ErrInvalidDAG on structural problems (empty,
// duplicate IDs, dangling edges, self-loops). It does NOT check for
// cycles — use TopoSort for that.
func (d *DAG) Validate() error {
	if d == nil {
		return fmt.Errorf("%w: nil dag", ErrInvalidDAG)
	}
	if len(d.Nodes) == 0 {
		return fmt.Errorf("%w: no nodes", ErrInvalidDAG)
	}
	seen := make(map[string]struct{}, len(d.Nodes))
	for i, n := range d.Nodes {
		if n.ID == "" {
			return fmt.Errorf("%w: node[%d] missing id", ErrInvalidDAG, i)
		}
		if n.Type == "" {
			return fmt.Errorf("%w: node[%s] missing type", ErrInvalidDAG, n.ID)
		}
		if _, dup := seen[n.ID]; dup {
			return fmt.Errorf("%w: duplicate node id %q", ErrInvalidDAG, n.ID)
		}
		seen[n.ID] = struct{}{}
	}
	for i, e := range d.Edges {
		if e.Source == "" || e.Target == "" {
			return fmt.Errorf("%w: edge[%d] missing endpoint", ErrInvalidDAG, i)
		}
		if e.Source == e.Target {
			return fmt.Errorf("%w: edge[%d] self-loop on %q", ErrInvalidDAG, i, e.Source)
		}
		if _, ok := seen[e.Source]; !ok {
			return fmt.Errorf("%w: edge[%d] unknown source %q", ErrInvalidDAG, i, e.Source)
		}
		if _, ok := seen[e.Target]; !ok {
			return fmt.Errorf("%w: edge[%d] unknown target %q", ErrInvalidDAG, i, e.Target)
		}
	}
	return nil
}

// HasNode reports whether the DAG contains a node with the given ID.
func (d *DAG) HasNode(id string) bool {
	if d == nil {
		return false
	}
	for i := range d.Nodes {
		if d.Nodes[i].ID == id {
			return true
		}
	}
	return false
}

// NodeByID returns the node with the given ID, or ErrNodeNotFound.
func (d *DAG) NodeByID(id string) (NodeDef, error) {
	if d == nil {
		return NodeDef{}, fmt.Errorf("%w: nil dag", ErrNodeNotFound)
	}
	for i := range d.Nodes {
		if d.Nodes[i].ID == id {
			return d.Nodes[i], nil
		}
	}
	return NodeDef{}, fmt.Errorf("%w: %q", ErrNodeNotFound, id)
}

// Successors returns the IDs of every node that has an incoming
// edge from the given node. The result order is stable (sorted by
// the slice as it appears in d.Edges) so test assertions are
// deterministic.
func (d *DAG) Successors(id string) []string {
	if d == nil {
		return nil
	}
	out := make([]string, 0)
	for _, e := range d.Edges {
		if e.Source == id {
			out = append(out, e.Target)
		}
	}
	return out
}

// Predecessors returns the IDs of every node that has an outgoing
// edge to the given node. Order is stable.
func (d *DAG) Predecessors(id string) []string {
	if d == nil {
		return nil
	}
	out := make([]string, 0)
	for _, e := range d.Edges {
		if e.Target == id {
			out = append(out, e.Source)
		}
	}
	return out
}

// Roots returns every node that has no incoming edge. The result is
// sorted by ID for determinism.
func (d *DAG) Roots() []string {
	if d == nil {
		return nil
	}
	hasIncoming := make(map[string]bool, len(d.Nodes))
	for _, e := range d.Edges {
		hasIncoming[e.Target] = true
	}
	out := make([]string, 0)
	for _, n := range d.Nodes {
		if !hasIncoming[n.ID] {
			out = append(out, n.ID)
		}
	}
	return out
}

// TopoSort returns a deterministic topological order of node IDs
// using Kahn's algorithm. Ties between nodes with the same indegree
// are broken by node ID (ascending), which keeps execution order
// reproducible across runs.
func (d *DAG) TopoSort() ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}

	indeg := make(map[string]int, len(d.Nodes))
	for _, n := range d.Nodes {
		indeg[n.ID] = 0
	}
	// Successors deterministically; build an adjacency map of node ID -> sorted successors.
	adj := make(map[string][]string, len(d.Nodes))
	for _, e := range d.Edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		indeg[e.Target]++
	}
	for k := range adj {
		sortStrings(adj[k])
	}

	// Initialize the ready queue with all roots, sorted by ID.
	ready := make([]string, 0)
	for _, n := range d.Nodes {
		if indeg[n.ID] == 0 {
			ready = append(ready, n.ID)
		}
	}
	sortStrings(ready)

	out := make([]string, 0, len(d.Nodes))
	for len(ready) > 0 {
		// Pop the smallest ID to keep order reproducible.
		head := ready[0]
		ready = ready[1:]
		out = append(out, head)
		for _, next := range adj[head] {
			indeg[next]--
			if indeg[next] == 0 {
				// Insert in sorted order.
				ready = insertSorted(ready, next)
			}
		}
	}
	if len(out) != len(d.Nodes) {
		return nil, fmt.Errorf("%w: %d/%d nodes sorted", ErrCycle, len(out), len(d.Nodes))
	}
	return out, nil
}

func sortStrings(s []string) {
	// Tiny in-place sort to avoid pulling sort package noise;
	// always small slices in practice.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func insertSorted(s []string, v string) []string {
	// Binary-search the insertion point.
	lo, hi := 0, len(s)
	for lo < hi {
		mid := (lo + hi) / 2
		if s[mid] < v {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	s = append(s, "")
	copy(s[lo+1:], s[lo:])
	s[lo] = v
	return s
}

// =============================================================================
// Run types
// =============================================================================

// RunRequest is the input to Executor.Execute.
type RunRequest struct {
	DAG    *DAG
	Inputs map[string]any
	// DryRun, when true, instructs nodes that touch external
	// systems (DB writes, notify sends) to skip their effects and
	// just report what they would have done.
	DryRun bool
}

// RunSummary is the output of Executor.Execute.
type RunSummary struct {
	RunID      uint64                   `json:"run_id"`
	StrategyID uint64                   `json:"strategy_id"`
	Status     string                   `json:"status"`
	DryRun     bool                     `json:"dry_run"`
	NodeResults map[string]NodeResult   `json:"node_results"`
	StartedAt  time.Time                `json:"started_at"`
	FinishedAt time.Time                `json:"finished_at"`
	Duration   time.Duration            `json:"duration"`
	Error      string                   `json:"error,omitempty"`
}

// NodeResult captures the outcome of running a single node.
type NodeResult struct {
	NodeID     string         `json:"node_id"`
	Status     string         `json:"status"`
	Error      string         `json:"error,omitempty"`
	SkipReason string         `json:"skip_reason,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Duration   time.Duration  `json:"duration"`
}

// Status constants used by both RunSummary and NodeResult.
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// SkipReason values for skipped nodes.
const (
	SkipReasonUpstreamFailed = "upstream_failed"
	SkipReasonCancelled      = "cancelled"
	SkipReasonNotInPath      = "not_in_target_path"
	SkipReasonInvalidParam   = "invalid_param"
)

// =============================================================================
// Sentinel errors
// =============================================================================

var (
	ErrInvalidDAG     = fmt.Errorf("orchestrator: invalid dag")
	ErrCycle          = fmt.Errorf("orchestrator: cycle detected")
	ErrUnknownType    = fmt.Errorf("orchestrator: unknown node type")
	ErrNodeNotFound   = fmt.Errorf("orchestrator: node not found")
	ErrUpstreamFailed = fmt.Errorf("orchestrator: upstream node failed")
)
