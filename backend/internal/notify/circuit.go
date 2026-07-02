// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package notify

import (
	"errors"
	"sync"
	"time"
)

// CircuitState describes the current state of a CircuitBreaker. The zero
// value (StateClosed) means "healthy, accept traffic".
type CircuitState int

const (
	// StateClosed is the normal state: traffic flows and failures are
	// counted. Once FailureThreshold failures accumulate within Window,
	// the breaker transitions to StateOpen.
	StateClosed CircuitState = iota
	// StateOpen means the breaker has tripped; all calls short-circuit
	// with ErrCircuitOpen until Cooldown elapses, at which point a single
	// probe is allowed (StateHalfOpen).
	StateOpen
	// StateHalfOpen is the probe state: exactly one call is allowed
	// through. If it succeeds the breaker closes; if it fails the
	// breaker re-opens for another Cooldown period.
	StateHalfOpen
)

// String makes CircuitState printable in logs and error messages.
func (s CircuitState) String() string {
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

// ErrCircuitOpen is returned by Allow when the breaker is open. Callers
// should treat this as "skip this channel, try the next fallback".
var ErrCircuitOpen = errors.New("notify: circuit breaker is open")

// CircuitConfig configures a CircuitBreaker. The defaults match
// tasks.md §7.5.6: open after 5 failures in 5 minutes, cooldown 10
// minutes.
type CircuitConfig struct {
	// FailureThreshold is the number of failures within Window that
	// trips the breaker. Default: 5.
	FailureThreshold int
	// Window is the rolling time window used to count failures.
	// Default: 5 minutes.
	Window time.Duration
	// Cooldown is how long the breaker stays open before allowing a
	// probe (half-open). Default: 10 minutes.
	Cooldown time.Duration
	// Now is injected for testing; nil falls back to time.Now.
	Now func() time.Time
}

func (c CircuitConfig) withDefaults() CircuitConfig {
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = 5
	}
	if c.Window <= 0 {
		c.Window = 5 * time.Minute
	}
	if c.Cooldown <= 0 {
		c.Cooldown = 10 * time.Minute
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// CircuitBreaker is a per-channel circuit breaker. The same struct is
// embedded by every channel implementation; it is safe for concurrent
// use by many goroutines.
//
// The algorithm:
//
//   - StateClosed + a call fails → record failure timestamp. If the
//     number of failures within the trailing Window has reached
//     FailureThreshold, transition to StateOpen with openedAt = now.
//   - StateOpen + Allow() called before Cooldown elapses → return
//     ErrCircuitOpen.
//   - StateOpen + Cooldown elapsed → transition to StateHalfOpen and
//     allow the call.
//   - StateHalfOpen + call succeeds → close the breaker (clear history).
//   - StateHalfOpen + call fails → re-open with a fresh Cooldown.
//
// successes in StateClosed do not clear the failure history — that
// would let a channel with intermittent failures run forever. The
// history is only reset when the breaker is closed-from-half-open, or
// when every recorded failure has aged out of the Window.
type CircuitBreaker struct {
	mu         sync.Mutex
	cfg        CircuitConfig
	state      CircuitState
	failures   []time.Time // timestamps of recent failures (within Window)
	openedAt   time.Time   // when we last transitioned to StateOpen
	halfOpenIn bool        // true while a half-open probe is in flight
}

func NewCircuitBreaker(cfg CircuitConfig) *CircuitBreaker {
	return &CircuitBreaker{cfg: cfg.withDefaults(), state: StateClosed}
}

// Allow asks the breaker whether a call may proceed.
//
//   - StateClosed → return true.
//   - StateOpen + Cooldown elapsed → transition to StateHalfOpen, return
//     true (the caller is the probe).
//   - StateOpen + Cooldown not elapsed → return false, ErrCircuitOpen.
//   - StateHalfOpen → only one in-flight probe at a time. If none is
//     in flight, the caller becomes the probe and we return true. If
//     one is already in flight, the call is rejected with
//     ErrCircuitOpen (so concurrent traffic doesn't all race into the
//     degraded channel).
func (b *CircuitBreaker) Allow() (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.cfg.Now()

	// Drop aged-out failures when in closed state so the count reflects
	// the trailing window only.
	if b.state == StateClosed {
		b.gcLocked(now)
	}

	switch b.state {
	case StateClosed:
		return true, nil

	case StateOpen:
		if now.Sub(b.openedAt) >= b.cfg.Cooldown {
			// Cooldown elapsed → allow a single probe.
			b.state = StateHalfOpen
			b.halfOpenIn = true
			return true, nil
		}
		return false, ErrCircuitOpen

	case StateHalfOpen:
		if b.halfOpenIn {
			return false, ErrCircuitOpen
		}
		b.halfOpenIn = true
		return true, nil
	}
	return false, ErrCircuitOpen
}

// OnSuccess records a successful call. Must be paired with every Allow
// that returned true.
func (b *CircuitBreaker) OnSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateHalfOpen:
		// Probe succeeded → close the breaker, wipe history.
		b.state = StateClosed
		b.halfOpenIn = false
		b.failures = b.failures[:0]
	case StateClosed:
		// Nothing to do; we deliberately do not clear history on a
		// closed-state success.
	}
}

// OnFailure records a failed call. Must be paired with every Allow
// that returned true.
func (b *CircuitBreaker) OnFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.cfg.Now()

	switch b.state {
	case StateHalfOpen:
		// Probe failed → re-open.
		b.state = StateOpen
		b.openedAt = now
		b.halfOpenIn = false
		b.failures = append(b.failures, now)

	case StateClosed:
		b.failures = append(b.failures, now)
		b.gcLocked(now)
		if len(b.failures) >= b.cfg.FailureThreshold {
			b.state = StateOpen
			b.openedAt = now
		}
	}
}

// State returns the current state. Used by Channel.IsHealthy and by
// /api/v1/notify/health.
func (b *CircuitBreaker) State() CircuitState {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Lazy transition on read: if we're open and the cooldown has
	// elapsed, flip to half-open so the next Allow becomes a probe
	// rather than returning a stale "open" state.
	if b.state == StateOpen && b.cfg.Now().Sub(b.openedAt) >= b.cfg.Cooldown {
		b.state = StateHalfOpen
		b.halfOpenIn = false
	}
	return b.state
}

// IsHealthy is a convenience wrapper for Channel.IsHealthy.
func (b *CircuitBreaker) IsHealthy() bool {
	return b.State() == StateClosed
}

// gcLocked drops failures older than Window. Caller must hold b.mu.
func (b *CircuitBreaker) gcLocked(now time.Time) {
	cutoff := now.Add(-b.cfg.Window)
	keep := b.failures[:0]
	for _, t := range b.failures {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	b.failures = keep
}
