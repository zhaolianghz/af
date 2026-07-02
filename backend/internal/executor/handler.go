// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package executor — HTTP handler (handler.go).
//
// Six endpoints exposed under /api/v1/runs plus a /api/v1/recommendations
// read-only endpoint:
//
//	POST   /api/v1/runs                       trigger a manual run
//	GET    /api/v1/runs                       list runs (filter by strategy/status/date)
//	GET    /api/v1/runs/:id                   detail
//	GET    /api/v1/runs/:id/logs              per-node logs
//	POST   /api/v1/runs/:id/retry             retry (clones into a new run)
//	GET    /api/v1/runs/:id/events            SSE stream of run-time events
//	GET    /api/v1/recommendations            list persisted recommendations
//
// POST /runs returns the run id within ~3s; the actual DAG walk runs in a
// goroutine and emits events via the in-process EventBus.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

// Handler is the route registrar for the executor endpoints. The
// strategy / template handlers live in their own packages — this
// handler only covers run lifecycle + recommendation reads.
type Handler struct {
	exec   *Executor
	logger *zap.Logger
	cfg    config.ExecutorConfig
}

// NewHandler wires a Handler. exec may be nil; the routes still
// register but every request returns 503 Unavailable.
func NewHandler(exec *Executor, l *zap.Logger, cfg config.ExecutorConfig) *Handler {
	if l == nil {
		l = zap.NewNop()
	}
	return &Handler{exec: exec, logger: l, cfg: cfg}
}

// RegisterRoutes mounts the executor routes onto the given group.
// The caller (router.Options) decides the URL prefix; for the v1
// API it is /api/v1.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup) {
	runs := api.Group("/runs")
	runs.POST("", h.Trigger)
	runs.GET("", h.List)
	runs.GET("/:id", h.Detail)
	runs.GET("/:id/logs", h.Logs)
	runs.POST("/:id/retry", h.Retry)
	runs.GET("/:id/events", h.Events)

	recs := api.Group("/recommendations")
	recs.GET("", h.ListRecommendations)

	api.GET("/dashboard", h.Dashboard)
}

// Dashboard handles GET /dashboard?window_days=N — the landing-page
// summary (today's recs, run success rate over a window, recent
// errors). Win-rate-by-strategy lives in /perf/aggregations; the
// frontend fetches both.
func (h *Handler) Dashboard(c *gin.Context) {
	if h.exec == nil {
		httpresp.Err(c, apperr.Unavailable("executor not wired"))
		return
	}
	window := atoiDefault(c.Query("window_days"), 7)
	sum, err := h.exec.DashboardStats(c.Request.Context(), window)
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, sum)
}

// =============================================================================
// POST /api/v1/runs
// =============================================================================

// triggerRequest is the body of POST /runs. Either StrategyID or
// StrategyCode must be set.
type triggerRequest struct {
	StrategyID   uint64         `json:"strategy_id"`
	StrategyCode string         `json:"strategy_code"`
	StrategyName string         `json:"strategy_name"`
	Inputs       map[string]any `json:"inputs"`
}

// Trigger creates a Run row and dispatches the DAG walk in a
// goroutine. Returns within ~3s with the new run id; clients
// subscribe via GET /runs/:id/events to see node progress.
func (h *Handler) Trigger(c *gin.Context) {
	if h.exec == nil {
		httpresp.Err(c,apperr.Unavailable("executor not wired"))
		return
	}
	var in triggerRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		httpresp.Err(c,apperr.InvalidArg(err.Error()))
		return
	}
	strategyID := in.StrategyID
	if strategyID == 0 && in.StrategyCode != "" {
		// Resolve by code so callers can trigger via a
		// stable identifier without an internal id.
		id, err := h.exec.strategyIDByCode(c.Request.Context(), in.StrategyCode)
		if err != nil {
			httpresp.Err(c,err)
			return
		}
		strategyID = id
	}
	if strategyID == 0 {
		httpresp.Err(c,apperr.InvalidArg("strategy_id or strategy_code is required"))
		return
	}

	// Create the Run row synchronously so we can return the id.
	run, err := h.exec.CreateRunRow(c.Request.Context(), strategyID, model.RunTriggerManual)
	if err != nil {
		httpresp.Err(c,err)
		return
	}

	// Dispatch the actual execution in a background goroutine
	// so the HTTP request returns promptly.
	go h.dispatch(run.ID, strategyID, in.Inputs)

	httpresp.Created(c,gin.H{"run_id": run.ID, "strategy_id": strategyID, "status": run.Status})
}

// dispatch is the goroutine entry point for the Trigger path.
// It uses a fresh context.Background() with a timeout so the run
// survives the HTTP request returning.
func (h *Handler) dispatch(runID, strategyID uint64, inputs map[string]any) {
	timeout := h.cfg.DefaultRunTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// Re-load the pre-created Run row so Execute can use it.
	run, err := h.exec.GetRun(ctx, runID)
	if err != nil {
		h.logger.Error("executor: dispatch: load run", zap.Uint64("run_id", runID), zap.Error(err))
		return
	}
	_, _ = h.exec.Execute(ctx, RunRequest{
		StrategyID:  strategyID,
		TriggerType: model.RunTriggerManual,
		Inputs:      inputs,
		ExistingRun: run,
	})
}

// =============================================================================
// GET /api/v1/runs
// =============================================================================

// List handles GET /runs?strategy_id=&status=&from=&to=&page=&page_size=.
func (h *Handler) List(c *gin.Context) {
	if h.exec == nil {
		httpresp.Err(c,apperr.Unavailable("executor not wired"))
		return
	}
	f := RunListFilter{
		StrategyID: parseUint(c.Query("strategy_id")),
		Status:     c.Query("status"),
		Page:       atoiDefault(c.Query("page"), 1),
		PageSize:   atoiDefault(c.Query("page_size"), 20),
	}
	if from, ok := parseTimeQuery(c.Query("from")); ok {
		f.From = &from
	}
	if to, ok := parseTimeQuery(c.Query("to")); ok {
		f.To = &to
	}
	rows, total, err := h.exec.ListRuns(c.Request.Context(), f)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,gin.H{"items": rows, "total": total, "page": f.Page, "page_size": f.PageSize})
}

// =============================================================================
// GET /api/v1/runs/:id
// =============================================================================

// Detail handles GET /runs/:id.
func (h *Handler) Detail(c *gin.Context) {
	if h.exec == nil {
		httpresp.Err(c,apperr.Unavailable("executor not wired"))
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperr.InvalidArg("id must be uint64"))
		return
	}
	r, err := h.exec.GetRun(c.Request.Context(), id)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,r)
}

// =============================================================================
// GET /api/v1/runs/:id/logs
// =============================================================================

// Logs handles GET /runs/:id/logs.
func (h *Handler) Logs(c *gin.Context) {
	if h.exec == nil {
		httpresp.Err(c,apperr.Unavailable("executor not wired"))
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperr.InvalidArg("id must be uint64"))
		return
	}
	// Make sure the run exists; surface a 404 instead of an
	// empty list when the caller fat-fingered the id.
	if _, err := h.exec.GetRun(c.Request.Context(), id); err != nil {
		httpresp.Err(c,err)
		return
	}
	rows, err := h.exec.GetRunLogs(c.Request.Context(), id)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,gin.H{"items": rows})
}

// =============================================================================
// POST /api/v1/runs/:id/retry
// =============================================================================

// Retry handles POST /runs/:id/retry. Returns the new run id; the
// actual run happens in a background goroutine.
func (h *Handler) Retry(c *gin.Context) {
	if h.exec == nil {
		httpresp.Err(c,apperr.Unavailable("executor not wired"))
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperr.InvalidArg("id must be uint64"))
		return
	}
	newID, err := h.exec.Retry(c.Request.Context(), id)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.Created(c,gin.H{"run_id": newID, "retry_of": id})
}

// =============================================================================
// GET /api/v1/runs/:id/events  (SSE)
// =============================================================================

// Events streams run-time events via SSE.
//
// Wire format:
//
//	event: node.started
//	id: 1737
//	data: {"run_id":1,"type":"node.started","ts":"...","node_id":"..."}
//
// A heartbeat (event: heartbeat) is emitted every cfg.SSEHeartbeat
// so proxies don't time the connection out. On disconnect we drop
// the EventBus subscription via the unsub callback returned by
// Subscribe.
func (h *Handler) Events(c *gin.Context) {
	if h.exec == nil {
		httpresp.Err(c,apperr.Unavailable("executor not wired"))
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperr.InvalidArg("id must be uint64"))
		return
	}
	if _, err := h.exec.GetRun(c.Request.Context(), id); err != nil {
		httpresp.Err(c,err)
		return
	}

	heartbeat := h.cfg.SSEHeartbeat
	if heartbeat <= 0 {
		heartbeat = 20 * time.Second
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		httpresp.Err(c,apperr.Unavailable("streaming unsupported"))
		return
	}

	// Initial event so the client knows the stream is live.
	if err := writeSSE(c.Writer, "ready", fmt.Sprintf(`{"run_id":%d}`, id)); err != nil {
		return
	}
	flusher.Flush()

	ctx := c.Request.Context()
	DrainEvents(ctx, h.exec.Bus(), id, heartbeat, func(ev orchestrator.Event) error {
		return writeEvent(c.Writer, ev)
	})
	flusher.Flush()
}

// =============================================================================
// GET /api/v1/recommendations
// =============================================================================

// ListRecommendations handles GET /recommendations?strategy_code=&tag=&from=&to=&page=&page_size=.
func (h *Handler) ListRecommendations(c *gin.Context) {
	if h.exec == nil {
		httpresp.Err(c,apperr.Unavailable("executor not wired"))
		return
	}
	f := RecommendationFilter{
		StrategyCode: c.Query("strategy_code"),
		Tag:          c.Query("tag"),
		Page:         atoiDefault(c.Query("page"), 1),
		PageSize:     atoiDefault(c.Query("page_size"), 20),
	}
	if from, ok := parseTimeQuery(c.Query("from")); ok {
		f.From = &from
	}
	if to, ok := parseTimeQuery(c.Query("to")); ok {
		f.To = &to
	}
	rows, total, err := h.exec.ListRecommendations(c.Request.Context(), f)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,gin.H{"items": rows, "total": total, "page": f.Page, "page_size": f.PageSize})
}

// =============================================================================
// Local helpers
// =============================================================================

// writeSSE writes a `data:` line. Used for the initial ready frame
// and for heartbeat ticks.
func writeSSE(w http.ResponseWriter, event string, data string) error {
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

// writeEvent formats an orchestrator.Event as an SSE frame. The id
// field uses the wall-clock unix-nano so EventSource's last-event-id
// resume protocol works.
func writeEvent(w http.ResponseWriter, ev orchestrator.Event) error {
	if ev.Type == "__heartbeat__" {
		return writeSSE(w, "heartbeat", fmt.Sprintf(`{"ts":%q}`, ev.Timestamp.Format(time.RFC3339Nano)))
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return writeSSE(w, "error", fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	if _, err := fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", ev.Type, ev.Timestamp.UnixNano(), payload); err != nil {
		return err
	}
	return nil
}

func parseUint(s string) uint64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// parseTimeQuery accepts RFC3339 or YYYY-MM-DD.
func parseTimeQuery(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}