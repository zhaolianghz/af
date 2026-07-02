// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCRUD(t *testing.T) {
	db := newTestDB(t)
	s := &Strategy{Code: "R", Name: "r", Status: StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(s).Error)

	now := newTestTime()
	started := now
	finished := now.Add(2 * time.Minute)
	run := &Run{
		StrategyID:  s.ID,
		TriggerType: RunTriggerManual,
		Status:      RunStatusSuccess,
		StartedAt:   &started,
		FinishedAt:  &finished,
	}
	require.NoError(t, db.Create(run).Error)

	var got Run
	require.NoError(t, db.First(&got, run.ID).Error)
	assert.Equal(t, s.ID, got.StrategyID)
	assert.Equal(t, RunStatusSuccess, got.Status)
	assert.Equal(t, 1, got.Attempts)
	assert.NotNil(t, got.StartedAt)
	assert.NotNil(t, got.FinishedAt)
}

func TestRunLogCRUD(t *testing.T) {
	db := newTestDB(t)
	s := &Strategy{Code: "RL", Name: "rl", Status: StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(s).Error)

	run := &Run{StrategyID: s.ID, TriggerType: RunTriggerCron, Status: RunStatusSuccess}
	require.NoError(t, db.Create(run).Error)

	now := newTestTime()
	log := &RunLog{
		RunID:      run.ID,
		NodeKey:    "ma20",
		Status:     RunLogStatusSuccess,
		StartedAt:  now,
		FinishedAt: now.Add(100 * time.Millisecond),
		PayloadIn:  `{"rows":5000}`,
		PayloadOut: `{"rows":347}`,
	}
	require.NoError(t, db.Create(log).Error)

	var logs []RunLog
	require.NoError(t, db.Where("run_id = ?", run.ID).Find(&logs).Error)
	assert.Len(t, logs, 1)
	assert.Equal(t, "ma20", logs[0].NodeKey)
}

func TestRunJSONTags(t *testing.T) {
	now := newTestTime()
	run := Run{
		BaseEntity:  BaseEntity{ID: 1, CreatedAt: now, UpdatedAt: now},
		StrategyID:  5,
		TriggerType: RunTriggerManual,
		Status:      RunStatusSuccess,
	}
	b, err := json.Marshal(run)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.EqualValues(t, 5, m["strategy_id"])
	assert.Equal(t, "manual", m["trigger_type"])
	assert.Equal(t, "success", m["status"])
}
