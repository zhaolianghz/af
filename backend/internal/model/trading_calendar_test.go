// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradingCalendarCRUD(t *testing.T) {
	db := newTestDB(t)
	date := newTestTime()
	tc := &TradingCalendar{
		Date:      date,
		IsTrading: true,
		IsWeekend: false,
		Source:    CalendarSourceTushare,
		SyncedAt:  date,
	}
	require.NoError(t, db.Create(tc).Error)

	var got TradingCalendar
	require.NoError(t, db.First(&got, tc.ID).Error)
	assert.Equal(t, CalendarSourceTushare, got.Source)
	assert.True(t, got.IsTrading)
	assert.False(t, got.IsWeekend)
}

func TestTradingCalendarUniqueDate(t *testing.T) {
	db := newTestDB(t)
	date := newTestTime()
	require.NoError(t, db.Create(&TradingCalendar{Date: date, IsTrading: true, Source: CalendarSourceTushare, SyncedAt: date}).Error)
	assert.Error(t, db.Create(&TradingCalendar{Date: date, IsTrading: false, Source: CalendarSourceTushare, SyncedAt: date}).Error)
}

func TestTradingCalendarJSONTags(t *testing.T) {
	now := newTestTime()
	tc := TradingCalendar{
		BaseEntity: BaseEntity{ID: 1, CreatedAt: now, UpdatedAt: now},
		Date:       now,
		IsTrading:  true,
		Source:     CalendarSourceAKShare,
	}
	b, err := json.Marshal(tc)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, CalendarSourceAKShare, m["source"])
	assert.Equal(t, true, m["is_trading"])
	// time fields render as RFC3339 strings
	_, ok := m["synced_at"].(string)
	assert.True(t, ok, "synced_at should serialize as RFC3339 string, got %T", m["synced_at"])
}

// ensure time package import remains used in future expansion
var _ = time.Time{}
