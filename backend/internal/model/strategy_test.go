// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestStrategyCRUD(t *testing.T) {
	db := newTestDB(t)

	s := &Strategy{
		Code:           "STR-1",
		Name:           "Momentum v1",
		Description:    "20-day MA crossover",
		Status:         StrategyStatusActive,
		Tags:           "momentum,mid-cap",
		CurrentVersion: 1,
		DAGJson:        `{"nodes":[]}`,
	}
	require.NoError(t, db.Create(s).Error)
	require.NotZero(t, s.ID)
	require.NotZero(t, s.CreatedAt)

	// Read back.
	var got Strategy
	require.NoError(t, db.First(&got, s.ID).Error)
	assert.Equal(t, "STR-1", got.Code)
	assert.Equal(t, "Momentum v1", got.Name)
	assert.Equal(t, StrategyStatusActive, got.Status)
	assert.Equal(t, 1, got.CurrentVersion)
}

func TestStrategyUniqueCode(t *testing.T) {
	db := newTestDB(t)
	a := &Strategy{Code: "DUP", Name: "a", Status: StrategyStatusDraft, DAGJson: "{}"}
	b := &Strategy{Code: "DUP", Name: "b", Status: StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(a).Error)
	assert.Error(t, db.Create(b).Error)
}

func TestStrategySoftDelete(t *testing.T) {
	db := newTestDB(t)
	s := &Strategy{Code: "S-SOFT", Name: "x", Status: StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(s).Error)
	require.NoError(t, db.Delete(s).Error)

	// Default find ignores.
	var got Strategy
	assert.Error(t, db.First(&got, s.ID).Error)

	// Unscoped returns the row with deleted_at set.
	require.NoError(t, db.Unscoped().First(&got, s.ID).Error)
	assert.True(t, got.DeletedAt.Valid)
}

func TestStrategyIndexes(t *testing.T) {
	db := newTestDB(t)
	m := db.Migrator()
	indexes, err := m.GetIndexes(&Strategy{})
	require.NoError(t, err)

	// Expect at least: unique on code, index on name, index on status, plus
	// the embedded BaseEntity indexes.
	//
	// GORM treats gorm.DeletedAt specially; the soft-delete column gets its
	// own index but is not concatenated into composite uniqueIndex tags in
	// the SQLite migrator. We therefore only assert that a unique index
	// exists with `code` in its columns (the soft-delete column is
	// enforced separately by the GORM delete callbacks).
	var hasCodeUnique, hasName, hasStatus bool
	for _, idx := range indexes {
		uniq, _ := idx.Unique()
		cols := idx.Columns()
		if uniq && containsCol(cols, "code") {
			hasCodeUnique = true
		}
		if !uniq && containsCol(cols, "name") {
			hasName = true
		}
		if !uniq && containsCol(cols, "status") {
			hasStatus = true
		}
	}
	assert.True(t, hasCodeUnique, "expected unique index on strategies.code")
	assert.True(t, hasName, "expected index on strategies.name")
	assert.True(t, hasStatus, "expected index on strategies.status")
}

func TestStrategyJSONTags(t *testing.T) {
	now := newTestTime()
	s := Strategy{
		BaseEntity:     BaseEntity{ID: 7, CreatedAt: now, UpdatedAt: now},
		Code:           "X",
		Name:           "y",
		Status:         "active",
		CurrentVersion: 3,
	}
	b, err := json.Marshal(s)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.EqualValues(t, 7, m["id"])
	assert.Equal(t, "X", m["code"])
	assert.Equal(t, "y", m["name"])
	assert.EqualValues(t, 3, m["current_version"])
	assert.Equal(t, "active", m["status"])
	assert.NotEmpty(t, m["created_at"])
}

func TestStrategyVersionCRUD(t *testing.T) {
	db := newTestDB(t)
	s := &Strategy{Code: "S", Name: "s", Status: StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(s).Error)

	sv := &StrategyVersion{
		StrategyID:      s.ID,
		Version:         1,
		DAGJson:         `{"v":1}`,
		ChangeNote:      "init",
		SnapshotTakenAt: newTestTime(),
	}
	require.NoError(t, db.Create(sv).Error)

	var got StrategyVersion
	require.NoError(t, db.First(&got, sv.ID).Error)
	assert.Equal(t, s.ID, got.StrategyID)
	assert.Equal(t, 1, got.Version)
}

func TestStrategyVersionUniquePerStrategy(t *testing.T) {
	db := newTestDB(t)
	s := &Strategy{Code: "S", Name: "s", Status: StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(s).Error)

	v1 := &StrategyVersion{StrategyID: s.ID, Version: 1, DAGJson: "{}", SnapshotTakenAt: newTestTime()}
	require.NoError(t, db.Create(v1).Error)
	dup := &StrategyVersion{StrategyID: s.ID, Version: 1, DAGJson: "{}", SnapshotTakenAt: newTestTime()}
	assert.Error(t, db.Create(dup).Error)
}

func TestNodeDefinitionCRUD(t *testing.T) {
	db := newTestDB(t)
	s := &Strategy{Code: "N", Name: "n", Status: StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(s).Error)

	nd := &NodeDefinition{
		StrategyID: s.ID,
		NodeKey:    "ma20",
		Type:       NodeTypeIndicator,
		Subtype:    "MA",
		ConfigJson: `{"window":20}`,
	}
	require.NoError(t, db.Create(nd).Error)

	nd2 := &NodeDefinition{
		StrategyID: s.ID,
		NodeKey:    "kdj9",
		Type:       NodeTypeIndicator,
		Subtype:    "KDJ",
		ConfigJson: `{}`,
	}
	require.NoError(t, db.Create(nd2).Error)

	var count int64
	require.NoError(t, db.Model(&NodeDefinition{}).Where("strategy_id = ?", s.ID).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}

func TestNodeDefinitionUniquePerStrategy(t *testing.T) {
	db := newTestDB(t)
	s := &Strategy{Code: "N", Name: "n", Status: StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(s).Error)

	a := &NodeDefinition{StrategyID: s.ID, NodeKey: "k", Type: NodeTypeFilter, ConfigJson: "{}"}
	require.NoError(t, db.Create(a).Error)
	b := &NodeDefinition{StrategyID: s.ID, NodeKey: "k", Type: NodeTypeFilter, ConfigJson: "{}"}
	assert.Error(t, db.Create(b).Error)
}

func containsCol(cols []string, target string) bool {
	for _, c := range cols {
		if c == target {
			return true
		}
	}
	return false
}

// touchTime is a helper to make timestamps round-trip deterministically.
func touchTime(ts time.Time) time.Time { return ts.Round(time.Second).UTC() }

// guard against gorm import linter complaints if a future test re-uses the
// import without code changes.
var _ = gorm.ErrRecordNotFound
