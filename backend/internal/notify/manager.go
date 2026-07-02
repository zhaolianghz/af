// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package notify

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Manager is the default Manager implementation. It is safe for
// concurrent use.
//
// The Send algorithm:
//
//  1. Iterate channels in [primary, fallback1, fallback2, ...] order.
//  2. For each candidate, check the channel's circuit breaker. If
//     the breaker is open, log and move on to the next candidate.
//  3. If the breaker allows, run Retry over Channel.Send. Record the
//     outcome on the breaker (OnSuccess / OnFailure).
//  4. If any candidate returns nil, Send returns nil.
//  5. If every candidate fails (or is short-circuited by an open
//     breaker), Send returns ErrAllChannelsFailed wrapped with the
//     last error encountered.
//
// Logger is required; nil loggers are replaced with a Nop logger so the
// manager never panics on nil deref in tests that forget to wire it.
type DefaultManager struct {
	mu sync.RWMutex

	primary  string
	fallback []string

	channels map[string]Channel

	retry   RetryConfig
	timeout time.Duration

	logger *zap.Logger
}

// ManagerOptions configures NewManager. Zero values get safe defaults.
type ManagerOptions struct {
	// Primary is the channel name to try first. Required.
	Primary string
	// Fallback is the ordered list of channel names to try if the
	// primary fails. Names not present in the registry are silently
	// skipped (this lets operators turn channels off via config without
	// crashing startup).
	Fallback []string
	// Retry is passed to Retry. Zero Attempts falls back to the
	// package default (3).
	Retry RetryConfig
	// Timeout is the per-send timeout (ctx.WithTimeout). Zero defaults
	// to 5s. Note: each retry attempt gets its own timeout, so the
	// wall-clock cost is Timeout * Attempts in the worst case.
	Timeout time.Duration
	// Logger is used for all log lines emitted by the manager.
	Logger *zap.Logger
}

// NewManager builds a *DefaultManager (which satisfies Manager). The
// channel registry starts empty — use RegisterChannel to add channels
// before calling Send.
func NewManager(opts ManagerOptions) *DefaultManager {
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = zap.NewNop()
	}
	// Carry the channel name through retry fields so each attempt can
	// be correlated in logs.
	opts.Retry.ChannelName = opts.Primary
	return &DefaultManager{
		primary:  opts.Primary,
		fallback: append([]string(nil), opts.Fallback...),
		channels: make(map[string]Channel),
		retry:    opts.Retry.withDefaults(),
		timeout:  opts.Timeout,
		logger:   opts.Logger,
	}
}

// RegisterChannel adds or replaces a channel by name. Safe for
// concurrent use.
func (m *DefaultManager) RegisterChannel(name string, ch Channel) {
	if name == "" || ch == nil {
		return
	}
	m.mu.Lock()
	m.channels[name] = ch
	m.mu.Unlock()
}

// List returns the registered channel names in an unspecified order.
func (m *DefaultManager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.channels))
	for n := range m.channels {
		out = append(out, n)
	}
	return out
}

// Send walks primary + fallbacks as described on the type. It always
// returns a non-nil error when no channel succeeds; that error wraps
// ErrAllChannelsFailed (so errors.Is(err, ErrAllChannelsFailed) works)
// and includes the most recent underlying error for diagnostics.
func (m *DefaultManager) Send(ctx context.Context, msg *Message) error {
	if msg == nil {
		return fmt.Errorf("notify.Manager.Send: nil message: %w", ErrAllChannelsFailed)
	}

	candidates := m.candidateOrder()
	if len(candidates) == 0 {
		return fmt.Errorf("notify: no channels registered (primary=%q): %w", m.primary, ErrAllChannelsFailed)
	}

	var lastErr error
	var lastTried string
	for _, name := range candidates {
		ch, ok := m.lookup(name)
		if !ok {
			m.logger.Debug("notify skip unknown channel",
				zap.String("channel", name),
				zap.String("msg_type", string(msg.Type)),
			)
			continue
		}
		if !ch.IsHealthy() {
			m.logger.Warn("notify skip unhealthy channel",
				zap.String("channel", name),
				zap.String("msg_type", string(msg.Type)),
			)
			lastErr = fmt.Errorf("channel %q unhealthy", name)
			lastTried = name
			continue
		}

		err := m.sendOne(ctx, ch, msg)
		if err == nil {
			m.logger.Info("notify send success",
				zap.String("channel", name),
				zap.String("msg_type", string(msg.Type)),
			)
			return nil
		}
		m.logger.Error("notify send failed",
			zap.String("channel", name),
			zap.String("msg_type", string(msg.Type)),
			zap.Error(err),
		)
		lastErr = err
		lastTried = name
	}

	if lastErr == nil {
		lastErr = errors.New("no channels configured")
	}
	return fmt.Errorf("notify: all channels failed (last=%s): %w", lastTried, errors.Join(ErrAllChannelsFailed, lastErr))
}

// sendOne runs Retry over a single channel's Send. The timeout is
// applied per-attempt via context.WithTimeout so a single slow call
// can't burn the whole retry budget.
func (m *DefaultManager) sendOne(parent context.Context, ch Channel, msg *Message) error {
	// Per-attempt retry config: rebind ChannelName/MessageType for
	// accurate log fields.
	cfg := m.retry
	cfg.ChannelName = ch.Name()
	cfg.MessageType = msg.Type
	cfg.Logger = m.logger

	return Retry(parent, cfg, func(ctx context.Context) error {
		attemptCtx, cancel := context.WithTimeout(ctx, m.timeout)
		defer cancel()
		return ch.Send(attemptCtx, msg)
	})
}

// candidateOrder returns [primary, fallback1, fallback2, ...] with
// duplicates and the empty string removed.
func (m *DefaultManager) candidateOrder() []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 1+len(m.fallback))
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	add(m.primary)
	for _, n := range m.fallback {
		add(n)
	}
	return out
}

func (m *DefaultManager) lookup(name string) (Channel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch, ok := m.channels[name]
	return ch, ok
}

// Compile-time check: *DefaultManager must satisfy Manager.
var _ Manager = (*DefaultManager)(nil)
