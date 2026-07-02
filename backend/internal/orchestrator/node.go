// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package orchestrator

import (
	"context"
	"fmt"

	"github.com/skyzhao/af/internal/apperr"
)

// Node is the contract every executable unit in a strategy must
// implement. Nodes are stateless — all per-run state flows through
// RunContext and the inputs map.
type Node interface {
	// Type returns the node's primary discriminator (e.g.
	// "data_source"). Combined with Subtype it keys Registry lookups.
	Type() string
	// Subtype returns the variant within Type (e.g. "ma" under
	// "indicator"). Empty string means "no subtype".
	Subtype() string
	// Schema describes the node's expected Params and the shape of
	// its output Payload. Used by the editor UI to render the
	// config panel and validate user input.
	Schema() NodeSchema
	// Run is the actual work. Inputs is keyed by predecessor node
	// ID: in["node_a"] is the Payload of node_a. The returned map
	// is the node's output Payload, also keyed by string.
	Run(ctx context.Context, rc *RunContext, in map[string]any) (map[string]any, error)
}

// NodeSchema is the static, UI-renderable description of a node.
type NodeSchema struct {
	Description string                  `json:"description"`
	Params      map[string]FieldSchema  `json:"params,omitempty"`
	Inputs      map[string]FieldSchema  `json:"inputs,omitempty"`
	Outputs     map[string]FieldSchema  `json:"outputs,omitempty"`
}

// FieldSchema describes a single named field.
type FieldSchema struct {
	Type        string   `json:"type"` // string|number|bool|array|object|enum
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
}

// ParamError indicates a node rejected its Params. It wraps an
// apperr.InvalidArg so the HTTP layer can map it to 400.
type ParamError struct {
	NodeID  string
	Field   string
	Reason  string
	Wrapped error
}

func (e *ParamError) Error() string {
	if e == nil {
		return ""
	}
	if e.Wrapped != nil {
		return fmt.Sprintf("orchestrator: node %q param %q: %s: %v", e.NodeID, e.Field, e.Reason, e.Wrapped)
	}
	return fmt.Sprintf("orchestrator: node %q param %q: %s", e.NodeID, e.Field, e.Reason)
}

func (e *ParamError) Unwrap() error { return e.Wrapped }

// NewParamError is a convenience constructor that wraps
// apperr.InvalidArg.
func NewParamError(nodeID, field, reason string) *ParamError {
	return &ParamError{
		NodeID:  nodeID,
		Field:   field,
		Reason:  reason,
		Wrapped: apperr.InvalidArg(fmt.Sprintf("node %q: %s: %s", nodeID, field, reason)),
	}
}
