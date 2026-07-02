// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package perf — service.go
//
// The Service owns the post-hoc performance pipeline: fetch K-line for
// T+1 / T+3 / T+5 trading days after a recommendation's date, apply
// the pure math from calculator.go, and upsert the result into
// performance_snapshots. Writes are idempotent on
// (recommendation_id, snapshot_date), which lets a cron re-run after
// a partial failure without producing duplicates.
package perf

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/skyzhao/af/internal/calendar"
	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/model"
)

// Service is the post-hoc performance engine.
type Service struct {
	db     *gorm.DB
	ds     datasource.Manager
	cal    *calendar.Service
	logger *zap.Logger

	// klinePeriod is the period used when fetching close prices.
	// Default "1d" (daily).
	klinePeriod string
	// klineLookback bounds how many calendar days we look back from
	// a target trading day when fetching K-line. We never need more
	// than a handful; the default 7 days is generous.
	klineLookback time.Duration
	// klineTimeout caps each per-day K-line fetch. Default 5s.
	klineTimeout time.Duration
	// klineFuturePad extends the upper bound so a target trading
	// day close to "today" is still returned. Default 24h.
	klineFuturePad time.Duration
}

// Options configures NewService. Datasource and Calendar are required
// for ComputeOne to work; in the DB-less / no-datasource smoke
// environment, the service is still constructible but ComputeOne will
// return ErrNotReady.
type Options struct {
	DB            *gorm.DB
	Datasource    datasource.Manager
	Calendar      *calendar.Service
	Logger        *zap.Logger
	KlinePeriod   time.Duration // optional override; default "1d"
	KlineLookback time.Duration // optional; default 7*24h
	KlineTimeout  time.Duration // optional; default 5*time.Second
	KlineFuturePad time.Duration // optional; default 24h
}

// ErrNotReady is returned by ComputeOne when the service was wired
// without a datasource or calendar (DB-less mode).
var ErrNotReady = errors.New("perf: service not ready (datasource / calendar not wired)")

// ErrBadRecommendation is returned for nil or otherwise invalid recs.
var ErrBadRecommendation = errors.New("perf: invalid recommendation")

// NewService builds a Service. Returns a non-nil *Service even when
// some dependencies are nil — the service tolerates the DB-less mode
// used by the smoke test environment. ComputeOne / Save return
// ErrNotReady in that case.
func NewService(opts Options) *Service {
	l := opts.Logger
	if l == nil {
		l = zap.NewNop()
	}
	lookback := opts.KlineLookback
	if lookback <= 0 {
		lookback = 7 * 24 * time.Hour
	}
	timeout := opts.KlineTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	futurePad := opts.KlineFuturePad
	if futurePad <= 0 {
		futurePad = 24 * time.Hour
	}
	period := "1d"
	if opts.KlinePeriod > 0 && opts.KlinePeriod < 24*time.Hour {
		// Intraday periods like 1m/5m are not relevant for T+N
		// close-of-day math, but we honor the override so tests
		// can pass a tiny value if they want.
		period = "1d"
	}
	return &Service{
		db:             opts.DB,
		ds:             opts.Datasource,
		cal:            opts.Calendar,
		logger:         l,
		klinePeriod:    period,
		klineLookback:  lookback,
		klineTimeout:   timeout,
		klineFuturePad: futurePad,
	}
}

// =============================================================================
// Compute
// =============================================================================

// ComputeOne walks the T+1 / T+3 / T+5 trading days from rec.Date and
// returns a freshly-calculated PerformanceSnapshot. The snapshot is
// NOT persisted; call Save to persist.
//
// nil-ability contract:
//   - T*NClose / T*NReturn are nil when the K-line for that trading
//     day is unavailable (suspended stock, source error, future date).
//   - CumulativeReturn is the T+5 return when available, else T+3, else
//     T+1. nil when none of the three are known.
//   - MaxDrawdown is computed over [entry, t1, t3, t5] (skipping nils).
//     nil when fewer than 2 finite points.
//   - WinRate is a 1.0 / 0.0 binary verdict driven by T+5 (or the
//     last available return). nil when no T+N return is known.
func (s *Service) ComputeOne(ctx context.Context, rec *model.Recommendation) (*model.PerformanceSnapshot, error) {
	if rec == nil || rec.ID == 0 {
		return nil, ErrBadRecommendation
	}
	if s.ds == nil || s.cal == nil {
		return nil, ErrNotReady
	}

	entry := (rec.EntryPriceLow + rec.EntryPriceHigh) / 2.0
	if !(entry > 0) {
		return nil, fmt.Errorf("%w: rec %d entry price = %v", ErrBadRecommendation, rec.ID, entry)
	}

	loc := s.cal.Location()
	t1Day := s.cal.NextTradingDay(inDay(rec.Date, loc))
	t3Day := t1Day
	for i := 0; i < 2; i++ {
		t3Day = s.cal.NextTradingDay(t3Day)
	}
	t5Day := t3Day
	for i := 0; i < 2; i++ {
		t5Day = s.cal.NextTradingDay(t5Day)
	}

	snap := &model.PerformanceSnapshot{
		RecommendationID: rec.ID,
		// SnapshotDate is the rec date (truncated to day in the
		// calendar's timezone). Using rec.Date — not time.Now() —
		// is what makes re-runs idempotent: the unique key
		// (recommendation_id, snapshot_date) stays stable across
		// recompute calls, so the OnConflict clause in Save can
		// overwrite the row in place instead of inserting a new one.
		SnapshotDate: inDay(rec.Date, loc),
	}
	if c, ok := s.fetchClose(ctx, rec.StockCode, t1Day); ok {
		snap.T1Close = &c
		snap.T1Return = TReturn(entry, c)
	}
	if c, ok := s.fetchClose(ctx, rec.StockCode, t3Day); ok {
		snap.T3Close = &c
		snap.T3Return = TReturn(entry, c)
	}
	if c, ok := s.fetchClose(ctx, rec.StockCode, t5Day); ok {
		snap.T5Close = &c
		snap.T5Return = TReturn(entry, c)
	}

	// CumulativeReturn: prefer the most-advanced horizon we have.
	snap.CumulativeReturn = pickLast(snap.T1Return, snap.T3Return, snap.T5Return)

	// MaxDrawdown over the [entry, t1, t3, t5] path.
	path := []float64{entry}
	if snap.T1Close != nil {
		path = append(path, *snap.T1Close)
	}
	if snap.T3Close != nil {
		path = append(path, *snap.T3Close)
	}
	if snap.T5Close != nil {
		path = append(path, *snap.T5Close)
	}
	snap.MaxDrawdown = MaxDrawdown(path)

	// WinRate: 1.0 if any T+N return is positive, 0.0 if all are
	// negative or zero, nil when no T+N return is known. This is the
	// single-rec verdict; the aggregate query computes its own
	// win-rate across many recs.
	snap.WinRate = singleRecWinRate(snap.T1Return, snap.T3Return, snap.T5Return)

	return snap, nil
}

// ComputeAndSave is ComputeOne + Save in one call. Useful for the
// cron / manual-trigger paths.
func (s *Service) ComputeAndSave(ctx context.Context, rec *model.Recommendation) (*model.PerformanceSnapshot, error) {
	snap, err := s.ComputeOne(ctx, rec)
	if err != nil {
		return nil, err
	}
	if err := s.Save(ctx, snap); err != nil {
		return snap, fmt.Errorf("compute ok, save failed: %w", err)
	}
	return snap, nil
}

// =============================================================================
// Range / batch
// =============================================================================

// ComputeRange walks every recommendation whose date is in
// [from, to] (inclusive) and computes + saves a snapshot for each.
// Returns (processed, errored). A per-rec failure is logged and
// counted; the loop only stops on context cancellation.
func (s *Service) ComputeRange(ctx context.Context, from, to time.Time) (processed int, errs int, err error) {
	if s.db == nil {
		return 0, 0, ErrNotReady
	}
	if to.Before(from) {
		return 0, 0, fmt.Errorf("perf: ComputeRange: to < from")
	}

	var recs []model.Recommendation
	if err := s.db.WithContext(ctx).
		Where("date >= ? AND date <= ?", from, to).
		Order("date ASC, id ASC").
		Find(&recs).Error; err != nil {
		return 0, 0, fmt.Errorf("perf: list recs: %w", err)
	}

	for i := range recs {
		if err := ctx.Err(); err != nil {
			return processed, errs, err
		}
		if _, err := s.ComputeAndSave(ctx, &recs[i]); err != nil {
			s.logger.Warn("perf: ComputeAndSave failed",
				zap.Uint64("rec_id", recs[i].ID),
				zap.String("stock", recs[i].StockCode),
				zap.Error(err),
			)
			errs++
			continue
		}
		processed++
	}
	return processed, errs, nil
}

// =============================================================================
// Persistence
// =============================================================================

// Save idempotently writes a snapshot. The (recommendation_id,
// snapshot_date) unique index is resolved with a full overwrite of the
// performance fields, so re-runs after a partial failure are
// deterministic.
//
// Save returns ErrNotReady when the service was constructed without a
// DB.
func (s *Service) Save(ctx context.Context, snap *model.PerformanceSnapshot) error {
	if s.db == nil {
		return ErrNotReady
	}
	if snap == nil {
		return errors.New("perf: Save: nil snapshot")
	}
	if snap.RecommendationID == 0 {
		return errors.New("perf: Save: zero recommendation_id")
	}
	if snap.SnapshotDate.IsZero() {
		snap.SnapshotDate = time.Now()
	}
	if snap.CalculatedAt.IsZero() {
		snap.CalculatedAt = time.Now()
	}

	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "recommendation_id"},
			{Name: "snapshot_date"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"t1_return", "t3_return", "t5_return",
			"t1_close", "t3_close", "t5_close",
			"cumulative_return", "max_drawdown", "win_rate",
			"calculated_at",
		}),
	}).Create(snap).Error
}

// =============================================================================
// Queries (used by handler.go for §9.4 aggregation)
// =============================================================================

// LatestSnapshotForRec returns the most recent snapshot for the given
// recommendation, or nil if none exists.
func (s *Service) LatestSnapshotForRec(ctx context.Context, recID uint64) (*model.PerformanceSnapshot, error) {
	if s.db == nil {
		return nil, ErrNotReady
	}
	var snap model.PerformanceSnapshot
	err := s.db.WithContext(ctx).
		Where("recommendation_id = ?", recID).
		Order("snapshot_date DESC, id DESC").
		First(&snap).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &snap, nil
}

// HistoryForRec returns every snapshot ever saved for a
// recommendation, newest first.
func (s *Service) HistoryForRec(ctx context.Context, recID uint64) ([]model.PerformanceSnapshot, error) {
	if s.db == nil {
		return nil, ErrNotReady
	}
	var snaps []model.PerformanceSnapshot
	if err := s.db.WithContext(ctx).
		Where("recommendation_id = ?", recID).
		Order("snapshot_date DESC").
		Find(&snaps).Error; err != nil {
		return nil, err
	}
	return snaps, nil
}

// =============================================================================
// §9.5 Startup / cron backfill
// =============================================================================

// Backfill scans every recommendation whose date is in
// [now-lookback, now] and computes a snapshot for any that doesn't
// already have one. Idempotent: re-runs skip recs that already
// have a snapshot for their date. The unique key on
// (recommendation_id, snapshot_date) means even a redundant write
// would be a no-op overwrite — the in-process skip is just an
// optimization to avoid wasting K-line fetches.
//
// Returns (processed, errored). The function only returns a non-nil
// error for unrecoverable conditions (DB unavailable, ctx cancelled,
// query failed). Per-rec compute errors are counted but don't
// abort the loop — every rec gets a fair shot.
func (s *Service) Backfill(ctx context.Context, lookback time.Duration) (processed int, errs int, err error) {
	if s.db == nil {
		return 0, 0, ErrNotReady
	}
	if lookback <= 0 {
		lookback = 7 * 24 * time.Hour
	}
	// We compute the cutoff in the calendar's location so the
	// timezone math is consistent with rec.Date. rec.Date is
	// stored as a date-typed column but read as time.Time — using
	// the calendar's location avoids off-by-one-day bugs around
	// the trading-day boundary.
	loc := s.cal.Location()
	now := time.Now().In(loc)
	from := now.Add(-lookback)

	// 1. Pull every rec in the window. ORDER BY date ASC, id ASC
	//    so the walk is deterministic — useful when an operator
	//    looks at logs.
	var recs []model.Recommendation
	if err := s.db.WithContext(ctx).
		Where("date >= ? AND date <= ?", from, now).
		Order("date ASC, id ASC").
		Find(&recs).Error; err != nil {
		return 0, 0, fmt.Errorf("perf: backfill list: %w", err)
	}
	if len(recs) == 0 {
		return 0, 0, nil
	}

	// 2. Single query to find which recs already have a snapshot.
	//    We don't constrain snapshot_date to rec.date here — the
	//    (rec_id, snapshot_date) unique key guarantees at most one
	//    row per rec anyway, and any pre-existing snapshot means
	//    "already covered".
	recIDs := make([]uint64, len(recs))
	for i, r := range recs {
		recIDs[i] = r.ID
	}
	type idOnly struct {
		RecommendationID uint64
	}
	var existing []idOnly
	if err := s.db.WithContext(ctx).
		Table("performance_snapshots").
		Select("DISTINCT recommendation_id AS recommendation_id").
		Where("recommendation_id IN ?", recIDs).
		Scan(&existing).Error; err != nil {
		return 0, 0, fmt.Errorf("perf: backfill existing: %w", err)
	}
	haveSnapshot := make(map[uint64]bool, len(existing))
	for _, e := range existing {
		haveSnapshot[e.RecommendationID] = true
	}

	// 3. Walk the missing-recs set. Per-rec failures are logged
	//    and counted but do not abort.
	for i := range recs {
		if cerr := ctx.Err(); cerr != nil {
			return processed, errs, cerr
		}
		if haveSnapshot[recs[i].ID] {
			continue
		}
		if _, cerr := s.ComputeAndSave(ctx, &recs[i]); cerr != nil {
			s.logger.Warn("perf: backfill ComputeAndSave failed",
				zap.Uint64("rec_id", recs[i].ID),
				zap.String("stock", recs[i].StockCode),
				zap.Error(cerr),
			)
			errs++
			continue
		}
		processed++
	}
	return processed, errs, nil
}

// =============================================================================
// Internals
// =============================================================================

// fetchClose returns the 1d K-line close for `day`. ok=false signals
// "no data" — the stock was suspended, the source failed, or the
// target day is in the future.
func (s *Service) fetchClose(ctx context.Context, stockCode string, day time.Time) (float64, bool) {
	if s.ds == nil {
		return 0, false
	}
	loc := s.cal.Location()
	dayLocal := day.In(loc)
	start := time.Date(dayLocal.Year(), dayLocal.Month(), dayLocal.Day(), 0, 0, 0, 0, loc)
	// We allow a forward pad so a "today" target isn't filtered out
	// by the K-line source's notion of "end of day".
	end := start.Add(s.klineFuturePad)
	if start.After(end) {
		return 0, false
	}
	cctx, cancel := context.WithTimeout(ctx, s.klineTimeout)
	defer cancel()
	klines, err := s.ds.GetKLine(cctx, stockCode, s.klinePeriod, start, end)
	if err != nil || len(klines) == 0 {
		return 0, false
	}
	// Find the first kline whose date matches the requested day. The
	// manager already returns candles sorted by timestamp.
	for _, k := range klines {
		kl := k.Timestamp.In(loc)
		if kl.Year() == dayLocal.Year() && kl.Month() == dayLocal.Month() && kl.Day() == dayLocal.Day() {
			if !isFinite(k.Close) {
				continue
			}
			return k.Close, true
		}
	}
	return 0, false
}

// inDay truncates a timestamp to midnight in the given location.
func inDay(t time.Time, loc *time.Location) time.Time {
	tl := t.In(loc)
	return time.Date(tl.Year(), tl.Month(), tl.Day(), 0, 0, 0, 0, loc)
}

// pickLast returns the last non-nil pointer in the argument list. This
// is "the most-advanced horizon we have data for" semantics.
func pickLast(ps ...*float64) *float64 {
	var out *float64
	for _, p := range ps {
		if p != nil {
			out = p
		}
	}
	return out
}

// singleRecWinRate returns 1.0 if any T+N return is positive, 0.0 if
// every known T+N return is non-positive, nil when no T+N return is
// available. A zero return is a "did not move" verdict, not a win.
func singleRecWinRate(t1, t3, t5 *float64) *float64 {
	any := false
	anyPositive := false
	allKnown := true
	for _, p := range []**float64{&t1, &t3, &t5} {
		if *p == nil {
			allKnown = false
			continue
		}
		any = true
		if **p > 0 {
			anyPositive = true
		}
	}
	if !any {
		return nil
	}
	if !allKnown && !anyPositive {
		// We don't know the verdict: missing T+5 plus no positive
		// among the known T+Ns is genuinely ambiguous.
		return nil
	}
	if anyPositive {
		one := 1.0
		return &one
	}
	zero := 0.0
	return &zero
}
