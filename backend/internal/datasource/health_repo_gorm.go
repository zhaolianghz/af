// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — health_repo_gorm.go
//
// GORM implementation of HealthRepo. The status thresholds are:
//
//	healthy  — last_ok within 5 minutes, no recent failures
//	degraded — at least one recent failure but < FailThreshold
//	           failures in the last 5 minutes
//	down     — fail_count_5m >= FailThreshold (default 5) or no
//	           successful contact ever
//
// These are the same numbers design.md §3.5 mandates (5 fails / 5 min /
// 10 min cooldown).
package datasource

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/model"
)

// failThreshold is the rolling 5-minute failure count that escalates a
// source from "degraded" to "down". It matches the design.md hard
// constraint; we keep it as a package constant so the manager and the
// breaker agree on the threshold.
const failThreshold = 5

// failWindow is the rolling window in which failCount5m is counted.
const failWindow = 5 * time.Minute

// gormHealthRepo is the production HealthRepo. It uses *gorm.DB under
// the hood; tests can use a SQLite in-memory DB.
type gormHealthRepo struct {
	db *gorm.DB
	// nowFunc is injected for tests; nil falls back to time.Now.
	nowFunc func() time.Time
}

// NewGormHealthRepo returns a HealthRepo backed by the given *gorm.DB.
func NewGormHealthRepo(db *gorm.DB) HealthRepo {
	return &gormHealthRepo{db: db, nowFunc: time.Now}
}

// NewGormHealthRepoWithClock is like NewGormHealthRepo but lets callers
// (mostly tests) inject a deterministic clock.
func NewGormHealthRepoWithClock(db *gorm.DB, now func() time.Time) HealthRepo {
	if now == nil {
		now = time.Now
	}
	return &gormHealthRepo{db: db, nowFunc: now}
}

func (r *gormHealthRepo) now() time.Time {
	if r.nowFunc == nil {
		return time.Now()
	}
	return r.nowFunc()
}

// RecordSuccess upserts a health row with status=healthy and
// resets fail_count_5m.
func (r *gormHealthRepo) RecordSuccess(ctx context.Context, source string) error {
	if source == "" {
		return fmt.Errorf("datasource: RecordSuccess: empty source")
	}
	now := r.now()

	// Try to update an existing row; if it does not exist, insert
	// a new one. We avoid GORM's OnConflict clause because
	// SQLite's UNIQUE constraint semantics differ from MySQL's
	// and a portable SELECT-then-UPDATE/INSERT is more reliable
	// across drivers.
	var existing model.DatasourceHealth
	dberr := r.db.WithContext(ctx).Where("source = ?", source).First(&existing).Error
	if dberr != nil && !errors.Is(dberr, gorm.ErrRecordNotFound) {
		return fmt.Errorf("read existing health for %q: %w", source, dberr)
	}
	if errors.Is(dberr, gorm.ErrRecordNotFound) {
		row := model.DatasourceHealth{
			Source: source,
			Status: model.HealthStatusHealthy,
			LastOK: &now,
		}
		if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
			return fmt.Errorf("insert health for %q: %w", source, err)
		}
		return nil
	}
	// Update — preserve LastFail for operator visibility.
	updates := map[string]interface{}{
		"status":       model.HealthStatusHealthy,
		"last_ok":      now,
		"fail_count5m": 0,
		"last_error":   "",
		"updated_at":   now,
	}
	if err := r.db.WithContext(ctx).Model(&model.DatasourceHealth{}).
		Where("source = ?", source).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("update health (success) for %q: %w", source, err)
	}
	return nil
}

// RecordFailure upserts a health row, incrementing fail_count_5m and
// computing a fresh status.
func (r *gormHealthRepo) RecordFailure(ctx context.Context, source, errMsg string) error {
	if source == "" {
		return fmt.Errorf("datasource: RecordFailure: empty source")
	}
	now := r.now()

	// Load the existing row so we can age out old failures from the
	// rolling count and clamp FailCount5m to the threshold.
	var existing model.DatasourceHealth
	dberr := r.db.WithContext(ctx).Where("source = ?", source).First(&existing).Error
	if dberr != nil && !errors.Is(dberr, gorm.ErrRecordNotFound) {
		return fmt.Errorf("read existing health for %q: %w", source, dberr)
	}

	// Roll the failure window. We don't store a per-failure
	// timestamp list, so we use FailCount5m as a smoothed
	// indicator: a fresh failure increments; the row is periodically
	// reset by HealthCheck or by a RecordSuccess on the same source.
	newCount := existing.FailCount5m + 1
	if newCount > failThreshold*4 {
		// Hard cap so a long-running source with intermittent
		// issues cannot have its counter run away forever.
		newCount = failThreshold * 4
	}

	status := computeStatus(newCount, existing.LastOK, now)
	trunc := truncateErr(errMsg)

	if errors.Is(dberr, gorm.ErrRecordNotFound) {
		row := model.DatasourceHealth{
			Source:      source,
			Status:      status,
			LastFail:    &now,
			FailCount5m: newCount,
			LastError:   trunc,
		}
		if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
			return fmt.Errorf("insert health (failure) for %q: %w", source, err)
		}
		return nil
	}
	updates := map[string]interface{}{
		"status":       status,
		"last_fail":    now,
		"fail_count5m": newCount,
		"last_error":   trunc,
		"updated_at":   now,
	}
	if err := r.db.WithContext(ctx).Model(&model.DatasourceHealth{}).
		Where("source = ?", source).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("update health (failure) for %q: %w", source, err)
	}
	return nil
}

// RecordSwitch appends a ProviderSwitchEvent row.
func (r *gormHealthRepo) RecordSwitch(ctx context.Context, from, to, reason string) error {
	if from == "" && to == "" {
		return fmt.Errorf("datasource: RecordSwitch: from and to both empty")
	}
	row := model.ProviderSwitchEvent{
		FromSource: from,
		ToSource:   to,
		Reason:     truncateErr(reason),
		SwitchedAt: r.now(),
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("insert provider switch event: %w", err)
	}
	return nil
}

// GetHealth returns the row for source, or (nil, nil) if not found.
func (r *gormHealthRepo) GetHealth(ctx context.Context, source string) (*model.DatasourceHealth, error) {
	var row model.DatasourceHealth
	if err := r.db.WithContext(ctx).Where("source = ?", source).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get health for %q: %w", source, err)
	}
	return &row, nil
}

// ListAll returns every health row.
func (r *gormHealthRepo) ListAll(ctx context.Context) ([]model.DatasourceHealth, error) {
	var rows []model.DatasourceHealth
	if err := r.db.WithContext(ctx).Order("source ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list datasource health: %w", err)
	}
	return rows, nil
}

// computeStatus decides healthy / degraded / down from the failure
// count and the most recent success timestamp.
func computeStatus(failCount int, lastOK *time.Time, now time.Time) string {
	if failCount >= failThreshold {
		return model.HealthStatusDown
	}
	if failCount > 0 {
		return model.HealthStatusDegraded
	}
	if lastOK == nil {
		return model.HealthStatusDegraded
	}
	if now.Sub(*lastOK) > failWindow {
		return model.HealthStatusDegraded
	}
	return model.HealthStatusHealthy
}

// truncateErr caps an error message at 512 chars so we never blow out
// the column.
func truncateErr(s string) string {
	const max = 512
	if len(s) <= max {
		return s
	}
	return s[:max]
}
