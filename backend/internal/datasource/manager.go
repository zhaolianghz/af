// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — manager.go
//
// The Manager wires Sources, Breakers, Cache, and HealthRepo together
// and exposes the unified API the rest of the system calls into.
//
// Routing algorithm (per call):
//
//  1. Try the cache first; on a hit return immediately.
//  2. Walk the configured source chain in order.
//  3. For each source, ask its Breaker.Allow(); if open, skip.
//  4. Call the source. On success: record success in repo + breaker,
//     populate cache, return.
//  5. On failure: record failure in repo + breaker, move to the next
//     source.
//  6. If every source failed, return ErrAllSourcesDown.
package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ManagerConfig holds tunables for NewManager. Zero values fall back to
// safe defaults.
type ManagerConfig struct {
	// CacheTTL is the TTL used for K-line and fundamental entries.
	// Default: 24h. Quote TTL is fixed at 5s.
	CacheTTL time.Duration
	// FailThreshold / FailWindow / Cooldown configure every per-
	// source Breaker.
	FailThreshold int
	FailWindow    time.Duration
	Cooldown      time.Duration
	// QuoteTTL is the TTL for the quote cache. Default: 5s.
	QuoteTTL time.Duration
	// ConsistencyThreshold is the relative price deviation above
	// which cross-source validation flags a quote pair. Default:
	// 0.005 (0.5 %).
	ConsistencyThreshold float64
	// UniverseURL is the akshare-sidecar base that serves the
	// /universe endpoint (stock-pool resolution). Empty disables
	// universe resolution (data_source then needs explicit codes).
	UniverseURL string
}

// withDefaults fills in zero values with the design.md defaults.
func (c ManagerConfig) withDefaults() ManagerConfig {
	if c.CacheTTL <= 0 {
		c.CacheTTL = 24 * time.Hour
	}
	if c.FailThreshold <= 0 {
		c.FailThreshold = 5
	}
	if c.FailWindow <= 0 {
		c.FailWindow = 5 * time.Minute
	}
	if c.Cooldown <= 0 {
		c.Cooldown = 10 * time.Minute
	}
	if c.QuoteTTL <= 0 {
		c.QuoteTTL = 5 * time.Second
	}
	if c.ConsistencyThreshold <= 0 {
		c.ConsistencyThreshold = 0.005
	}
	return c
}

// Manager is the public-facing facade. Use NewManager to construct.
type manager struct {
	cfg      ManagerConfig
	cache    Cache
	repo     HealthRepo
	logger   *zap.Logger
	nowFunc  func() time.Time
	mu       sync.RWMutex
	breakers []*Breaker
	sources  []Source
}

// NewManager builds a manager from a slice of sources (in call order,
// primary first) plus the supporting dependencies.
func NewManager(
	sources []Source,
	cache Cache,
	repo HealthRepo,
	cfg ManagerConfig,
	logger *zap.Logger,
) Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	cfg = cfg.withDefaults()

	breakers := make([]*Breaker, 0, len(sources))
	for _, s := range sources {
		if s == nil {
			continue
		}
		breakers = append(breakers, NewBreaker(s, cfg.FailThreshold, cfg.FailWindow, cfg.Cooldown, time.Now))
	}
	return &manager{
		cfg:      cfg,
		cache:    cache,
		repo:     repo,
		logger:   logger,
		nowFunc:  time.Now,
		breakers: breakers,
		sources:  sources,
	}
}

// NewManagerWithClock is like NewManager but injects a deterministic
// clock — used by tests to exercise breaker state transitions.
func NewManagerWithClock(
	sources []Source,
	cache Cache,
	repo HealthRepo,
	cfg ManagerConfig,
	logger *zap.Logger,
	now func() time.Time,
) Manager {
	mgr := NewManager(sources, cache, repo, cfg, logger).(*manager)
	if now != nil {
		mgr.nowFunc = now
		for _, b := range mgr.breakers {
			b.nowFunc = now
		}
	}
	return mgr
}

// ListSources returns the source names in the order they are tried.
func (m *manager) ListSources() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.breakers))
	for _, b := range m.breakers {
		names = append(names, b.Source().Name())
	}
	return names
}

// BreakerSnapshot is a per-source summary exposed by HealthCheck /
// the /api/v1/datasource/health route.
type BreakerSnapshot struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	FailureCount int    `json:"failure_count"`
}

// BreakerSnapshots returns a snapshot of every per-source breaker.
func (m *manager) BreakerSnapshots() []BreakerSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]BreakerSnapshot, 0, len(m.breakers))
	for _, b := range m.breakers {
		out = append(out, BreakerSnapshot{
			Name:         b.Source().Name(),
			State:        b.StateName(),
			FailureCount: b.FailureCount(),
		})
	}
	return out
}

// GetQuote returns a real-time quote, preferring the cache and
// falling back to the source chain.
func (m *manager) GetQuote(ctx context.Context, stockCode string) (*Quote, error) {
	if stockCode == "" {
		return nil, fmt.Errorf("datasource: GetQuote: empty stockCode")
	}
	key := QuoteCacheKey(stockCode)

	if m.cache != nil {
		raw, err := m.cache.Get(ctx, key)
		if err == nil && raw != "" {
			var q Quote
			if jerr := json.Unmarshal([]byte(raw), &q); jerr == nil {
				return &q, nil
			}
		}
	}

	q, used, err := m.callQuote(ctx, stockCode)
	if err != nil {
		return nil, err
	}
	_ = used

	if m.cache != nil && q != nil {
		if buf, jerr := json.Marshal(q); jerr == nil {
			_ = m.cache.Set(ctx, key, string(buf), m.cfg.QuoteTTL)
		}
	}
	return q, nil
}

// GetKLine returns K-line candles for the given window. Results are
// cached per trade date.
func (m *manager) GetKLine(ctx context.Context, stockCode, period string, start, end time.Time) ([]KLine, error) {
	if stockCode == "" {
		return nil, fmt.Errorf("datasource: GetKLine: empty stockCode")
	}
	if period == "" {
		return nil, fmt.Errorf("datasource: GetKLine: empty period")
	}
	if !end.After(start) {
		return nil, fmt.Errorf("datasource: GetKLine: end <= start")
	}

	days := splitByDay(start, end)
	if len(days) == 0 {
		return nil, fmt.Errorf("datasource: GetKLine: no days in range")
	}

	merged := make([]KLine, 0, 64)
	missing := make([]timeRange, 0)

	if m.cache != nil {
		for _, d := range days {
			dateStr := d.start.Format("2006-01-02")
			key := KLineCacheKey(stockCode, period, dateStr)
			raw, err := m.cache.Get(ctx, key)
			if err == nil && raw != "" {
				var day []KLine
				if jerr := json.Unmarshal([]byte(raw), &day); jerr == nil {
					merged = append(merged, day...)
					continue
				}
			}
			missing = append(missing, d)
		}
	} else {
		missing = days
	}

	// Coalesce all cache-missing days into ONE ranged fetch instead
	// of one HTTP call per day. The old per-day loop fired ~60 calls
	// for a 60-day window, which is exactly what trips a provider's
	// rate limiter (eastmoney closes the connection after the first
	// few rapid hits). One ranged call per (stock, fetch) is both
	// faster and far less likely to be throttled. Results are then
	// bucketed back per-day so the per-day cache still works.
	if len(missing) > 0 {
		lo := missing[0].start
		hi := missing[0].end
		for _, d := range missing[1:] {
			if d.start.Before(lo) {
				lo = d.start
			}
			if d.end.After(hi) {
				hi = d.end
			}
		}
		klines, err := m.callKLine(ctx, stockCode, period, lo, hi)
		if err != nil {
			if len(merged) > 0 {
				return merged, fmt.Errorf("datasource: GetKLine: partial: %w", err)
			}
			return nil, err
		}

		// Bucket returned bars by calendar day for both the merge
		// and the per-day cache write.
		byDay := make(map[string][]KLine, len(missing))
		for _, k := range klines {
			ds := k.Timestamp.Format("2006-01-02")
			byDay[ds] = append(byDay[ds], k)
		}
		merged = append(merged, klines...)

		if m.cache != nil {
			// Cache every MISSING day — including days the provider
			// returned nothing for (weekends / holidays / halts) as
			// an empty slice, so we never refetch them and never
			// fail a future range just because one day is barless.
			for _, d := range missing {
				dateStr := d.start.Format("2006-01-02")
				key := KLineCacheKey(stockCode, period, dateStr)
				if buf, jerr := json.Marshal(byDay[dateStr]); jerr == nil {
					_ = m.cache.Set(ctx, key, string(buf), m.cfg.CacheTTL)
				}
			}
		}
	}

	return merged, nil
}

// GetFundamental returns a fundamental snapshot, with 24h cache.
func (m *manager) GetFundamental(ctx context.Context, stockCode string) (*Fundamental, error) {
	if stockCode == "" {
		return nil, fmt.Errorf("datasource: GetFundamental: empty stockCode")
	}
	key := FundamentalCacheKey(stockCode)

	if m.cache != nil {
		raw, err := m.cache.Get(ctx, key)
		if err == nil && raw != "" {
			var f Fundamental
			if jerr := json.Unmarshal([]byte(raw), &f); jerr == nil {
				return &f, nil
			}
		}
	}

	f, err := m.callFundamental(ctx, stockCode)
	if err != nil {
		return nil, err
	}

	if m.cache != nil && f != nil {
		if buf, jerr := json.Marshal(f); jerr == nil {
			_ = m.cache.Set(ctx, key, string(buf), m.cfg.CacheTTL)
		}
	}
	return f, nil
}

// GetNews returns up to limit news items. Not cached.
func (m *manager) GetNews(ctx context.Context, stockCode string, limit int) ([]News, error) {
	if stockCode == "" {
		return nil, fmt.Errorf("datasource: GetNews: empty stockCode")
	}
	if limit <= 0 {
		limit = 10
	}
	news, err := m.callNews(ctx, stockCode, limit)
	if err != nil {
		return nil, err
	}
	return news, nil
}

// HealthCheck pings every configured source and refreshes the
// DatasourceHealth rows. Returns the first error encountered (if any),
// but still attempts to record all sources.
func (m *manager) HealthCheck(ctx context.Context) error {
	m.mu.RLock()
	breakers := make([]*Breaker, len(m.breakers))
	copy(breakers, m.breakers)
	m.mu.RUnlock()

	var firstErr error
	for _, b := range breakers {
		s := b.Source()
		_, err := s.GetQuote(ctx, "000000")
		if err != nil {
			if m.repo != nil {
				if rerr := m.repo.RecordFailure(ctx, s.Name(), err.Error()); rerr != nil {
					m.logger.Warn("health repo write failed",
						zap.String("source", s.Name()),
						zap.Error(rerr),
					)
				}
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("datasource: health %q: %w", s.Name(), err)
			}
			continue
		}
		if m.repo != nil {
			if rerr := m.repo.RecordSuccess(ctx, s.Name()); rerr != nil {
				m.logger.Warn("health repo write failed",
					zap.String("source", s.Name()),
					zap.Error(rerr),
				)
			}
		}
	}
	return firstErr
}

// =============================================================================
// Internal helpers
// =============================================================================

// timeRange is a half-open [start, end) window for one trading day.
type timeRange struct {
	start, end time.Time
}

// splitByDay partitions [start, end) into per-day windows. A caller
// that wants a single day back gets a single window. This matches
// the spec's (stock_code, period, trade_date) cache key.
func splitByDay(start, end time.Time) []timeRange {
	out := make([]timeRange, 0)
	if !end.After(start) {
		return out
	}
	cur := start
	for cur.Before(end) {
		next := time.Date(cur.Year(), cur.Month(), cur.Day()+1, 0, 0, 0, 0, cur.Location())
		if next.After(end) {
			next = end
		}
		out = append(out, timeRange{start: cur, end: next})
		cur = next
	}
	return out
}

// routeOnce encapsulates the per-call failure bookkeeping: try
// `call`, record success/failure, record switch events. Returns
// (result, usedSourceName, error).
func (m *manager) routeOnce(
	ctx context.Context,
	prevSource string,
	b *Breaker,
	call func(ctx context.Context, s Source) (interface{}, error),
) (interface{}, string, error) {
	if !b.Allow() {
		m.logger.Debug("skipping source (breaker open)",
			zap.String("source", b.Source().Name()),
			zap.String("state", b.StateName()),
		)
		return nil, "", errSkipped
	}
	s := b.Source()
	v, err := call(ctx, s)
	if err != nil {
		b.RecordFailure()
		if m.repo != nil {
			if rerr := m.repo.RecordFailure(ctx, s.Name(), err.Error()); rerr != nil {
				m.logger.Warn("repo write failed",
					zap.String("source", s.Name()),
					zap.Error(rerr),
				)
			}
		}
		if prevSource != "" && prevSource != s.Name() {
			if m.repo != nil {
				_ = m.repo.RecordSwitch(ctx, prevSource, s.Name(),
					fmt.Sprintf("fallback after error: %v", err))
			}
		}
		return nil, s.Name(), err
	}
	b.RecordSuccess()
	if m.repo != nil {
		if rerr := m.repo.RecordSuccess(ctx, s.Name()); rerr != nil {
			m.logger.Warn("repo write failed",
				zap.String("source", s.Name()),
				zap.Error(rerr),
			)
		}
	}
	if prevSource != "" && prevSource != s.Name() {
		if m.repo != nil {
			_ = m.repo.RecordSwitch(ctx, prevSource, s.Name(), "successful fallback")
		}
	}
	return v, s.Name(), nil
}

// errSkipped indicates the breaker for a given source is open; the
// caller moves on to the next source.
var errSkipped = fmt.Errorf("datasource: source skipped (breaker open)")

// callQuote is the typed routing helper for GetQuote.
func (m *manager) callQuote(ctx context.Context, stockCode string) (*Quote, string, error) {
	m.mu.RLock()
	breakers := make([]*Breaker, len(m.breakers))
	copy(breakers, m.breakers)
	m.mu.RUnlock()

	var firstErr error
	prevSource := ""
	for _, b := range breakers {
		v, name, err := m.routeOnce(ctx, prevSource, b, func(ctx context.Context, s Source) (interface{}, error) {
			return s.GetQuote(ctx, stockCode)
		})
		if err == nil {
			q, _ := v.(*Quote)
			return q, name, nil
		}
		if err != errSkipped {
			prevSource = name
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr == nil {
		firstErr = ErrAllSourcesDown
	}
	return nil, "", fmt.Errorf("datasource: GetQuote all sources failed: %w: %w", ErrAllSourcesDown, firstErr)
}

// callKLine is the typed routing helper for GetKLine.
func (m *manager) callKLine(ctx context.Context, stockCode, period string, start, end time.Time) ([]KLine, error) {
	m.mu.RLock()
	breakers := make([]*Breaker, len(m.breakers))
	copy(breakers, m.breakers)
	m.mu.RUnlock()

	var firstErr error
	prevSource := ""
	for _, b := range breakers {
		v, name, err := m.routeOnce(ctx, prevSource, b, func(ctx context.Context, s Source) (interface{}, error) {
			return s.GetKLine(ctx, stockCode, period, start, end)
		})
		if err == nil {
			klines, _ := v.([]KLine)
			// Tag each line with its source for downstream
			// attribution.
			for i := range klines {
				if klines[i].Source == "" {
					klines[i].Source = name
				}
			}
			return klines, nil
		}
		if err != errSkipped {
			prevSource = name
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr == nil {
		firstErr = ErrAllSourcesDown
	}
	return nil, fmt.Errorf("datasource: GetKLine all sources failed: %w: %w", ErrAllSourcesDown, firstErr)
}

// callFundamental is the typed routing helper for GetFundamental.
func (m *manager) callFundamental(ctx context.Context, stockCode string) (*Fundamental, error) {
	m.mu.RLock()
	breakers := make([]*Breaker, len(m.breakers))
	copy(breakers, m.breakers)
	m.mu.RUnlock()

	var firstErr error
	prevSource := ""
	for _, b := range breakers {
		v, name, err := m.routeOnce(ctx, prevSource, b, func(ctx context.Context, s Source) (interface{}, error) {
			return s.GetFundamental(ctx, stockCode)
		})
		if err == nil {
			f, _ := v.(*Fundamental)
			return f, nil
		}
		if err != errSkipped {
			prevSource = name
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr == nil {
		firstErr = ErrAllSourcesDown
	}
	return nil, fmt.Errorf("datasource: GetFundamental all sources failed: %w: %w", ErrAllSourcesDown, firstErr)
}

// callNews is the typed routing helper for GetNews.
func (m *manager) callNews(ctx context.Context, stockCode string, limit int) ([]News, error) {
	m.mu.RLock()
	breakers := make([]*Breaker, len(m.breakers))
	copy(breakers, m.breakers)
	m.mu.RUnlock()

	var firstErr error
	prevSource := ""
	for _, b := range breakers {
		v, name, err := m.routeOnce(ctx, prevSource, b, func(ctx context.Context, s Source) (interface{}, error) {
			return s.GetNews(ctx, stockCode, limit)
		})
		if err == nil {
			news, _ := v.([]News)
			return news, nil
		}
		if err != errSkipped {
			prevSource = name
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr == nil {
		firstErr = ErrAllSourcesDown
	}
	return nil, fmt.Errorf("datasource: GetNews all sources failed: %w: %w", ErrAllSourcesDown, firstErr)
}
