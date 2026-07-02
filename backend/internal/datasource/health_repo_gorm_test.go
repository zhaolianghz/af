// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — health_repo_gorm_test.go
package datasource

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/model"
)

// newTestDB returns a SQLite-backed *gorm.DB with the DatasourceHealth
// and ProviderSwitchEvent tables migrated.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.DatasourceHealth{}, &model.ProviderSwitchEvent{}))
	return db
}

func TestHealthRepoRecordSuccessCreatesRow(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := NewGormHealthRepo(db)
	ctx := context.Background()

	require.NoError(t, repo.RecordSuccess(ctx, "eastmoney"))

	h, err := repo.GetHealth(ctx, "eastmoney")
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, "eastmoney", h.Source)
	assert.Equal(t, model.HealthStatusHealthy, h.Status)
	assert.NotNil(t, h.LastOK)
	assert.Equal(t, 0, h.FailCount5m)
}

func TestHealthRepoRecordFailureIncrementsCounter(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := NewGormHealthRepo(db)
	ctx := context.Background()

	require.NoError(t, repo.RecordFailure(ctx, "sina", "timeout"))
	h, err := repo.GetHealth(ctx, "sina")
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, "sina", h.Source)
	assert.Equal(t, model.HealthStatusDegraded, h.Status)
	assert.Equal(t, 1, h.FailCount5m)
	assert.Equal(t, "timeout", h.LastError)
	assert.NotNil(t, h.LastFail)
}

func TestHealthRepoStatusEscalation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := NewGormHealthRepo(db)
	ctx := context.Background()

	for i := 0; i < failThreshold; i++ {
		require.NoError(t, repo.RecordFailure(ctx, "akshare", "boom"))
	}
	h, err := repo.GetHealth(ctx, "akshare")
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, model.HealthStatusDown, h.Status, "after %d failures status should be down", failThreshold)
}

func TestHealthRepoSuccessResetsCounter(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := NewGormHealthRepo(db)
	ctx := context.Background()

	require.NoError(t, repo.RecordFailure(ctx, "sina", "boom"))
	require.NoError(t, repo.RecordFailure(ctx, "sina", "boom"))
	require.NoError(t, repo.RecordSuccess(ctx, "sina"))
	h, err := repo.GetHealth(ctx, "sina")
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, model.HealthStatusHealthy, h.Status)
	assert.Equal(t, 0, h.FailCount5m)
}

func TestHealthRepoRecordSwitch(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := NewGormHealthRepo(db)
	ctx := context.Background()

	require.NoError(t, repo.RecordSwitch(ctx, "eastmoney", "sina", "timeout on eastmoney"))
	require.NoError(t, repo.RecordSwitch(ctx, "sina", "akshare", "sina also down"))

	var events []model.ProviderSwitchEvent
	require.NoError(t, db.WithContext(ctx).Order("id ASC").Find(&events).Error)
	require.Len(t, events, 2)
	assert.Equal(t, "eastmoney", events[0].FromSource)
	assert.Equal(t, "sina", events[0].ToSource)
	assert.Equal(t, "timeout on eastmoney", events[0].Reason)
	assert.Equal(t, "sina", events[1].FromSource)
	assert.Equal(t, "akshare", events[1].ToSource)
}

func TestHealthRepoGetHealthNotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := NewGormHealthRepo(db)
	h, err := repo.GetHealth(context.Background(), "absent")
	require.NoError(t, err)
	assert.Nil(t, h)
}

func TestHealthRepoListAll(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := NewGormHealthRepo(db)
	ctx := context.Background()

	require.NoError(t, repo.RecordSuccess(ctx, "eastmoney"))
	require.NoError(t, repo.RecordFailure(ctx, "sina", "x"))
	require.NoError(t, repo.RecordSuccess(ctx, "akshare"))

	rows, err := repo.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	// Should be sorted by source.
	assert.Equal(t, "akshare", rows[0].Source)
	assert.Equal(t, "eastmoney", rows[1].Source)
	assert.Equal(t, "sina", rows[2].Source)
}

func TestHealthRepoEmptySourceRejected(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := NewGormHealthRepo(db)
	ctx := context.Background()
	assert.Error(t, repo.RecordSuccess(ctx, ""))
	assert.Error(t, repo.RecordFailure(ctx, "", "x"))
	assert.Error(t, repo.RecordSwitch(ctx, "", "", ""))
}

func TestHealthRepoLongErrorTruncated(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := NewGormHealthRepo(db)
	ctx := context.Background()

	long := make([]byte, 1024)
	for i := range long {
		long[i] = 'a'
	}
	require.NoError(t, repo.RecordFailure(ctx, "sina", string(long)))
	h, err := repo.GetHealth(ctx, "sina")
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.LessOrEqual(t, len(h.LastError), 512)
}

func TestHealthRepoFailureCountClamped(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := NewGormHealthRepo(db)
	ctx := context.Background()

	// Record many failures; the counter should clamp at
	// failThreshold * 4.
	for i := 0; i < failThreshold*10; i++ {
		require.NoError(t, repo.RecordFailure(ctx, "sina", "boom"))
	}
	h, err := repo.GetHealth(ctx, "sina")
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.LessOrEqual(t, h.FailCount5m, failThreshold*4)
}

func TestHealthRepoInjectedClock(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	fixed := time.Date(2025, 6, 10, 9, 30, 0, 0, time.UTC)
	repo := NewGormHealthRepoWithClock(db, func() time.Time { return fixed })
	ctx := context.Background()
	require.NoError(t, repo.RecordSuccess(ctx, "eastmoney"))
	h, err := repo.GetHealth(ctx, "eastmoney")
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, fixed.Unix(), h.LastOK.Unix())
}

func TestComputeStatus(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 6, 10, 9, 30, 0, 0, time.UTC)

	// No failures, recent success → healthy.
	assert.Equal(t, model.HealthStatusHealthy,
		computeStatus(0, &now, now))

	// No failures, but success was long ago → degraded.
	oldSuccess := now.Add(-1 * time.Hour)
	assert.Equal(t, model.HealthStatusDegraded,
		computeStatus(0, &oldSuccess, now))

	// No failures, never succeeded → degraded.
	assert.Equal(t, model.HealthStatusDegraded,
		computeStatus(0, nil, now))

	// A few failures but below threshold → degraded.
	assert.Equal(t, model.HealthStatusDegraded,
		computeStatus(failThreshold-1, &now, now))

	// At threshold → down.
	assert.Equal(t, model.HealthStatusDown,
		computeStatus(failThreshold, &now, now))
}
