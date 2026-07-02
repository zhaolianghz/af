// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — redis_cache_test.go
package datasource

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisCacheGetSet(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	c := NewRedisCache(client)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", "v", time.Minute))
	v, err := c.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, "v", v)
}

func TestRedisCacheGetMiss(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	c := NewRedisCache(client)
	v, err := c.Get(context.Background(), "absent")
	require.NoError(t, err)
	assert.Equal(t, "", v)
}

func TestRedisCacheTTL(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	c := NewRedisCache(client)
	require.NoError(t, c.Set(context.Background(), "k", "v", time.Minute))
	mr.FastForward(2 * time.Minute)
	v, err := c.Get(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "", v)
}

func TestRedisCacheDel(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	c := NewRedisCache(client)
	require.NoError(t, c.Set(context.Background(), "k1", "v1", time.Minute))
	require.NoError(t, c.Set(context.Background(), "k2", "v2", time.Minute))
	require.NoError(t, c.Del(context.Background(), "k1", "k2"))
	for _, k := range []string{"k1", "k2"} {
		v, _ := c.Get(context.Background(), k)
		assert.Equal(t, "", v)
	}
}

func TestRedisCachePing(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	c := NewRedisCache(client)
	assert.NoError(t, c.Ping(context.Background()))
}

func TestRedisCachePingUnreachable(t *testing.T) {
	t.Parallel()
	// Point at an unused port so Ping fails fast.
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond})
	defer client.Close()
	c := NewRedisCache(client)
	assert.Error(t, c.Ping(context.Background()))
}

func TestRedisCacheGetError(t *testing.T) {
	t.Parallel()
	// Use a closed miniredis and then build the client — but
	// miniredis.RunT cleanup will close at the end; here we use
	// a custom miniredis we own so we can close it explicitly.
	mr, err := miniredis.Run()
	require.NoError(t, err)
	addr := mr.Addr()
	mr.Close()
	client := redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond})
	defer client.Close()
	c := NewRedisCache(client)
	_, err = c.Get(context.Background(), "k")
	assert.Error(t, err)
}

func TestRedisCacheDelEmpty(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	c := NewRedisCache(client)
	assert.NoError(t, c.Del(context.Background()))
}

func TestNewRedisCacheFromURLBad(t *testing.T) {
	t.Parallel()
	_, err := NewRedisCacheFromURL("not a url")
	assert.Error(t, err)
}
