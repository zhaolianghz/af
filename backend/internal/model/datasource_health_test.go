// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatasourceHealthCRUD(t *testing.T) {
	db := newTestDB(t)
	now := newTestTime()
	dh := &DatasourceHealth{
		Source:      DataSourceEastMoney,
		Status:      HealthStatusHealthy,
		LastOK:      &now,
		FailCount5m: 0,
	}
	require.NoError(t, db.Create(dh).Error)

	dh2 := &DatasourceHealth{
		Source:      DataSourceSina,
		Status:      HealthStatusDegraded,
		LastFail:    &now,
		FailCount5m: 3,
		LastError:   "timeout",
	}
	require.NoError(t, db.Create(dh2).Error)

	var all []DatasourceHealth
	require.NoError(t, db.Find(&all).Error)
	assert.Len(t, all, 2)
}

func TestDatasourceHealthUniqueSource(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Create(&DatasourceHealth{Source: DataSourceSina, Status: HealthStatusHealthy}).Error)
	assert.Error(t, db.Create(&DatasourceHealth{Source: DataSourceSina, Status: HealthStatusDown}).Error)
}

func TestProviderSwitchEventCRUD(t *testing.T) {
	db := newTestDB(t)
	now := newTestTime()
	ev := &ProviderSwitchEvent{
		FromSource: DataSourceSina,
		ToSource:   DataSourceEastMoney,
		Reason:     "Sina returned 503",
		SwitchedAt: now,
	}
	require.NoError(t, db.Create(ev).Error)

	var got ProviderSwitchEvent
	require.NoError(t, db.First(&got, ev.ID).Error)
	assert.Equal(t, DataSourceSina, got.FromSource)
	assert.Equal(t, DataSourceEastMoney, got.ToSource)
}

func TestProviderSwitchEventIndexOnSwitchedAt(t *testing.T) {
	db := newTestDB(t)
	m := db.Migrator()
	indexes, err := m.GetIndexes(&ProviderSwitchEvent{})
	require.NoError(t, err)
	hasIdx := false
	for _, idx := range indexes {
		for _, c := range idx.Columns() {
			if c == "switched_at" {
				hasIdx = true
			}
		}
	}
	assert.True(t, hasIdx, "expected index on provider_switch_events.switched_at")
}

var _ = time.Second // keep time import used
