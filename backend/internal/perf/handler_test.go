// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/model"
)

func init() { gin.SetMode(gin.TestMode) }

// newHandlerTestDB returns an in-memory sqlite DB with the perf tables
// migrated, plus one recommendation (id=100) for the handlers to hang
// snapshots off of.
func newHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Recommendation{}, &model.PerformanceSnapshot{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rec := model.Recommendation{
		BaseEntity:   model.BaseEntity{ID: 100},
		RunID:        1,
		Date:         time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC),
		StockCode:    "600519",
		StrategyCode: "morning_breakout",
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("create rec: %v", err)
	}
	return db
}

// newHandlerRouter wires the perf routes against a Handler backed by db.
func newHandlerRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	svc := NewService(Options{DB: db, Logger: zap.NewNop()})
	h := NewHandler(svc, nil)
	r := gin.New()
	g := r.Group("/api/v1/perf")
	h.RegisterRoutes(g)
	return r
}

func doReq(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// =============================================================================
// Latest
// =============================================================================

func TestHandler_Latest_404_NoSnapshot(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "GET", "/api/v1/perf/recommendations/100", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandler_Latest_400_BadID(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "GET", "/api/v1/perf/recommendations/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandler_Latest_200(t *testing.T) {
	db := newHandlerTestDB(t)
	svc := NewService(Options{DB: db, Logger: zap.NewNop()})
	if err := svc.Save(context.Background(), &model.PerformanceSnapshot{
		RecommendationID: 100,
		SnapshotDate:     time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		T1Return:         ptrF(0.05),
		CalculatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "GET", "/api/v1/perf/recommendations/100", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != 0 || resp.Data["snapshot"] == nil {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

// =============================================================================
// History
// =============================================================================

func TestHandler_History_200_Empty(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "GET", "/api/v1/perf/recommendations/100/history", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Total != 0 {
		t.Errorf("total = %d, want 0", resp.Data.Total)
	}
}

func TestHandler_History_400_BadID(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "GET", "/api/v1/perf/recommendations/xyz/history", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// =============================================================================
// Calculate
// =============================================================================

func TestHandler_Calculate_400_NoInput(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "POST", "/api/v1/perf/calculate", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (no input)", w.Code)
	}
}

func TestHandler_Calculate_400_BadJSON(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	req, _ := http.NewRequest("POST", "/api/v1/perf/calculate", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (bad json)", w.Code)
	}
}

func TestHandler_Calculate_400_FromWithoutTo(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "POST", "/api/v1/perf/calculate", map[string]any{"from": "2026-05-01"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (from without to)", w.Code)
	}
}

func TestHandler_Calculate_400_BadFromDate(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "POST", "/api/v1/perf/calculate", map[string]any{"from": "nope", "to": "2026-05-10"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (bad from date)", w.Code)
	}
}

func TestHandler_Calculate_404_RecNotFound(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "POST", "/api/v1/perf/calculate", map[string]any{"recommendation_id": 99999})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (rec not found); body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Calculate_Range_200(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	// Range mode with no recs in range still returns 200 (processed=0).
	w := doReq(t, r, "POST", "/api/v1/perf/calculate", map[string]any{
		"from": "2020-01-01", "to": "2020-01-02",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (empty range); body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// Aggregations
// =============================================================================

func TestHandler_Aggregations_400_BadGroupBy(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "GET", "/api/v1/perf/aggregations?group_by=banana", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (bad group_by)", w.Code)
	}
}

func TestHandler_Aggregations_400_BadFromDate(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "GET", "/api/v1/perf/aggregations?from=notadate", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (bad from)", w.Code)
	}
}

func TestHandler_Aggregations_200_Default(t *testing.T) {
	db := newHandlerTestDB(t)
	svc := NewService(Options{DB: db, Logger: zap.NewNop()})
	_ = svc.Save(context.Background(), &model.PerformanceSnapshot{
		RecommendationID: 100,
		SnapshotDate:     time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		T1Return:         ptrF(0.05),
		T5Return:         ptrF(0.10),
		CalculatedAt:     time.Now(),
	})
	r := newHandlerRouter(t, db)
	w := doReq(t, r, "GET", "/api/v1/perf/aggregations?group_by=strategy", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			GroupBy string `json:"group_by"`
			Total   int64  `json:"total"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.GroupBy != "strategy" {
		t.Errorf("group_by = %q, want strategy", resp.Data.GroupBy)
	}
}

func TestHandler_Aggregations_200_PageClamp(t *testing.T) {
	db := newHandlerTestDB(t)
	r := newHandlerRouter(t, db)
	// page_size over the 200 cap + page<=0 both clamp to defaults.
	w := doReq(t, r, "GET", "/api/v1/perf/aggregations?group_by=stock&page=0&page_size=9999", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Data struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Page != 1 || resp.Data.PageSize != 20 {
		t.Errorf("page=%d page_size=%d, want 1/20 (clamped)", resp.Data.Page, resp.Data.PageSize)
	}
}

// =============================================================================
// helpers: parseDate / defaultStr / atoiDefault
// =============================================================================

func TestParseDate(t *testing.T) {
	if _, err := parseDate("2026-05-26"); err != nil {
		t.Errorf("valid date errored: %v", err)
	}
	if _, err := parseDate("not-a-date"); err == nil {
		t.Error("invalid date did not error")
	}
}

func TestDefaultStr(t *testing.T) {
	if got := defaultStr("", "fallback"); got != "fallback" {
		t.Errorf("empty → %q, want fallback", got)
	}
	if got := defaultStr("x", "fallback"); got != "x" {
		t.Errorf("non-empty → %q, want x", got)
	}
}

func TestAtoiDefault(t *testing.T) {
	cases := []struct {
		in  string
		def int
		out int
	}{
		{"", 5, 5},
		{"42", 1, 42},
		{"-3", 1, -3}, // negative parses fine; handler clamps it
		{"abc", 7, 7}, // non-numeric → default
	}
	for _, c := range cases {
		if got := atoiDefault(c.in, c.def); got != c.out {
			t.Errorf("atoiDefault(%q,%d) = %d, want %d", c.in, c.def, got, c.out)
		}
	}
}

// =============================================================================
// groupKeysFor — pure grouping logic, all branches (C2a coverage)
// =============================================================================

func TestGroupKeysFor(t *testing.T) {
	h := &Handler{}
	tagsByRec := map[uint64][]string{
		1: {"morning", "breakout"},
		2: {"", ""}, // all-empty tags → UNGROUPED
		3: {},       // no tags → UNGROUPED
	}

	cases := []struct {
		name     string
		groupBy  string
		recID    uint64
		strategy string
		stock    string
		want     []string
	}{
		{"strategy", "strategy", 0, "ma_breakout", "", []string{"ma_breakout"}},
		{"strategy empty → UNGROUPED", "strategy", 0, "", "", []string{"UNGROUPED"}},
		{"stock", "stock", 0, "", "600519", []string{"600519"}},
		{"stock empty → UNGROUPED", "stock", 0, "", "", []string{"UNGROUPED"}},
		{"session_tag multi", "session_tag", 1, "", "", []string{"morning", "breakout"}},
		{"session_tag all-empty → UNGROUPED", "session_tag", 2, "", "", []string{"UNGROUPED"}},
		{"session_tag none → UNGROUPED", "session_tag", 3, "", "", []string{"UNGROUPED"}},
		{"unknown groupBy → UNGROUPED", "banana", 0, "x", "y", []string{"UNGROUPED"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := h.groupKeysFor(c.groupBy, c.recID, c.strategy, c.stock, tagsByRec)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("got %v, want %v", got, c.want)
					break
				}
			}
		})
	}
}

// =============================================================================
// ComputeRange error paths (C2a coverage buffer)
// =============================================================================

func TestComputeRange_NilDB(t *testing.T) {
	svc := NewService(Options{Logger: zap.NewNop()}) // no DB
	_, _, err := svc.ComputeRange(context.Background(),
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("ComputeRange with nil DB should error (ErrNotReady)")
	}
}

func TestComputeRange_ToBeforeFrom(t *testing.T) {
	db := newHandlerTestDB(t)
	svc := NewService(Options{DB: db, Logger: zap.NewNop()})
	_, _, err := svc.ComputeRange(context.Background(),
		time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) // to < from
	if err == nil {
		t.Fatal("ComputeRange with to<from should error")
	}
}

func TestComputeRange_EmptyOK(t *testing.T) {
	db := newHandlerTestDB(t)
	svc := NewService(Options{DB: db, Calendar: newCalendarForTests(t), Logger: zap.NewNop()})
	// Range with no recs → processed=0, errs=0, nil.
	proc, errs, err := svc.ComputeRange(context.Background(),
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC))
	if err != nil || proc != 0 || errs != 0 {
		t.Errorf("empty range → proc=%d errs=%d err=%v, want 0/0/nil", proc, errs, err)
	}
}
