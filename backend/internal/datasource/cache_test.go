// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — cache_test.go
package datasource

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKLineCacheKey(t *testing.T) {
	t.Parallel()
	got := KLineCacheKey("600519", "1d", "2025-06-09")
	assert.Equal(t, "kl:600519:1d:2025-06-09", got)
}

func TestQuoteCacheKey(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "q:600519", QuoteCacheKey("600519"))
}

func TestFundamentalCacheKey(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "f:600519", FundamentalCacheKey("600519"))
}

func TestMemoryCacheSetGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewMemoryCache()
	require.NoError(t, c.Set(ctx, "k", "v", time.Minute))
	v, err := c.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, "v", v)
}

func TestMemoryCacheGetMiss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewMemoryCache()
	v, err := c.Get(ctx, "absent")
	require.NoError(t, err)
	assert.Equal(t, "", v, "miss should return empty string, not error")
}

func TestMemoryCacheExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewMemoryCacheWithClock(func() time.Time { return now })

	require.NoError(t, c.Set(context.Background(), "k", "v", time.Minute))
	now = now.Add(2 * time.Minute)
	v, err := c.Get(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "", v, "expired entry should be removed")
}

func TestMemoryCacheDel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewMemoryCache()
	require.NoError(t, c.Set(ctx, "k1", "v1", time.Minute))
	require.NoError(t, c.Set(ctx, "k2", "v2", time.Minute))
	require.NoError(t, c.Del(ctx, "k1"))
	v, _ := c.Get(ctx, "k1")
	assert.Equal(t, "", v)
	v, _ = c.Get(ctx, "k2")
	assert.Equal(t, "v2", v)
}

func TestMemoryCachePing(t *testing.T) {
	t.Parallel()
	assert.NoError(t, NewMemoryCache().Ping(context.Background()))
}

func TestMemoryCacheZeroTTL(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := NewMemoryCacheWithClock(clock)
	// Zero TTL should not store forever — model as 1µs.
	require.NoError(t, c.Set(context.Background(), "k", "v", 0))
	// Advance the clock past the 1µs expiration.
	now = now.Add(time.Millisecond)
	v, _ := c.Get(context.Background(), "k")
	assert.Equal(t, "", v, "zero-TTL entry should not be retrievable after the 1µs grace")
}

func TestGetOrComputeCacheMissPopulates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewMemoryCache()

	calls := 0
	v, err := GetOrCompute(ctx, c, "k", time.Minute, func(ctx context.Context) (string, error) {
		calls++
		return "computed", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "computed", v)
	assert.Equal(t, 1, calls)

	// Second call: should hit the cache, fn not called again.
	v2, err := GetOrCompute(ctx, c, "k", time.Minute, func(ctx context.Context) (string, error) {
		calls++
		return "should-not-run", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "computed", v2)
	assert.Equal(t, 1, calls, "fn should not be called on a cache hit")
}

func TestGetOrComputeWithStruct(t *testing.T) {
	t.Parallel()
	type sample struct {
		N int    `json:"n"`
		S string `json:"s"`
	}
	ctx := context.Background()
	c := NewMemoryCache()

	got, err := GetOrCompute(ctx, c, "k", time.Minute, func(ctx context.Context) (sample, error) {
		return sample{N: 42, S: "hi"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, sample{N: 42, S: "hi"}, got)

	// Verify the cache stored valid JSON.
	raw, err := c.Get(ctx, "k")
	require.NoError(t, err)
	var back sample
	require.NoError(t, json.Unmarshal([]byte(raw), &back))
	assert.Equal(t, got, back)
}

func TestGetOrComputeNilCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	v, err := GetOrCompute(ctx, nil, "k", time.Minute, func(ctx context.Context) (int, error) {
		return 7, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 7, v)
}

func TestGetOrComputeFnError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewMemoryCache()
	wantErr := errors.New("nope")
	v, err := GetOrCompute(ctx, c, "k", time.Minute, func(ctx context.Context) (int, error) {
		return 0, wantErr
	})
	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, 0, v)
	// Cache should be untouched.
	raw, _ := c.Get(ctx, "k")
	assert.Equal(t, "", raw)
}

func TestGetOrComputeBadCachedValueRecomputes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewMemoryCache()
	// Inject a non-JSON value into the cache directly.
	require.NoError(t, c.Set(ctx, "k", "not-json", time.Minute))
	v, err := GetOrCompute(ctx, c, "k", time.Minute, func(ctx context.Context) (string, error) {
		return "fallback", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "fallback", v, "bad cache payload should be replaced")
}

func TestGetOrComputeBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewMemoryCache()
	body, err := GetOrComputeBytes(ctx, c, "k", time.Minute, func(ctx context.Context) ([]byte, error) {
		return []byte("hello"), nil
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))

	// Cache hit.
	body, err = GetOrComputeBytes(ctx, c, "k", time.Minute, func(ctx context.Context) ([]byte, error) {
		return []byte("WRONG"), nil
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))
}

func TestNewKLineCacherDefaultTTL(t *testing.T) {
	t.Parallel()
	c := NewKLineCacher(NewMemoryCache(), 0)
	assert.Equal(t, 24*time.Hour, c.TTL)
}
