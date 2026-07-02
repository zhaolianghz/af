// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import "time"

// Run records a single execution of a Strategy.
type Run struct {
	BaseEntity

	StrategyID  uint64 `gorm:"index" json:"strategy_id"`
	TriggerType string `gorm:"size:16" json:"trigger_type"` // manual / cron
	Status      string `gorm:"size:16;index" json:"status"` // pending / running / success / failed / partial / skipped

	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	SkipReason  string     `gorm:"size:64" json:"skip_reason"`
	Error       string     `gorm:"type:text" json:"error"`
	LogURL      string     `gorm:"size:512" json:"log_url"`
	Attempts    int        `gorm:"default:1" json:"attempts"`
}

// TableName returns the explicit table name.
func (Run) TableName() string { return "runs" }

// RunLog records the per-node execution log of a Run.
type RunLog struct {
	BaseEntity

	RunID        uint64    `gorm:"index" json:"run_id"`
	NodeKey      string    `gorm:"size:64;index" json:"node_key"`
	Status       string    `gorm:"size:16" json:"status"` // success / failed / skipped
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	PayloadIn    string    `json:"payload_in"`
	PayloadOut   string    `json:"payload_out"`
	Error        string    `gorm:"type:text" json:"error"`
}

// TableName returns the explicit table name.
func (RunLog) TableName() string { return "run_logs" }

// Run status constants.
const (
	RunStatusPending  = "pending"
	RunStatusRunning  = "running"
	RunStatusSuccess  = "success"
	RunStatusFailed   = "failed"
	RunStatusPartial  = "partial"
	RunStatusSkipped  = "skipped"
)

// Run trigger type constants.
const (
	RunTriggerManual = "manual"
	RunTriggerCron   = "cron"
)

// RunLog status constants.
const (
	RunLogStatusSuccess = "success"
	RunLogStatusFailed  = "failed"
	RunLogStatusSkipped = "skipped"
)
