// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// RetryConfig controls Retry behavior. Zero values fall back to the
// defaults required by the OpenSpec (3 attempts, 1s/2s/4s backoff).
type RetryConfig struct {
	// Attempts is the total number of tries (>= 1). Default: 3.
	Attempts int
	// InitialBackoff is the wait before the 2nd attempt. Default: 1s.
	// Subsequent waits double: 1s, 2s, 4s, ...
	InitialBackoff time.Duration
	// MaxBackoff caps the per-attempt wait. Default: 30s.
	MaxBackoff time.Duration
	// Logger is used to emit per-attempt fields. Optional; nil disables.
	Logger *zap.Logger
	// ChannelName is included in log fields so retries from different
	// channels can be told apart in aggregate logs.
	ChannelName string
	// MessageType is included in log fields. Optional.
	MessageType MessageType
}

func (c RetryConfig) withDefaults() RetryConfig {
	if c.Attempts <= 0 {
		c.Attempts = 3
	}
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Second
	}
	return c
}

// Retry runs fn up to cfg.Attempts times, sleeping with exponential
// backoff between attempts. It returns nil on the first success, or the
// last error if every attempt fails. ctx cancellation is honored between
// attempts: if ctx is done while sleeping, the function returns
// ctx.Err() wrapped with the most recent error.
//
// The behavior required by the OpenSpec (tasks.md §7.5.5) — "指数退避 × 3
// 次" — is the default. The backoff sequence is InitialBackoff,
// 2*InitialBackoff, 4*InitialBackoff, ... capped at MaxBackoff.
func Retry(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) error) error {
	cfg = cfg.withDefaults()

	var lastErr error
	backoff := cfg.InitialBackoff

	for attempt := 1; attempt <= cfg.Attempts; attempt++ {
		// Surface context cancellation early so we don't burn an attempt
		// when the caller has already given up.
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("retry aborted (attempt %d/%d, ctx: %v): %w", attempt, cfg.Attempts, err, lastErr)
			}
			return err
		}

		err := fn(ctx)
		if err == nil {
			if cfg.Logger != nil && attempt > 1 {
				cfg.Logger.Info("notify retry succeeded",
					zap.String("channel", cfg.ChannelName),
					zap.String("msg_type", string(cfg.MessageType)),
					zap.Int("attempt", attempt),
				)
			}
			return nil
		}
		lastErr = err

		// Don't sleep after the final attempt.
		if attempt == cfg.Attempts {
			break
		}

		if cfg.Logger != nil {
			cfg.Logger.Warn("notify retry attempt failed; backing off",
				zap.String("channel", cfg.ChannelName),
				zap.String("msg_type", string(cfg.MessageType)),
				zap.Int("attempt", attempt),
				zap.Int("max_attempts", cfg.Attempts),
				zap.Duration("backoff", backoff),
				zap.Error(err),
			)
		}

		// Wait with cancellation. We use a derived timer so we don't leak
		// it if we exit early via ctx cancellation.
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return fmt.Errorf("retry canceled after attempt %d/%d: %w (last err: %v)", attempt, cfg.Attempts, ctx.Err(), lastErr)
		case <-t.C:
		}

		// Exponential backoff with cap.
		backoff *= 2
		if backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
	}

	return fmt.Errorf("retry exhausted after %d attempts: %w", cfg.Attempts, lastErr)
}

// IsRetryable reports whether err looks transient. The retry helper
// itself does not use this — it always retries up to Attempts — but
// callers can use it to short-circuit when an error is clearly permanent
// (e.g. ErrAllChannelsFailed, context.Canceled, ...).
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, ErrAllChannelsFailed) {
		return false
	}
	return true
}
