// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for FilterNode — verifies parameter validation, the nine
// comparison operators, the "each" vs "root" target switch, and
// edge cases (empty input, no slice, mixed types).
package nodes

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/orchestrator"
)

func newFilterRC() *orchestrator.RunContext {
	return orchestrator.NewRunContext(orchestrator.RunContextOptions{})
}

func params(t *testing.T, p any) []byte {
	t.Helper()
	b, err := json.Marshal(p)
	require.NoError(t, err)
	return b
}

// =============================================================================
// Parameter validation
// =============================================================================

func TestFilter_MissingField(t *testing.T) {
	n := NewFilterNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, filterParams{Field: "", Op: "==", Value: 1}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
	require.Nil(t, out)
}

func TestFilter_InvalidParamsJSON(t *testing.T) {
	n := NewFilterNode()
	in := map[string]any{
		orchestrator.InputKeyParams: []byte(`{not json`),
	}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestFilter_Defaults(t *testing.T) {
	// Missing op and target should default to "==" and "each".
	n := NewFilterNode()
	items := []any{
		map[string]any{"x": 1},
		map[string]any{"x": 2},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "x", Op: "", Target: "",
			Value: 1,
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["matched"])
}

// =============================================================================
// Operators
// =============================================================================

func TestFilter_Eq_Ne(t *testing.T) {
	n := NewFilterNode()
	items := []any{
		map[string]any{"x": 1},
		map[string]any{"x": 2},
		map[string]any{"x": 1},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "x", Op: "==", Value: 1,
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["matched"])

	in[orchestrator.InputKeyParams] = params(t, filterParams{
		Field: "x", Op: "!=", Value: 1,
	})
	out, err = n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["matched"])
}

func TestFilter_NumericComparisons(t *testing.T) {
	n := NewFilterNode()
	items := []any{
		map[string]any{"x": 1.0},
		map[string]any{"x": 2.0},
		map[string]any{"x": 3.0},
		map[string]any{"x": 4.0},
		map[string]any{"x": 5.0},
	}
	cases := []struct {
		op   string
		val  any
		want int
	}{
		{">", 2, 3}, // 3,4,5
		{"<", 4, 3}, // 1,2,3
		{">=", 3, 3},
		{"<=", 3, 3},
	}
	for _, c := range cases {
		t.Run(c.op, func(t *testing.T) {
			in := map[string]any{
				"pred": map[string]any{"items": items},
				orchestrator.InputKeyParams: params(t, filterParams{
					Field: "x", Op: c.op, Value: c.val,
				}),
			}
			out, err := n.Run(context.Background(), newFilterRC(), in)
			require.NoError(t, err)
			require.Equal(t, c.want, out["matched"])
		})
	}
}

func TestFilter_Between(t *testing.T) {
	n := NewFilterNode()
	items := []any{
		map[string]any{"x": 1.0},
		map[string]any{"x": 5.0},
		map[string]any{"x": 10.0},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "x", Op: "between",
			Value: []any{2.0, 7.0},
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["matched"])
}

func TestFilter_Between_BadValue(t *testing.T) {
	n := NewFilterNode()
	in := map[string]any{
		"pred": map[string]any{"items": []any{map[string]any{"x": 1}}},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "x", Op: "between", Value: "not-array",
		}),
	}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestFilter_In(t *testing.T) {
	n := NewFilterNode()
	items := []any{
		map[string]any{"x": "a"},
		map[string]any{"x": "b"},
		map[string]any{"x": "c"},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "x", Op: "in", Value: []any{"a", "c"},
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["matched"])
}

func TestFilter_In_BadValue(t *testing.T) {
	n := NewFilterNode()
	in := map[string]any{
		"pred": map[string]any{"items": []any{map[string]any{"x": "a"}}},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "x", Op: "in", Value: "scalar",
		}),
	}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestFilter_Contains(t *testing.T) {
	n := NewFilterNode()
	items := []any{
		map[string]any{"name": "alpha"},
		map[string]any{"name": "beta"},
		map[string]any{"name": "alphabet"},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "name", Op: "contains", Value: "alpha",
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["matched"])
}

func TestFilter_ContainsAny_Array(t *testing.T) {
	n := NewFilterNode()
	items := []any{
		map[string]any{"title": "贵州茅台分红350亿"},
		map[string]any{"title": "某公司重大资产重组"},
		map[string]any{"title": "日常经营公告"},
	}
	in := map[string]any{
		"pred":                      map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, filterParams{Field: "title", Op: "contains_any", Value: []any{"分红", "重组"}}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["matched"]) // 分红 + 重组 match; 日常公告 dropped
	require.Equal(t, 1, out["dropped"])
}

func TestFilter_ContainsAny_CommaString(t *testing.T) {
	// Comma-separated string form, with whitespace around terms.
	n := NewFilterNode()
	items := []any{
		map[string]any{"title": "公司拟增持回购股份"},
		map[string]any{"title": "中标重大工程项目"},
		map[string]any{"title": "无关新闻"},
	}
	in := map[string]any{
		"pred":                      map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, filterParams{Field: "title", Op: "contains_any", Value: " 增持 , 中标 "}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["matched"])
}

func TestFilter_ContainsAny_NoMatch(t *testing.T) {
	n := NewFilterNode()
	items := []any{map[string]any{"title": "日常公告"}}
	in := map[string]any{
		"pred":                      map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, filterParams{Field: "title", Op: "contains_any", Value: []any{"分红", "重组"}}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 0, out["matched"])
}

func TestFilter_ContainsAny_EmptyValue(t *testing.T) {
	// Blank-only value is a param error (nothing to match).
	n := NewFilterNode()
	in := map[string]any{
		"pred":                      map[string]any{"items": []any{map[string]any{"title": "x"}}},
		orchestrator.InputKeyParams: params(t, filterParams{Field: "title", Op: "contains_any", Value: " , "}),
	}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestFilter_UnknownOp(t *testing.T) {
	n := NewFilterNode()
	in := map[string]any{
		"pred": map[string]any{"items": []any{map[string]any{"x": 1}}},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "x", Op: "??", Value: 1,
		}),
	}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

// =============================================================================
// Target=root
// =============================================================================

func TestFilter_TargetRoot_Match(t *testing.T) {
	n := NewFilterNode()
	in := map[string]any{
		"pred": map[string]any{"value": 42, "label": "ok"},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "value", Op: "==", Value: 42, Target: "root",
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, true, out["matched"])
}

func TestFilter_TargetRoot_NoMatch(t *testing.T) {
	n := NewFilterNode()
	in := map[string]any{
		"pred": map[string]any{"value": 0},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "value", Op: "==", Value: 42, Target: "root",
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, false, out["matched"])
}

// =============================================================================
// Empty / missing inputs
// =============================================================================

func TestFilter_NoSlice(t *testing.T) {
	n := NewFilterNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "x", Op: "==", Value: 1,
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 0, out["matched"])
}

func TestFilter_NonMapItem(t *testing.T) {
	// Non-map items are dropped (counted as dropped).
	n := NewFilterNode()
	items := []any{
		"not a map",
		map[string]any{"x": 1},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "x", Op: "==", Value: 1,
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["matched"])
	require.Equal(t, 1, out["dropped"])
}

// =============================================================================
// Dot-notation field
// =============================================================================

func TestFilter_DotField(t *testing.T) {
	// dot-notation walks nested map[string]any — array
	// indexing ("klines.0.close") is NOT supported by
	// lookupField, so we use a nested map shape.
	n := NewFilterNode()
	items := []any{
		map[string]any{"klines": map[string]any{"close": 10.0, "open": 9.0}},
		map[string]any{"klines": map[string]any{"close": 20.0, "open": 19.0}},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "klines.close", Op: ">", Value: 15.0,
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["matched"])
}

// =============================================================================
// findFirstSlice helper
// =============================================================================

func TestFindFirstSlice_PicksFirstArray(t *testing.T) {
	in := map[string]any{
		orchestrator.InputKeyParams: []byte(`{}`),
		orchestrator.InputKeyType:   "x",
		"pred": map[string]any{
			"scalars": "no",
			"items":   []any{1, 2, 3},
		},
	}
	got := findFirstSlice(in)
	require.NotNil(t, got)
	require.Len(t, got, 3)
}

func TestFindFirstSlice_SkipsReservedKeys(t *testing.T) {
	in := map[string]any{
		orchestrator.InputKeyParams: []byte(`{"foo": [1]}`), // array inside the params blob
		"pred": map[string]any{
			"items": []any{10, 20},
		},
	}
	got := findFirstSlice(in)
	require.NotNil(t, got)
	require.Equal(t, []any{10, 20}, got)
}

func TestFindFirstSlice_Nil(t *testing.T) {
	require.Nil(t, findFirstSlice(nil))
	require.Nil(t, findFirstSlice(map[string]any{}))
}
