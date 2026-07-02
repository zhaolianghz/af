// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for the DAG Registry. Exercises the type/subtype
// lookup, duplicate-detection, and "empty subtype wins over
// typed match" resolution rule.
package orchestrator

import (
	"context"
	"errors"
	"testing"
)

// stubNode is a minimal Node implementation used by the tests.
type stubNode struct {
	typ, sub string
	runOut   map[string]any
	runErr   error
}

func (s *stubNode) Type() string    { return s.typ }
func (s *stubNode) Subtype() string { return s.sub }
func (s *stubNode) Schema() NodeSchema {
	return NodeSchema{Description: "stub " + s.typ + "/" + s.sub}
}
func (s *stubNode) Run(ctx context.Context, rc *RunContext, in map[string]any) (map[string]any, error) {
	return s.runOut, s.runErr
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	n := &stubNode{typ: "alpha", sub: ""}
	if err := r.Register(n); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Get("alpha")
	if !ok || got != n {
		t.Errorf("Get(alpha): ok=%v, got=%p want=%p", ok, got, n)
	}
	if _, ok := r.Get("missing"); ok {
		t.Error("Get(missing) should return ok=false")
	}
}

func TestRegistry_RegisterRejectsNil(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Error("Register(nil) should error")
	}
}

func TestRegistry_RegisterRejectsEmptyType(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&stubNode{typ: "", sub: ""}); err == nil {
		t.Error("Register(empty type) should error")
	}
}

func TestRegistry_RegisterRejectsDuplicates(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&stubNode{typ: "alpha"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(&stubNode{typ: "alpha"}); err == nil {
		t.Error("duplicate type should error")
	}
	// Subtype collision: an empty-subtype node is
	// auto-registered under byType only; a second node
	// with the same type+subtype is detected.
	if err := r.Register(&stubNode{typ: "beta", sub: "x"}); err != nil {
		t.Fatalf("beta/x first: %v", err)
	}
	if err := r.Register(&stubNode{typ: "beta", sub: "x"}); err == nil {
		t.Error("duplicate type/subtype should error")
	}
}

func TestRegistry_MustRegisterPanics(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(&stubNode{typ: "alpha"})
	defer func() {
		if rec := recover(); rec == nil {
			t.Error("MustRegister on duplicate should panic")
		}
	}()
	r.MustRegister(&stubNode{typ: "alpha"})
}

func TestRegistry_GetBySubtype(t *testing.T) {
	r := NewRegistry()
	// Only one entry per Type is allowed. The Subtype()
	// returning "" makes the node a "catch-all" that
	// handles any subtype via runtime dispatch.
	catchAll := &stubNode{typ: "indicator", sub: ""}
	if err := r.Register(catchAll); err != nil {
		t.Fatalf("catchAll Register: %v", err)
	}

	// GetBySubtype with empty subtype → returns the
	// catch-all entry.
	got, ok := r.GetBySubtype("indicator", "")
	if !ok || got != catchAll {
		t.Errorf("GetBySubtype(empty): ok=%v, got=%p want catchAll", ok, got)
	}
	// GetBySubtype with any subtype (registered or not)
	// falls back to the catch-all when no typed entry
	// exists.
	got, ok = r.GetBySubtype("indicator", "ma")
	if !ok || got != catchAll {
		t.Errorf("GetBySubtype(ma): ok=%v, got=%p want catchAll", ok, got)
	}
	// GetBySubtype with a registered subtype that has a
	// non-empty Subtype() — that node is only used when
	// the requested subtype matches.
	typedN := &stubNode{typ: "filter_eq", sub: "filter_eq"}
	if err := r.Register(typedN); err != nil {
		t.Fatalf("typedN Register: %v", err)
	}
	got, ok = r.GetBySubtype("filter_eq", "filter_eq")
	if !ok || got != typedN {
		t.Errorf("GetBySubtype(filter_eq): ok=%v, got=%p want typedN", ok, got)
	}
	// Empty subtype → no fallback (the typed node has
	// non-empty Subtype()).
	if _, ok := r.GetBySubtype("filter_eq", ""); ok {
		t.Error("GetBySubtype(empty) with typed-only node should return false")
	}
	// Unknown type altogether.
	if _, ok := r.GetBySubtype("ghost", ""); ok {
		t.Error("GetBySubtype(unknown type) should return false")
	}
}

func TestRegistry_ListTypesSorted(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"gamma", "alpha", "beta"} {
		if err := r.Register(&stubNode{typ: n}); err != nil {
			t.Fatalf("Register %s: %v", n, err)
		}
	}
	got := r.ListTypes()
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("ListTypes len: got %d want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("ListTypes[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}

// =============================================================================
// Compile-time interface assertion: stubNode must satisfy Node.
// =============================================================================

var _ Node = (*stubNode)(nil)

// justForLint is here so the imports of "context" / "errors" are
// retained if/when this file's body shrinks.
var _ = errors.New
