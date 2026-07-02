// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for the DAG data structure: Validate, HasNode,
// NodeByID, Successors, Predecessors, Roots, and TopoSort.
package orchestrator

import (
	"errors"
	"strings"
	"testing"
)

func newDAG(nodes []NodeDef, edges []Edge) *DAG {
	return &DAG{Nodes: nodes, Edges: edges}
}

func TestDAG_Validate_Nil(t *testing.T) {
	var d *DAG
	if err := d.Validate(); !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("Validate(nil): got %v want ErrInvalidDAG", err)
	}
}

func TestDAG_Validate_Empty(t *testing.T) {
	d := &DAG{}
	if err := d.Validate(); !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("Validate(empty): got %v want ErrInvalidDAG", err)
	}
}

func TestDAG_Validate_NodeErrors(t *testing.T) {
	tests := []struct {
		name  string
		dag   *DAG
		wantS string
	}{
		{
			name: "missing id",
			dag: newDAG(
				[]NodeDef{{ID: "", Type: "x"}, {ID: "b", Type: "y"}},
				nil,
			),
			wantS: "missing id",
		},
		{
			name: "missing type",
			dag: newDAG(
				[]NodeDef{{ID: "a", Type: ""}},
				nil,
			),
			wantS: "missing type",
		},
		{
			name: "duplicate id",
			dag: newDAG(
				[]NodeDef{{ID: "a", Type: "x"}, {ID: "a", Type: "y"}},
				nil,
			),
			wantS: "duplicate",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.dag.Validate()
			if !errors.Is(err, ErrInvalidDAG) {
				t.Errorf("Validate: got %v want ErrInvalidDAG", err)
			}
			if !strings.Contains(err.Error(), tc.wantS) {
				t.Errorf("Validate: got %q want substring %q", err.Error(), tc.wantS)
			}
		})
	}
}

func TestDAG_Validate_EdgeErrors(t *testing.T) {
	nodes := []NodeDef{{ID: "a", Type: "x"}, {ID: "b", Type: "y"}}
	tests := []struct {
		name  string
		edges []Edge
		wantS string
	}{
		{
			name:  "missing endpoint",
			edges: []Edge{{ID: "e1", Source: "a", Target: ""}},
			wantS: "missing endpoint",
		},
		{
			name:  "self loop",
			edges: []Edge{{ID: "e1", Source: "a", Target: "a"}},
			wantS: "self-loop",
		},
		{
			name:  "unknown source",
			edges: []Edge{{ID: "e1", Source: "ghost", Target: "a"}},
			wantS: "unknown source",
		},
		{
			name:  "unknown target",
			edges: []Edge{{ID: "e1", Source: "a", Target: "ghost"}},
			wantS: "unknown target",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDAG(nodes, tc.edges)
			err := d.Validate()
			if !errors.Is(err, ErrInvalidDAG) {
				t.Errorf("Validate: got %v want ErrInvalidDAG", err)
			}
			if !strings.Contains(err.Error(), tc.wantS) {
				t.Errorf("Validate: got %q want substring %q", err.Error(), tc.wantS)
			}
		})
	}
}

func TestDAG_Validate_HappyPath(t *testing.T) {
	d := newDAG(
		[]NodeDef{{ID: "a", Type: "x"}, {ID: "b", Type: "y"}},
		[]Edge{{ID: "e1", Source: "a", Target: "b"}},
	)
	if err := d.Validate(); err != nil {
		t.Errorf("Validate: unexpected err %v", err)
	}
}

func TestDAG_HasNode(t *testing.T) {
	d := newDAG(
		[]NodeDef{{ID: "a", Type: "x"}, {ID: "b", Type: "y"}},
		nil,
	)
	if !d.HasNode("a") {
		t.Error("HasNode(a) should be true")
	}
	if d.HasNode("ghost") {
		t.Error("HasNode(ghost) should be false")
	}
	var nilDAG *DAG
	if nilDAG.HasNode("a") {
		t.Error("HasNode on nil DAG should be false")
	}
}

func TestDAG_NodeByID(t *testing.T) {
	d := newDAG(
		[]NodeDef{{ID: "a", Type: "x", Subtype: "s"}, {ID: "b", Type: "y"}},
		nil,
	)
	got, err := d.NodeByID("a")
	if err != nil {
		t.Fatalf("NodeByID: %v", err)
	}
	if got.Type != "x" || got.Subtype != "s" {
		t.Errorf("NodeByID: got %+v", got)
	}
	if _, err := d.NodeByID("ghost"); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("NodeByID(ghost): got %v want ErrNodeNotFound", err)
	}
	var nilDAG *DAG
	if _, err := nilDAG.NodeByID("a"); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("NodeByID on nil: got %v want ErrNodeNotFound", err)
	}
}

func TestDAG_SuccessorsPredecessors(t *testing.T) {
	d := newDAG(
		[]NodeDef{
			{ID: "a", Type: "x"},
			{ID: "b", Type: "y"},
			{ID: "c", Type: "z"},
		},
		[]Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "a", Target: "c"},
			{ID: "e3", Source: "b", Target: "c"},
		},
	)
	if got := d.Successors("a"); len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("Successors(a): got %v", got)
	}
	if got := d.Predecessors("c"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Predecessors(c): got %v", got)
	}
	if got := d.Successors("none"); len(got) != 0 {
		t.Errorf("Successors(none): got %v", got)
	}
	if got := d.Successors(""); len(got) != 0 {
		t.Errorf("Successors(\"\"): got %v", got)
	}
}

func TestDAG_Roots(t *testing.T) {
	d := newDAG(
		[]NodeDef{
			{ID: "a", Type: "x"},
			{ID: "b", Type: "y"},
			{ID: "c", Type: "z"},
			{ID: "d", Type: "w"},
		},
		[]Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "c", Target: "b"},
		},
	)
	roots := d.Roots()
	want := []string{"a", "c", "d"}
	if len(roots) != len(want) {
		t.Fatalf("Roots: got %v want %v", roots, want)
	}
	for i := range roots {
		if roots[i] != want[i] {
			t.Errorf("Roots[%d]: got %q want %q", i, roots[i], want[i])
		}
	}
}

func TestDAG_Roots_Nil(t *testing.T) {
	var nilDAG *DAG
	if got := nilDAG.Roots(); got != nil {
		t.Errorf("Roots on nil: got %v want nil", got)
	}
}

func TestDAG_TopoSort_Linear(t *testing.T) {
	d := newDAG(
		[]NodeDef{
			{ID: "a", Type: "x"},
			{ID: "b", Type: "y"},
			{ID: "c", Type: "z"},
		},
		[]Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "c"},
		},
	)
	got, err := d.TopoSort()
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	want := []string{"a", "b", "c"}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("TopoSort[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestDAG_TopoSort_Diamond(t *testing.T) {
	//     a
	//    / \
	//   b   c
	//    \ /
	//     d
	d := newDAG(
		[]NodeDef{
			{ID: "a", Type: "x"},
			{ID: "b", Type: "x"},
			{ID: "c", Type: "x"},
			{ID: "d", Type: "x"},
		},
		[]Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "a", Target: "c"},
			{ID: "e3", Source: "b", Target: "d"},
			{ID: "e4", Source: "c", Target: "d"},
		},
	)
	got, err := d.TopoSort()
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len: got %d", len(got))
	}
	// a is first; d is last.
	if got[0] != "a" {
		t.Errorf("first: got %q want a", got[0])
	}
	if got[3] != "d" {
		t.Errorf("last: got %q want d", got[3])
	}
	// b before d, c before d.
	pos := map[string]int{}
	for i, id := range got {
		pos[id] = i
	}
	if pos["b"] > pos["d"] || pos["c"] > pos["d"] {
		t.Errorf("ordering: %v", got)
	}
}

func TestDAG_TopoSort_Cycle(t *testing.T) {
	d := newDAG(
		[]NodeDef{
			{ID: "a", Type: "x"},
			{ID: "b", Type: "y"},
		},
		[]Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "a"},
		},
	)
	_, err := d.TopoSort()
	if !errors.Is(err, ErrCycle) {
		t.Errorf("TopoSort: got %v want ErrCycle", err)
	}
}

func TestDAG_TopoSort_RejectsInvalidDAG(t *testing.T) {
	// Empty DAG.
	d := &DAG{}
	_, err := d.TopoSort()
	if !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("TopoSort(empty): got %v want ErrInvalidDAG", err)
	}
}

func TestSortStringsAndInsertSorted(t *testing.T) {
	s := []string{"b", "d", "a", "c"}
	sortStrings(s)
	want := []string{"a", "b", "c", "d"}
	for i := range s {
		if s[i] != want[i] {
			t.Errorf("sortStrings[%d]: got %q want %q", i, s[i], want[i])
		}
	}
	out := insertSorted([]string{"a", "c", "e"}, "b")
	want2 := []string{"a", "b", "c", "e"}
	if len(out) != len(want2) {
		t.Fatalf("insertSorted len: got %d", len(out))
	}
	for i := range out {
		if out[i] != want2[i] {
			t.Errorf("insertSorted[%d]: got %q want %q", i, out[i], want2[i])
		}
	}
}
