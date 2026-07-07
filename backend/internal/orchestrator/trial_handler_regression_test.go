// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Regression: ISSUE-001 — a node failure in a trial-run surfaced as a
// bare 500 ({code:10000}) and the frontend banner lost the per-node
// detail. The handler must return 200 with the RunSummary whenever the
// executor produced one, even if a node failed.
// Found by /qa on 2026-07-07
// Report: .gstack/qa-reports/qa-report-localhost-2026-07-07.md
package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// errNode always fails — the trial equivalent of the indicator node
// blowing up on "no kline series found in input".
type errNode struct{ typ string }

func (n *errNode) Type() string       { return n.typ }
func (n *errNode) Subtype() string    { return "" }
func (n *errNode) Schema() NodeSchema { return NodeSchema{Description: "err " + n.typ} }
func (n *errNode) Run(ctx context.Context, rc *RunContext, in map[string]any) (map[string]any, error) {
	return nil, errors.New("boom: simulated node failure")
}

func newFailingTrialRouter(t *testing.T) (*gin.Engine, *StrategyService) {
	t.Helper()
	svc, _ := newSvc(t)
	reg := NewRegistry()
	reg.MustRegister(&noopNode{typ: "data_source"})
	reg.MustRegister(&errNode{typ: "filter"})
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

func TestTrialHandler_TrialRun_NodeFailure_Returns200WithSummary(t *testing.T) {
	r, svc := newFailingTrialRouter(t)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "trial_nodefail", Name: "NodeFail", DAGJson: sampleDAG,
	})
	require.NoError(t, err)

	w := doRequest(t, r, "POST", "/api/v1/strategies/"+itoa(int(s.ID))+"/trial-run",
		map[string]any{})
	// Precondition of the bug: filter node fails → executor returns
	// (summary, firstErr). The handler used to turn that into a 500.
	require.Equal(t, http.StatusOK, w.Code)

	resp := decodeOK(t, w)
	require.Equal(t, 0, resp.Code)
	summary, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "failed", summary["status"])
	require.Equal(t, true, summary["dry_run"])
	// Per-node detail must survive: the failed node carries its error.
	results, ok := summary["node_results"].(map[string]any)
	require.True(t, ok)
	f1, ok := results["f1"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "failed", f1["status"])
	require.Contains(t, f1["error"], "boom")
}

func TestTrialHandler_TrialRunToNode_NodeFailure_Returns200WithSummary(t *testing.T) {
	r, svc := newFailingTrialRouter(t)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "trial_nodefail2", Name: "NodeFail2", DAGJson: sampleDAG,
	})
	require.NoError(t, err)

	w := doRequest(t, r, "POST", "/api/v1/strategies/"+itoa(int(s.ID))+"/trial-run/node/f1",
		map[string]any{})
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeOK(t, w)
	summary, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "failed", summary["status"])
}

// A genuinely summary-less error (unknown strategy) must still be an
// HTTP error — the 200-with-summary rule only applies when the
// executor ran.
func TestTrialHandler_TrialRun_NoSummaryErrorStays404(t *testing.T) {
	r, _ := newFailingTrialRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies/424242/trial-run", map[string]any{})
	require.Equal(t, http.StatusNotFound, w.Code)
}
