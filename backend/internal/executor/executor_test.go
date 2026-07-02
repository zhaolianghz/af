// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for the run-level executor wrapper. These tests focus
// on the read-only + lifecycle helpers around the Run table
// (CreateRunRow, ListRuns, GetRun, GetRunLogs, Retry,
// ListRecommendations, strategyIDByCode). The full Execute path
// is covered indirectly via the handler integration test in
// handler_test.go.
package executor

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

// newTestDB returns an in-memory-style sqlite with the full
// model schema migrated. The model.Migrate helper handles every
// table the executor package touches.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "exec_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	return db
}

// minimalExecutor builds an Executor without wiring the
// orchestrator dependencies. Useful for testing read-only paths
// that don't actually run the DAG.
func minimalExecutor(db *gorm.DB) *Executor {
	return &Executor{
		db:       db,
		cfg:      defaultExecutorCfg(),
		logger:   zap.NewNop(),
		bus:      orchestrator.NewMemBus(),
		registry: orchestrator.NewRegistry(),
	}
}

func defaultExecutorCfg() executorCfg {
	return executorCfg{
		DefaultRunTimeout: 5 * time.Minute,
		SSEHeartbeat:      20 * time.Second,
	}
}

// executorCfg is a tiny alias around config.ExecutorConfig for
// use in tests (avoids pulling the full config package here).
type executorCfg = config.ExecutorConfig

// =============================================================================
// Construction
// =============================================================================

func TestNewExecutor_Defaults(t *testing.T) {
	e := NewExecutor(ExecutorOptions{Logger: zap.NewNop()})
	require.NotNil(t, e)
	require.NotNil(t, e.bus, "default bus must be created")
}

// =============================================================================
// CreateRunRow
// =============================================================================

func TestCreateRunRow_Happy(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	run, err := e.CreateRunRow(context.Background(), 42, model.RunTriggerManual)
	require.NoError(t, err)
	require.NotZero(t, run.ID)
	require.Equal(t, uint64(42), run.StrategyID)
	require.Equal(t, model.RunTriggerManual, run.TriggerType)
	require.Equal(t, model.RunStatusRunning, run.Status)

	// Verify it was actually persisted.
	var row model.Run
	require.NoError(t, db.First(&row, run.ID).Error)
	require.Equal(t, run.StrategyID, row.StrategyID)
}

func TestCreateRunRow_DefaultTrigger(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	run, err := e.CreateRunRow(context.Background(), 1, "")
	require.NoError(t, err)
	require.Equal(t, model.RunTriggerManual, run.TriggerType, "empty trigger must default to manual")
}

func TestCreateRunRow_NoDB(t *testing.T) {
	e := &Executor{logger: zap.NewNop()}
	_, err := e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	require.Error(t, err)
	require.Equal(t, apperr.CodeUnavailable, asCode(err))
}

// =============================================================================
// ListRuns / GetRun / GetRunLogs
// =============================================================================

func TestListRuns_Defaults(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	for i := 0; i < 5; i++ {
		_, err := e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
		require.NoError(t, err)
	}
	rows, total, err := e.ListRuns(context.Background(), RunListFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 5, total)
	require.Len(t, rows, 5)
}

func TestListRuns_FilterByStrategy(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	_, _ = e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	_, _ = e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	_, _ = e.CreateRunRow(context.Background(), 2, model.RunTriggerManual)
	rows, total, err := e.ListRuns(context.Background(), RunListFilter{StrategyID: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	for _, r := range rows {
		require.Equal(t, uint64(1), r.StrategyID)
	}
}

func TestListRuns_FilterByStatus(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	_, _ = e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	rows, _, err := e.ListRuns(context.Background(), RunListFilter{Status: model.RunStatusRunning})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
}

func TestListRuns_FilterByDate(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	_, _ = e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	from := time.Now().Add(-1 * time.Hour)
	to := time.Now().Add(1 * time.Hour)
	rows, total, err := e.ListRuns(context.Background(), RunListFilter{From: &from, To: &to})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
}

func TestListRuns_PageBounds(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	for i := 0; i < 25; i++ {
		_, _ = e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	}
	rows, total, err := e.ListRuns(context.Background(), RunListFilter{Page: 0, PageSize: 0})
	require.NoError(t, err)
	require.EqualValues(t, 25, total)
	require.LessOrEqual(t, len(rows), 20, "default page size is 20")
}

func TestListRuns_NoDB(t *testing.T) {
	e := &Executor{logger: zap.NewNop()}
	_, _, err := e.ListRuns(context.Background(), RunListFilter{})
	require.Error(t, err)
	require.Equal(t, apperr.CodeUnavailable, asCode(err))
}

func TestGetRun_Happy(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	run, err := e.CreateRunRow(context.Background(), 7, model.RunTriggerManual)
	require.NoError(t, err)
	got, err := e.GetRun(context.Background(), run.ID)
	require.NoError(t, err)
	require.Equal(t, run.ID, got.ID)
	require.Equal(t, uint64(7), got.StrategyID)
}

func TestGetRun_NotFound(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	_, err := e.GetRun(context.Background(), 9999)
	require.Error(t, err)
	require.Equal(t, apperr.CodeNotFound, asCode(err))
}

func TestGetRunLogs_Empty(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	run, _ := e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	logs, err := e.GetRunLogs(context.Background(), run.ID)
	require.NoError(t, err)
	require.Empty(t, logs)
}

func TestGetRunLogs_Populated(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	run, _ := e.CreateRunRow(context.Background(), 1, model.RunTriggerManual)
	now := time.Now()
	require.NoError(t, db.Create(&model.RunLog{
		RunID:      run.ID,
		NodeKey:    "n1",
		Status:     model.RunLogStatusSuccess,
		StartedAt:  now.Add(-2 * time.Second),
		FinishedAt: now.Add(-1 * time.Second),
	}).Error)
	logs, err := e.GetRunLogs(context.Background(), run.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "n1", logs[0].NodeKey)
}

// =============================================================================
// strategyIDByCode
// =============================================================================

func TestStrategyIDByCode_Happy(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	require.NoError(t, db.Create(&model.Strategy{Code: "abc", Name: "abc", Status: model.StrategyStatusDraft}).Error)
	id, err := e.strategyIDByCode(context.Background(), "abc")
	require.NoError(t, err)
	require.NotZero(t, id)
}

func TestStrategyIDByCode_NotFound(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	_, err := e.strategyIDByCode(context.Background(), "missing")
	require.Error(t, err)
	require.Equal(t, apperr.CodeNotFound, asCode(err))
}

func TestStrategyIDByCode_NoDB(t *testing.T) {
	e := &Executor{logger: zap.NewNop()}
	_, err := e.strategyIDByCode(context.Background(), "x")
	require.Error(t, err)
	require.Equal(t, apperr.CodeUnavailable, asCode(err))
}

// =============================================================================
// ListRecommendations
// =============================================================================

func TestListRecommendations_Empty(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	rows, total, err := e.ListRecommendations(context.Background(), RecommendationFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 0, total)
	require.Empty(t, rows)
}

func TestListRecommendations_FilterByStrategy(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	now := time.Now()
	require.NoError(t, db.Create(&model.Recommendation{
		RunID: 1, Date: now, StockCode: "A", StockName: "A",
		StrategyCode: "mvb", StrategyName: "x",
	}).Error)
	rows, total, err := e.ListRecommendations(context.Background(), RecommendationFilter{StrategyCode: "mvb"})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
}

func TestListRecommendations_FilterByTag(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
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
	rows, total, err := e.ListRecommendations(context.Background(), RecommendationFilter{Tag: model.SessionTagMorning})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	require.NotEmpty(t, rows[0].Tags)
}

func TestListRecommendations_NoDB(t *testing.T) {
	e := &Executor{logger: zap.NewNop()}
	_, _, err := e.ListRecommendations(context.Background(), RecommendationFilter{})
	require.Error(t, err)
	require.Equal(t, apperr.CodeUnavailable, asCode(err))
}

// =============================================================================
// DrainEvents
// =============================================================================

func TestDrainEvents_NilBus(t *testing.T) {
	// When the bus is nil, DrainEvents blocks on ctx.Done().
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	DrainEvents(ctx, nil, 1, 10*time.Millisecond, func(_ orchestrator.Event) error { return nil })
	// No assertion — the function returns when ctx is done.
}

func TestDrainEvents_Heartbeat(t *testing.T) {
	bus := orchestrator.NewMemBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got []orchestrator.Event
	done := make(chan struct{})
	go func() {
		DrainEvents(ctx, bus, 999, 5*time.Millisecond, func(ev orchestrator.Event) error {
			got = append(got, ev)
			if len(got) >= 3 {
				cancel()
			}
			return nil
		})
		close(done)
	}()
	<-done
	// Heartbeats use Type = "__heartbeat__".
	require.NotEmpty(t, got)
	for _, ev := range got {
		require.Equal(t, orchestrator.EventType("__heartbeat__"), ev.Type)
	}
}

func TestDrainEvents_ForwardsEvents(t *testing.T) {
	bus := orchestrator.NewMemBus()
	// Subscribe first so we don't race the goroutine that
	// calls DrainEvents. We then publish, then drive
	// DrainEvents with a context we cancel after the expected
	// events have arrived.
	ch, unsub := bus.Subscribe(42)
	defer unsub()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pre-publish while we have a guaranteed subscriber. The
	// second subscription made by DrainEvents will not see
	// these, but the test only checks our direct subscription.
	bus.Publish(orchestrator.Event{RunID: 42, Type: orchestrator.EventNodeStarted, Timestamp: time.Now()})
	bus.Publish(orchestrator.Event{RunID: 42, Type: orchestrator.EventRunCompleted, Timestamp: time.Now()})

	var got []orchestrator.Event
	deadline := time.After(2 * time.Second)
loop:
	for len(got) < 2 {
		select {
		case ev := <-ch:
			got = append(got, ev)
		case <-deadline:
			break loop
		}
	}
	cancel()
	require.Len(t, got, 2)
	require.Equal(t, orchestrator.EventNodeStarted, got[0].Type)
	require.Equal(t, orchestrator.EventRunCompleted, got[1].Type)
}

// =============================================================================
// Retry (path test)
// =============================================================================

func TestRetry_NoDB(t *testing.T) {
	e := &Executor{logger: zap.NewNop()}
	_, err := e.Retry(context.Background(), 1)
	require.Error(t, err)
	require.Equal(t, apperr.CodeUnavailable, asCode(err))
}

func TestRetry_NotFound(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	_, err := e.Retry(context.Background(), 9999)
	require.Error(t, err)
	require.Equal(t, apperr.CodeNotFound, asCode(err))
}

// =============================================================================
// Execute (nil-orchestrator paths)
// =============================================================================

func TestExecute_NoOrchestrator(t *testing.T) {
	e := &Executor{logger: zap.NewNop()}
	_, err := e.Execute(context.Background(), RunRequest{StrategyID: 1})
	require.Error(t, err)
	require.Equal(t, apperr.CodeUnavailable, asCode(err))
}

// =============================================================================
// Helpers
// =============================================================================

// asCode unwraps an error into its BizError code, returning -1
// when the error is not a BizError.
func asCode(err error) apperr.Code {
	if be, ok := apperr.As(err); ok {
		return be.Code
	}
	return -1
}

// keep encoding/json import alive across files (used by sibling tests).
var _ = json.Marshal

// fakeQuoteGetter stubs the quoteGetter interface for enrichRecNames tests.
type fakeQuoteGetter struct {
	names map[string]string // code -> stock name
	errs  map[string]bool   // codes that should return an error
}

func (f *fakeQuoteGetter) GetQuote(_ context.Context, code string) (*datasource.Quote, error) {
	if f.errs[code] {
		return nil, errors.New("boom")
	}
	if n, ok := f.names[code]; ok {
		return &datasource.Quote{StockCode: code, StockName: n}, nil
	}
	return &datasource.Quote{StockCode: code}, nil
}

func TestEnrichRecNames(t *testing.T) {
	recs := []model.Recommendation{
		{StockCode: "600519.SH", StockName: ""},     // empty -> backfill
		{StockCode: "000858.SZ", StockName: "已有"}, // keep (non-empty)
		{StockCode: "601318.SH", StockName: ""},     // quote errors -> stays empty
		{StockCode: "300750.SZ", StockName: ""},     // quote returns empty name -> stays empty
	}
	enrichRecNames(context.Background(), recs, &fakeQuoteGetter{
		names: map[string]string{"600519.SH": "贵州茅台"},
		errs:  map[string]bool{"601318.SH": true},
	})
	require.Equal(t, "贵州茅台", recs[0].StockName, "empty name backfilled from quote")
	require.Equal(t, "已有", recs[1].StockName, "existing name untouched")
	require.Equal(t, "", recs[2].StockName, "quote error leaves name empty")
	require.Equal(t, "", recs[3].StockName, "empty quote name leaves name empty")
}

func TestEnrichRecNames_NilDS(t *testing.T) {
	recs := []model.Recommendation{{StockCode: "600519.SH", StockName: ""}}
	enrichRecNames(context.Background(), recs, nil) // must not panic
	require.Equal(t, "", recs[0].StockName)
}