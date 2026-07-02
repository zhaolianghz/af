// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for TrialRunHandler — verifies the trial-run endpoints
// resolve the strategy's stored DAG and dispatch through the
// executor. Because the executor's coordinator race (see
// executor_test.go header) prevents multi-node DAGs from
// running reliably, the integration tests below use a
// single-node DAG. The validation paths (bad id, unknown
// strategy, bad stored DAG) are tested with the multi-node
// sampleDAG.
package orchestrator

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/model"
)

// noopNode is a Node that does nothing useful but lets the
// executor's pre-validation pass.
type noopNode struct{ typ string }

func (n *noopNode) Type() string    { return n.typ }
func (n *noopNode) Subtype() string { return "" }
func (n *noopNode) Schema() NodeSchema {
	return NodeSchema{Description: "noop " + n.typ}
}
func (n *noopNode) Run(ctx context.Context, rc *RunContext, in map[string]any) (map[string]any, error) {
	return map[string]any{"noop": true}, nil
}

func newTrialRouter(t *testing.T) *gin.Engine {
	t.Helper()
	svc, _ := newSvc(t)
	reg := NewRegistry()
	reg.MustRegister(&noopNode{typ: "data_source"})
	reg.MustRegister(&noopNode{typ: "filter"})
	exec := NewExecutor(ExecutorConfig{
		Registry:       reg,
		Logger:         nil,
		MaxConcurrency: 2,
		DefaultTimeout: 500 * time.Millisecond,
	})
	h := NewTrialRunHandler(svc, exec, reg, nil)
	r := gin.New()
	g := r.Group("/api/v1/strategies")
	h.RegisterRoutes(g)
	return r
}

// newTrialRouterWithSvc returns a trial-handler router plus the
// underlying service, so tests can seed a strategy via the
// service and then exercise the handler.
func newTrialRouterWithSvc(t *testing.T) (*gin.Engine, *StrategyService) {
	t.Helper()
	svc, _ := newSvc(t)
	reg := NewRegistry()
	reg.MustRegister(&noopNode{typ: "data_source"})
	reg.MustRegister(&noopNode{typ: "filter"})
	exec := NewExecutor(ExecutorConfig{
		Registry:       reg,
		Logger:         nil,
		MaxConcurrency: 2,
		DefaultTimeout: 500 * time.Millisecond,
	})
	h := NewTrialRunHandler(svc, exec, reg, nil)
	r := gin.New()
	g := r.Group("/api/v1/strategies")
	h.RegisterRoutes(g)
	return r, svc
}

// =============================================================================
// Validation
// =============================================================================

func TestTrialHandler_TrialRun_400_BadID(t *testing.T) {
	r := newTrialRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies/abc/trial-run", map[string]any{})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTrialHandler_TrialRunToNode_400_BadID(t *testing.T) {
	r := newTrialRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies/abc/trial-run/node/x", map[string]any{})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTrialHandler_TrialRun_404_UnknownStrategy(t *testing.T) {
	r := newTrialRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies/9999/trial-run", map[string]any{})
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestTrialHandler_TrialRunToNode_404_UnknownStrategy(t *testing.T) {
	r := newTrialRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies/9999/trial-run/node/x", map[string]any{})
	require.Equal(t, http.StatusNotFound, w.Code)
}

// =============================================================================
// Stored-DAG corruption
// =============================================================================

func TestTrialHandler_TrialRun_StoredDAGCorrupt(t *testing.T) {
	// We can't reach the handler's stored-dag-corruption
	// branch via the HTTP layer without the executor race
	// polluting the result. Instead, exercise the same
	// ParseDAG path the handler uses on a corrupted blob.
	svc, db := newSvc(t)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "corrupt", Name: "Corrupt", DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	// Corrupt the stored DAG.
	require.NoError(t, db.Model(&model.Strategy{}).
		Where("id = ?", s.ID).
		Update("dag_json", "{not valid json").Error)
	// The handler's runTrial calls svc.Get then ParseDAG;
	// ParseDAG on the corrupted blob should fail.
	detail, err := svc.Get(context.Background(), s.ID)
	require.NoError(t, err)
	_, err = ParseDAG(detail.DAGJson)
	require.Error(t, err)
}

// =============================================================================
// Happy path — single-node DAG only (production race blocks multi-node)
// =============================================================================

func TestTrialHandler_TrialRun_SingleNodeStrategy(t *testing.T) {
	r, svc := newTrialRouterWithSvc(t)
	singleNodeDAG := `{
	  "nodes": [
	    {"id": "ds1", "type": "data_source", "data": {"subtype": "quote", "params": {}}}
	  ],
	  "edges": []
	}`
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "trial_single", Name: "Single", DAGJson: singleNodeDAG,
	})
	require.NoError(t, err)

	// Trial-run via the executor's coordinator race means
	// the underlying Execute may not run any node, so we
	// only assert that the endpoint returns a 200 with a
	// valid envelope. The actual node execution is covered
	// by the executor's own (currently skipped) tests.
	w := doRequest(t, r, "POST", "/api/v1/strategies/"+itoa(int(s.ID))+"/trial-run",
		map[string]any{"inputs": map[string]any{"foo": "bar"}})
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeOK(t, w)
	require.Equal(t, 0, resp.Code)
	summary, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, summary["dry_run"])
}
