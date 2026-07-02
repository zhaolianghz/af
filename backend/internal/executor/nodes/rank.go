// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package nodes

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/skyzhao/af/internal/orchestrator"
)

// RankNode sorts a predecessor's slice by `field` and keeps
// the top N. Order defaults to "desc" (largest first).
type RankNode struct{}

// NewRankNode returns a fresh instance.
func NewRankNode() *RankNode { return &RankNode{} }

func (n *RankNode) Type() string    { return orchestrator.NodeTypeRank }
func (n *RankNode) Subtype() string { return "" }
func (n *RankNode) Schema() orchestrator.NodeSchema {
	return orchestrator.NodeSchema{
		Description: "Sort by a numeric field and keep top N.",
		Params: map[string]orchestrator.FieldSchema{
			"field": {Type: "string", Required: true},
			"order": {Type: "enum", Enum: []string{"asc", "desc"}, Default: "desc"},
			"top":   {Type: "number", Default: 10},
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

type rankParams struct {
	Field string `json:"field"`
	Order string `json:"order"`
	Top   int    `json:"top"`
}

func (n *RankNode) Run(ctx context.Context, rc *orchestrator.RunContext, in map[string]any) (map[string]any, error) {
	var p rankParams
	if raw, ok := in["_params"].([]byte); ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, orchestrator.NewParamError("rank", "params", "invalid json: "+err.Error())
		}
	}
	if p.Field == "" {
		return nil, orchestrator.NewParamError("rank", "field", "is required")
	}
	if p.Order == "" {
		p.Order = "desc"
	}
	if p.Top <= 0 {
		p.Top = 10
	}
	items := findFirstSlice(in)
	if items == nil {
		return map[string]any{"items": []any{}, "count": 0}, nil
	}
	// Copy + sort. The sort is stable in Go's sort.SliceStable.
	cp := make([]any, len(items))
	copy(cp, items)
	sort.SliceStable(cp, func(i, j int) bool {
		mi, _ := cp[i].(map[string]any)
		mj, _ := cp[j].(map[string]any)
		ai := toFloat(lookupField(mi, p.Field))
		aj := toFloat(lookupField(mj, p.Field))
		if p.Order == "asc" {
			return ai < aj
		}
		return ai > aj
	})
	if p.Top < len(cp) {
		cp = cp[:p.Top]
	}
	return map[string]any{"items": cp, "count": len(cp)}, nil
}

var _ orchestrator.Node = (*RankNode)(nil)
