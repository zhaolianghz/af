// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource defines the unified market-data adapter layer for the
// AF backend. It exposes a stable Quote / KLine / Fundamental / News
// interface that the rest of the system talks to, and a Manager that
// routes calls to a configurable chain of underlying Source
// implementations (eastmoney, sina, akshare, …).
//
// A3 implements the manager, the circuit-breaker, the cache wrapper, the
// health repo, and the three baseline sources. Everything in this
// package talks through interfaces, so tests do not need a real Redis or
// database.
package datasource

import (
	"context"
	"errors"
	"time"
)

// ErrAllSourcesDown is returned by the Manager when every underlying
// source has failed. Callers (typically the alert pipeline, see
// tasks.md §3.10) should treat this as "all-sources-down" and trigger
// the global alarm.
var ErrAllSourcesDown = errors.New("datasource: all sources failed")

// Quote is the normalized real-time snapshot returned by every
// underlying source. The Source field is filled in by the source itself
// so the manager can attribute successes/failures to the right backend.
type Quote struct {
	StockCode string    `json:"stock_code"`
	StockName string    `json:"stock_name"`
	Price     float64   `json:"price"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	PrevClose float64   `json:"prev_close"`
	Volume    int64     `json:"volume"`
	Amount    float64   `json:"amount"`
	Bid5      []BidAsk  `json:"bid5"`
	Ask5      []BidAsk  `json:"ask5"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
}

// BidAsk represents a single level of the five-level bid/ask ladder.
type BidAsk struct {
	Price  float64 `json:"price"`
	Volume int64   `json:"volume"`
}

// KLine is one OHLCV candle. Source is set by the underlying source.
type KLine struct {
	StockCode string    `json:"stock_code"`
	Period    string    `json:"period"`
	Timestamp time.Time `json:"timestamp"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    int64     `json:"volume"`
	Amount    float64   `json:"amount"`
	Source    string    `json:"source"`
}

// Fundamental is the normalized fundamental snapshot (used for PE / PB /
// ROE driven selection rules). Real implementations may leave zero
// values for fields their source does not cover.
type Fundamental struct {
	StockCode     string    `json:"stock_code"`
	StockName     string    `json:"stock_name"`
	PE            float64   `json:"pe"`
	PB            float64   `json:"pb"`
	ROE           float64   `json:"roe"`
	DividendYield float64   `json:"dividend_yield"`
	Revenue       float64   `json:"revenue"`
	NetProfit     float64   `json:"net_profit"`
	UpdateTime    time.Time `json:"update_time"`
	Source        string    `json:"source"`
}

// News is a normalized news article. StockCode is optional because some
// news is market-wide rather than stock-specific.
type News struct {
	StockCode   string    `json:"stock_code,omitempty"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
	Source      string    `json:"source"`
}

// Source is the contract every underlying data provider must
// implement. Implementations are expected to be safe for concurrent
// use; the manager calls into the same instance from many goroutines.
type Source interface {
	// Name returns the source identifier used in logs, health records
	// and ProviderSwitchEvents (e.g. "eastmoney", "sina",
	// "akshare"). Must be stable across calls.
	Name() string

	// GetQuote returns a real-time quote for stockCode. A non-nil
	// error aborts the manager's fallback chain.
	GetQuote(ctx context.Context, stockCode string) (*Quote, error)

	// GetKLine returns candles in the half-open interval [start, end)
	// for the given period. Periods follow the A-share convention
	// (1m / 5m / 15m / 30m / 60m / 1d / 1w / 1M).
	GetKLine(ctx context.Context, stockCode, period string, start, end time.Time) ([]KLine, error)

	// GetFundamental returns the most recent fundamental snapshot.
	// May return ErrNotImplemented for sources that do not cover
	// fundamentals.
	GetFundamental(ctx context.Context, stockCode string) (*Fundamental, error)

	// GetNews returns up to limit news items, newest first.
	// May return ErrNotImplemented for sources that do not cover
	// news.
	GetNews(ctx context.Context, stockCode string, limit int) ([]News, error)

	// IsHealthy reports the in-process liveness opinion of the
	// source. The manager uses this as a fast-path filter before
	// attempting a real call.
	IsHealthy() bool
}

// ErrNotImplemented is returned by Source methods that a particular
// provider does not implement. The manager treats this as a permanent
// failure for the request and moves on.
var ErrNotImplemented = errors.New("datasource: not implemented by source")

// Cache is the minimal Redis surface the manager depends on. Tests can
// substitute an in-memory implementation.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Ping(ctx context.Context) error
}

// Manager is the public API the rest of the system uses. It is
// constructed via NewManager and routes calls to a configurable chain
// of sources, with circuit-breaker failover, caching, and health
// recording.
type Manager interface {
	GetQuote(ctx context.Context, stockCode string) (*Quote, error)
	GetKLine(ctx context.Context, stockCode, period string, start, end time.Time) ([]KLine, error)
	GetFundamental(ctx context.Context, stockCode string) (*Fundamental, error)
	GetNews(ctx context.Context, stockCode string, limit int) ([]News, error)

	// ListSources returns the configured source names in the order
	// they are tried.
	ListSources() []string

	// BreakerSnapshots returns a snapshot of every per-source
	// breaker for /api/v1/datasource/health.
	BreakerSnapshots() []BreakerSnapshot

	// HealthCheck forces a ping of every configured source and
	// refreshes the DatasourceHealth rows. Returns an error only on
	// systemic problems (e.g. repo write failure).
	HealthCheck(ctx context.Context) error
}
