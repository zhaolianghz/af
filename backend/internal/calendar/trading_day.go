// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package calendar exposes trading-day + trading-session helpers
// used by the scheduler and the session_tag node.
//
// v1 implements a static model: weekends are always non-trading,
// and the A-share session runs 09:30-11:30 / 13:00-15:00 in
// Asia/Shanghai. Holidays are loaded from the trading_calendar
// table (model.TradingCalendar); when the table is empty for a
// given date we fall back to the weekend rule.
//
// Future revisions can swap the in-memory loader for a tushare
// or akshare sync job without changing this API.
package calendar

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/model"
)

// Session names returned by SessionAt. They mirror the values
// emitted by orchestrator.VarKeySessionTag.
const (
	SessionMorning   = "MORNING"
	SessionAfternoon = "AFTERNOON"
	SessionReview    = "REVIEW"
	SessionNoPost    = "NO_POST"
)

// Service is the dependency the scheduler / nodes import. It is
// safe for concurrent use: the in-memory holiday cache is
// guarded by mu and is repopulated lazily on miss.
type Service struct {
	mu sync.RWMutex
	// cache maps the YYYY-MM-DD key (in cfg.Timezone) to
	// (isTrading, ok). ok=false means we have no row for that
	// date and the caller should fall back to the weekend rule.
	cache map[string]cachedDay

	db  *gorm.DB
	cfg calendarConfig
}

// calendarConfig is the parsed, validated subset of
// config.CalendarConfig. Constructed once at NewService time so
// per-call time parsing happens up front.
type calendarConfig struct {
	timezone       *time.Location
	morningStart   int // minutes since 00:00
	morningEnd     int
	afternoonStart int
	afternoonEnd   int
	reviewStart    int
	reviewEnd      int
}

type cachedDay struct {
	isTrading bool
	ok        bool
	// transient marks entries written after a DB ERROR (not a clean
	// "no row"). They expire quickly so a recovered DB is picked up
	// promptly, while still absorbing the per-call query storm.
	transient bool
	expiresAt time.Time
}

// NewService builds a calendar Service. cfg.Timezone must be a
// valid IANA name. db may be nil — the service still works
// against the weekend rule, it just won't consult the
// trading_calendar table.
func NewService(cfg config.CalendarConfig, db *gorm.DB) (*Service, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, fmt.Errorf("calendar: load timezone %q: %w", cfg.Timezone, err)
	}
	parsed := calendarConfig{timezone: loc}
	for _, p := range []struct {
		dst *int
		src string
	}{
		{&parsed.morningStart, cfg.MorningStart},
		{&parsed.morningEnd, cfg.MorningEnd},
		{&parsed.afternoonStart, cfg.AfternoonStart},
		{&parsed.afternoonEnd, cfg.AfternoonEnd},
		{&parsed.reviewStart, cfg.ReviewStart},
		{&parsed.reviewEnd, cfg.ReviewEnd},
	} {
		m, err := parseHHMM(p.src)
		if err != nil {
			return nil, fmt.Errorf("calendar: %w", err)
		}
		*p.dst = m
	}
	return &Service{
		cache: make(map[string]cachedDay),
		db:    db,
		cfg:   parsed,
	}, nil
}

func parseHHMM(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("invalid HH:MM %q: %w", s, err)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// =============================================================================
// Public API
// =============================================================================

// IsTradingDay reports whether `day` (in cfg.Timezone) is a
// trading day. Lookup order:
//  1. trading_calendar table (if db != nil and the row exists)
//  2. weekend rule (Sat/Sun → non-trading)
//  3. default: trading day (only if the date is a weekday)
func (s *Service) IsTradingDay(day time.Time) bool {
	key := s.key(day)
	if c, ok := s.lookup(key, day); ok {
		return c.isTrading
	}
	// Fallback: weekend rule.
	wd := day.In(s.cfg.timezone).Weekday()
	return wd != time.Saturday && wd != time.Sunday
}

// NextTradingDay returns the first trading day strictly after
// `from`. ok=false means no trading day was found within the
// 366-day scan window (calendar data badly broken) — callers MUST
// handle this instead of treating `from` as a trading day.
func (s *Service) NextTradingDay(from time.Time) (time.Time, bool) {
	day := from.In(s.cfg.timezone)
	for i := 1; i <= 366; i++ {
		cand := day.AddDate(0, 0, i)
		if s.IsTradingDay(cand) {
			return cand, true
		}
	}
	return from, false
}

// PreviousTradingDay returns the first trading day strictly
// before `from`. ok=false when nothing found within 366 days.
func (s *Service) PreviousTradingDay(from time.Time) (time.Time, bool) {
	day := from.In(s.cfg.timezone)
	for i := 1; i <= 366; i++ {
		cand := day.AddDate(0, 0, -i)
		if s.IsTradingDay(cand) {
			return cand, true
		}
	}
	return from, false
}

// IsInTradingSession reports whether `at` (interpreted in
// cfg.Timezone) falls inside an A-share trading session:
// 09:30-11:30 or 13:00-15:00. The 11:30-13:00 lunch gap is
// non-trading. After-hours (15:00-17:00) returns false — the
// "review" window is a UI concept, not a market one.
func (s *Service) IsInTradingSession(at time.Time) bool {
	loc := at.In(s.cfg.timezone)
	hm := loc.Hour()*60 + loc.Minute()
	if hm >= s.cfg.morningStart && hm < s.cfg.morningEnd {
		return true
	}
	if hm >= s.cfg.afternoonStart && hm < s.cfg.afternoonEnd {
		return true
	}
	return false
}

// SessionAt returns the session label for `at`. Mirrors the
// derivation in the session_tag node so the scheduler can pre-
// stamp runs without an extra dependency on the orchestrator.
func (s *Service) SessionAt(at time.Time) string {
	loc := at.In(s.cfg.timezone)
	hm := loc.Hour()*60 + loc.Minute()
	switch {
	case hm >= s.cfg.morningStart && hm < s.cfg.morningEnd:
		return SessionMorning
	case hm >= s.cfg.afternoonStart && hm < s.cfg.afternoonEnd:
		return SessionAfternoon
	case hm >= s.cfg.reviewStart && hm < s.cfg.reviewEnd:
		return SessionReview
	default:
		return SessionNoPost
	}
}

// Location returns the configured timezone. Useful for callers
// that need to format timestamps consistently.
func (s *Service) Location() *time.Location { return s.cfg.timezone }

// ReloadCache drops the in-memory holiday cache. The next
// IsTradingDay call re-reads the trading_calendar table.
func (s *Service) ReloadCache() {
	s.mu.Lock()
	s.cache = make(map[string]cachedDay)
	s.mu.Unlock()
}

// =============================================================================
// Internals
// =============================================================================

// key produces the YYYY-MM-DD cache key for `day` in the
// configured timezone.
func (s *Service) key(day time.Time) string {
	return day.In(s.cfg.timezone).Format("2006-01-02")
}

// negErrTTL bounds how long a DB-error negative cache entry is
// trusted before we retry the DB.
const negErrTTL = 30 * time.Second

// lookup consults the cache, then the DB, then returns ok=false
// (caller should fall back to the weekend rule).
func (s *Service) lookup(key string, day time.Time) (cachedDay, bool) {
	s.mu.RLock()
	c, ok := s.cache[key]
	s.mu.RUnlock()
	if ok && (!c.transient || time.Now().Before(c.expiresAt)) {
		return c, true
	}
	if s.db == nil {
		return cachedDay{}, false
	}
	var row model.TradingCalendar
	// Match the date part only. SQLite's date type stores
	// midnight UTC; we compare in the configured timezone to
	// avoid off-by-one in the surrounding days.
	dayLocal := day.In(s.cfg.timezone)
	start := time.Date(dayLocal.Year(), dayLocal.Month(), dayLocal.Day(), 0, 0, 0, 0, s.cfg.timezone)
	end := start.Add(24 * time.Hour)
	if err := s.db.Where("date >= ? AND date < ?", start, end).First(&row).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			// Treat DB errors as "no data" rather than crashing the
			// scheduler — but negative-cache them with a short TTL so
			// every IsTradingDay call doesn't hammer a flapping DB.
			s.mu.Lock()
			s.cache[key] = cachedDay{ok: false, transient: true, expiresAt: time.Now().Add(negErrTTL)}
			s.mu.Unlock()
			return cachedDay{}, false
		}
		// Not found: still cache the miss so we don't query
		// the table for every node-run.
		s.mu.Lock()
		s.cache[key] = cachedDay{ok: false}
		s.mu.Unlock()
		return cachedDay{}, false
	}
	s.mu.Lock()
	s.cache[key] = cachedDay{isTrading: row.IsTrading, ok: true}
	s.mu.Unlock()
	return cachedDay{isTrading: row.IsTrading, ok: true}, true
}
