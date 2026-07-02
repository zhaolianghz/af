// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package nodes

import (
	"context"
	"encoding/json"

	"github.com/skyzhao/af/internal/orchestrator"
)

// DedupeNode removes duplicate items from a predecessor's slice
// based on a key field. Default key is "stock_code". The first
// occurrence of each key wins.
type DedupeNode struct{}

// NewDedupeNode returns a fresh instance.
func NewDedupeNode() *DedupeNode { return &DedupeNode{} }

func (n *DedupeNode) Type() string    { return orchestrator.NodeTypeDedupe }
func (n *DedupeNode) Subtype() string { return "" }
func (n *DedupeNode) Schema() orchestrator.NodeSchema {
	return orchestrator.NodeSchema{
		Description: "Remove duplicate items by key field.",
		Params: map[string]orchestrator.FieldSchema{
			"key": {Type: "string", Default: "stock_code"},
		},
		Inputs: map[string]orchestrator.FieldSchema{
			"<any>": {Type: "array"},
		},
		Outputs: map[string]orchestrator.FieldSchema{
			"items": {Type: "array"},
			"count": {Type: "number"},
		},
	}
}

type dedupeParams struct {
	Key string `json:"key"`
}

func (n *DedupeNode) Run(ctx context.Context, rc *orchestrator.RunContext, in map[string]any) (map[string]any, error) {
	var p dedupeParams
	if raw, ok := in["_params"].([]byte); ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, orchestrator.NewParamError("dedupe", "params", "invalid json: "+err.Error())
		}
	}
	if p.Key == "" {
		p.Key = "stock_code"
	}
	items := findFirstSlice(in)
	if items == nil {
		return map[string]any{"items": []any{}, "count": 0}, nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]any, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		k := stringOf(lookupField(m, p.Key))
		if k == "" {
			out = append(out, it)
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, it)
	}
	return map[string]any{"items": out, "count": len(out)}, nil
}

var _ orchestrator.Node = (*DedupeNode)(nil)
