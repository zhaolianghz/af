// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package executor

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

func TestTriggerOrDefault(t *testing.T) {
	if got := triggerOrDefault(""); got != model.RunTriggerManual {
		t.Errorf("empty → %q, want %q", got, model.RunTriggerManual)
	}
	if got := triggerOrDefault("cron"); got != "cron" {
		t.Errorf("cron → %q, want cron", got)
	}
}

func TestRunIDOrZero(t *testing.T) {
	if got := runIDOrZero(nil); got != 0 {
		t.Errorf("nil → %d, want 0", got)
	}
	if got := runIDOrZero(&model.Run{BaseEntity: model.BaseEntity{ID: 7}}); got != 7 {
		t.Errorf("id=7 → %d, want 7", got)
	}
}

func TestMarshalPayload(t *testing.T) {
	if got := marshalPayload(nil); got != "" {
		t.Errorf("nil → %q, want empty", got)
	}
	if got := marshalPayload(map[string]any{}); got != "" {
		t.Errorf("empty map → %q, want empty", got)
	}
	got := marshalPayload(map[string]any{"k": "v"})
	if got != `{"k":"v"}` {
		t.Errorf("map → %q, want {\"k\":\"v\"}", got)
	}
}

func TestFinalizeRun(t *testing.T) {
	// Summary present: status + timestamps copied from summary.
	run := &model.Run{}
	start := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	fin := time.Date(2026, 5, 1, 9, 1, 0, 0, time.UTC)
	finalizeRun(run, &orchestrator.RunSummary{
		Status:     model.RunStatusSuccess,
		StartedAt:  start,
		FinishedAt: fin,
	}, nil)
	if run.Status != model.RunStatusSuccess {
		t.Errorf("status = %q, want succeeded", run.Status)
	}
	if run.FinishedAt == nil || !run.FinishedAt.Equal(fin) {
		t.Errorf("finishedAt not copied from summary")
	}

	// No summary + execErr: status forced to failed.
	run2 := &model.Run{}
	finalizeRun(run2, nil, context.DeadlineExceeded)
	if run2.Status != model.RunStatusFailed {
		t.Errorf("status = %q, want failed (execErr, no summary)", run2.Status)
	}
	if run2.FinishedAt == nil {
		t.Error("finishedAt should be set even with no summary")
	}
}

func TestPersistLogs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := model.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	e := &Executor{db: db}

	// nil summary → no-op, no error.
	if err := e.persistLogs(context.Background(), 1, nil, nil); err != nil {
		t.Errorf("nil summary errored: %v", err)
	}

	// Real summary → rows written.
	summary := &orchestrator.RunSummary{
		Status: model.RunStatusSuccess,
		NodeResults: map[string]orchestrator.NodeResult{
			"n1": {Status: "succeeded", Payload: map[string]any{"count": 3}},
		},
	}
	if err := e.persistLogs(context.Background(), 42, summary, nil); err != nil {
		t.Fatalf("persistLogs: %v", err)
	}
	var n int64
	db.Model(&model.RunLog{}).Where("run_id = ?", 42).Count(&n)
	if n != 1 {
		t.Errorf("run_log count = %d, want 1", n)
	}

	// nil DB executor → no-op.
	e2 := &Executor{db: nil}
	if err := e2.persistLogs(context.Background(), 1, summary, nil); err != nil {
		t.Errorf("nil db errored: %v", err)
	}
}

func TestPublishRunCompleted(t *testing.T) {
	bus := orchestrator.NewMemBus()
	e := &Executor{bus: bus}
	run := &model.Run{
		BaseEntity:  model.BaseEntity{ID: 9},
		StrategyID:  1,
		Status:      model.RunStatusFailed,
		TriggerType: model.RunTriggerManual,
	}
	// Should not panic; publishes a run-completed event with error data.
	e.publishRunCompleted(run, &orchestrator.RunSummary{Duration: time.Second}, context.DeadlineExceeded)

	// nil bus / nil run → no-op, no panic.
	e2 := &Executor{bus: nil}
	e2.publishRunCompleted(run, nil, nil)
	e.publishRunCompleted(nil, nil, nil)
}
