// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — breaker.go
//
// A minimal in-process circuit breaker that wraps a single Source. It
// matches the manager's per-source breaker (one Breaker per Source) and
// uses the same threshold / window / cooldown that design.md §3.5
// mandates: 5 failures within 5 minutes trips the breaker for 10
// minutes.
//
// The breaker is a separate concern from the manager's failover logic:
// the manager uses the breaker to decide "should I even try this
// source right now?" and the breaker records outcomes from the
// manager's perspective.
package datasource

import (
	"sync"
	"time"
)

// BreakerState describes the current state of a Breaker.
type BreakerState int

const (
	// StateClosed is the normal state: traffic flows.
	StateClosed BreakerState = iota
	// StateOpen means the breaker has tripped; Allow returns false
	// until Cooldown elapses.
	StateOpen
	// StateHalfOpen allows exactly one probe through. If the probe
	// succeeds the breaker closes; if it fails the breaker
	// re-opens.
	StateHalfOpen
)

// String renders a BreakerState for logs.
func (s BreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Breaker is a per-source circuit breaker. Constructed by the manager
// for each configured source. Safe for concurrent use.
type Breaker struct {
	mu       sync.Mutex
	source   Source
	threshold int
	window    time.Duration
	cooldown  time.Duration
	nowFunc   func() time.Time

	state       BreakerState
	failures    []time.Time
	openedAt    time.Time
	halfOpenIn  bool
}

// NewBreaker wraps source with a fresh breaker using the given
// threshold / window / cooldown. now is injected for tests; nil falls
// back to time.Now.
func NewBreaker(source Source, threshold int, window, cooldown time.Duration, now func() time.Time) *Breaker {
	if source == nil {
		panic("datasource: NewBreaker called with nil source")
	}
	if threshold <= 0 {
		threshold = 5
	}
	if window <= 0 {
		window = 5 * time.Minute
	}
	if cooldown <= 0 {
		cooldown = 10 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &Breaker{
		source:    source,
		threshold: threshold,
		window:    window,
		cooldown:  cooldown,
		nowFunc:   now,
		state:     StateClosed,
	}
}

// Source returns the wrapped source. Useful for the manager when it
// needs to call a method that doesn't go through the breaker (e.g.
// IsHealthy).
func (b *Breaker) Source() Source { return b.source }

// Allow asks the breaker whether a call may proceed.
//
//   - StateClosed → true.
//   - StateOpen + Cooldown elapsed → flip to half-open, allow one probe.
//   - StateOpen + Cooldown not elapsed → false.
//   - StateHalfOpen → only one in-flight probe at a time.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.nowFunc()

	if b.state == StateClosed {
		b.gcLocked(now)
	}

	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if now.Sub(b.openedAt) >= b.cooldown {
			b.state = StateHalfOpen
			b.halfOpenIn = true
			return true
		}
		return false
	case StateHalfOpen:
		if b.halfOpenIn {
			return false
		}
		b.halfOpenIn = true
		return true
	}
	return false
}

// RecordSuccess records a successful call.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateHalfOpen:
		// Probe succeeded → close, clear history.
		b.state = StateClosed
		b.halfOpenIn = false
		b.failures = b.failures[:0]
	case StateClosed:
		// Keep history: a stream of successes does not paper over
		// the recent failures that put us near the threshold.
	}
}

// RecordFailure records a failed call.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.nowFunc()

	switch b.state {
	case StateHalfOpen:
		// Probe failed → re-open with a fresh cooldown.
		b.state = StateOpen
		b.openedAt = now
		b.halfOpenIn = false
		b.failures = append(b.failures, now)
	case StateClosed:
		b.failures = append(b.failures, now)
		b.gcLocked(now)
		if len(b.failures) >= b.threshold {
			b.state = StateOpen
			b.openedAt = now
		}
	}
}

// State returns the current state. If the breaker is open and the
// cooldown has elapsed, this is updated lazily so the next Allow
// becomes a probe rather than returning a stale "open" state.
func (b *Breaker) State() BreakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateOpen && b.nowFunc().Sub(b.openedAt) >= b.cooldown {
		b.state = StateHalfOpen
		b.halfOpenIn = false
	}
	return b.state
}

// StateName returns the human-readable state name.
func (b *Breaker) StateName() string {
	return b.State().String()
}

// FailureCount returns the count of failures still inside the rolling
// window. Useful for tests and /api/v1/datasource/health.
func (b *Breaker) FailureCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gcLocked(b.nowFunc())
	return len(b.failures)
}

// gcLocked drops failures older than the rolling window. Caller must
// hold b.mu.
func (b *Breaker) gcLocked(now time.Time) {
	cutoff := now.Add(-b.window)
	keep := b.failures[:0]
	for _, t := range b.failures {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	b.failures = keep
}
