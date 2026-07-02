// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — manager_test.go
package datasource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Manager tests
// =============================================================================

func TestManagerGetQuotePrimarySuccess(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("eastmoney", &Quote{Price: 1820.0, StockName: "Kweichow Moutai"})
	secondary := newFakeSource("sina", &Quote{Price: 1820.5, StockName: "Kweichow Moutai"})

	repo := newRecordingRepo()
	mgr := NewManager([]Source{primary, secondary}, NewMemoryCache(), repo, ManagerConfig{}, nil)

	q, err := mgr.GetQuote(context.Background(), "600519")
	require.NoError(t, err)
	require.NotNil(t, q)
	assert.Equal(t, "eastmoney", q.Source)
	assert.Equal(t, int64(1), primary.quoteCalls.Load())
	assert.Equal(t, int64(0), secondary.quoteCalls.Load(), "secondary should not be hit when primary succeeds")
}

func TestManagerGetQuoteFallback(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("eastmoney", &Quote{Price: 1820.0})
	primary.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		return nil, errors.New("primary timeout")
	})
	secondary := newFakeSource("sina", &Quote{Price: 1820.5})

	repo := newRecordingRepo()
	mgr := NewManager([]Source{primary, secondary}, NewMemoryCache(), repo, ManagerConfig{}, nil)

	q, err := mgr.GetQuote(context.Background(), "600519")
	require.NoError(t, err)
	require.NotNil(t, q)
	assert.Equal(t, "sina", q.Source)
	assert.Equal(t, int64(1), primary.quoteCalls.Load())
	assert.Equal(t, int64(1), secondary.quoteCalls.Load())
	assert.Contains(t, repo.failures, "eastmoney:primary timeout")
	assert.Contains(t, repo.successes, "sina")
	// A switch from eastmoney to sina should be recorded.
	assert.NotEmpty(t, repo.switches, "switch event should be recorded on fallback")
}

func TestManagerGetQuoteAllSourcesDown(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("eastmoney", nil)
	primary.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		return nil, errors.New("boom")
	})
	secondary := newFakeSource("sina", nil)
	secondary.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		return nil, errors.New("kaboom")
	})

	repo := newRecordingRepo()
	mgr := NewManager([]Source{primary, secondary}, NewMemoryCache(), repo, ManagerConfig{}, nil)

	_, err := mgr.GetQuote(context.Background(), "600519")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrAllSourcesDown)
	assert.Equal(t, int64(1), primary.quoteCalls.Load())
	assert.Equal(t, int64(1), secondary.quoteCalls.Load())
}

func TestManagerGetQuoteCacheHitSkipsSource(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("eastmoney", &Quote{Price: 1820.0})

	cache := NewMemoryCache()
	repo := newRecordingRepo()
	mgr := NewManager([]Source{primary}, cache, repo, ManagerConfig{}, nil)

	// Prime the cache directly.
	primed := &Quote{StockCode: "600519", Price: 1820.0, Source: "cache"}
	buf, _ := jsonMarshal(primed)
	require.NoError(t, cache.Set(context.Background(), QuoteCacheKey("600519"), string(buf), time.Minute))

	q, err := mgr.GetQuote(context.Background(), "600519")
	require.NoError(t, err)
	assert.Equal(t, "cache", q.Source, "should return the cached value as-is")
	assert.Equal(t, int64(0), primary.quoteCalls.Load(), "no source call on cache hit")
}

func TestManagerCachePopulatesOnSuccess(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("eastmoney", &Quote{Price: 1820.0})
	cache := NewMemoryCache()
	repo := newRecordingRepo()
	mgr := NewManager([]Source{primary}, cache, repo, ManagerConfig{QuoteTTL: time.Minute}, nil)

	_, err := mgr.GetQuote(context.Background(), "600519")
	require.NoError(t, err)

	// Cache should now contain the entry.
	raw, err := cache.Get(context.Background(), QuoteCacheKey("600519"))
	require.NoError(t, err)
	assert.NotEmpty(t, raw)
}

func TestManagerBreakerOpenSkipsSource(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 9, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	primary := newFakeSource("eastmoney", nil)
	primary.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		return nil, errors.New("boom")
	})
	secondary := newFakeSource("sina", &Quote{Price: 1820.0})

	// Use a memory cache tied to the same injected clock so the
	// quote TTL expires as we advance time.
	cache := NewMemoryCacheWithClock(clock)
	repo := newRecordingRepo()
	mgr := NewManagerWithClock([]Source{primary, secondary}, cache, repo, ManagerConfig{
		FailThreshold: 2,
		FailWindow:    time.Minute,
		Cooldown:      time.Minute,
		QuoteTTL:      5 * time.Second,
	}, nil, clock)

	// Trip the primary breaker.
	_, _ = mgr.GetQuote(context.Background(), "600519")
	now = now.Add(10 * time.Second) // cache expired, breaker still closed
	_, _ = mgr.GetQuote(context.Background(), "600519")

	// Now the primary breaker should be open; the third call
	// should skip primary entirely.
	primary.quoteCalls.Store(0)
	secondary.quoteCalls.Store(0)
	now = now.Add(10 * time.Second) // cache expired again
	q, err := mgr.GetQuote(context.Background(), "600519")
	require.NoError(t, err)
	assert.Equal(t, "sina", q.Source)
	assert.Equal(t, int64(0), primary.quoteCalls.Load(), "primary should be skipped while breaker is open")
	assert.Equal(t, int64(1), secondary.quoteCalls.Load())
}

func TestManagerListSources(t *testing.T) {
	t.Parallel()
	a := newFakeSource("eastmoney", &Quote{})
	b := newFakeSource("sina", &Quote{})
	mgr := NewManager([]Source{a, b}, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	assert.Equal(t, []string{"eastmoney", "sina"}, mgr.ListSources())
}

func TestManagerGetKLineCache(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("eastmoney", nil)
	primary.setKLineFn(func(ctx context.Context, code, period string, start, end time.Time) ([]KLine, error) {
		return []KLine{
			{StockCode: code, Period: period, Timestamp: start, Open: 1, Close: 2, High: 3, Low: 0.5, Volume: 100},
		}, nil
	})
	mgr := NewManager([]Source{primary}, NewMemoryCache(), newRecordingRepo(), ManagerConfig{CacheTTL: time.Minute}, nil)

	start := time.Date(2025, 6, 9, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	// First call: cache miss.
	kl, err := mgr.GetKLine(context.Background(), "600519", "1d", start, end)
	require.NoError(t, err)
	require.Len(t, kl, 1)
	assert.Equal(t, int64(1), primary.klineCalls.Load())

	// Second call: cache hit, no source call.
	kl, err = mgr.GetKLine(context.Background(), "600519", "1d", start, end)
	require.NoError(t, err)
	require.Len(t, kl, 1)
	assert.Equal(t, int64(1), primary.klineCalls.Load(), "second call should be served from cache")
}

// TestManagerGetKLineCoalescesToOneCall guards the ranged-fetch fix:
// a multi-day window must hit the source exactly ONCE (one ranged
// call), not once per day. The old per-day loop fired N calls for an
// N-day window, which tripped provider rate limiters.
func TestManagerGetKLineCoalescesToOneCall(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("eastmoney", nil)
	primary.setKLineFn(func(ctx context.Context, code, period string, start, end time.Time) ([]KLine, error) {
		// Return one bar per calendar day in [start, end).
		out := make([]KLine, 0)
		for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
			out = append(out, KLine{StockCode: code, Period: period, Timestamp: d, Close: 10})
		}
		return out, nil
	})
	mgr := NewManager([]Source{primary}, NewMemoryCache(), newRecordingRepo(), ManagerConfig{CacheTTL: time.Minute}, nil)

	start := time.Date(2025, 6, 9, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 10) // 10-day window

	kl, err := mgr.GetKLine(context.Background(), "600519", "1d", start, end)
	require.NoError(t, err)
	require.Len(t, kl, 10, "should return one bar per day")
	assert.Equal(t, int64(1), primary.klineCalls.Load(), "10-day window must be ONE ranged call, not 10")

	// Fully cached now: a repeat makes zero new source calls.
	_, err = mgr.GetKLine(context.Background(), "600519", "1d", start, end)
	require.NoError(t, err)
	assert.Equal(t, int64(1), primary.klineCalls.Load(), "repeat must be fully cache-served")
}

func TestManagerGetKLineEmptyStockCode(t *testing.T) {
	t.Parallel()
	mgr := NewManager(nil, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	_, err := mgr.GetKLine(context.Background(), "", "1d", time.Now(), time.Now())
	assert.Error(t, err)
}

func TestManagerGetKLineEndNotAfterStart(t *testing.T) {
	t.Parallel()
	mgr := NewManager(nil, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	now := time.Now()
	_, err := mgr.GetKLine(context.Background(), "600519", "1d", now, now)
	assert.Error(t, err)
}

func TestManagerGetFundamentalFallback(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("eastmoney", nil)
	primary.setFundFn(func(ctx context.Context, code string) (*Fundamental, error) {
		return nil, errors.New("no data")
	})
	secondary := newFakeSource("akshare", nil)
	secondary.setFundFn(func(ctx context.Context, code string) (*Fundamental, error) {
		return &Fundamental{StockCode: code, StockName: "Kweichow Moutai", PE: 25.5, Source: "akshare"}, nil
	})

	mgr := NewManager([]Source{primary, secondary}, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	f, err := mgr.GetFundamental(context.Background(), "600519")
	require.NoError(t, err)
	assert.Equal(t, "akshare", f.Source)
	assert.Equal(t, 25.5, f.PE)
}

func TestManagerGetNewsEmptyCode(t *testing.T) {
	t.Parallel()
	mgr := NewManager(nil, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	_, err := mgr.GetNews(context.Background(), "", 5)
	assert.Error(t, err)
}

func TestManagerGetNewsSuccess(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("akshare", nil)
	primary.setNewsFn(func(ctx context.Context, code string, limit int) ([]News, error) {
		return []News{
			{Title: "A", Source: "akshare"},
			{Title: "B", Source: "akshare"},
		}, nil
	})
	mgr := NewManager([]Source{primary}, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	news, err := mgr.GetNews(context.Background(), "600519", 5)
	require.NoError(t, err)
	assert.Len(t, news, 2)
}

func TestManagerHealthCheck(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("eastmoney", &Quote{Price: 1.0})
	primary.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		// Accept the health-check placeholder code (000000)
		// but fail anything else.
		if code == "000000" {
			return &Quote{Price: 1.0, StockCode: code, StockName: "ok"}, nil
		}
		return nil, errors.New("not used")
	})
	secondary := newFakeSource("sina", nil)
	secondary.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		return nil, errors.New("always fails")
	})

	repo := newRecordingRepo()
	mgr := NewManager([]Source{primary, secondary}, NewMemoryCache(), repo, ManagerConfig{}, nil)
	err := mgr.HealthCheck(context.Background())
	assert.Error(t, err, "at least one source failed, so HealthCheck returns non-nil")
	assert.Contains(t, repo.successes, "eastmoney")
	assert.Contains(t, repo.failures, "sina:always fails")
}

func TestManagerHealthCheckAllHealthy(t *testing.T) {
	t.Parallel()
	primary := newFakeSource("eastmoney", &Quote{Price: 1.0})
	primary.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		return &Quote{Price: 1.0, StockCode: code, StockName: "ok"}, nil
	})
	repo := newRecordingRepo()
	mgr := NewManager([]Source{primary}, NewMemoryCache(), repo, ManagerConfig{}, nil)
	assert.NoError(t, mgr.HealthCheck(context.Background()))
	assert.Equal(t, []string{"eastmoney"}, repo.successes)
}

func TestManagerBreakerSnapshots(t *testing.T) {
	t.Parallel()
	a := newFakeSource("eastmoney", &Quote{})
	b := newFakeSource("sina", &Quote{})
	mgr := NewManager([]Source{a, b}, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	snaps := mgr.BreakerSnapshots()
	require.Len(t, snaps, 2)
	assert.Equal(t, "eastmoney", snaps[0].Name)
	assert.Equal(t, "sina", snaps[1].Name)
	assert.Equal(t, "closed", snaps[0].State)
}

func TestSplitByDay(t *testing.T) {
	t.Parallel()
	start := time.Date(2025, 6, 9, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 11, 0, 0, 0, 0, time.UTC)
	days := splitByDay(start, end)
	require.Len(t, days, 2)
	assert.Equal(t, start, days[0].start)
	assert.Equal(t, time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC), days[0].end)
	assert.Equal(t, time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC), days[1].start)
	assert.Equal(t, end, days[1].end)
}

func TestSplitByDayEmpty(t *testing.T) {
	t.Parallel()
	now := time.Now()
	assert.Empty(t, splitByDay(now, now))
	assert.Empty(t, splitByDay(now.Add(time.Hour), now))
}

func TestManagerConfigDefaults(t *testing.T) {
	t.Parallel()
	c := ManagerConfig{}.withDefaults()
	assert.Equal(t, 24*time.Hour, c.CacheTTL)
	assert.Equal(t, 5, c.FailThreshold)
	assert.Equal(t, 5*time.Minute, c.FailWindow)
	assert.Equal(t, 10*time.Minute, c.Cooldown)
	assert.Equal(t, 5*time.Second, c.QuoteTTL)
	assert.Equal(t, 0.005, c.ConsistencyThreshold)
}

func TestManagerGetQuoteEmptyCode(t *testing.T) {
	t.Parallel()
	mgr := NewManager(nil, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	_, err := mgr.GetQuote(context.Background(), "")
	assert.Error(t, err)
}

func TestManagerGetFundamentalEmptyCode(t *testing.T) {
	t.Parallel()
	mgr := NewManager(nil, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	_, err := mgr.GetFundamental(context.Background(), "")
	assert.Error(t, err)
}

func TestManagerGetKLineEmptyPeriod(t *testing.T) {
	t.Parallel()
	mgr := NewManager(nil, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	now := time.Now()
	_, err := mgr.GetKLine(context.Background(), "600519", "", now, now.Add(time.Hour))
	assert.Error(t, err)
}

func TestManagerNewManagerWithClock(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	primary := newFakeSource("eastmoney", &Quote{Price: 1.0})
	mgr := NewManagerWithClock([]Source{primary}, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil, clock)
	require.NotNil(t, mgr)
	// Breaker clock should be the same as the manager clock.
	mgrImpl := mgr.(*manager)
	assert.NotNil(t, mgrImpl.nowFunc)
}

// jsonMarshal is a tiny convenience to avoid pulling encoding/json
// into the test file (the manager already imports it).
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func TestValidateNilSource(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]Source{nil, newFakeSource("sina", &Quote{Price: 1.0})}, NewMemoryCache(), newRecordingRepo(), ManagerConfig{}, nil)
	require.Equal(t, []string{"sina"}, mgr.ListSources())
}

// Ensure we use errors / fmt somewhere to keep the linter happy.
var (
	_ = errors.New
	_ = fmt.Sprintf
)
