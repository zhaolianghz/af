// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package executor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/model"
)

func TestDashboardStats(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	ctx := context.Background()
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// 2 recs today, 1 rec last week.
	require.NoError(t, db.Create(&model.Recommendation{StockCode: "600519.SH", Date: today}).Error)
	require.NoError(t, db.Create(&model.Recommendation{StockCode: "000858.SZ", Date: today}).Error)
	require.NoError(t, db.Create(&model.Recommendation{StockCode: "601318.SH", Date: today.AddDate(0, 0, -10)}).Error)

	// Runs in the 7-day window: 2 success, 1 failed. One old failed run
	// (outside window for rate, but recent-errors is window-agnostic).
	fin := now
	mk := func(status string, age time.Duration, errMsg string) {
		r := &model.Run{StrategyID: 1, Status: status, Error: errMsg, FinishedAt: &fin}
		require.NoError(t, db.Create(r).Error)
		// backdate created_at
		require.NoError(t, db.Model(r).Update("created_at", now.Add(-age)).Error)
	}
	mk(model.RunStatusSuccess, time.Hour, "")
	mk(model.RunStatusSuccess, 2*time.Hour, "")
	mk(model.RunStatusFailed, 3*time.Hour, "boom")
	mk(model.RunStatusSuccess, 30*24*time.Hour, "") // old, outside 7d window

	sum, err := e.DashboardStats(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, int64(2), sum.TodayRecommendations)
	require.Equal(t, int64(3), sum.TotalRecommendations)
	require.Equal(t, int64(3), sum.RunsTotal) // old run excluded from window
	require.Equal(t, int64(2), sum.RunsSuccess)
	require.Equal(t, int64(1), sum.RunsFailed)
	require.NotNil(t, sum.SuccessRate)
	require.InDelta(t, 2.0/3.0, *sum.SuccessRate, 1e-9)
	require.Len(t, sum.RecentErrors, 1)
	require.Equal(t, "boom", sum.RecentErrors[0].Error)
}

func TestDashboardStats_Empty(t *testing.T) {
	db := newTestDB(t)
	e := minimalExecutor(db)
	sum, err := e.DashboardStats(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(0), sum.TodayRecommendations)
	require.Equal(t, int64(0), sum.RunsTotal)
	require.Nil(t, sum.SuccessRate) // no runs => undefined, not 0
	require.Empty(t, sum.RecentErrors)
}
