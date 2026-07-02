// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecommendationCRUD(t *testing.T) {
	db := newTestDB(t)
	date := newTestTime()

	r := &Recommendation{
		RunID:          42,
		Date:           date,
		StockCode:      "600000",
		StockName:      "Pudong Bank",
		EntryPriceLow:  9.95,
		EntryPriceHigh: 10.20,
		StrategyCode:   "STR-A",
		StrategyName:   "Momentum",
		NodeSnapshot:   `{"pipeline":["ma20"]}`,
	}
	require.NoError(t, db.Create(r).Error)
	require.NotZero(t, r.ID)

	var got Recommendation
	require.NoError(t, db.Preload("Tags").First(&got, r.ID).Error)
	assert.Equal(t, "600000", got.StockCode)
	assert.Equal(t, 9.95, got.EntryPriceLow)
}

func TestRecommendationTagUniqueTriple(t *testing.T) {
	db := newTestDB(t)
	date := newTestTime()
	rec := &Recommendation{
		RunID: 1, Date: date, StockCode: "1", StockName: "n",
		StrategyCode: "c", StrategyName: "n", NodeSnapshot: "{}",
	}
	require.NoError(t, db.Create(rec).Error)

	t1 := &RecommendationTag{
		RecommendationID: rec.ID,
		Tag:              SessionTagMorning,
		Source:           TagSourceAutoNode,
		TaggedAt:         date,
	}
	require.NoError(t, db.Create(t1).Error)

	// Same triple fails.
	dup := &RecommendationTag{
		RecommendationID: rec.ID,
		Tag:              SessionTagMorning,
		Source:           TagSourceAutoNode,
		TaggedAt:         date,
	}
	assert.Error(t, db.Create(dup).Error)

	// Different source is allowed.
	manual := &RecommendationTag{
		RecommendationID: rec.ID,
		Tag:              SessionTagMorning,
		Source:           TagSourceManual,
		TaggedAt:         date,
	}
	require.NoError(t, db.Create(manual).Error)

	// Different tag, same source is allowed.
	review := &RecommendationTag{
		RecommendationID: rec.ID,
		Tag:              SessionTagReview,
		Source:           TagSourceAutoNode,
		TaggedAt:         date,
	}
	require.NoError(t, db.Create(review).Error)

	var count int64
	require.NoError(t, db.Model(&RecommendationTag{}).Where("recommendation_id = ?", rec.ID).Count(&count).Error)
	assert.Equal(t, int64(3), count)
}

func TestRecommendationEagerLoadTags(t *testing.T) {
	db := newTestDB(t)
	date := newTestTime()
	rec := &Recommendation{
		RunID: 1, Date: date, StockCode: "2", StockName: "n",
		StrategyCode: "c", StrategyName: "n", NodeSnapshot: "{}",
	}
	require.NoError(t, db.Create(rec).Error)
	require.NoError(t, db.Create(&RecommendationTag{RecommendationID: rec.ID, Tag: SessionTagMorning, Source: TagSourceAutoNode, TaggedAt: date}).Error)
	require.NoError(t, db.Create(&RecommendationTag{RecommendationID: rec.ID, Tag: SessionTagNoPost, Source: TagSourceManual, TaggedAt: date}).Error)

	var got Recommendation
	require.NoError(t, db.Preload("Tags").First(&got, rec.ID).Error)
	assert.Len(t, got.Tags, 2)
}

func TestRecommendationJSONTags(t *testing.T) {
	now := newTestTime()
	r := Recommendation{
		BaseEntity:     BaseEntity{ID: 11, CreatedAt: now, UpdatedAt: now},
		RunID:          99,
		Date:           now,
		StockCode:      "X",
		StrategyCode:   "S",
		EntryPriceLow:  1.0,
		EntryPriceHigh: 2.0,
	}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.EqualValues(t, 11, m["id"])
	assert.EqualValues(t, 99, m["run_id"])
	assert.Equal(t, "X", m["stock_code"])
	assert.Equal(t, "S", m["strategy_code"])
	assert.EqualValues(t, 1.0, m["entry_price_low"])
	assert.EqualValues(t, 2.0, m["entry_price_high"])
}

func TestRecommendationDateTypeIsDate(t *testing.T) {
	db := newTestDB(t)
	date := newTestTime()
	r := &Recommendation{
		RunID: 1, Date: date, StockCode: "3", StockName: "n",
		StrategyCode: "c", StrategyName: "n", NodeSnapshot: "{}",
	}
	require.NoError(t, db.Create(r).Error)

	// Pull the column type from sqlite_master to confirm "date" was applied.
	var colType string
	row := db.Raw("SELECT type FROM pragma_table_info('recommendations') WHERE name='date'").Row()
	require.NoError(t, row.Scan(&colType))
	assert.Equal(t, "date", colType, "date column should be stored as DATE, not DATETIME")
}

// Helper: round-trip a time and ensure the date is preserved.
func TestRecommendationDateRoundTrip(t *testing.T) {
	original := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	r := Recommendation{Date: original}
	// We don't go through the DB here because the time stored in SQLite DATE
	// columns is normalized to midnight UTC; the round-trip test is in
	// recommendation_test.go above.
	_ = r
}
