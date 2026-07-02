// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — health_repo.go
//
// The HealthRepo interface is the persistence boundary the manager
// uses to record per-source health. We keep the interface narrow so
// tests can swap in a fake; the GORM implementation lives in
// health_repo_gorm.go.
package datasource

import (
	"context"

	"github.com/skyzhao/af/internal/model"
)

// HealthRepo is the persistence-side contract for the datasource
// health monitor. Implementations must be safe for concurrent use.
type HealthRepo interface {
	// RecordSuccess marks a successful call to source. The repo is
	// expected to update last_ok, reset fail_count_5m when the
	// previous failure window has elapsed, and set status to
	// "healthy".
	RecordSuccess(ctx context.Context, source string) error

	// RecordFailure marks a failed call to source. The repo is
	// expected to update last_fail, increment fail_count_5m and
	// recompute status. The status thresholds are owned by the repo
	// (so they can be tuned in one place).
	RecordFailure(ctx context.Context, source, errMsg string) error

	// RecordSwitch appends a row to provider_switch_events. The
	// manager does this whenever it transparently swaps to a
	// fallback source.
	RecordSwitch(ctx context.Context, from, to, reason string) error

	// GetHealth returns the current DatasourceHealth row for
	// source. Returns (nil, nil) if the source has never been
	// recorded.
	GetHealth(ctx context.Context, source string) (*model.DatasourceHealth, error)

	// ListAll returns every DatasourceHealth row, one per source
	// that has been seen.
	ListAll(ctx context.Context) ([]model.DatasourceHealth, error)
}
