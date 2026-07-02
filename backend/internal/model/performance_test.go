// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerformanceSnapshotCRUD(t *testing.T) {
	db := newTestDB(t)
	date := newTestTime()
	rec := &Recommendation{
		RunID: 1, Date: date, StockCode: "1", StockName: "n",
		StrategyCode: "c", StrategyName: "n", NodeSnapshot: "{}",
	}
	require.NoError(t, db.Create(rec).Error)

	t1 := 0.0123
	t3 := 0.0456
	t5 := -0.0123
	ps := &PerformanceSnapshot{
		RecommendationID: rec.ID,
		SnapshotDate:     date,
		T1Return:         &t1,
		T3Return:         &t3,
		T5Return:         &t5,
		CalculatedAt:     date,
	}
	require.NoError(t, db.Create(ps).Error)

	var got PerformanceSnapshot
	require.NoError(t, db.First(&got, ps.ID).Error)
	require.NotNil(t, got.T1Return)
	assert.InDelta(t, 0.0123, *got.T1Return, 1e-6)
}

func TestPerformanceSnapshotUniqueRecDate(t *testing.T) {
	db := newTestDB(t)
	date := newTestTime()
	rec := &Recommendation{
		RunID: 1, Date: date, StockCode: "1", StockName: "n",
		StrategyCode: "c", StrategyName: "n", NodeSnapshot: "{}",
	}
	require.NoError(t, db.Create(rec).Error)

	require.NoError(t, db.Create(&PerformanceSnapshot{RecommendationID: rec.ID, SnapshotDate: date, CalculatedAt: date}).Error)
	assert.Error(t, db.Create(&PerformanceSnapshot{RecommendationID: rec.ID, SnapshotDate: date, CalculatedAt: date}).Error)
}

func TestPerformanceSnapshotJSONOmitNil(t *testing.T) {
	ps := PerformanceSnapshot{}
	b, err := json.Marshal(ps)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	// Nil pointers should be omitted because of omitempty.
	_, hasT1 := m["t1_return"]
	assert.False(t, hasT1, "nil t1_return should be omitted from JSON")
}
