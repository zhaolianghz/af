// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — redis_cache.go
//
// A production-grade Cache implementation backed by go-redis/v9, plus
// an in-memory implementation used by the test suite. The two share
// the same interface (Cache) so callers can substitute freely.
package datasource

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache implements Cache using a *redis.Client. The zero value is
// not usable; construct via NewRedisCache.
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache wraps an existing *redis.Client. The client is owned
// by the caller; closing the client is the caller's responsibility.
func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

// NewRedisCacheFromURL is a convenience for tests and one-off setups.
func NewRedisCacheFromURL(url string) (*RedisCache, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return &RedisCache{client: redis.NewClient(opts)}, nil
}

// Get implements Cache.
func (c *RedisCache) Get(ctx context.Context, key string) (string, error) {
	v, err := c.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil // cache miss, not an error
	}
	if err != nil {
		return "", fmt.Errorf("redis get %q: %w", key, err)
	}
	return v, nil
}

// Set implements Cache.
func (c *RedisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := c.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("redis set %q: %w", key, err)
	}
	return nil
}

// Del implements Cache.
func (c *RedisCache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := c.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	return nil
}

// Ping implements Cache.
func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close releases the underlying redis client.
func (c *RedisCache) Close() error {
	return c.client.Close()
}

// =============================================================================
// In-memory Cache (used by tests and as a no-op Redis substitute).
// =============================================================================

// MemoryCache is a goroutine-safe in-memory Cache. Values are stored
// verbatim; expirations are enforced lazily on Get. Suitable for
// tests; do not use in production.
type MemoryCache struct {
	mu   sync.Mutex
	data map[string]memoryCacheEntry
	now  func() time.Time
}

type memoryCacheEntry struct {
	value     string
	expiresAt time.Time
}

// NewMemoryCache returns an empty in-memory cache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{data: make(map[string]memoryCacheEntry), now: time.Now}
}

// NewMemoryCacheWithClock is like NewMemoryCache but with an injected
// clock.
func NewMemoryCacheWithClock(now func() time.Time) *MemoryCache {
	if now == nil {
		now = time.Now
	}
	return &MemoryCache{data: make(map[string]memoryCacheEntry), now: now}
}

// Get implements Cache.
func (c *MemoryCache) Get(ctx context.Context, key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.data[key]
	if !ok {
		return "", nil
	}
	if !c.now().Before(e.expiresAt) {
		delete(c.data, key)
		return "", nil
	}
	return e.value, nil
}

// Set implements Cache.
func (c *MemoryCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ttl <= 0 {
		// A non-positive TTL means "expire immediately", which we
		// model as "store for a microsecond". Callers should not
		// pass <= 0 in practice; this avoids accidentally
		// persisting forever.
		ttl = time.Microsecond
	}
	c.data[key] = memoryCacheEntry{value: value, expiresAt: c.now().Add(ttl)}
	return nil
}

// Del implements Cache.
func (c *MemoryCache) Del(ctx context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		delete(c.data, k)
	}
	return nil
}

// Ping implements Cache. Always succeeds.
func (c *MemoryCache) Ping(ctx context.Context) error { return nil }
