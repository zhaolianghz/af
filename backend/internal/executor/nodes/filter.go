// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/skyzhao/af/internal/orchestrator"
)

// FilterNode keeps elements of a predecessor's slice payload
// that satisfy a comparison. The "field" param uses dot
// notation for nested access (e.g. "klines.0.close"). The
// "target" param selects "each" (default; iterate items in the
// predecessor's output root array) or "root" (evaluate the
// expression against the root map).
type FilterNode struct{}

// NewFilterNode returns a fresh instance.
func NewFilterNode() *FilterNode { return &FilterNode{} }

func (n *FilterNode) Type() string    { return orchestrator.NodeTypeFilter }
func (n *FilterNode) Subtype() string { return "" }
func (n *FilterNode) Schema() orchestrator.NodeSchema {
	return orchestrator.NodeSchema{
		Description: "Filter a list of records by a comparison expression.",
		Params: map[string]orchestrator.FieldSchema{
			"field":  {Type: "string", Required: true, Description: "Field name (dot-notation supported)"},
			"op":     {Type: "enum", Enum: []string{">", "<", "==", "!=", ">=", "<=", "between", "in", "contains", "contains_any"}, Required: true},
			"value":  {Type: "any", Required: true},
			"target": {Type: "enum", Enum: []string{"each", "root"}, Default: "each"},
		},
		Inputs: map[string]orchestrator.FieldSchema{
			"<any>": {Type: "array", Description: "Slice of records from a preceding node (any predecessor)"},
		},
		Outputs: map[string]orchestrator.FieldSchema{
			"items":   {Type: "array"},
			"matched": {Type: "number"},
			"dropped": {Type: "number"},
		},
	}
}

type filterParams struct {
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
	Target string `json:"target"`
}

func (n *FilterNode) Run(ctx context.Context, rc *orchestrator.RunContext, in map[string]any) (map[string]any, error) {
	var p filterParams
	if raw, ok := in["_params"].([]byte); ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, orchestrator.NewParamError("filter", "params", "invalid json: "+err.Error())
		}
	}
	if p.Field == "" {
		return nil, orchestrator.NewParamError("filter", "field", "is required")
	}
	if p.Op == "" {
		p.Op = "=="
	}
	if p.Target == "" {
		p.Target = "each"
	}

	if p.Target == "root" {
		// Evaluate the expression against the first
		// predecessor's output root.
		for _, v := range in {
			if v == nil {
				continue
			}
			if root, ok := v.(map[string]any); ok {
				ok, err := matchExpr(root, p.Field, p.Op, p.Value)
				if err != nil {
					return nil, err
				}
				if !ok {
					return map[string]any{"matched": false}, nil
				}
				return map[string]any{"matched": true, "value": root}, nil
			}
		}
		return map[string]any{"matched": false}, nil
	}

	// target=each: find the first slice in the inputs and
	// filter it.
	items := findFirstSlice(in)
	if items == nil {
		return map[string]any{"items": []any{}, "matched": 0, "dropped": 0}, nil
	}
	kept := make([]any, 0, len(items))
	dropped := 0
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			dropped++
			continue
		}
		pass, err := matchExpr(m, p.Field, p.Op, p.Value)
		if err != nil {
			return nil, err
		}
		if pass {
			kept = append(kept, it)
		} else {
			dropped++
		}
	}
	return map[string]any{"items": kept, "matched": len(kept), "dropped": dropped}, nil
}

// findFirstSlice returns the first []any found in any
// predecessor payload, or nil.
func findFirstSlice(in map[string]any) []any {
	for k, v := range in {
		if k == "_params" || k == "_type" || k == "_subtype" || k == "_id" {
			continue
		}
		if v == nil {
			continue
		}
		if payload, ok := v.(map[string]any); ok {
			for _, fv := range payload {
				if arr, ok := fv.([]any); ok {
					return arr
				}
			}
		}
	}
	return nil
}

func matchExpr(item map[string]any, field, op string, value any) (bool, error) {
	got := lookupField(item, field)
	switch op {
	case "==", "eq":
		return equalish(got, value), nil
	case "!=", "ne":
		return !equalish(got, value), nil
	case ">", "gt":
		return compareNum(got, value, func(a, b float64) bool { return a > b })
	case "<", "lt":
		return compareNum(got, value, func(a, b float64) bool { return a < b })
	case ">=", "gte":
		return compareNum(got, value, func(a, b float64) bool { return a >= b })
	case "<=", "lte":
		return compareNum(got, value, func(a, b float64) bool { return a <= b })
	case "between":
		arr, ok := value.([]any)
		if !ok || len(arr) != 2 {
			return false, orchestrator.NewParamError("filter", "value", "between requires [low, high]")
		}
		lo := toFloat(arr[0])
		hi := toFloat(arr[1])
		v := toFloat(got)
		return v >= lo && v <= hi, nil
	case "in":
		arr, ok := value.([]any)
		if !ok {
			return false, orchestrator.NewParamError("filter", "value", "in requires [...]")
		}
		for _, e := range arr {
			if equalish(got, e) {
				return true, nil
			}
		}
		return false, nil
	case "contains":
		gs, gok := got.(string)
		vs, vok := value.(string)
		if gok && vok {
			return strings.Contains(gs, vs), nil
		}
		return false, nil
	case "contains_any":
		// OR across multiple substrings — true if the field contains
		// ANY of them. Value may be a JSON array (["分红","重组"]) or a
		// comma-separated string ("分红,重组"). Used for news keyword
		// monitoring where a single `contains` can't express OR and
		// chaining filters ANDs instead.
		gs, gok := got.(string)
		if !gok {
			return false, nil
		}
		needles := toStringList(value)
		if len(needles) == 0 {
			return false, orchestrator.NewParamError("filter", "value", "contains_any requires a non-empty list or comma-separated string")
		}
		for _, ndl := range needles {
			if ndl != "" && strings.Contains(gs, ndl) {
				return true, nil
			}
		}
		return false, nil
	}
	return false, orchestrator.NewParamError("filter", "op", "unknown: "+op)
}

// toStringList coerces a filter value into a list of non-empty strings.
// Accepts a JSON array (mixed types are stringified) or a single
// comma-separated string. Whitespace around each element is trimmed.
func toStringList(value any) []string {
	var raw []string
	switch v := value.(type) {
	case []any:
		for _, e := range v {
			raw = append(raw, fmt.Sprintf("%v", e))
		}
	case []string:
		raw = append(raw, v...)
	case string:
		raw = strings.Split(v, ",")
	default:
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func lookupField(item map[string]any, path string) any {
	parts := strings.Split(path, ".")
	var cur any = item
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[p]
		if !ok {
			return nil
		}
	}
	return cur
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

func equalish(a, b any) bool {
	if af, aok := toFloatOK(a); aok {
		if bf, bok := toFloatOK(b); bok {
			return af == bf
		}
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func toFloatOK(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func compareNum(a, b any, cmp func(a, b float64) bool) (bool, error) {
	af := toFloat(a)
	bf := toFloat(b)
	return cmp(af, bf), nil
}

var _ orchestrator.Node = (*FilterNode)(nil)
