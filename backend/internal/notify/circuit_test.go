// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package notify

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_Defaults(t *testing.T) {
	b := NewCircuitBreaker(CircuitConfig{})
	assert.Equal(t, 5, b.cfg.FailureThreshold)
	assert.Equal(t, 5*time.Minute, b.cfg.Window)
	assert.Equal(t, 10*time.Minute, b.cfg.Cooldown)
	assert.Equal(t, StateClosed, b.State())
	assert.True(t, b.IsHealthy())
}

func TestCircuitBreaker_OpensAfterThresholdFailures(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	b := NewCircuitBreaker(CircuitConfig{
		FailureThreshold: 3,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
		Now:              func() time.Time { return now },
	})

	// 2 failures should not trip the breaker.
	for i := 0; i < 2; i++ {
		ok, err := b.Allow()
		require.NoError(t, err)
		require.True(t, ok)
		b.OnFailure()
		assert.Equal(t, StateClosed, b.State(), "after %d failures, breaker must stay closed", i+1)
	}

	// 3rd failure trips it.
	ok, err := b.Allow()
	require.NoError(t, err)
	require.True(t, ok)
	b.OnFailure()
	assert.Equal(t, StateOpen, b.State())
	assert.False(t, b.IsHealthy())
}

func TestCircuitBreaker_StaysOpenDuringCooldown(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	b := NewCircuitBreaker(CircuitConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
		Now:              func() time.Time { return now },
	})

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		ok, _ := b.Allow()
		require.True(t, ok)
		b.OnFailure()
	}
	require.Equal(t, StateOpen, b.State())

	// Move forward 5 minutes — still inside cooldown.
	now = now.Add(5 * time.Minute)
	ok, err := b.Allow()
	assert.False(t, ok)
	assert.True(t, errors.Is(err, ErrCircuitOpen))
	assert.Equal(t, StateOpen, b.State())
}

func TestCircuitBreaker_HalfOpenAfterCooldown(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	b := NewCircuitBreaker(CircuitConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
		Now:              func() time.Time { return now },
	})

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		ok, _ := b.Allow()
		require.True(t, ok)
		b.OnFailure()
	}
	require.Equal(t, StateOpen, b.State())

	// Move past the cooldown.
	now = now.Add(10*time.Minute + time.Second)

	// First Allow after cooldown must be admitted as the probe.
	ok, err := b.Allow()
	require.NoError(t, err)
	require.True(t, ok, "cooldown elapsed → probe should be allowed")
	assert.Equal(t, StateHalfOpen, b.State())

	// While the probe is in flight, concurrent traffic is rejected.
	ok2, err2 := b.Allow()
	assert.False(t, ok2)
	assert.True(t, errors.Is(err2, ErrCircuitOpen))

	// Probe success closes the breaker.
	b.OnSuccess()
	assert.Equal(t, StateClosed, b.State())
	assert.True(t, b.IsHealthy())

	// After closing, traffic flows again.
	ok, err = b.Allow()
	assert.NoError(t, err)
	assert.True(t, ok)
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	b := NewCircuitBreaker(CircuitConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
		Now:              func() time.Time { return now },
	})

	for i := 0; i < 2; i++ {
		ok, _ := b.Allow()
		require.True(t, ok)
		b.OnFailure()
	}
	require.Equal(t, StateOpen, b.State())

	now = now.Add(15 * time.Minute) // past cooldown
	ok, err := b.Allow()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, StateHalfOpen, b.State())

	// Probe fails → back to open with a fresh cooldown.
	b.OnFailure()
	assert.Equal(t, StateOpen, b.State())
	assert.False(t, b.IsHealthy())
}

func TestCircuitBreaker_OldFailuresExpire(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	b := NewCircuitBreaker(CircuitConfig{
		FailureThreshold: 3,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
		Now:              func() time.Time { return now },
	})

	// 2 failures in the window.
	for i := 0; i < 2; i++ {
		ok, _ := b.Allow()
		require.True(t, ok)
		b.OnFailure()
	}
	// Advance past the window so these failures age out.
	now = now.Add(6 * time.Minute)

	// New failure — only 1 in the trailing window, must not trip.
	ok, _ := b.Allow()
	require.True(t, ok)
	b.OnFailure()
	assert.Equal(t, StateClosed, b.State())
}

func TestCircuitBreaker_ConcurrentSafe(t *testing.T) {
	b := NewCircuitBreaker(CircuitConfig{
		FailureThreshold: 100, // high so the breaker won't trip
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ok, _ := b.Allow()
				if ok {
					if j%2 == 0 {
						b.OnSuccess()
					} else {
						b.OnFailure()
					}
				}
			}
		}()
	}
	wg.Wait()
	// No assertion on final state — we just want no race / panic.
	// Run with `-race` to verify.
}

func TestCircuitState_String(t *testing.T) {
	assert.Equal(t, "closed", StateClosed.String())
	assert.Equal(t, "open", StateOpen.String())
	assert.Equal(t, "half-open", StateHalfOpen.String())
	assert.Equal(t, "unknown", CircuitState(99).String())
}

func TestCircuitBreaker_StateMethodLazyTransitions(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	b := NewCircuitBreaker(CircuitConfig{
		FailureThreshold: 1,
		Window:           5 * time.Minute,
		Cooldown:         time.Minute,
		Now:              func() time.Time { return now },
	})

	ok, _ := b.Allow()
	require.True(t, ok)
	b.OnFailure()
	require.Equal(t, StateOpen, b.State())

	// Move past cooldown; State() should report half-open without
	// needing an Allow() to trigger the transition.
	now = now.Add(2 * time.Minute)
	assert.Equal(t, StateHalfOpen, b.State())
}
