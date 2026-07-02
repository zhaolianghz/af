// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package model — shared test helpers.
package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestDB returns a fresh in-memory SQLite GORM connection with every model
// migrated. Each test gets its own connection, fully isolated from siblings.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Use a per-test unique DSN so tests run in parallel don't share state.
	dsn := "file:model_test_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	return db
}

// newTestTime returns a deterministic UTC time.
func newTestTime() time.Time {
	return time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
}
