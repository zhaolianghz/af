// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for PersistNode — verifies the dry-run path, the actual
// write path with a SQLite DB, session tag attachment, and
// edge cases (no items, missing stock_code, extra tags, custom
// date).
package nodes

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

func newPersistDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "persist_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	return db
}

func newPersistRC(db *gorm.DB) *orchestrator.RunContext {
	return orchestrator.NewRunContext(orchestrator.RunContextOptions{
		DB:           db,
		Logger:       zap.NewNop(),
		RunID:        42,
		StrategyCode: "test_strat",
		StrategyName: "Test Strategy",
		Clock: func() time.Time {
			return time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
		},
	})
}

func pickList() []any {
	return []any{
		map[string]any{
			"stock_code":      "600000.SH",
			"stock_name":      "浦发银行",
			"entry_price_low": 10.0,
			"entry_price_high": 10.5,
		},
		map[string]any{
			"stock_code": "000001.SZ",
		},
	}
}

// =============================================================================
// Validation
// =============================================================================

func TestPersist_InvalidParamsJSON(t *testing.T) {
	n := NewPersistNode()
	in := map[string]any{orchestrator.InputKeyParams: []byte(`not json`)}
	_, err := n.Run(context.Background(), newPersistRC(nil), in)
	require.Error(t, err)
}

func TestPersist_NoItems(t *testing.T) {
	n := NewPersistNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, persistParams{}),
	}
	out, err := n.Run(context.Background(), newPersistRC(nil), in)
	require.NoError(t, err)
	require.Equal(t, 0, out["persisted"])
}

// =============================================================================
// Dry-run
// =============================================================================

func TestPersist_DryRun_NoDB(t *testing.T) {
	n := NewPersistNode()
	rc := newPersistRC(nil)
	items := pickList()
	in := map[string]any{
		"pred":                     map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, persistParams{}),
	}
	out, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	require.Equal(t, true, out["dry_run"])
	require.Equal(t, 2, out["persisted"])
}

func TestPersist_DryRun_PicksUpSessionTag(t *testing.T) {
	// If a session_tag was set in RunContext.Vars, the dry-run
	// summary includes it in "tags".
	n := NewPersistNode()
	rc := newPersistRC(nil)
	rc.SetVar(orchestrator.VarKeySessionTag, model.SessionTagMorning)

	in := map[string]any{
		"pred": map[string]any{"items": pickList()},
		orchestrator.InputKeyParams: params(t, persistParams{
			ExtraTags: []string{"hot"},
		}),
	}
	out, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	require.Equal(t, true, out["dry_run"])
	tags, ok := out["tags"].([]string)
	require.True(t, ok)
	require.Contains(t, tags, model.SessionTagMorning)
	require.Contains(t, tags, "hot")
}

// =============================================================================
// Real DB path
// =============================================================================

func TestPersist_WritesRecommendations(t *testing.T) {
	db := newPersistDB(t)
	rc := newPersistRC(db)
	rc.SetVar(orchestrator.VarKeySessionTag, model.SessionTagMorning)

	n := NewPersistNode()
	in := map[string]any{
		"pred": map[string]any{"items": pickList()},
		orchestrator.InputKeyParams: params(t, persistParams{}),
	}
	out, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	require.Equal(t, 2, out["persisted"])

	// Verify rows in DB.
	var recs []model.Recommendation
	require.NoError(t, db.Find(&recs).Error)
	require.Len(t, recs, 2)
	codes := []string{recs[0].StockCode, recs[1].StockCode}
	require.Contains(t, codes, "600000.SH")
	require.Contains(t, codes, "000001.SZ")

	// Tags should be attached.
	var tags []model.RecommendationTag
	require.NoError(t, db.Find(&tags).Error)
	require.Len(t, tags, 2) // one tag per rec
	for _, tag := range tags {
		require.Equal(t, model.SessionTagMorning, tag.Tag)
		require.Equal(t, model.TagSourceAutoNode, tag.Source)
	}
}

func TestPersist_EntryPriceFallsBackToClose(t *testing.T) {
	db := newPersistDB(t)
	rc := newPersistRC(db)

	n := NewPersistNode()
	in := map[string]any{
		"pred": map[string]any{"items": []any{
			// No entry_price_* — only close (the indicator-row shape).
			map[string]any{"stock_code": "600519.SH", "close": 1241.41},
			// Explicit entry prices must be preserved, not overwritten.
			map[string]any{"stock_code": "000001.SZ", "close": 12.0,
				"entry_price_low": 11.5, "entry_price_high": 12.5},
		}},
		orchestrator.InputKeyParams: params(t, persistParams{}),
	}
	out, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	require.Equal(t, 2, out["persisted"])

	var recs []model.Recommendation
	require.NoError(t, db.Order("stock_code").Find(&recs).Error)
	require.Len(t, recs, 2)
	// 000001.SZ keeps its explicit prices.
	require.Equal(t, 11.5, recs[0].EntryPriceLow)
	require.Equal(t, 12.5, recs[0].EntryPriceHigh)
	// 600519.SH falls back to close for both.
	require.Equal(t, 1241.41, recs[1].EntryPriceLow)
	require.Equal(t, 1241.41, recs[1].EntryPriceHigh)
}

func TestPersist_DropsItemsWithoutStockCode(t *testing.T) {
	db := newPersistDB(t)
	rc := newPersistRC(db)

	n := NewPersistNode()
	items := []any{
		map[string]any{"stock_code": "600000.SH"},
		map[string]any{"no_code": true},     // dropped
		"not a map",                          // dropped
	}
	in := map[string]any{
		"pred":                     map[string]any{"items": items},
		orchestrator.InputKeyParams: params(t, persistParams{}),
	}
	out, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	require.Equal(t, 1, out["persisted"])
}

func TestPersist_CustomDate(t *testing.T) {
	db := newPersistDB(t)
	rc := newPersistRC(db)
	n := NewPersistNode()
	in := map[string]any{
		"pred": map[string]any{"items": []any{
			map[string]any{"stock_code": "600000.SH"},
		}},
		orchestrator.InputKeyParams: params(t, persistParams{
			Date: "2025-12-31",
		}),
	}
	out, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	require.Equal(t, "2025-12-31", out["date"])
}

func TestPersist_ExtraTagsDeduplicated(t *testing.T) {
	db := newPersistDB(t)
	rc := newPersistRC(db)
	rc.SetVar(orchestrator.VarKeySessionTag, model.SessionTagMorning)
	n := NewPersistNode()
	in := map[string]any{
		"pred": map[string]any{"items": []any{
			map[string]any{"stock_code": "600000.SH"},
		}},
		orchestrator.InputKeyParams: params(t, persistParams{
			ExtraTags: []string{"hot", "hot", "momentum"},
		}),
	}
	_, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)

	var tags []model.RecommendationTag
	require.NoError(t, db.Find(&tags).Error)
	// MORNING + hot + momentum = 3 unique tags
	require.Len(t, tags, 3)
}

func TestPersist_EmptyTags_NoTagRows(t *testing.T) {
	db := newPersistDB(t)
	rc := newPersistRC(db)
	// No session_tag set, no extra tags.
	n := NewPersistNode()
	in := map[string]any{
		"pred": map[string]any{"items": []any{
			map[string]any{"stock_code": "600000.SH"},
		}},
		orchestrator.InputKeyParams: params(t, persistParams{}),
	}
	_, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)

	var tags []model.RecommendationTag
	require.NoError(t, db.Find(&tags).Error)
	require.Len(t, tags, 0)
}
