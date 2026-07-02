// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for the cron-based scheduler: Add/Remove/ActiveEntries,
// LoadFromDB, SetOnFire hook, trading-day + session skip
// semantics, and constructor validation.
package executor

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/calendar"
	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/model"
)

func defaultCfg() config.CronConfig {
	return config.CronConfig{
		Timezone: "Asia/Shanghai",
	}
}

// newSchedulerNoDB builds a Scheduler with no DB / executor /
// calendar — sufficient for Add/Remove/ActiveEntries tests.
func newSchedulerNoDB(t *testing.T) *Scheduler {
	t.Helper()
	s, err := NewScheduler(SchedulerConfig{
		Logger: zap.NewNop(),
		Cron:   defaultCfg(),
	})
	require.NoError(t, err)
	return s
}

func newSchedulerWithDB(t *testing.T, withCalendar bool) (*Scheduler, *gorm.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sched_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))

	cfg := SchedulerConfig{
		DB:     db,
		Logger: zap.NewNop(),
		Cron:   defaultCfg(),
		Executor: &Executor{
			db:     db,
			cfg:    defaultExecutorCfg(),
			logger: zap.NewNop(),
		},
	}
	if withCalendar {
		cal, err := calendar.NewService(calendarConfig(), db)
		require.NoError(t, err)
		cfg.Calendar = cal
	}
	s, err := NewScheduler(cfg)
	require.NoError(t, err)
	return s, db
}

func calendarConfig() config.CalendarConfig {
	return config.CalendarConfig{
		Timezone:       "Asia/Shanghai",
		MorningStart:   "09:30",
		MorningEnd:     "11:30",
		AfternoonStart: "13:00",
		AfternoonEnd:   "15:00",
		ReviewStart:    "15:00",
		ReviewEnd:      "17:00",
	}
}

// =============================================================================
// Construction
// =============================================================================

func TestNewScheduler_BadTimezone(t *testing.T) {
	_, err := NewScheduler(SchedulerConfig{
		Logger: zap.NewNop(),
		Cron:   config.CronConfig{Timezone: "Mars/Olympus"},
	})
	require.Error(t, err)
}

func TestNewScheduler_Defaults(t *testing.T) {
	s, err := NewScheduler(SchedulerConfig{
		Logger: zap.NewNop(),
		Cron:   defaultCfg(),
	})
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NotNil(t, s.c)
	require.NotNil(t, s.parser)
	require.NotNil(t, s.active)
}

// =============================================================================
// Add / Remove / ActiveEntries
// =============================================================================

func TestAdd_Happy(t *testing.T) {
	s := newSchedulerNoDB(t)
	require.NoError(t, s.Add(1, "*/5 * * * *"))
	require.Contains(t, s.ActiveEntries(), uint64(1))
}

func TestAdd_EmptyExpr(t *testing.T) {
	s := newSchedulerNoDB(t)
	require.Error(t, s.Add(1, ""))
}

func TestAdd_BadExpr(t *testing.T) {
	s := newSchedulerNoDB(t)
	require.Error(t, s.Add(1, "not a cron expr"))
}

func TestAdd_ReplacesExisting(t *testing.T) {
	s := newSchedulerNoDB(t)
	require.NoError(t, s.Add(1, "*/5 * * * *"))
	require.NoError(t, s.Add(1, "*/10 * * * *"))
	// Same strategy id, only one entry should remain.
	count := 0
	for _, id := range s.ActiveEntries() {
		if id == 1 {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestRemove(t *testing.T) {
	s := newSchedulerNoDB(t)
	require.NoError(t, s.Add(1, "*/5 * * * *"))
	require.Contains(t, s.ActiveEntries(), uint64(1))
	s.Remove(1)
	require.NotContains(t, s.ActiveEntries(), uint64(1))
}

func TestRemove_NonExistent(t *testing.T) {
	s := newSchedulerNoDB(t)
	s.Remove(99) // no panic, no error
}

// =============================================================================
// SetOnFire
// =============================================================================

func TestSetOnFire(t *testing.T) {
	s := newSchedulerNoDB(t)
	var fired uint64
	s.SetOnFire(func(strategyID uint64, trigger string) {
		atomic.AddUint64(&fired, 1)
	})
	// Trigger fires are not directly callable in the public
	// API; verify the hook is stored.
	require.NotNil(t, s.onFire)
}

// =============================================================================
// LoadFromDB
// =============================================================================

func TestLoadFromDB_NoDB(t *testing.T) {
	s := newSchedulerNoDB(t)
	require.NoError(t, s.LoadFromDB(context.Background()))
}

func TestLoadFromDB_RegistersActive(t *testing.T) {
	s, db := newSchedulerWithDB(t, false)
	require.NoError(t, db.Create(&model.Strategy{
		Code: "x", Name: "x", Status: model.StrategyStatusActive,
		CronExpression: "*/5 * * * *",
	}).Error)
	require.NoError(t, s.LoadFromDB(context.Background()))
	entries := s.ActiveEntries()
	require.NotEmpty(t, entries)
}

func TestLoadFromDB_SkipsDraftAndDisabled(t *testing.T) {
	s, db := newSchedulerWithDB(t, false)
	require.NoError(t, db.Create(&model.Strategy{
		Code: "d", Name: "d", Status: model.StrategyStatusDraft,
		CronExpression: "*/5 * * * *",
	}).Error)
	require.NoError(t, db.Create(&model.Strategy{
		Code: "p", Name: "p", Status: model.StrategyStatusDisabled,
		CronExpression: "*/5 * * * *",
	}).Error)
	require.NoError(t, db.Create(&model.Strategy{
		Code: "e", Name: "e", Status: model.StrategyStatusActive,
		CronExpression: "",
	}).Error)
	require.NoError(t, s.LoadFromDB(context.Background()))
	// No cron expressions registered for draft/disabled/empty.
	require.Empty(t, s.ActiveEntries())
}

func TestLoadFromDB_BadExprLoggedButNotFatal(t *testing.T) {
	s, db := newSchedulerWithDB(t, false)
	require.NoError(t, db.Create(&model.Strategy{
		Code: "bad", Name: "bad", Status: model.StrategyStatusActive,
		CronExpression: "not-a-cron",
	}).Error)
	require.NoError(t, s.LoadFromDB(context.Background()))
	require.Empty(t, s.ActiveEntries())
}

// =============================================================================
// Stop
// =============================================================================

func TestStop_BeforeStart(t *testing.T) {
	s := newSchedulerNoDB(t)
	// Stop is safe even when Start has not been called.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, s.Stop(ctx))
}

// =============================================================================
// fire (trading-day / session skip)
// =============================================================================

// TestFire_TradingDaySkip verifies that when calendar.IsTradingDay
// returns false, the cron callback short-circuits without firing.
func TestFire_TradingDaySkip(t *testing.T) {
	s := newSchedulerNoDB(t)
	var fired uint64
	s.SetOnFire(func(strategyID uint64, trigger string) {
		atomic.AddUint64(&fired, 1)
	})
	// Inject a calendar that says "not a trading day" for today.
	s.cfg.Calendar = notTradingDayCalendarService(t)
	now := time.Now()
	s.fire(42)
	require.Equal(t, uint64(0), atomic.LoadUint64(&fired))
	_ = now
}

// TestFire_SessionSkip verifies that when IsInTradingSession
// returns false (but it IS a trading day), the callback also
// short-circuits.
func TestFire_SessionSkip(t *testing.T) {
	s := newSchedulerNoDB(t)
	var fired uint64
	s.SetOnFire(func(strategyID uint64, trigger string) {
		atomic.AddUint64(&fired, 1)
	})
	s.cfg.Calendar = notInSessionCalendarService(t)
	s.fire(42)
	require.Equal(t, uint64(0), atomic.LoadUint64(&fired))
}

// TestFire_HappyDay invokes the OnFire hook when both checks pass.
func TestFire_HappyDay(t *testing.T) {
	s := newSchedulerNoDB(t)
	var fired uint64
	s.SetOnFire(func(strategyID uint64, trigger string) {
		atomic.AddUint64(&fired, 1)
	})
	s.cfg.Calendar = alwaysInSessionCalendarService(t)
	s.fire(42)
	require.Equal(t, uint64(1), atomic.LoadUint64(&fired))
}

// TestFire_NilCalendar fires regardless of trading-day — the
// scheduler is permissive when no calendar is wired.
func TestFire_NilCalendar(t *testing.T) {
	s := newSchedulerNoDB(t)
	var fired uint64
	s.SetOnFire(func(strategyID uint64, trigger string) {
		atomic.AddUint64(&fired, 1)
	})
	require.Nil(t, s.cfg.Calendar)
	s.fire(42)
	require.Equal(t, uint64(1), atomic.LoadUint64(&fired))
}

// =============================================================================
// Calendar fakes — calendar.Service is a concrete struct, so
// these helpers construct a real *Service backed by a sqlite
// trading_calendar table (or a narrow session window) so the
// test gets the desired IsTradingDay / IsInTradingSession
// answer for "now".
// =============================================================================

// notTradingDayCalendarService returns a *calendar.Service
// where IsTradingDay(now) is false (via a trading_calendar row
// with IsTrading=false for today).
func notTradingDayCalendarService(t *testing.T) *calendar.Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "cal_notrd.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))

	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Now().In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	require.NoError(t, db.Create(&model.TradingCalendar{
		Date: day, IsTrading: false, IsWeekend: false, Source: model.CalendarSourceManual,
		SyncedAt: time.Now(),
	}).Error)

	cal, err := calendar.NewService(calendarConfig(), db)
	require.NoError(t, err)
	return cal
}

// notInSessionCalendarService returns a *calendar.Service
// where IsTradingDay(now) is true (via a positive row) and
// IsInTradingSession(now) is false (session windows are set to
// a 1-minute range that is guaranteed not to include "now" in
// UTC — the test runs against UTC, so 00:00-00:01 is past).
func notInSessionCalendarService(t *testing.T) *calendar.Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "cal_nses.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))

	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Now().In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	require.NoError(t, db.Create(&model.TradingCalendar{
		Date: day, IsTrading: true, IsWeekend: false, Source: model.CalendarSourceManual,
		SyncedAt: time.Now(),
	}).Error)

	cfg := calendarConfig()
	// Use UTC so the test is timezone-independent; the
	// session window is set to a tiny range that doesn't
	// cover "now" (it sits at 00:00-00:01, i.e. the first
	// minute of the day).
	cfg.Timezone = "UTC"
	cfg.MorningStart = "00:00"
	cfg.MorningEnd = "00:01"
	cfg.AfternoonStart = "00:00"
	cfg.AfternoonEnd = "00:01"
	cfg.ReviewStart = "00:00"
	cfg.ReviewEnd = "00:01"
	cal, err := calendar.NewService(cfg, db)
	require.NoError(t, err)
	return cal
}

// alwaysInSessionCalendarService returns a *calendar.Service
// where IsTradingDay(now) and IsInTradingSession(now) are
// always true. The session window covers 00:00-23:59 and the
// day is marked as a trading day in the table.
func alwaysInSessionCalendarService(t *testing.T) *calendar.Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "cal_yes.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))

	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Now().In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	require.NoError(t, db.Create(&model.TradingCalendar{
		Date: day, IsTrading: true, IsWeekend: false, Source: model.CalendarSourceManual,
		SyncedAt: time.Now(),
	}).Error)

	cfg := calendarConfig()
	cfg.Timezone = "UTC"
	cfg.MorningStart = "00:00"
	cfg.MorningEnd = "23:59"
	cfg.AfternoonStart = "00:00"
	cfg.AfternoonEnd = "00:00" // empty afternoon
	cfg.ReviewStart = "00:00"
	cfg.ReviewEnd = "00:00" // empty review
	cal, err := calendar.NewService(cfg, db)
	require.NoError(t, err)
	return cal
}
// =============================================================================
// Start + cfgExecutorTimeout (C2a coverage)
// =============================================================================

func TestStart_ThenStop(t *testing.T) {
	s := newSchedulerNoDB(t)
	s.Start()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Errorf("Stop after Start errored: %v", err)
	}
}

func TestCfgExecutorTimeout_NilExecutorDefault(t *testing.T) {
	s := newSchedulerNoDB(t) // no executor wired
	if got := s.cfgExecutorTimeout(); got != 5*time.Minute {
		t.Errorf("nil executor → %v, want 5m default", got)
	}
}

func TestCfgExecutorTimeout_FromExecutor(t *testing.T) {
	s, _ := newSchedulerWithDB(t, false) // executor wired with defaultExecutorCfg
	got := s.cfgExecutorTimeout()
	if got <= 0 {
		t.Errorf("executor timeout = %v, want > 0", got)
	}
}
