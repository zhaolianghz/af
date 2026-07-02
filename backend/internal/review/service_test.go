// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package review

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/model"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "rev.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	return db
}

// echoLLM returns a deterministic summary so we can assert it was used.
type echoLLM struct{}

func (echoLLM) Name() string { return "echo" }
func (echoLLM) Summarize(_ context.Context, _ string, user string) (string, error) {
	return "SUMMARY:" + user[:0] + "ok", nil
}

func f64(v float64) *float64 { return &v }

func TestReview_GenerateComputesMetricsAndSummarizes(t *testing.T) {
	db := newDB(t)
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)

	// 3 recs today; 2 have T+5 (one win, one loss), 1 has none.
	mk := func(code string, t5 *float64) {
		r := &model.Recommendation{StockCode: code, Date: day}
		require.NoError(t, db.Create(r).Error)
		if t5 != nil {
			require.NoError(t, db.Create(&model.PerformanceSnapshot{
				RecommendationID: r.ID, SnapshotDate: day, CalculatedAt: day, T5Return: t5,
			}).Error)
		}
	}
	mk("600519.SH", f64(0.05))  // win
	mk("000858.SZ", f64(-0.02)) // loss
	mk("601318.SH", nil)        // no T+5 yet

	svc := NewService(db, echoLLM{})
	rep, err := svc.Generate(context.Background(), model.ReviewKindDaily, day, day.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Equal(t, 3, rep.RecommendationCount)
	require.NotNil(t, rep.WinRateT5)
	require.InDelta(t, 0.5, *rep.WinRateT5, 1e-9) // 1 win of 2 known
	require.NotNil(t, rep.AvgT5Return)
	require.InDelta(t, 0.015, *rep.AvgT5Return, 1e-9) // (0.05-0.02)/2
	require.Equal(t, "echo", rep.LLM)
	require.Contains(t, rep.Summary, "SUMMARY:")

	// Persisted + listable.
	got, err := svc.List(context.Background(), "", 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, model.ReviewKindDaily, got[0].Kind)
}

func TestReview_NoLLMFallsBackToDataBlock(t *testing.T) {
	db := newDB(t)
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&model.Recommendation{StockCode: "600519.SH", Date: day}).Error)

	svc := NewService(db, nil) // no LLM
	rep, err := svc.Generate(context.Background(), model.ReviewKindDaily, day, day.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Equal(t, "none", rep.LLM)
	require.Contains(t, rep.Summary, "推荐数: 1")
	require.Nil(t, rep.WinRateT5) // no T+5 data
}

// errLLM always fails; emptyLLM returns blank. Both must fall back to
// the structured data block so a report always has a body.
type errLLM struct{}

func (errLLM) Name() string { return "err" }
func (errLLM) Summarize(_ context.Context, _, _ string) (string, error) {
	return "", context.DeadlineExceeded
}

type emptyLLM struct{}

func (emptyLLM) Name() string                                             { return "empty" }
func (emptyLLM) Summarize(_ context.Context, _, _ string) (string, error) { return "   ", nil }

func TestReview_LLMErrorFallsBackToDataBlock(t *testing.T) {
	db := newDB(t)
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&model.Recommendation{StockCode: "600519.SH", Date: day}).Error)
	svc := NewService(db, errLLM{})
	rep, err := svc.Generate(context.Background(), model.ReviewKindDaily, day, day.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Equal(t, "none", rep.LLM, "LLM error → llm marked none")
	require.Contains(t, rep.Summary, "推荐数: 1", "body falls back to data block")
}

func TestReview_LLMEmptyFallsBackToDataBlock(t *testing.T) {
	db := newDB(t)
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&model.Recommendation{StockCode: "600519.SH", Date: day}).Error)
	svc := NewService(db, emptyLLM{})
	rep, err := svc.Generate(context.Background(), model.ReviewKindDaily, day, day.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Equal(t, "none", rep.LLM, "blank LLM output → llm marked none")
	require.Contains(t, rep.Summary, "推荐数: 1")
}

func TestReview_InvalidPeriod(t *testing.T) {
	svc := NewService(newDB(t), nil)
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	// period_end == period_start (not after) → invalid.
	_, err := svc.Generate(context.Background(), model.ReviewKindDaily, day, day)
	require.Error(t, err)
	// period_end before start → invalid.
	_, err = svc.Generate(context.Background(), model.ReviewKindDaily, day, day.AddDate(0, 0, -1))
	require.Error(t, err)
}

func TestReview_EmptyWindow(t *testing.T) {
	// No recs in the window → report with count 0, no win rate, still persists.
	svc := NewService(newDB(t), nil)
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	rep, err := svc.Generate(context.Background(), model.ReviewKindDaily, day, day.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Equal(t, 0, rep.RecommendationCount)
	require.Nil(t, rep.WinRateT5)
	require.Nil(t, rep.AvgT5Return)
	require.Contains(t, rep.Summary, "推荐数: 0")
	require.NotZero(t, rep.ID, "empty report still persisted")
}

func TestReview_LatestSnapshotPerRec(t *testing.T) {
	// Two snapshots for one rec on different dates; the LATEST (by
	// snapshot_date DESC) T5 must drive the metric, not the older one.
	db := newDB(t)
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	r := &model.Recommendation{StockCode: "600519.SH", Date: day}
	require.NoError(t, db.Create(r).Error)
	require.NoError(t, db.Create(&model.PerformanceSnapshot{
		RecommendationID: r.ID, SnapshotDate: day, CalculatedAt: day, T5Return: f64(-0.10),
	}).Error)
	require.NoError(t, db.Create(&model.PerformanceSnapshot{
		RecommendationID: r.ID, SnapshotDate: day.AddDate(0, 0, 5), CalculatedAt: day, T5Return: f64(0.08),
	}).Error)

	svc := NewService(db, nil)
	rep, err := svc.Generate(context.Background(), model.ReviewKindDaily, day, day.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.NotNil(t, rep.AvgT5Return)
	require.InDelta(t, 0.08, *rep.AvgT5Return, 1e-9, "latest snapshot wins")
	require.InDelta(t, 1.0, *rep.WinRateT5, 1e-9) // 0.08 > 0 → win
}

func TestReview_ListKindFilterAndCap(t *testing.T) {
	db := newDB(t)
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	svc := NewService(db, nil)
	_, err := svc.Generate(context.Background(), model.ReviewKindDaily, day, day.AddDate(0, 0, 1))
	require.NoError(t, err)
	_, err = svc.GenerateWeekly(context.Background(), day.AddDate(0, 0, 8))
	require.NoError(t, err)

	all, err := svc.List(context.Background(), "", 0) // 0 → default cap
	require.NoError(t, err)
	require.Len(t, all, 2)
	// newest first (id DESC).
	require.Equal(t, model.ReviewKindWeekly, all[0].Kind)

	daily, err := svc.List(context.Background(), model.ReviewKindDaily, 10)
	require.NoError(t, err)
	require.Len(t, daily, 1)
	require.Equal(t, model.ReviewKindDaily, daily[0].Kind)
}

func TestReview_GetAndNotFound(t *testing.T) {
	db := newDB(t)
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	svc := NewService(db, nil)
	rep, err := svc.Generate(context.Background(), model.ReviewKindDaily, day, day.AddDate(0, 0, 1))
	require.NoError(t, err)

	got, err := svc.Get(context.Background(), rep.ID)
	require.NoError(t, err)
	require.Equal(t, rep.ID, got.ID)

	_, err = svc.Get(context.Background(), 999999)
	require.Error(t, err)
}

func TestReview_GenerateDailyWindow(t *testing.T) {
	db := newDB(t)
	now := time.Date(2026, 6, 12, 15, 30, 0, 0, time.UTC)
	dayStart := now.UTC().Truncate(24 * time.Hour)
	// A rec stamped today is in [today, tomorrow); one yesterday is not.
	require.NoError(t, db.Create(&model.Recommendation{StockCode: "600519.SH", Date: dayStart}).Error)
	require.NoError(t, db.Create(&model.Recommendation{StockCode: "000858.SZ", Date: dayStart.AddDate(0, 0, -1)}).Error)
	svc := NewService(db, nil)
	rep, err := svc.GenerateDaily(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, model.ReviewKindDaily, rep.Kind)
	require.Equal(t, 1, rep.RecommendationCount, "only today's rec counts")
}

func TestReview_NilDBUnavailable(t *testing.T) {
	svc := NewService(nil, nil)
	ctx := context.Background()
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	_, err := svc.Generate(ctx, model.ReviewKindDaily, day, day.AddDate(0, 0, 1))
	require.Error(t, err)
	_, err = svc.List(ctx, "", 10)
	require.Error(t, err)
	_, err = svc.Get(ctx, 1)
	require.Error(t, err)
}
