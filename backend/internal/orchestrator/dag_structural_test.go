// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Supplementary DAG structural tests — adds coverage that the
// existing types_test.go does not provide:
//
//   - TopoSort on a disconnected multi-component DAG (two
//     independent linear sub-graphs in the same document).
//   - TopoSort on a self-loop edge (the path through TopoSort
//     rather than through Validate).
//   - Trial-run shape acceptance: ParseDAG accepts each of the
//     8 canonical node-type documents the ReactFlow canvas
//     emits, and TopoSort produces a valid root-first order.
//
// The existing types_test.go already pins:
//   - Validate on nil / empty / duplicate-ID / missing-type /
//     self-loop / dangling-source / dangling-target /
//     missing-endpoint.
//   - TopoSort on linear / diamond / 2-node cycle / empty DAG.
//   - HasNode / NodeByID / Roots / Predecessors / Successors.
package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// TopoSort — disconnected multi-component DAGs
// =============================================================================

func TestDAG_TopoSort_DisconnectedComponents(t *testing.T) {
	// Two independent linear sub-graphs in one DAG document:
	//   a → b
	//   c → d
	// Both sub-graphs must surface in the output, and within
	// each sub-graph the predecessor must precede its successor.
	// Disconnected DAGs are structurally valid (Validate
	// passes) and the trial-run contract accepts them — this
	// test pins that contract.
	d := &DAG{
		Nodes: []NodeDef{
			{ID: "a", Type: "data_source"},
			{ID: "b", Type: "filter"},
			{ID: "c", Type: "data_source"},
			{ID: "d", Type: "filter"},
		},
		Edges: []Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "c", Target: "d"},
		},
	}
	require.NoError(t, d.Validate())

	order, err := d.TopoSort()
	require.NoError(t, err)
	require.Len(t, order, 4)

	// Each predecessor precedes its successor.
	require.Less(t, idxOf(order, "a"), idxOf(order, "b"))
	require.Less(t, idxOf(order, "c"), idxOf(order, "d"))

	// The Kahn-style tie-break pops the smallest ready ID first,
	// so the deterministic order here is a, b, c, d.
	require.Equal(t, []string{"a", "b", "c", "d"}, order)
}

func TestDAG_TopoSort_SelfLoopRoute(t *testing.T) {
	// Validate rejects the self-loop before TopoSort's cycle
	// detection kicks in. Pin that TopoSort surfaces this as
	// an error (not as a silent empty order, not as a panic).
	d := &DAG{
		Nodes: []NodeDef{{ID: "a", Type: "data_source"}},
		Edges: []Edge{{ID: "e1", Source: "a", Target: "a"}},
	}
	order, err := d.TopoSort()
	require.Error(t, err)
	require.Nil(t, order)
}

// =============================================================================
// Trial-run shape acceptance — each of the 8 node-type documents
// ParseReactFlowJSON + TopoSort must accept.
// =============================================================================

func TestTrialRunShape_AllNodeTypes_ParseAndSort(t *testing.T) {
	// One document per node type, using the same param shape
	// the ReactFlow canvas emits (see
	// frontend/src/stores/canvasStore.ts:defaultParamsFor).
	// Trial-run on any of these documents must reach the
	// executor — if ParseDAG or TopoSort rejects them, the
	// user-facing "试运行" button fails before any node runs.
	cases := []struct {
		name string
		typ  string
		sub  string
		params string
	}{
		{"data_source", "data_source", "kline", `{"stock_codes":["600519.SH"],"period":"1d","days":30}`},
		{"indicator ma", "indicator", "ma", `{"period":20}`},
		{"indicator ema", "indicator", "ema", `{"period":12}`},
		{"indicator macd", "indicator", "macd", `{"fast":12,"slow":26,"signal":9}`},
		{"indicator kdj", "indicator", "kdj", `{"n":9,"m1":3,"m2":3}`},
		{"indicator boll", "indicator", "boll", `{"period":20,"k_stddev":2}`},
		{"indicator volume_ratio", "indicator", "volume_ratio", `{"period":5}`},
		{"indicator turnover_rate", "indicator", "turnover_rate", `{"period":5}`},
		{"filter", "filter", "", `{"field":"chg_pct","op":">","value":0}`},
		{"rank", "rank", "", `{"field":"chg_pct","order":"desc","top":20}`},
		{"dedupe", "dedupe", "", `{"key":"stock_code"}`},
		{"session_tag", "session_tag", "", `{}`},
		{"persist", "persist", "", `{"extra_tags":[]}`},
		{"notify", "notify", "", `{"subtype":"morning"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := `{
			  "nodes": [
			    {"id": "n1", "type": "` + tc.typ + `", "position": {"x": 0, "y": 0},
			     "data": {"subtype": "` + tc.sub + `", "params": ` + tc.params + `}}
			  ],
			  "edges": []
			}`
			dag, err := ParseDAG(doc)
			require.NoError(t, err, "ParseDAG rejected %s", tc.name)
			require.Len(t, dag.Nodes, 1)
			require.Equal(t, tc.typ, dag.Nodes[0].Type)
			require.Equal(t, tc.sub, dag.Nodes[0].Subtype)

			order, err := dag.TopoSort()
			require.NoError(t, err, "TopoSort rejected %s", tc.name)
			require.Equal(t, []string{"n1"}, order)
		})
	}
}

func TestTrialRunShape_CycleDocument_Rejected(t *testing.T) {
	// A user can absolutely draw a cycle on the canvas.
	// Trial-run must reject it with a clear error rather
	// than executing forever or returning stale results.
	doc := `{
	  "nodes": [
	    {"id": "a", "type": "filter", "data": {"params": {"field":"x"}}},
	    {"id": "b", "type": "filter", "data": {"params": {"field":"x"}}}
	  ],
	  "edges": [
	    {"id": "e1", "source": "a", "target": "b"},
	    {"id": "e2", "source": "b", "target": "a"}
	  ]
	}`
	dag, err := ParseDAG(doc)
	require.NoError(t, err) // ParseDAG does not check for cycles
	_, err = dag.TopoSort()
	require.ErrorIs(t, err, ErrCycle)
}

// =============================================================================
// Helpers
// =============================================================================

func idxOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
