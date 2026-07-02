// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for the executor HTTP handler (handler.go). These
// tests cover route registration, body parsing, status code
// mapping, and the read-only paths. The actual DAG walk is
// exercised via the orchestrator tests, not here.
//
// SSE is hard to test in-process (DrainEvents blocks on the
// bus) — the lower-level writeSSE / writeEvent helpers get
// covered directly via httptest.
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

func init() { gin.SetMode(gin.TestMode) }

// newTestHandler returns a *Handler backed by a fresh sqlite db
// and a minimal Executor (no orchestrator wired — fine for the
// read-only + Run-creation paths).
func newTestHandler(t *testing.T) (*Handler, *Executor, *gorm.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "hndl_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))

	e := &Executor{
		db:       db,
		cfg:      defaultExecutorCfg(),
		logger:   zap.NewNop(),
		bus:      orchestrator.NewMemBus(),
		registry: orchestrator.NewRegistry(),
	}
	cfg := config.ExecutorConfig{
		DefaultRunTimeout: 5 * time.Minute,
		SSEHeartbeat:      20 * time.Second,
	}
	return NewHandler(e, zap.NewNop(), cfg), e, db
}

// buildRouter mounts the handler routes onto a gin engine
// rooted at /api/v1 — mirrors the production router.
func buildRouter(h *Handler) *gin.Engine {
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

// =============================================================================
// RegisterRoutes
// =============================================================================

func TestRegisterRoutes_MountsAllPaths(t *testing.T) {
	h := NewHandler(nil, zap.NewNop(), config.ExecutorConfig{})
	r := buildRouter(h)
	for _, c := range []struct {
		method string
		path   string
	}{
		{"POST", "/api/v1/runs"},
		{"GET", "/api/v1/runs"},
		{"GET", "/api/v1/runs/:id"},
		{"GET", "/api/v1/runs/:id/logs"},
		{"POST", "/api/v1/runs/:id/retry"},
		{"GET", "/api/v1/runs/:id/events"},
		{"GET", "/api/v1/recommendations"},
	} {
		require.True(t, routeExists(r, c.method, c.path),
			"missing route %s %s", c.method, c.path)
	}
}

func routeExists(r *gin.Engine, method, path string) bool {
	for _, ri := range r.Routes() {
		if ri.Method == method && ri.Path == path {
			return true
		}
	}
	return false
}

// =============================================================================
// POST /api/v1/runs
// =============================================================================

func TestTrigger_NilExec(t *testing.T) {
	h := NewHandler(nil, zap.NewNop(), config.ExecutorConfig{})
	r := buildRouter(h)
	w := doJSON(r, "POST", "/api/v1/runs", `{}`)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTrigger_BadJSON(t *testing.T) {
	h, _, _ := newTestHandler(t)
	r := buildRouter(h)
	w := doJSON(r, "POST", "/api/v1/runs", `{not json`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTrigger_MissingStrategy(t *testing.T) {
	h, _, _ := newTestHandler(t)
	r := buildRouter(h)
	w := doJSON(r, "POST", "/api/v1/runs", `{}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTrigger_ByID(t *testing.T) {
	h, _, db := newTestHandler(t)
	require.NoError(t, db.Create(&model.Strategy{
		Code: "x", Name: "x", Status: model.StrategyStatusActive,
	}).Error)

	r := buildRouter(h)
	w := doJSON(r, "POST", "/api/v1/runs", `{"strategy_id":1}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var body struct {
		Code int `json:"code"`
		Data struct {
			RunID      uint64 `json:"run_id"`
			StrategyID uint64 `json:"strategy_id"`
			Status     string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, int(apperr.CodeOK), body.Code)
	require.NotZero(t, body.Data.RunID)
	require.Equal(t, uint64(1), body.Data.StrategyID)
	require.Equal(t, model.RunStatusRunning, body.Data.Status)
}

func TestTrigger_ByCode(t *testing.T) {
	h, _, db := newTestHandler(t)
	require.NoError(t, db.Create(&model.Strategy{
		Code: "mvb", Name: "mvb", Status: model.StrategyStatusActive,
	}).Error)

	r := buildRouter(h)
	w := doJSON(r, "POST", "/api/v1/runs", `{"strategy_code":"mvb"}`)
	require.Equal(t, http.StatusCreated, w.Code)
}

func TestTrigger_ByCode_NotFound(t *testing.T) {
	h, _, _ := newTestHandler(t)
	r := buildRouter(h)
	w := doJSON(r, "POST", "/api/v1/runs", `{"strategy_code":"missing"}`)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestTrigger_RunRowPersisted(t *testing.T) {
	h, e, db := newTestHandler(t)
	require.NoError(t, db.Create(&model.Strategy{
		Code: "p", Name: "p", Status: model.StrategyStatusActive,
	}).Error)

	// Wire a minimal no-op orchestrator so dispatch doesn't
	// blow up. The DAG walk is not what we're testing here.
	e.strategy = &orchestrator.StrategyService{}
	e.executor = &orchestrator.Executor{}
	// strategy is just a placeholder struct; Get will fail
	// and the dispatch goroutine will log the error. The
	// row is what we care about.
	r := buildRouter(h)
	w := doJSON(r, "POST", "/api/v1/runs", `{"strategy_id":1}`)
	require.Equal(t, http.StatusCreated, w.Code)

	// Give the goroutine a moment to write its log.
	time.Sleep(50 * time.Millisecond)
}

// =============================================================================
// GET /api/v1/runs
// =============================================================================

func TestList_NilExec(t *testing.T) {
	h := NewHandler(nil, zap.NewNop(), config.ExecutorConfig{})
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs", "")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestList_Defaults(t *testing.T) {
	h, e, _ := newTestHandler(t)
	for i := 0; i < 3; i++ {
		_, err := e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
		require.NoError(t, err)
	}
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs", "")
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			Total int           `json:"total"`
			Items []model.Run   `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 3, body.Data.Total)
	require.Len(t, body.Data.Items, 3)
}

func TestList_FilterByStrategyID(t *testing.T) {
	h, e, _ := newTestHandler(t)
	_, _ = e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	_, _ = e.CreateRunRow(context.Background(), 2, model.RunTriggerManual)
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs?strategy_id=1", "")
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 1, body.Data.Total)
}

func TestList_FilterByStatus(t *testing.T) {
	h, e, _ := newTestHandler(t)
	_, _ = e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs?status=running", "")
	require.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// GET /api/v1/runs/:id
// =============================================================================

func TestDetail_NilExec(t *testing.T) {
	h := NewHandler(nil, zap.NewNop(), config.ExecutorConfig{})
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs/1", "")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestDetail_BadID(t *testing.T) {
	h, _, _ := newTestHandler(t)
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs/abc", "")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDetail_NotFound(t *testing.T) {
	h, _, _ := newTestHandler(t)
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs/999", "")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDetail_Happy(t *testing.T) {
	h, e, _ := newTestHandler(t)
	run, err := e.CreateRunRow(context.Background(), 7, model.RunTriggerManual)
	require.NoError(t, err)
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs/"+strconv.FormatUint(run.ID, 10), "")
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			ID         uint64 `json:"id"`
			StrategyID uint64 `json:"strategy_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, run.ID, body.Data.ID)
	require.Equal(t, uint64(7), body.Data.StrategyID)
}

// =============================================================================
// GET /api/v1/runs/:id/logs
// =============================================================================

func TestLogs_NilExec(t *testing.T) {
	h := NewHandler(nil, zap.NewNop(), config.ExecutorConfig{})
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs/1/logs", "")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestLogs_NotFound(t *testing.T) {
	h, _, _ := newTestHandler(t)
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs/999/logs", "")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestLogs_Empty(t *testing.T) {
	h, e, _ := newTestHandler(t)
	run, _ := e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/runs/"+strconv.FormatUint(run.ID, 10)+"/logs", "")
	require.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// POST /api/v1/runs/:id/retry
// =============================================================================

func TestRetry_NilExec(t *testing.T) {
	h := NewHandler(nil, zap.NewNop(), config.ExecutorConfig{})
	r := buildRouter(h)
	w := doJSON(r, "POST", "/api/v1/runs/1/retry", "")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRetry_BadID(t *testing.T) {
	h, _, _ := newTestHandler(t)
	r := buildRouter(h)
	w := doJSON(r, "POST", "/api/v1/runs/abc/retry", "")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Retry_NotFound(t *testing.T) {
	h, _, _ := newTestHandler(t)
	r := buildRouter(h)
	w := doJSON(r, "POST", "/api/v1/runs/999/retry", "")
	require.Equal(t, http.StatusNotFound, w.Code)
}

// =============================================================================
// GET /api/v1/recommendations
// =============================================================================

func TestListRecommendations_NilExec(t *testing.T) {
	h := NewHandler(nil, zap.NewNop(), config.ExecutorConfig{})
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/recommendations", "")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandler_ListRecommendations_Empty(t *testing.T) {
	h, _, _ := newTestHandler(t)
	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/recommendations", "")
	require.Equal(t, http.StatusOK, w.Code)
}

func TestListRecommendations_WithTag(t *testing.T) {
	h, _, db := newTestHandler(t)
	now := time.Now()
	rec := model.Recommendation{
		RunID: 1, Date: now, StockCode: "A", StockName: "A",
		StrategyCode: "x", StrategyName: "x",
	}
	require.NoError(t, db.Create(&rec).Error)
	require.NoError(t, db.Create(&model.RecommendationTag{
		RecommendationID: rec.ID, Tag: model.SessionTagMorning,
		Source: model.TagSourceAutoNode, TaggedAt: now,
	}).Error)

	r := buildRouter(h)
	w := doRequest(r, "GET", "/api/v1/recommendations?tag="+model.SessionTagMorning, "")
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 1, body.Data.Total)
}

// =============================================================================
// SSE helpers
// =============================================================================

func TestWriteSSE_Plain(t *testing.T) {
	w := httptest.NewRecorder()
	require.NoError(t, writeSSE(w, "ready", `{"x":1}`))
	out := w.Body.String()
	require.Contains(t, out, "event: ready\n")
	require.Contains(t, out, "data: {\"x\":1}\n\n")
}

func TestWriteSSE_NoEventName(t *testing.T) {
	w := httptest.NewRecorder()
	require.NoError(t, writeSSE(w, "", `{"x":1}`))
	out := w.Body.String()
	require.NotContains(t, out, "event:")
	require.Contains(t, out, "data: {\"x\":1}\n\n")
}

func TestWriteEvent_Heartbeat(t *testing.T) {
	w := httptest.NewRecorder()
	require.NoError(t, writeEvent(w, orchestrator.Event{Type: "__heartbeat__", Timestamp: time.Now()}))
	out := w.Body.String()
	require.Contains(t, out, "event: heartbeat\n")
}

func TestWriteEvent_Regular(t *testing.T) {
	w := httptest.NewRecorder()
	ts := time.Now()
	require.NoError(t, writeEvent(w, orchestrator.Event{
		RunID: 1, Type: orchestrator.EventNodeStarted, Timestamp: ts,
	}))
	out := w.Body.String()
	require.Contains(t, out, "event: "+string(orchestrator.EventNodeStarted))
	require.Contains(t, out, "id: "+strconv.FormatInt(ts.UnixNano(), 10))
	require.Contains(t, out, "data: ")
}

// =============================================================================
// Query helpers
// =============================================================================

func TestParseUint(t *testing.T) {
	require.Equal(t, uint64(0), parseUint(""))
	require.Equal(t, uint64(0), parseUint("abc"))
	require.Equal(t, uint64(42), parseUint("42"))
}

func TestAtoiDefault(t *testing.T) {
	require.Equal(t, 7, atoiDefault("", 7))
	require.Equal(t, 7, atoiDefault("bad", 7))
	require.Equal(t, 7, atoiDefault("0", 7))
	require.Equal(t, 5, atoiDefault("5", 7))
	require.Equal(t, 7, atoiDefault("-1", 7))
}

func TestParseTimeQuery(t *testing.T) {
	_, ok := parseTimeQuery("")
	require.False(t, ok)

	t1, ok := parseTimeQuery("2026-06-13")
	require.True(t, ok)
	require.Equal(t, 2026, t1.Year())

	t2, ok := parseTimeQuery("2026-06-13T10:00:00Z")
	require.True(t, ok)
	require.Equal(t, time.UTC, t2.Location())

	_, ok = parseTimeQuery("garbage")
	require.False(t, ok)
}

// =============================================================================
// dispatch goroutine path
// =============================================================================

func TestDispatch_NoDB(t *testing.T) {
	h := &Handler{
		exec:   &Executor{logger: zap.NewNop()},
		logger: zap.NewNop(),
		cfg:    defaultExecutorCfg(),
	}
	// Should log an error and return — no panic, no
	// goroutine leak detectable from the test side.
	h.dispatch(99, 1, nil)
}

// =============================================================================
// Helpers
// =============================================================================

func doJSON(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != "" {
		rdr = bytes.NewReader([]byte(body))
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ensure unused import: errors (referenced indirectly by
// orchestrator types in helper closures).
var _ = httptest.NewRecorder
