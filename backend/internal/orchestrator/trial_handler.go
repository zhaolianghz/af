// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package orchestrator

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
)

// TrialRunHandler exposes the two trial-run endpoints:
//   POST /api/v1/strategies/:id/trial-run
//   POST /api/v1/strategies/:id/trial-run/node/:nodeId
//
// Trial-runs use a RunContext with DB=nil and Notify=nil, and
// the request is marked DryRun=true. Nodes that touch external
// systems (persist, notify) must short-circuit on a nil
// dependency and report a "would have done X" summary.
type TrialRunHandler struct {
	svc    *StrategyService
	exec   *Executor
	reg    *Registry
	logger *zap.Logger
}

// NewTrialRunHandler wires the handler.
func NewTrialRunHandler(svc *StrategyService, exec *Executor, reg *Registry, l *zap.Logger) *TrialRunHandler {
	if l == nil {
		l = zap.NewNop()
	}
	return &TrialRunHandler{svc: svc, exec: exec, reg: reg, logger: l}
}

// RegisterRoutes mounts the trial-run routes onto the given
// router group. The group's prefix is the caller's
// responsibility (router.Options wires /api/v1/strategies).
func (h *TrialRunHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("/:id/trial-run", h.TrialRun)
	r.POST("/:id/trial-run/node/:nodeId", h.TrialRunToNode)
}

// TrialRun handles POST /strategies/:id/trial-run. Body: {inputs? map[string]any}.
func (h *TrialRunHandler) TrialRun(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperr.InvalidArg("id must be uint64"))
		return
	}
	var body struct {
		Inputs map[string]any `json:"inputs"`
	}
	_ = c.ShouldBindJSON(&body) // optional

	summary, err := h.runTrial(c, id, body.Inputs, "")
	if err != nil {
		// A node-level failure still produces a summary — for a dry
		// run the summary IS the result (per-node status + error), so
		// return it with 200 instead of a 500 that hides the detail.
		if summary != nil {
			httpresp.OK(c, summary)
			return
		}
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,summary)
}

// TrialRunToNode handles POST /strategies/:id/trial-run/node/:nodeId.
func (h *TrialRunHandler) TrialRunToNode(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperr.InvalidArg("id must be uint64"))
		return
	}
	target := c.Param("nodeId")
	if target == "" {
		httpresp.Err(c,apperr.InvalidArg("nodeId is required"))
		return
	}
	var body struct {
		Inputs map[string]any `json:"inputs"`
	}
	_ = c.ShouldBindJSON(&body)

	summary, err := h.runTrial(c, id, body.Inputs, target)
	if err != nil {
		// Same node-failure rule as TrialRun above.
		if summary != nil {
			httpresp.OK(c, summary)
			return
		}
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,summary)
}

// runTrial is the shared trial-run path. The targetNodeID
// argument is empty for full-graph trials.
func (h *TrialRunHandler) runTrial(c *gin.Context, strategyID uint64, inputs map[string]any, targetNodeID string) (*RunSummary, error) {
	detail, err := h.svc.Get(c.Request.Context(), strategyID)
	if err != nil {
		return nil, err
	}
	dag, err := ParseDAG(detail.DAGJson)
	if err != nil {
		return nil, apperr.InvalidArg("stored dag_json is invalid: " + err.Error())
	}

	// Trial-run RunContext: no DB, no Notify, fresh EventBus.
	rc := NewRunContext(RunContextOptions{
		DB:           nil,
		Logger:       h.logger,
		DataSource:   nil, // optional; data_source node will report a no-op
		Notify:       nil,
		Bus:          NewMemBus(),
		RunID:        0, // not persisted
		StrategyID:   strategyID,
		StrategyCode: detail.Code,
		StrategyName: detail.Name,
	})

	req := &RunRequest{DAG: dag, Inputs: inputs, DryRun: true}
	if targetNodeID == "" {
		return h.exec.Execute(c.Request.Context(), rc, req)
	}
	return h.exec.ExecuteToNode(c.Request.Context(), rc, req, targetNodeID)
}
