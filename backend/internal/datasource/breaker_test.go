// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — breaker_test.go
package datasource

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBreakerClosedToOpen(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 9, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	s := &fakeSource{name: "fake"}
	b := NewBreaker(s, 3, 5*time.Minute, 10*time.Minute, clock)

	require.Equal(t, StateClosed, b.State())
	require.True(t, b.Allow())

	b.RecordFailure()
	now = now.Add(10 * time.Second)
	require.Equal(t, StateClosed, b.State())

	b.RecordFailure()
	now = now.Add(10 * time.Second)
	require.Equal(t, StateClosed, b.State())

	b.RecordFailure()
	now = now.Add(10 * time.Second)
	require.Equal(t, StateOpen, b.State())
	require.False(t, b.Allow(), "breaker should reject traffic once open")
}

func TestBreakerOpenToHalfOpen(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 9, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	s := &fakeSource{name: "fake"}
	b := NewBreaker(s, 1, 5*time.Minute, 10*time.Minute, clock)

	// Trip the breaker.
	b.RecordFailure()
	require.Equal(t, StateOpen, b.State())

	// Cooldown not elapsed → reject.
	now = now.Add(5 * time.Minute)
	require.False(t, b.Allow())
	require.Equal(t, StateOpen, b.State())

	// Cooldown elapsed → allow one probe.
	now = now.Add(6 * time.Minute)
	require.Equal(t, StateHalfOpen, b.State())
	require.True(t, b.Allow())

	// Second caller during half-open should be rejected.
	require.False(t, b.Allow())
}

func TestBreakerHalfOpenSuccessClosesBreaker(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 9, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	s := &fakeSource{name: "fake"}
	b := NewBreaker(s, 1, 5*time.Minute, 10*time.Minute, clock)

	b.RecordFailure()
	now = now.Add(11 * time.Minute)
	require.True(t, b.Allow(), "first call after cooldown should be allowed as a probe")
	require.Equal(t, StateHalfOpen, b.State())

	b.RecordSuccess()
	require.Equal(t, StateClosed, b.State())
	require.Equal(t, 0, b.FailureCount())
}

func TestBreakerHalfOpenFailureReopens(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 9, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	s := &fakeSource{name: "fake"}
	b := NewBreaker(s, 1, 5*time.Minute, 10*time.Minute, clock)

	b.RecordFailure()
	now = now.Add(11 * time.Minute)
	require.True(t, b.Allow())
	b.RecordFailure()
	require.Equal(t, StateOpen, b.State())
}

func TestBreakerFailureWindowGC(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 9, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	s := &fakeSource{name: "fake"}
	b := NewBreaker(s, 3, 1*time.Minute, 10*time.Minute, clock)

	// 2 failures 30s apart.
	b.RecordFailure()
	now = now.Add(30 * time.Second)
	b.RecordFailure()
	require.Equal(t, 2, b.FailureCount())

	// Jump past the window; the old failures should be aged out on
	// the next Allow.
	now = now.Add(2 * time.Minute)
	require.True(t, b.Allow())
	require.Equal(t, 0, b.FailureCount())
}

func TestBreakerStateName(t *testing.T) {
	t.Parallel()
	s := &fakeSource{name: "fake"}
	b := NewBreaker(s, 5, time.Minute, time.Minute, time.Now)
	assert.Equal(t, "closed", b.StateName())
	assert.Equal(t, "unknown", BreakerState(99).String())
}

func TestBreakerSourceAccess(t *testing.T) {
	t.Parallel()
	s := &fakeSource{name: "fake"}
	b := NewBreaker(s, 5, time.Minute, time.Minute, time.Now)
	assert.Equal(t, s, b.Source())
}

func TestBreakerPanicsOnNilSource(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		NewBreaker(nil, 5, time.Minute, time.Minute, time.Now)
	})
}

func TestBreakerDefaults(t *testing.T) {
	t.Parallel()
	s := &fakeSource{name: "fake"}
	b := NewBreaker(s, 0, 0, 0, nil)
	assert.Equal(t, 5, b.threshold)
	assert.Equal(t, 5*time.Minute, b.window)
	assert.Equal(t, 10*time.Minute, b.cooldown)
}
