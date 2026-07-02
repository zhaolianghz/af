// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for DedupeNode — verifies default key, custom key, the
// empty-key passthrough, and edge cases.
package nodes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/orchestrator"
)

func TestDedupe_InvalidParamsJSON(t *testing.T) {
	n := NewDedupeNode()
	in := map[string]any{
		orchestrator.InputKeyParams: []byte(`not json`),
	}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestDedupe_DefaultKey(t *testing.T) {
	n := NewDedupeNode()
	items := []any{
		map[string]any{"stock_code": "600000.SH", "x": 1},
		map[string]any{"stock_code": "000001.SZ", "x": 2},
		map[string]any{"stock_code": "600000.SH", "x": 3}, // dup
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, dedupeParams{Key: ""}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["count"])
	got := out["items"].([]any)
	require.Equal(t, "600000.SH", got[0].(map[string]any)["stock_code"])
	require.Equal(t, "000001.SZ", got[1].(map[string]any)["stock_code"])
}

func TestDedupe_FirstWins(t *testing.T) {
	n := NewDedupeNode()
	items := []any{
		map[string]any{"k": "a", "v": 1},
		map[string]any{"k": "a", "v": 2}, // dup, dropped
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, dedupeParams{Key: "k"}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	got := out["items"].([]any)
	require.Len(t, got, 1)
	require.Equal(t, 1, got[0].(map[string]any)["v"])
}

func TestDedupe_EmptyKey_PassesThrough(t *testing.T) {
	// Items whose key is "" are passed through (not
	// deduped), since stringOf returns "" for missing keys.
	n := NewDedupeNode()
	items := []any{
		map[string]any{"stock_code": ""},
		map[string]any{"stock_code": ""},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, dedupeParams{Key: "stock_code"}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["count"])
}

func TestDedupe_CustomKey(t *testing.T) {
	n := NewDedupeNode()
	items := []any{
		map[string]any{"group": "X", "name": "a"},
		map[string]any{"group": "X", "name": "b"},
		map[string]any{"group": "Y", "name": "c"},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, dedupeParams{Key: "group"}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["count"])
}

func TestDedupe_NoSlice(t *testing.T) {
	n := NewDedupeNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dedupeParams{Key: "x"}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 0, out["count"])
}

func TestDedupe_NonMapItemSkipped(t *testing.T) {
	n := NewDedupeNode()
	items := []any{
		"not a map",
		map[string]any{"stock_code": "a"},
	}
	in := map[string]any{
		"pred": map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, dedupeParams{Key: "stock_code"}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["count"])
}
