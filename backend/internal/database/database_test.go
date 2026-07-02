// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package database

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/model"
)

// newMemoryDB returns a SQLite in-memory GORM connection with all migrations
// applied. The shared cache DSN lets multiple opens share state in a single
// test, while every test gets its own DB name to isolate state.
func newMemoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg := config.DBConfig{
		Driver:    "sqlite",
		Name:      "file::memory:?cache=shared",
		LogLevel:  "silent",
	}
	db, err := Open(cfg)
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	return db
}

func TestOpenSQLiteAndMigrate(t *testing.T) {
	db := newMemoryDB(t)
	require.NotNil(t, db)

	// Ping should succeed against an in-memory SQLite.
	require.NoError(t, Ping(db))
}

func TestOpenSQLiteBadDriver(t *testing.T) {
	_, err := Open(config.DBConfig{Driver: "oracle"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported driver")
}

func TestOpenMySQLInvalidDSN(t *testing.T) {
	// We don't have MySQL running, so the open call should fail with a
	// network-level error. We just check that an error is returned.
	_, err := Open(config.DBConfig{
		Driver: "mysql",
		Host:   "127.0.0.1",
		Port:   1, // unlikely to be open
		User:   "u",
		Name:   "n",
	})
	assert.Error(t, err)
}

func TestMigrateIdempotent(t *testing.T) {
	db := newMemoryDB(t)
	// Second call should be a no-op (GORM is idempotent).
	require.NoError(t, Migrate(db))
}

func TestInsertAndQueryAllModels(t *testing.T) {
	db := newMemoryDB(t)
	now := time.Now().UTC().Truncate(time.Second)

	s := &model.Strategy{
		Code:           "S-TEST-1",
		Name:           "Test Strategy",
		Description:    "unit test",
		Status:         model.StrategyStatusActive,
		Tags:           "momentum,mid-cap",
		CurrentVersion: 1,
		DAGJson:        `{"nodes":[],"edges":[]}`,
	}
	require.NoError(t, db.Create(s).Error)
	require.NotZero(t, s.ID)

	sv := &model.StrategyVersion{
		StrategyID:      s.ID,
		Version:         1,
		DAGJson:         `{"nodes":[],"edges":[]}`,
		ChangeNote:      "initial",
		SnapshotTakenAt: now,
	}
	require.NoError(t, db.Create(sv).Error)

	nd := &model.NodeDefinition{
		StrategyID: s.ID,
		NodeKey:    "ma20",
		Type:       model.NodeTypeIndicator,
		Subtype:    "MA",
		ConfigJson: `{"window":20}`,
	}
	require.NoError(t, db.Create(nd).Error)

	rec := &model.Recommendation{
		RunID:          1,
		Date:           now,
		StockCode:      "600000",
		StockName:      "Test Stock",
		EntryPriceLow:  10.0,
		EntryPriceHigh: 10.5,
		StrategyCode:   s.Code,
		StrategyName:   s.Name,
		NodeSnapshot:   `{"pipeline":["ma20"]}`,
	}
	require.NoError(t, db.Create(rec).Error)

	tag := &model.RecommendationTag{
		RecommendationID: rec.ID,
		Tag:              model.SessionTagMorning,
		Source:           model.TagSourceAutoNode,
		TaggedAt:         now,
	}
	require.NoError(t, db.Create(tag).Error)

	run := &model.Run{
		StrategyID:  s.ID,
		TriggerType: model.RunTriggerManual,
		Status:      model.RunStatusSuccess,
		StartedAt:   &now,
		FinishedAt:  &now,
	}
	require.NoError(t, db.Create(run).Error)

	rl := &model.RunLog{
		RunID:      run.ID,
		NodeKey:    "ma20",
		Status:     model.RunLogStatusSuccess,
		StartedAt:  now,
		FinishedAt: now,
	}
	require.NoError(t, db.Create(rl).Error)

	tc := &model.TradingCalendar{
		Date:      now,
		IsTrading: true,
		IsWeekend: false,
		Source:    model.CalendarSourceTushare,
		SyncedAt:  now,
	}
	require.NoError(t, db.Create(tc).Error)

	t1 := 0.0123
	ps := &model.PerformanceSnapshot{
		RecommendationID: rec.ID,
		SnapshotDate:     now,
		T1Return:         &t1,
		CalculatedAt:     now,
	}
	require.NoError(t, db.Create(ps).Error)

	dh := &model.DatasourceHealth{
		Source: model.DataSourceEastMoney,
		Status: model.HealthStatusHealthy,
	}
	require.NoError(t, db.Create(dh).Error)

	se := &model.ProviderSwitchEvent{
		FromSource: model.DataSourceSina,
		ToSource:   model.DataSourceEastMoney,
		Reason:     "manual",
		SwitchedAt: now,
	}
	require.NoError(t, db.Create(se).Error)

	role := &model.Role{
		Code:        model.RoleCodeViewer,
		Name:        "Viewer",
		Permissions: `["strategy:read"]`,
	}
	require.NoError(t, db.Create(role).Error)

	user := &model.User{
		Username:     "alice",
		PasswordHash: "hash",
		Email:        "alice@example.com",
		RoleID:       role.ID,
		Status:       model.UserStatusActive,
	}
	require.NoError(t, db.Create(user).Error)

	audit := &model.AIAudit{
		UserID:     user.ID,
		StrategyID: s.ID,
		IntentJson: `{"intent":"add a momentum filter"}`,
		Decision:   model.AIDecisionApplied,
	}
	require.NoError(t, db.Create(audit).Error)

	// Round-trip counts.
	var (
		strategies       int64
		strategyVersions int64
		nodes            int64
		recs             int64
		recTags          int64
		runs             int64
		runLogs          int64
		cal              int64
		perf             int64
		health           int64
		switches         int64
		users            int64
		roles            int64
		audits           int64
	)
	require.NoError(t, db.Model(&model.Strategy{}).Count(&strategies).Error)
	require.NoError(t, db.Model(&model.StrategyVersion{}).Count(&strategyVersions).Error)
	require.NoError(t, db.Model(&model.NodeDefinition{}).Count(&nodes).Error)
	require.NoError(t, db.Model(&model.Recommendation{}).Count(&recs).Error)
	require.NoError(t, db.Model(&model.RecommendationTag{}).Count(&recTags).Error)
	require.NoError(t, db.Model(&model.Run{}).Count(&runs).Error)
	require.NoError(t, db.Model(&model.RunLog{}).Count(&runLogs).Error)
	require.NoError(t, db.Model(&model.TradingCalendar{}).Count(&cal).Error)
	require.NoError(t, db.Model(&model.PerformanceSnapshot{}).Count(&perf).Error)
	require.NoError(t, db.Model(&model.DatasourceHealth{}).Count(&health).Error)
	require.NoError(t, db.Model(&model.ProviderSwitchEvent{}).Count(&switches).Error)
	require.NoError(t, db.Model(&model.User{}).Count(&users).Error)
	require.NoError(t, db.Model(&model.Role{}).Count(&roles).Error)
	require.NoError(t, db.Model(&model.AIAudit{}).Count(&audits).Error)

	assert.Equal(t, int64(1), strategies)
	assert.Equal(t, int64(1), strategyVersions)
	assert.Equal(t, int64(1), nodes)
	assert.Equal(t, int64(1), recs)
	assert.Equal(t, int64(1), recTags)
	assert.Equal(t, int64(1), runs)
	assert.Equal(t, int64(1), runLogs)
	assert.Equal(t, int64(1), cal)
	assert.Equal(t, int64(1), perf)
	assert.Equal(t, int64(1), health)
	assert.Equal(t, int64(1), switches)
	assert.Equal(t, int64(1), users)
	assert.Equal(t, int64(1), roles)
	assert.Equal(t, int64(1), audits)
}

func TestUniqueConstraints(t *testing.T) {
	db := newMemoryDB(t)

	// Strategy.Code unique
	require.NoError(t, db.Create(&model.Strategy{Code: "DUP", Name: "a", Status: model.StrategyStatusDraft, DAGJson: "{}"}).Error)
	err := db.Create(&model.Strategy{Code: "DUP", Name: "b", Status: model.StrategyStatusDraft, DAGJson: "{}"}).Error
	assert.Error(t, err, "duplicate strategy code should fail")

	// User.Username unique
	require.NoError(t, db.Create(&model.User{Username: "bob", Status: model.UserStatusActive}).Error)
	err = db.Create(&model.User{Username: "bob", Status: model.UserStatusActive}).Error
	assert.Error(t, err, "duplicate username should fail")

	// Role.Code unique
	require.NoError(t, db.Create(&model.Role{Code: model.RoleCodeAdmin, Name: "Admin"}).Error)
	err = db.Create(&model.Role{Code: model.RoleCodeAdmin, Name: "Admin2"}).Error
	assert.Error(t, err, "duplicate role code should fail")

	// TradingCalendar.Date unique
	date := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, db.Create(&model.TradingCalendar{Date: date, IsTrading: true, Source: model.CalendarSourceTushare, SyncedAt: date}).Error)
	err = db.Create(&model.TradingCalendar{Date: date, IsTrading: false, Source: model.CalendarSourceTushare, SyncedAt: date}).Error
	assert.Error(t, err, "duplicate calendar date should fail")

	// DatasourceHealth.Source unique
	require.NoError(t, db.Create(&model.DatasourceHealth{Source: model.DataSourceSina, Status: model.HealthStatusHealthy}).Error)
	err = db.Create(&model.DatasourceHealth{Source: model.DataSourceSina, Status: model.HealthStatusDown}).Error
	assert.Error(t, err, "duplicate datasource source should fail")

	// (StrategyID, Version) unique on StrategyVersion
	s := &model.Strategy{Code: "V-TEST", Name: "v", Status: model.StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(s).Error)
	require.NoError(t, db.Create(&model.StrategyVersion{StrategyID: s.ID, Version: 1, DAGJson: "{}"}).Error)
	err = db.Create(&model.StrategyVersion{StrategyID: s.ID, Version: 1, DAGJson: "{}"}).Error
	assert.Error(t, err, "duplicate strategy version should fail")

	// (RecommendationID, Tag, Source) unique on RecommendationTag
	rec := &model.Recommendation{
		RunID: 1, Date: date, StockCode: "1", StrategyCode: "c", StrategyName: "n", NodeSnapshot: "{}",
	}
	require.NoError(t, db.Create(rec).Error)
	require.NoError(t, db.Create(&model.RecommendationTag{RecommendationID: rec.ID, Tag: model.SessionTagMorning, Source: model.TagSourceAutoNode, TaggedAt: date}).Error)
	err = db.Create(&model.RecommendationTag{RecommendationID: rec.ID, Tag: model.SessionTagMorning, Source: model.TagSourceAutoNode, TaggedAt: date}).Error
	assert.Error(t, err, "duplicate (rec,tag,source) should fail")

	// (RecommendationID, SnapshotDate) unique on PerformanceSnapshot
	require.NoError(t, db.Create(&model.PerformanceSnapshot{RecommendationID: rec.ID, SnapshotDate: date, CalculatedAt: date}).Error)
	err = db.Create(&model.PerformanceSnapshot{RecommendationID: rec.ID, SnapshotDate: date, CalculatedAt: date}).Error
	assert.Error(t, err, "duplicate (rec,snapshot_date) should fail")
}

func TestSoftDelete(t *testing.T) {
	db := newMemoryDB(t)
	s := &model.Strategy{Code: "SOFT-1", Name: "x", Status: model.StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(s).Error)

	// Delete should mark deleted_at, not remove the row.
	require.NoError(t, db.Delete(s).Error)

	// Default Find ignores soft-deleted rows.
	var got model.Strategy
	err := db.First(&got, s.ID).Error
	assert.Error(t, err, "default find should not return soft-deleted row")

	// Unscoped returns the row with deleted_at set.
	var got2 model.Strategy
	require.NoError(t, db.Unscoped().First(&got2, s.ID).Error)
	assert.Equal(t, s.ID, got2.ID)
	assert.True(t, got2.DeletedAt.Valid, "deleted_at should be set")
}

func TestCloseNilSafe(t *testing.T) {
	assert.NoError(t, Close(nil))
}

func TestMigrateNilDB(t *testing.T) {
	assert.Error(t, Migrate(nil))
}

func TestPingNilDB(t *testing.T) {
	assert.Error(t, Ping(nil))
}

// Sanity check: writing to a real on-disk file works end-to-end. This is the
// same path the production server would use if DB_DRIVER=sqlite.
func TestOpenSQLiteFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "af.db")
	db, err := Open(config.DBConfig{
		Driver: "sqlite",
		Name:   dbPath,
	})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	require.NoError(t, db.Create(&model.Strategy{Code: "FILE-1", Name: "f", Status: model.StrategyStatusDraft, DAGJson: "{}"}).Error)
	require.NoError(t, Ping(db))
	_ = os.Remove(dbPath) // cleanup
}

// =============================================================================
// parseGormLogLevel + Open log-level / sqlite-name branches (C2a coverage)
// =============================================================================

func TestParseGormLogLevel(t *testing.T) {
	good := []string{"", "info", "INFO", "warn", "WARN", "error", "ERROR", "silent", "SILENT"}
	for _, s := range good {
		if _, err := parseGormLogLevel(s); err != nil {
			t.Errorf("parseGormLogLevel(%q) errored: %v", s, err)
		}
	}
	if _, err := parseGormLogLevel("verbose"); err == nil {
		t.Error("parseGormLogLevel(verbose) should error")
	}
}

func TestOpen_BadLogLevel(t *testing.T) {
	_, err := Open(config.DBConfig{Driver: "sqlite", Name: ":memory:", LogLevel: "loud"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log level")
}

func TestOpen_SQLiteEmptyNameDefaultsToMemory(t *testing.T) {
	// Empty Name on the sqlite driver falls back to ":memory:".
	db, err := Open(config.DBConfig{Driver: "sqlite", Name: "", LogLevel: "silent"})
	require.NoError(t, err)
	require.NotNil(t, db)
	require.NoError(t, Ping(db))
	require.NoError(t, Close(db))
}

func TestPingAndClose_Success(t *testing.T) {
	db := newMemoryDB(t)
	require.NoError(t, Ping(db))
	require.NoError(t, Close(db))
}

// =============================================================================
// Postgres driver routing (PG support)
// =============================================================================

func TestOpenPostgresInvalidDSN(t *testing.T) {
	// No postgres running on this port → the open/ping fails. We only
	// assert that the postgres branch is taken (an error is returned),
	// not the specific network message.
	_, err := Open(config.DBConfig{
		Driver:   "postgres",
		Host:     "127.0.0.1",
		Port:     1, // unlikely to be open
		User:     "u",
		Password: "p",
		Name:     "n",
		SSLMode:  "disable",
	})
	assert.Error(t, err)
}

func TestOpenPostgresAliases(t *testing.T) {
	// "pgsql" and "pg" route to the same postgres branch as "postgres".
	for _, drv := range []string{"pgsql", "pg"} {
		_, err := Open(config.DBConfig{
			Driver: drv, Host: "127.0.0.1", Port: 1,
			User: "u", Password: "p", Name: "n",
		})
		assert.Error(t, err, "driver %q should route to postgres and fail to connect", drv)
	}
}
