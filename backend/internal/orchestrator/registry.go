// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package orchestrator

import (
	"fmt"
	"sort"
	"sync"
)

// Registry maps a node Type (and optional Type/Subtype) to its
// Node implementation. It is populated at process start (via
// RegisterAll) and then read-only during execution.
type Registry struct {
	mu          sync.RWMutex
	byType      map[string]Node
	bySubtype   map[subtypeKey]Node
}

// subtypeKey is a Type+Subtype pair.
type subtypeKey struct {
	Type    string
	Subtype string
}

// NewRegistry returns an empty registry. Use RegisterAll to
// populate it with the built-in nodes.
func NewRegistry() *Registry {
	return &Registry{
		byType:    make(map[string]Node),
		bySubtype: make(map[subtypeKey]Node),
	}
}

// Register adds n to the registry. It is a no-op for nil nodes
// and panics on duplicate type or subtype registrations — those
// represent a programming error and must be caught at startup.
func (r *Registry) Register(n Node) error {
	if n == nil {
		return fmt.Errorf("orchestrator: nil node")
	}
	t := n.Type()
	if t == "" {
		return fmt.Errorf("orchestrator: node has empty type")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, dup := r.byType[t]; dup {
		return fmt.Errorf("orchestrator: duplicate type %q", t)
	}
	r.byType[t] = n

	if n.Subtype() != "" {
		k := subtypeKey{Type: t, Subtype: n.Subtype()}
		if _, dup := r.bySubtype[k]; dup {
			return fmt.Errorf("orchestrator: duplicate type/subtype %q/%q", t, n.Subtype())
		}
		r.bySubtype[k] = n
	}
	return nil
}

// MustRegister is the panic-on-error variant for init() paths.
func (r *Registry) MustRegister(n Node) {
	if err := r.Register(n); err != nil {
		panic(err)
	}
}

// Get looks up a node by its primary Type. The empty subtype
// match wins over typed matches when both exist (i.e. a node
// that supports all subtypes is preferred over nothing).
func (r *Registry) Get(typeName string) (Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.byType[typeName]
	return n, ok
}

// GetBySubtype looks up a node by Type + Subtype.
func (r *Registry) GetBySubtype(typeName, subtype string) (Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if subtype != "" {
		if n, ok := r.bySubtype[subtypeKey{Type: typeName, Subtype: subtype}]; ok {
			return n, true
		}
	}
	if n, ok := r.byType[typeName]; ok {
		// Only return the primary entry if it has no subtype
		// (i.e. it advertises it can handle any subtype).
		if n.Subtype() == "" {
			return n, true
		}
	}
	return nil, false
}

// ListTypes returns the registered primary types in sorted order.
func (r *Registry) ListTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byType))
	for t := range r.byType {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
