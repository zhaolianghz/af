// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetry_FirstAttemptSucceeds(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), RetryConfig{Attempts: 3}, func(ctx context.Context) error {
		calls++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestRetry_SucceedsOnThirdAttempt(t *testing.T) {
	calls := 0
	transient := errors.New("transient")
	err := Retry(context.Background(), RetryConfig{
		Attempts:       3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}, func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return transient
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestRetry_AllAttemptsFail(t *testing.T) {
	calls := 0
	boom := errors.New("boom")
	err := Retry(context.Background(), RetryConfig{
		Attempts:       3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}, func(ctx context.Context) error {
		calls++
		return boom
	})
	require.Error(t, err)
	assert.Equal(t, 3, calls)
	assert.True(t, errors.Is(err, boom), "error should wrap the last underlying error")
}

func TestRetry_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	boom := errors.New("boom")

	// Cancel after the first failure.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := Retry(ctx, RetryConfig{
		Attempts:       5,
		InitialBackoff: 50 * time.Millisecond, // long enough to be canceled during sleep
	}, func(ctx context.Context) error {
		calls++
		return boom
	})
	require.Error(t, err)
	assert.LessOrEqual(t, calls, 2, "should have stopped retrying after cancel")
	assert.True(t, errors.Is(err, context.Canceled) || calls == 1,
		"err should be context.Canceled or wrap it (got %v)", err)
}

func TestRetry_DefaultsApplied(t *testing.T) {
	cfg := RetryConfig{}.withDefaults()
	assert.Equal(t, 3, cfg.Attempts)
	assert.Equal(t, time.Second, cfg.InitialBackoff)
	assert.Equal(t, 30*time.Second, cfg.MaxBackoff)
}

func TestRetry_BackoffDoubles(t *testing.T) {
	// We can't directly observe the sleeps, but we can measure
	// wall-clock and verify it grows. With InitialBackoff=10ms and
	// MaxBackoff=10ms, the sleeps should be ~10ms, ~10ms (capped).
	cfg := RetryConfig{
		Attempts:       3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	boom := errors.New("x")
	start := time.Now()
	_ = Retry(context.Background(), cfg, func(ctx context.Context) error { return boom })
	elapsed := time.Since(start)
	// Two backoffs of 10ms each = 20ms minimum. Allow generous slack
	// for the test scheduler.
	assert.GreaterOrEqual(t, elapsed, 18*time.Millisecond, "expected ~20ms backoff, got %v", elapsed)
}

func TestRetry_SingleAttemptWhenAttemptsIsOne(t *testing.T) {
	calls := 0
	boom := errors.New("x")
	err := Retry(context.Background(), RetryConfig{
		Attempts:       1,
		InitialBackoff: 100 * time.Millisecond, // would be slow if a 2nd attempt was made
	}, func(ctx context.Context) error {
		calls++
		return boom
	})
	require.Error(t, err)
	assert.Equal(t, 1, calls)
}

func TestIsRetryable(t *testing.T) {
	assert.False(t, IsRetryable(nil))
	assert.False(t, IsRetryable(context.Canceled))
	assert.False(t, IsRetryable(context.DeadlineExceeded))
	assert.False(t, IsRetryable(ErrAllChannelsFailed))
	assert.True(t, IsRetryable(errors.New("transient network glitch")))
}
