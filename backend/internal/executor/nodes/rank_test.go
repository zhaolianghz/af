// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for RankNode — verifies the sort direction, top-N
// truncation, default values, and edge cases.
package nodes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/orchestrator"
)

func TestRank_MissingField(t *testing.T) {
	n := NewRankNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, rankParams{Field: "", Order: "desc", Top: 5}),
	}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestRank_InvalidParamsJSON(t *testing.T) {
	n := NewRankNode()
	in := map[string]any{
		orchestrator.InputKeyParams: []byte(`not json`),
	}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestRank_Desc_Default(t *testing.T) {
	n := NewRankNode()
	items := []any{
		map[string]any{"score": 1.0},
		map[string]any{"score": 5.0},
		map[string]any{"score": 3.0},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, rankParams{
			Field: "score", Order: "", Top: 0, // defaults
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 3, out["count"])
	got := out["items"].([]any)
	require.Equal(t, 5.0, got[0].(map[string]any)["score"])
	require.Equal(t, 3.0, got[1].(map[string]any)["score"])
	require.Equal(t, 1.0, got[2].(map[string]any)["score"])
}

func TestRank_Asc(t *testing.T) {
	n := NewRankNode()
	items := []any{
		map[string]any{"x": 3.0},
		map[string]any{"x": 1.0},
		map[string]any{"x": 2.0},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, rankParams{
			Field: "x", Order: "asc", Top: 10,
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	got := out["items"].([]any)
	require.Equal(t, 1.0, got[0].(map[string]any)["x"])
	require.Equal(t, 2.0, got[1].(map[string]any)["x"])
	require.Equal(t, 3.0, got[2].(map[string]any)["x"])
}

func TestRank_TopN(t *testing.T) {
	n := NewRankNode()
	items := []any{
		map[string]any{"x": 1.0},
		map[string]any{"x": 2.0},
		map[string]any{"x": 3.0},
		map[string]any{"x": 4.0},
		map[string]any{"x": 5.0},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, rankParams{
			Field: "x", Order: "desc", Top: 2,
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["count"])
	got := out["items"].([]any)
	require.Equal(t, 5.0, got[0].(map[string]any)["x"])
	require.Equal(t, 4.0, got[1].(map[string]any)["x"])
}

func TestRank_Stable(t *testing.T) {
	// Stable sort: equal values preserve input order.
	n := NewRankNode()
	items := []any{
		map[string]any{"x": 1.0, "tag": "a"},
		map[string]any{"x": 1.0, "tag": "b"},
		map[string]any{"x": 1.0, "tag": "c"},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, rankParams{
			Field: "x", Order: "asc", Top: 10,
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	got := out["items"].([]any)
	require.Equal(t, "a", got[0].(map[string]any)["tag"])
	require.Equal(t, "b", got[1].(map[string]any)["tag"])
	require.Equal(t, "c", got[2].(map[string]any)["tag"])
}

func TestRank_NoSlice(t *testing.T) {
	n := NewRankNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, rankParams{
			Field: "x", Order: "desc", Top: 5,
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 0, out["count"])
}

func TestRank_TopLargerThanSlice(t *testing.T) {
	// Top > len(items) should return all items.
	n := NewRankNode()
	items := []any{
		map[string]any{"x": 1.0},
		map[string]any{"x": 2.0},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, rankParams{
			Field: "x", Order: "desc", Top: 100,
		}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["count"])
}
