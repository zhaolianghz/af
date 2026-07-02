// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package perf

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/calendar"
	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/model"
)

// =============================================================================
// fakeManager — implements datasource.Manager with canned K-line data.
// =============================================================================

type fakeManager struct {
	klines map[string][]datasource.KLine // key: "STOCK|YYYY-MM-DD"
}

func newFakeManager() *fakeManager {
	return &fakeManager{klines: map[string][]datasource.KLine{}}
}

func (f *fakeManager) addKLine(stockCode string, day time.Time, close float64) {
	key := stockCode + "|" + day.Format("2006-01-02")
	f.klines[key] = []datasource.KLine{{
		StockCode: stockCode,
		Period:    "1d",
		Timestamp: day,
		Open:      close,
		High:      close,
		Low:       close,
		Close:     close,
		Volume:    1000,
	}}
}

func (f *fakeManager) GetQuote(ctx context.Context, stockCode string) (*datasource.Quote, error) {
	return nil, errors.New("not used in perf tests")
}

func (f *fakeManager) GetKLine(ctx context.Context, stockCode, period string, start, end time.Time) ([]datasource.KLine, error) {
	var out []datasource.KLine
	// Walk our canned map; this is a tiny in-memory fake so a linear
	// scan is fine.
	for k, v := range f.klines {
		// Match prefix "<stock>|"
		prefix := stockCode + "|"
		if len(k) < len(prefix) || k[:len(prefix)] != prefix {
			continue
		}
		for _, line := range v {
			if !line.Timestamp.Before(start) && line.Timestamp.Before(end) {
				out = append(out, line)
			}
		}
	}
	return out, nil
}

func (f *fakeManager) GetFundamental(ctx context.Context, stockCode string) (*datasource.Fundamental, error) {
	return nil, errors.New("not used in perf tests")
}

func (f *fakeManager) GetNews(ctx context.Context, stockCode string, limit int) ([]datasource.News, error) {
	return nil, errors.New("not used in perf tests")
}

func (f *fakeManager) ListSources() []string { return []string{"fake"} }
func (f *fakeManager) BreakerSnapshots() []datasource.BreakerSnapshot {
	return nil
}
func (f *fakeManager) HealthCheck(ctx context.Context) error { return nil }

// =============================================================================
// Helpers
// =============================================================================

func newCalendarForTests(t *testing.T) *calendar.Service {
	t.Helper()
	svc, err := calendar.NewService(config.CalendarConfig{
		Timezone:       "Asia/Shanghai",
		MorningStart:   "09:30",
		MorningEnd:     "11:30",
		AfternoonStart: "13:00",
		AfternoonEnd:   "15:00",
		ReviewStart:    "15:00",
		ReviewEnd:      "17:00",
	}, nil) // nil DB → weekend rule
	if err != nil {
		t.Fatalf("calendar.NewService: %v", err)
	}
	return svc
}

// =============================================================================
// ComputeOne tests
// =============================================================================

func TestComputeOne_HappyPath(t *testing.T) {
	// Rec date: 2026-05-26 (Tue).
	// T+1: 2026-05-27 (Wed)
	// T+2: 2026-05-28 (Thu)
	// T+3: 2026-05-29 (Fri)
	// T+4: 2026-06-01 (Mon)
	// T+5: 2026-06-02 (Tue)
	recDate := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	t5 := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

	mgr := newFakeManager()
	mgr.addKLine("600519", t1, 110.0)
	mgr.addKLine("600519", t3, 108.0)
	mgr.addKLine("600519", t5, 115.0)

	svc := NewService(Options{
		Datasource: mgr,
		Calendar:   newCalendarForTests(t),
		Logger:     zap.NewNop(),
	})

	rec := &model.Recommendation{
		BaseEntity:     model.BaseEntity{ID: 1},
		Date:           recDate,
		StockCode:      "600519",
		EntryPriceLow:  100.0,
		EntryPriceHigh: 100.0,
	}
	snap, err := svc.ComputeOne(context.Background(), rec)
	if err != nil {
		t.Fatalf("ComputeOne: %v", err)
	}
	// entry = 100.0
	wantT1 := 0.10  // (110-100)/100
	wantT3 := 0.08  // (108-100)/100
	wantT5 := 0.15  // (115-100)/100
	assertApproxPtr(t, "T1Return", snap.T1Return, wantT1, 1e-9)
	assertApproxPtr(t, "T3Return", snap.T3Return, wantT3, 1e-9)
	assertApproxPtr(t, "T5Return", snap.T5Return, wantT5, 1e-9)
	// CumulativeReturn is the last-available horizon = T+5.
	// Use approx compare — floating-point subtraction of
	// (115/100 - 1) is not exactly 0.15.
	assertApproxPtr(t, "CumulativeReturn", snap.CumulativeReturn, wantT5, 1e-9)
	// Path [100, 110, 108, 115]. Peak after 100 is 110 (i=1), then
	// 108 dips below → dd = (110-108)/110. Then 115 > 110, peak
	// becomes 115, no further dip. mdd = 2/110.
	assertApproxPtr(t, "MaxDrawdown", snap.MaxDrawdown, 2.0/110.0, 1e-9)
	// Win rate: any T+N positive → 1.0
	assertApproxPtr(t, "WinRate", snap.WinRate, 1.0, 1e-9)
}

func TestComputeOne_PartialData(t *testing.T) {
	// Only T+1 has data; T+3 + T+5 are missing (suspended).
	recDate := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	mgr := newFakeManager()
	mgr.addKLine("600519", t1, 95.0)

	svc := NewService(Options{
		Datasource: mgr,
		Calendar:   newCalendarForTests(t),
		Logger:     zap.NewNop(),
	})
	rec := &model.Recommendation{
		BaseEntity:     model.BaseEntity{ID: 2},
		Date:           recDate,
		StockCode:      "600519",
		EntryPriceLow:  100.0,
		EntryPriceHigh: 100.0,
	}
	snap, err := svc.ComputeOne(context.Background(), rec)
	if err != nil {
		t.Fatalf("ComputeOne: %v", err)
	}
	assertApproxPtr(t, "T1Return", snap.T1Return, -0.05, 1e-9)
	if snap.T3Return != nil {
		t.Fatalf("T3Return should be nil, got %v", snap.T3Return)
	}
	if snap.T5Return != nil {
		t.Fatalf("T5Return should be nil, got %v", snap.T5Return)
	}
	// CumulativeReturn falls back to T+1.
	assertApproxPtr(t, "CumulativeReturn", snap.CumulativeReturn, -0.05, 1e-9)
	// WinRate: T+1 negative, T+3/T+5 unknown → "no positive but partial unknown" → nil
	if snap.WinRate != nil {
		t.Fatalf("WinRate want nil (ambiguous), got %v", *snap.WinRate)
	}
	// MaxDrawdown: path [100, 95], peak=100, dd=5/100=0.05
	assertApproxPtr(t, "MaxDrawdown", snap.MaxDrawdown, 0.05, 1e-9)
}

func TestComputeOne_NoDataAtAll(t *testing.T) {
	recDate := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	mgr := newFakeManager() // empty
	svc := NewService(Options{
		Datasource: mgr,
		Calendar:   newCalendarForTests(t),
		Logger:     zap.NewNop(),
	})
	rec := &model.Recommendation{
		BaseEntity:     model.BaseEntity{ID: 3},
		Date:           recDate,
		StockCode:      "600519",
		EntryPriceLow:  100.0,
		EntryPriceHigh: 100.0,
	}
	snap, err := svc.ComputeOne(context.Background(), rec)
	if err != nil {
		t.Fatalf("ComputeOne: %v", err)
	}
	if snap.T1Return != nil || snap.T3Return != nil || snap.T5Return != nil {
		t.Fatalf("all T+N returns should be nil, got %+v", snap)
	}
	if snap.CumulativeReturn != nil {
		t.Fatalf("CumulativeReturn should be nil, got %v", snap.CumulativeReturn)
	}
	if snap.WinRate != nil {
		t.Fatalf("WinRate should be nil, got %v", snap.WinRate)
	}
	// MaxDrawdown: only entry in path; need >=2 points → nil
	if snap.MaxDrawdown != nil {
		t.Fatalf("MaxDrawdown should be nil, got %v", snap.MaxDrawdown)
	}
}

func TestComputeOne_InvalidRec(t *testing.T) {
	svc := NewService(Options{Logger: zap.NewNop()})
	if _, err := svc.ComputeOne(context.Background(), nil); !errors.Is(err, ErrBadRecommendation) {
		t.Fatalf("nil rec: want ErrBadRecommendation, got %v", err)
	}
	rec := &model.Recommendation{StockCode: "x"} // ID == 0
	if _, err := svc.ComputeOne(context.Background(), rec); !errors.Is(err, ErrBadRecommendation) {
		t.Fatalf("zero-id rec: want ErrBadRecommendation, got %v", err)
	}
	// No datasource + calendar wired → ErrNotReady
	rec2 := &model.Recommendation{BaseEntity: model.BaseEntity{ID: 1}, EntryPriceLow: 1, EntryPriceHigh: 1}
	if _, err := svc.ComputeOne(context.Background(), rec2); !errors.Is(err, ErrNotReady) {
		t.Fatalf("no-ds: want ErrNotReady, got %v", err)
	}
}

// =============================================================================
// Save idempotency (uses sqlite in-memory)
// =============================================================================

func TestSave_Idempotent(t *testing.T) {
	db := openSQLite(t)
	if err := db.AutoMigrate(&model.Recommendation{}, &model.PerformanceSnapshot{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	rec := model.Recommendation{
		BaseEntity:     model.BaseEntity{ID: 100},
		RunID:          1,
		Date:           time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC),
		StockCode:      "600519",
		EntryPriceLow:  100.0,
		EntryPriceHigh: 100.0,
		StrategyCode:   "morning_breakout",
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("create rec: %v", err)
	}

	svc := NewService(Options{DB: db, Logger: zap.NewNop()})
	snap1 := &model.PerformanceSnapshot{
		RecommendationID: rec.ID,
		SnapshotDate:     time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		T1Return:         ptrF(0.05),
		T5Return:         ptrF(0.10),
		CalculatedAt:     time.Now(),
	}
	if err := svc.Save(context.Background(), snap1); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// Second save: same (rec_id, snapshot_date), different fields.
	// Should overwrite, not insert a duplicate.
	snap2 := &model.PerformanceSnapshot{
		RecommendationID: rec.ID,
		SnapshotDate:     time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		T1Return:         ptrF(0.07),
		T3Return:         ptrF(0.08),
		T5Return:         ptrF(0.12),
		CalculatedAt:     time.Now().Add(time.Second),
	}
	if err := svc.Save(context.Background(), snap2); err != nil {
		t.Fatalf("second save: %v", err)
	}
	// Verify: exactly one row, with the second's values.
	var count int64
	if err := db.Model(&model.PerformanceSnapshot{}).Where("recommendation_id = ?", rec.ID).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
	var got model.PerformanceSnapshot
	if err := db.Where("recommendation_id = ?", rec.ID).First(&got).Error; err != nil {
		t.Fatalf("first: %v", err)
	}
	if got.T1Return == nil || *got.T1Return != 0.07 {
		t.Fatalf("T1Return not overwritten: %v", got.T1Return)
	}
	if got.T3Return == nil || *got.T3Return != 0.08 {
		t.Fatalf("T3Return not written: %v", got.T3Return)
	}
	if got.T5Return == nil || *got.T5Return != 0.12 {
		t.Fatalf("T5Return not overwritten: %v", got.T5Return)
	}
}

func TestSave_NilDB(t *testing.T) {
	svc := NewService(Options{Logger: zap.NewNop()})
	if err := svc.Save(context.Background(), &model.PerformanceSnapshot{}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("want ErrNotReady, got %v", err)
	}
}

func TestSave_ValidationErrors(t *testing.T) {
	db := openSQLite(t)
	svc := NewService(Options{DB: db, Logger: zap.NewNop()})
	if err := svc.Save(context.Background(), nil); err == nil {
		t.Fatal("want error on nil snapshot")
	}
	if err := svc.Save(context.Background(), &model.PerformanceSnapshot{}); err == nil {
		t.Fatal("want error on zero rec_id")
	}
}

// =============================================================================
// ComputeRange + idempotent re-run (uses sqlite in-memory)
// =============================================================================

func TestComputeRange_EndToEnd(t *testing.T) {
	db := openSQLite(t)
	if err := db.AutoMigrate(&model.Recommendation{}, &model.PerformanceSnapshot{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	// Seed 3 recs across 3 days with the same stock.
	recs := []model.Recommendation{
		{BaseEntity: model.BaseEntity{ID: 1}, Date: time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC), StockCode: "600519", EntryPriceLow: 100, EntryPriceHigh: 100, StrategyCode: "s1"},
		{BaseEntity: model.BaseEntity{ID: 2}, Date: time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC), StockCode: "600519", EntryPriceLow: 100, EntryPriceHigh: 100, StrategyCode: "s1"},
		{BaseEntity: model.BaseEntity{ID: 3}, Date: time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC), StockCode: "600519", EntryPriceLow: 100, EntryPriceHigh: 100, StrategyCode: "s2"},
	}
	for i := range recs {
		if err := db.Create(&recs[i]).Error; err != nil {
			t.Fatalf("create rec %d: %v", recs[i].ID, err)
		}
	}

	// T+1, T+3, T+5 for each rec; we'll only seed T+1 to keep
	// assertions simple.
	mgr := newFakeManager()
	// Rec 1: 2026-05-26 → T+1 = 2026-05-27
	mgr.addKLine("600519", time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC), 110.0)
	// Rec 2: 2026-05-27 → T+1 = 2026-05-28
	mgr.addKLine("600519", time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC), 90.0)
	// Rec 3: 2026-05-28 → T+1 = 2026-05-29
	mgr.addKLine("600519", time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC), 100.0)

	svc := NewService(Options{
		DB:         db,
		Datasource: mgr,
		Calendar:   newCalendarForTests(t),
		Logger:     zap.NewNop(),
	})

	processed, errs, err := svc.ComputeRange(context.Background(),
		time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("ComputeRange: %v", err)
	}
	if processed != 3 || errs != 0 {
		t.Fatalf("want processed=3 errs=0, got processed=%d errs=%d", processed, errs)
	}
	var n int64
	if err := db.Model(&model.PerformanceSnapshot{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("want 3 snapshots, got %d", n)
	}

	// Re-run: should not duplicate.
	processed2, errs2, err := svc.ComputeRange(context.Background(),
		time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("ComputeRange rerun: %v", err)
	}
	if processed2 != 3 || errs2 != 0 {
		t.Fatalf("rerun: want processed=3 errs=0, got processed=%d errs=%d", processed2, errs2)
	}
	if err := db.Model(&model.PerformanceSnapshot{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("rerun: want 3 snapshots (idempotent), got %d", n)
	}
}

// =============================================================================
// §9.5 Backfill — only computes recs missing a snapshot.
// =============================================================================

func TestBackfill_SkipsExisting(t *testing.T) {
	db := openSQLite(t)
	if err := db.AutoMigrate(&model.Recommendation{}, &model.PerformanceSnapshot{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	// Seed 3 recs in the recent past (so they fall inside the
	// default 7-day lookback window). Rec 1 already has a
	// snapshot; recs 2 + 3 don't.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	recs := []model.Recommendation{
		{BaseEntity: model.BaseEntity{ID: 1}, Date: today.AddDate(0, 0, -3), StockCode: "600519", EntryPriceLow: 100, EntryPriceHigh: 100, StrategyCode: "s1"},
		{BaseEntity: model.BaseEntity{ID: 2}, Date: today.AddDate(0, 0, -2), StockCode: "600519", EntryPriceLow: 100, EntryPriceHigh: 100, StrategyCode: "s1"},
		{BaseEntity: model.BaseEntity{ID: 3}, Date: today.AddDate(0, 0, -1), StockCode: "600519", EntryPriceLow: 100, EntryPriceHigh: 100, StrategyCode: "s2"},
	}
	for i := range recs {
		if err := db.Create(&recs[i]).Error; err != nil {
			t.Fatalf("create rec %d: %v", recs[i].ID, err)
		}
	}
	// Rec 1 already has a snapshot.
	have := model.PerformanceSnapshot{
		RecommendationID: 1,
		SnapshotDate:     recs[0].Date,
		CalculatedAt:     time.Now(),
	}
	if err := db.Create(&have).Error; err != nil {
		t.Fatalf("create existing snap: %v", err)
	}

	// Fake K-line so the compute path can produce t+1 / t+3 / t+5.
	mgr := newFakeManager()
	// We don't need precise dates — ComputeOne will skip the K-line
	// fetch if the manager returns nothing. The point of the test
	// is the dedup logic, not the math.

	svc := NewService(Options{
		DB:         db,
		Datasource: mgr,
		Calendar:   newCalendarForTests(t),
		Logger:     zap.NewNop(),
	})

	processed, errs, err := svc.Backfill(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	// Rec 1 is skipped (already has a snap). Recs 2 + 3 are
	// processed. K-line is empty so the compute step still
	// creates rows (with nil T+Ns), so we expect processed=2.
	if processed != 2 || errs != 0 {
		t.Fatalf("want processed=2 errs=0, got processed=%d errs=%d", processed, errs)
	}
	// Exactly 3 snapshots now (1 pre-existing + 2 just-created).
	var n int64
	if err := db.Model(&model.PerformanceSnapshot{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("want 3 snapshots, got %d", n)
	}

	// Re-run: nothing missing → processed=0.
	processed2, errs2, err := svc.Backfill(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Backfill rerun: %v", err)
	}
	if processed2 != 0 || errs2 != 0 {
		t.Fatalf("rerun: want processed=0 errs=0, got processed=%d errs=%d", processed2, errs2)
	}
}

func TestBackfill_NilDB(t *testing.T) {
	svc := NewService(Options{Logger: zap.NewNop()})
	_, _, err := svc.Backfill(context.Background(), time.Hour)
	if !errors.Is(err, ErrNotReady) {
		t.Fatalf("want ErrNotReady, got %v", err)
	}
}

func TestBackfill_ZeroLookbackDefaults(t *testing.T) {
	// lookback=0 should fall back to 7 days; nil DB still wins.
	svc := NewService(Options{Logger: zap.NewNop()})
	_, _, err := svc.Backfill(context.Background(), 0)
	if !errors.Is(err, ErrNotReady) {
		t.Fatalf("want ErrNotReady, got %v", err)
	}
}

// =============================================================================
// shared helpers
// =============================================================================

func openSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func ptrF(v float64) *float64 { return &v }

func assertApproxPtr(t *testing.T, name string, got *float64, want, tol float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: want %v, got nil", name, want)
	}
	if abs(*got-want) > tol {
		t.Fatalf("%s: want %v, got %v", name, want, *got)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
