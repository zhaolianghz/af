// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — cache.go
//
// Two things live here:
//
//   - Key helpers: KLineCacheKey and QuoteCacheKey. The K-line key
//     follows the spec's (stock_code, period, trade_date) tuple, the
//     quote key is keyed on the stock code only (real-time quotes are
//     short-lived).
//   - KLineCacher: a thin wrapper around Cache that implements the
//     GetOrCompute pattern. If the cache key is present, the cached
//     value is returned; otherwise fn is called and the result is
//     written back with the configured TTL.
//
// JSON encoding is used for the cached payload so we can store
// structured data without bespoke (de)serialization.
package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// KLineCacheKey returns the canonical Redis key for a K-line entry.
// The format matches the spec: "kl:<stock_code>:<period>:<trade_date>".
// tradeDate is expected in YYYY-MM-DD form (e.g. "2025-06-09"); the
// caller is responsible for converting the source timestamp to a
// trade-date string.
func KLineCacheKey(stockCode, period, tradeDate string) string {
	return fmt.Sprintf("kl:%s:%s:%s", stockCode, period, tradeDate)
}

// QuoteCacheKey returns the canonical Redis key for a quote entry.
// TTL is expected to be very short (5s) since the value is the
// real-time price.
func QuoteCacheKey(stockCode string) string {
	return fmt.Sprintf("q:%s", stockCode)
}

// FundamentalCacheKey returns the canonical Redis key for a
// fundamental entry.
func FundamentalCacheKey(stockCode string) string {
	return fmt.Sprintf("f:%s", stockCode)
}

// KLineCacher wraps a Cache with K-line aware GetOrCompute. The
// generic payload type T lets the same cacher store any structured
// value (KLine, Quote, etc.).
type KLineCacher struct {
	Cache Cache
	TTL   time.Duration
}

// NewKLineCacher returns a cacher with a 24-hour default TTL, matching
// the spec.
func NewKLineCacher(cache Cache, ttl time.Duration) *KLineCacher {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &KLineCacher{Cache: cache, TTL: ttl}
}

// GetOrCompute tries the cache first; on a miss it calls fn, stores
// the result as JSON, and returns it. Errors from the cache are
// non-fatal: we log-and-pass-through so a Redis blip cannot wedge the
// whole manager. Errors from fn are returned to the caller untouched.
//
// The function is generic over T; in practice we use it for
// []KLine, *Quote, *Fundamental.
func GetOrCompute[T any](
	ctx context.Context,
	cache Cache,
	key string,
	ttl time.Duration,
	fn func(ctx context.Context) (T, error),
) (T, error) {
	var zero T

	if cache != nil {
		raw, err := cache.Get(ctx, key)
		if err == nil && raw != "" {
			var v T
			if jerr := json.Unmarshal([]byte(raw), &v); jerr == nil {
				return v, nil
			}
			// fall through to recompute on decode error
		}
		// Cache miss or cache error: fall through to fn().
	}

	v, err := fn(ctx)
	if err != nil {
		return zero, err
	}

	if cache != nil {
		// Best-effort write; if encode fails or the cache is down we
		// do not want to surface that to the caller.
		buf, jerr := json.Marshal(v)
		if jerr == nil {
			_ = cache.Set(ctx, key, string(buf), ttl)
		}
	}
	return v, nil
}

// GetOrComputeBytes is a byte-payload variant, useful for cases where
// the caller has already encoded the value (e.g. when the underlying
// source returns JSON that we want to forward verbatim).
func GetOrComputeBytes(
	ctx context.Context,
	cache Cache,
	key string,
	ttl time.Duration,
	fn func(ctx context.Context) ([]byte, error),
) ([]byte, error) {
	if cache != nil {
		raw, err := cache.Get(ctx, key)
		if err == nil && raw != "" {
			return []byte(raw), nil
		}
	}
	v, err := fn(ctx)
	if err != nil {
		return nil, err
	}
	if cache != nil {
		_ = cache.Set(ctx, key, string(v), ttl)
	}
	return v, nil
}
