// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for the calendar package — covers weekend fallback, the
// trading-session windows, the SessionAt derivation, holiday
// overrides loaded from the DB, and edge cases at the session
// boundaries.
package calendar

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/model"
)

func defaultCfg() config.CalendarConfig {
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

func newSvcNoDB(t *testing.T) *Service {
	t.Helper()
	s, err := NewService(defaultCfg(), nil)
	require.NoError(t, err)
	return s
}

func newSvcWithDB(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "cal_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	s, err := NewService(defaultCfg(), db)
	require.NoError(t, err)
	return s, db
}

func inTZ(t time.Time) time.Time {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	// Convert the wall-clock time to Shanghai. The caller
	// passes a UTC time but we want the calendar code to
	// see the Shanghai wall-clock — so we shift by the
	// Shanghai offset.
	sh := time.FixedZone("SHA", 8*3600)
	return t.In(sh).In(loc)
}

// shanghaiAt returns a time with the given wall-clock in
// Asia/Shanghai. Use this when you care about the local hour.
func shanghaiAt(y int, m time.Month, d, h, min int) time.Time {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	return time.Date(y, m, d, h, min, 0, 0, loc)
}

// =============================================================================
// Construction
// =============================================================================

func TestNewService_BadTimezone(t *testing.T) {
	_, err := NewService(config.CalendarConfig{Timezone: "Mars/Olympus"}, nil)
	require.Error(t, err)
}

func TestNewService_BadHHMM(t *testing.T) {
	_, err := NewService(config.CalendarConfig{
		Timezone:     "Asia/Shanghai",
		MorningStart: "not-a-time",
	}, nil)
	require.Error(t, err)
}

// =============================================================================
// Weekend fallback (no DB)
// =============================================================================

func TestIsTradingDay_WeekdayWithoutDB(t *testing.T) {
	s := newSvcNoDB(t)
	// 2026-06-08 is a Monday.
	day := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	require.True(t, s.IsTradingDay(day))
}

func TestIsTradingDay_SaturdayWithoutDB(t *testing.T) {
	s := newSvcNoDB(t)
	// 2026-06-13 is a Saturday.
	day := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	require.False(t, s.IsTradingDay(day))
}

func TestIsTradingDay_SundayWithoutDB(t *testing.T) {
	s := newSvcNoDB(t)
	// 2026-06-14 is a Sunday.
	day := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	require.False(t, s.IsTradingDay(day))
}

// =============================================================================
// Trading session
// =============================================================================

func TestIsInTradingSession_Morning(t *testing.T) {
	s := newSvcNoDB(t)
	day := shanghaiAt(2026, 6, 8, 9, 30)
	require.True(t, s.IsInTradingSession(day))
}

func TestIsInTradingSession_Boundary_0930_Open(t *testing.T) {
	s := newSvcNoDB(t)
	day := shanghaiAt(2026, 6, 8, 9, 30)
	require.True(t, s.IsInTradingSession(day))
}

func TestIsInTradingSession_Boundary_1130_Close(t *testing.T) {
	s := newSvcNoDB(t)
	// 11:30 is exclusive end → outside.
	day := shanghaiAt(2026, 6, 8, 11, 30)
	require.False(t, s.IsInTradingSession(day))
}

func TestIsInTradingSession_Lunch(t *testing.T) {
	s := newSvcNoDB(t)
	day := shanghaiAt(2026, 6, 8, 12, 0)
	require.False(t, s.IsInTradingSession(day))
}

func TestIsInTradingSession_Afternoon(t *testing.T) {
	s := newSvcNoDB(t)
	day := shanghaiAt(2026, 6, 8, 14, 0)
	require.True(t, s.IsInTradingSession(day))
}

func TestIsInTradingSession_AfterHours(t *testing.T) {
	s := newSvcNoDB(t)
	day := shanghaiAt(2026, 6, 8, 18, 0)
	require.False(t, s.IsInTradingSession(day))
}

// =============================================================================
// SessionAt
// =============================================================================

func TestSessionAt_Mapping(t *testing.T) {
	s := newSvcNoDB(t)
	sh, _ := time.LoadLocation("Asia/Shanghai")
	cases := []struct {
		h, m int
		want string
	}{
		{9, 30, SessionMorning},
		{10, 0, SessionMorning},
		{11, 29, SessionMorning},
		{12, 0, SessionNoPost},
		{13, 0, SessionAfternoon},
		{14, 59, SessionAfternoon},
		{15, 1, SessionReview},
		{17, 0, SessionNoPost},
		{3, 0, SessionNoPost},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			day := time.Date(2026, 6, 8, c.h, c.m, 0, 0, sh)
			require.Equal(t, c.want, s.SessionAt(day))
		})
	}
}

// =============================================================================
// Holiday override (with DB)
// =============================================================================

func TestIsTradingDay_HolidayOverride(t *testing.T) {
	s, db := newSvcWithDB(t)
	// Mark 2026-06-08 (Monday) as a holiday.
	day := inTZ(time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC))
	require.NoError(t, db.Create(&model.TradingCalendar{
		Date:      day,
		IsTrading: false,
		IsWeekend: false,
		Note:      "Dragon Boat Festival",
		Source:    model.CalendarSourceManual,
		SyncedAt:  time.Now(),
	}).Error)
	// Reload to bust the cache (the insert above populated
	// the DB but not the cache, but we still call this to
	// document the lifecycle).
	s.ReloadCache()
	require.False(t, s.IsTradingDay(day))
}

func TestIsTradingDay_ExplicitlyTradingOnWeekend(t *testing.T) {
	// Rare case: weekend catch-up day. The DB overrides
	// the weekend rule.
	s, db := newSvcWithDB(t)
	day := inTZ(time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)) // Saturday
	require.NoError(t, db.Create(&model.TradingCalendar{
		Date:      day,
		IsTrading: true,
		IsWeekend: true,
		Note:      "Make-up trading day",
		Source:    model.CalendarSourceManual,
		SyncedAt:  time.Now(),
	}).Error)
	s.ReloadCache()
	require.True(t, s.IsTradingDay(day))
}

func TestReloadCache(t *testing.T) {
	s, db := newSvcWithDB(t)
	day := inTZ(time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC))
	// Populate cache via a non-trading lookup.
	require.NoError(t, db.Create(&model.TradingCalendar{
		Date:      day,
		IsTrading: false,
		IsWeekend: false,
		Source:    model.CalendarSourceManual,
		SyncedAt:  time.Now(),
	}).Error)
	require.False(t, s.IsTradingDay(day))
	// Now flip the row in the DB and reload.
	require.NoError(t, db.Model(&model.TradingCalendar{}).
		Where("date = ?", day).Update("is_trading", true).Error)
	s.ReloadCache()
	require.True(t, s.IsTradingDay(day))
}

// =============================================================================
// Next/Previous
// =============================================================================

func TestNextTradingDay_SkipsWeekend(t *testing.T) {
	s := newSvcNoDB(t)
	// Friday 2026-06-12 → next is Monday 2026-06-15.
	friday := inTZ(time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC))
	next, ok := s.NextTradingDay(friday)
	require.True(t, ok, "NextTradingDay should find a trading day")
	require.Equal(t, "2026-06-15", next.Format("2006-01-02"))
}

func TestPreviousTradingDay_SkipsWeekend(t *testing.T) {
	s := newSvcNoDB(t)
	// Monday 2026-06-15 → previous is Friday 2026-06-12.
	monday := inTZ(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))
	prev, ok := s.PreviousTradingDay(monday)
	require.True(t, ok, "PreviousTradingDay should find a trading day")
	require.Equal(t, "2026-06-12", prev.Format("2006-01-02"))
}

func TestNextTradingDay_SkipsHoliday(t *testing.T) {
	s, db := newSvcWithDB(t)
	// Make Friday 2026-06-12 a holiday. Next from Thursday
	// 2026-06-11 should be Monday 2026-06-15.
	holiday := inTZ(time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC))
	require.NoError(t, db.Create(&model.TradingCalendar{
		Date:      holiday,
		IsTrading: false,
		IsWeekend: false,
		Source:    model.CalendarSourceManual,
		SyncedAt:  time.Now(),
	}).Error)
	thursday := inTZ(time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC))
	next, ok := s.NextTradingDay(thursday)
	require.True(t, ok, "NextTradingDay should find a trading day")
	require.Equal(t, "2026-06-15", next.Format("2006-01-02"))
}

// =============================================================================
// Location
// =============================================================================

func TestLocation(t *testing.T) {
	s := newSvcNoDB(t)
	loc := s.Location()
	require.Equal(t, "Asia/Shanghai", loc.String())
}
