// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package perf

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/model"
)

// =============================================================================
// aggregate() — narrow SELECT + nil-when-undefined contract
//
// The aggregation SELECT projects only T1Return / T3Return / T5Return /
// MaxDrawdown (plus recommendation_id for the dedupe walk) into the
// narrow aggregateCol. These tests pin:
//   1. The Scan into aggregateCol works — happy path returns the
//      expected means and win-rate.
//   2. The nil-when-undefined contract holds: a snapshot with nil
//      T+5 produces a row whose AvgT5 / WinRateT5 are JSON-`null`,
//      NOT 0.0. This is the M2 documentation surface.
// =============================================================================

func TestAggregate_HappyPath(t *testing.T) {
	db := openSQLite(t)
	if err := db.AutoMigrate(&model.Recommendation{}, &model.PerformanceSnapshot{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	rec := model.Recommendation{
		BaseEntity:     model.BaseEntity{ID: 1},
		Date:           time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC),
		StockCode:      "600519",
		StrategyCode:   "morning_breakout",
		EntryPriceLow:  100,
		EntryPriceHigh: 100,
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("create rec: %v", err)
	}
	snap := model.PerformanceSnapshot{
		RecommendationID: 1,
		SnapshotDate:     rec.Date,
		T1Return:         ptrF(0.05),
		T3Return:         ptrF(0.10),
		T5Return:         ptrF(0.15),
		MaxDrawdown:      ptrF(0.05),
	}
	if err := db.Create(&snap).Error; err != nil {
		t.Fatalf("create snap: %v", err)
	}

	svc := NewService(Options{DB: db, Logger: zap.NewNop()})
	h := NewHandler(svc, zap.NewNop())

	rows, total, err := h.aggregate(context.Background(),
		AggregationsQuery{GroupBy: "strategy", Page: 1, PageSize: 20},
		time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if total != 1 {
		t.Fatalf("total: want 1, got %d", total)
	}
	if len(rows) != 1 {
		t.Fatalf("rows: want 1, got %d", len(rows))
	}
	r := rows[0]
	if r.Key != "morning_breakout" {
		t.Errorf("key: want morning_breakout, got %s", r.Key)
	}
	if r.Count != 1 {
		t.Errorf("count: want 1, got %d", r.Count)
	}
	if r.AvgT1 == nil || *r.AvgT1 != 0.05 {
		t.Errorf("avg_t1: want 0.05, got %v", r.AvgT1)
	}
	if r.AvgT3 == nil || *r.AvgT3 != 0.10 {
		t.Errorf("avg_t3: want 0.10, got %v", r.AvgT3)
	}
	if r.AvgT5 == nil || *r.AvgT5 != 0.15 {
		t.Errorf("avg_t5: want 0.15, got %v", r.AvgT5)
	}
	if r.WinRateT5 == nil || *r.WinRateT5 != 1.0 {
		t.Errorf("win_rate_t5: want 1.0, got %v", r.WinRateT5)
	}
	if r.AvgDrawdown == nil || *r.AvgDrawdown != 0.05 {
		t.Errorf("avg_max_drawdown: want 0.05, got %v", r.AvgDrawdown)
	}
}

// TestAggregate_NilFields pins the nil-when-undefined contract that
// aggregationRow documents: when the source snapshot has nil T+5
// (e.g. the stock was suspended, so T+5 close is unavailable), the
// group's AvgT5 and WinRateT5 must be nil — not 0.0. This is the
// surface that M2's per-field comments describe.
func TestAggregate_NilFields(t *testing.T) {
	db := openSQLite(t)
	if err := db.AutoMigrate(&model.Recommendation{}, &model.PerformanceSnapshot{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	rec := model.Recommendation{
		BaseEntity:     model.BaseEntity{ID: 1},
		Date:           time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC),
		StockCode:      "600519",
		StrategyCode:   "morning_breakout",
		EntryPriceLow:  100,
		EntryPriceHigh: 100,
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("create rec: %v", err)
	}
	// Snapshot exists but T+3 / T+5 / drawdown are nil. Only T+1
	// is finite.
	snap := model.PerformanceSnapshot{
		RecommendationID: 1,
		SnapshotDate:     rec.Date,
		T1Return:         ptrF(0.05),
		// T3Return, T5Return, MaxDrawdown: nil (suspended / unknown)
	}
	if err := db.Create(&snap).Error; err != nil {
		t.Fatalf("create snap: %v", err)
	}

	svc := NewService(Options{DB: db, Logger: zap.NewNop()})
	h := NewHandler(svc, zap.NewNop())

	rows, total, err := h.aggregate(context.Background(),
		AggregationsQuery{GroupBy: "strategy", Page: 1, PageSize: 20},
		time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("want 1 row, got total=%d len=%d", total, len(rows))
	}
	r := rows[0]
	// T+1 is known → AvgT1 = 0.05.
	if r.AvgT1 == nil || *r.AvgT1 != 0.05 {
		t.Errorf("avg_t1: want 0.05, got %v", r.AvgT1)
	}
	// T+3 unknown for the only rec in the group → AvgT3 = nil.
	if r.AvgT3 != nil {
		t.Errorf("avg_t3: want nil, got %v", *r.AvgT3)
	}
	// T+5 unknown → AvgT5 = nil. NOT 0.0.
	if r.AvgT5 != nil {
		t.Errorf("avg_t5: want nil (T+5 unknown), got %v", *r.AvgT5)
	}
	// WinRateT5 denominator is "recs with known T+5" → nil.
	if r.WinRateT5 != nil {
		t.Errorf("win_rate_t5: want nil (no T+5), got %v", *r.WinRateT5)
	}
	// Drawdown needs >=2 finite closes → nil.
	if r.AvgDrawdown != nil {
		t.Errorf("avg_max_drawdown: want nil, got %v", *r.AvgDrawdown)
	}
}
