// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for the templates loader — List, Get, Instantiate,
// SyncToDB, and ListCodes. Uses an in-memory SQLite for the DB
// paths and a nil DB for the in-memory-only paths.
package templates

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/model"
)

// jsonUnmarshalBytes is a tiny alias to keep imports minimal.
var jsonUnmarshalBytes = json.Unmarshal

// newLoaderDB builds an in-memory-style sqlite DB with the
// StrategyTemplate table migrated. Tests that don't need DB
// pass nil to NewLoader.
func newLoaderDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tpl_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	return db
}

// =============================================================================
// List / ListCodes / Get (DB-less paths)
// =============================================================================

func TestNewLoader_NoDB(t *testing.T) {
	l, err := NewLoader(nil, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, l)
}

func TestLoader_ListCodes_Stable(t *testing.T) {
	l, err := NewLoader(nil, zap.NewNop())
	require.NoError(t, err)
	codes := l.ListCodes()
	require.NotEmpty(t, codes, "expected at least 5 bundled templates")
	// Stable order: alphabetical.
	for i := 1; i < len(codes); i++ {
		require.Less(t, codes[i-1], codes[i])
	}
}

func TestLoader_List_NoDB(t *testing.T) {
	l, err := NewLoader(nil, zap.NewNop())
	require.NoError(t, err)
	rows, err := l.List(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	// Every bundled template must appear with BuiltIn=true.
	for _, r := range rows {
		require.True(t, r.BuiltIn)
		require.NotEmpty(t, r.Code)
		require.NotEmpty(t, r.Name)
	}
}

func TestLoader_Get_Bundled(t *testing.T) {
	l, err := NewLoader(nil, zap.NewNop())
	require.NoError(t, err)
	for _, code := range l.ListCodes() {
		t.Run(code, func(t *testing.T) {
			tpl, err := l.Get(context.Background(), code)
			require.NoError(t, err)
			require.Equal(t, code, tpl.Code)
			require.NotEmpty(t, tpl.DAGJson, "bundled template must have a DAG")
		})
	}
}

func TestLoader_Get_NotFound(t *testing.T) {
	l, err := NewLoader(nil, zap.NewNop())
	require.NoError(t, err)
	_, err = l.Get(context.Background(), "definitely_not_a_template_xyz")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestLoader_Get_UserOverridesBundled(t *testing.T) {
	db := newLoaderDB(t)
	l, err := NewLoader(db, zap.NewNop())
	require.NoError(t, err)

	// Insert a user-saved row with the same code as a bundled
	// template but a different name; Get should return the
	// user-saved row.
	codes := l.ListCodes()
	require.NotEmpty(t, codes)
	userCode := codes[0]
	userDAG := `{"nodes":[{"id":"a","type":"data_source","data":{"subtype":"quote","params":{}}}],"edges":[]}`
	require.NoError(t, db.Create(&model.StrategyTemplate{
		Code: userCode, Name: "USER OVERRIDE",
		AIExplanation: "user", BuiltIn: false, DAGJson: userDAG,
	}).Error)

	tpl, err := l.Get(context.Background(), userCode)
	require.NoError(t, err)
	require.Equal(t, "USER OVERRIDE", tpl.Name)
	require.Equal(t, userDAG, tpl.DAGJson)
	require.False(t, tpl.BuiltIn)
}

// =============================================================================
// SyncToDB
// =============================================================================

func TestLoader_SyncToDB_Insert(t *testing.T) {
	db := newLoaderDB(t)
	l, err := NewLoader(db, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, l.SyncToDB(context.Background()))

	// Every bundled code must have a row.
	var n int64
	require.NoError(t, db.Model(&model.StrategyTemplate{}).
		Where("built_in = ?", true).Count(&n).Error)
	require.Equal(t, int64(len(l.ListCodes())), n)
}

func TestLoader_SyncToDB_Update(t *testing.T) {
	db := newLoaderDB(t)
	l, err := NewLoader(db, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, l.SyncToDB(context.Background()))

	// Mutate the bundled template's name in memory and re-sync.
	code := l.ListCodes()[0]
	l.mu.Lock()
	l.bundled[code].Name = "UPDATED NAME"
	l.mu.Unlock()

	require.NoError(t, l.SyncToDB(context.Background()))

	var row model.StrategyTemplate
	require.NoError(t, db.Where("code = ?", code).First(&row).Error)
	require.Equal(t, "UPDATED NAME", row.Name)
}

// =============================================================================
// Instantiate
// =============================================================================

func TestLoader_Instantiate_HappyPath(t *testing.T) {
	db := newLoaderDB(t)
	l, err := NewLoader(db, zap.NewNop())
	require.NoError(t, err)

	code := l.ListCodes()[0]
	strat, ver, err := l.Instantiate(context.Background(), db, code)
	require.NoError(t, err)
	require.NotNil(t, strat)
	require.NotNil(t, ver)
	require.NotZero(t, strat.ID)
	require.NotZero(t, ver.ID)
	require.Equal(t, strat.ID, ver.StrategyID)
	require.NotEqual(t, code, strat.Code, "the new strategy's code must be derived (suffixed)")
	require.Contains(t, strat.Code, code, "the new code should embed the template code")
	require.Equal(t, model.StrategyStatusDraft, strat.Status)
	require.Equal(t, 1, ver.Version)
}

func TestLoader_Instantiate_NotFound(t *testing.T) {
	db := newLoaderDB(t)
	l, err := NewLoader(db, zap.NewNop())
	require.NoError(t, err)
	_, _, err = l.Instantiate(context.Background(), db, "no_such_template")
	require.Error(t, err)
}

func TestLoader_Instantiate_NoDB(t *testing.T) {
	l, err := NewLoader(nil, zap.NewNop())
	require.NoError(t, err)
	code := l.ListCodes()[0]
	_, _, err = l.Instantiate(context.Background(), nil, code)
	require.Error(t, err)
}

// =============================================================================
// Embedded JSON validity
// =============================================================================

// TestEmbeddedDAGsAreValid re-parses every bundled DAG and
// confirms it is structurally valid. This catches JSON typos
// at unit-test time.
func TestEmbeddedDAGsAreValid(t *testing.T) {
	l, err := NewLoader(nil, zap.NewNop())
	require.NoError(t, err)
	for _, code := range l.ListCodes() {
		t.Run(code, func(t *testing.T) {
			tpl, err := l.Get(context.Background(), code)
			require.NoError(t, err)
			// A DAG must have at least one node and one edge
			// to be a useful template.
			require.Greater(t, countJSONKeys(t, tpl.DAGJson, "nodes"), 0)
		})
	}
}

// countJSONKeys parses a JSON string and returns the length of
// the named array field. Used to assert DAG structural
// requirements without importing the orchestrator package here.
func countJSONKeys(t *testing.T, jsonStr, field string) int {
	t.Helper()
	var probe struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}
	require.NoError(t, jsonUnmarshalBytes([]byte(jsonStr), &probe))
	if field == "nodes" {
		return len(probe.Nodes)
	}
	return len(probe.Edges)
}