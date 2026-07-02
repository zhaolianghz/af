// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import "time"

// Strategy is a business-level selector strategy. It owns a DAG (stored as
// JSON in DAGJson) and tracks a current version pointer.
type Strategy struct {
	BaseEntity

	// Code is the unique business code for the strategy.
	Code string `gorm:"size:64;uniqueIndex:uk_code_deleted,priority:1" json:"code"`

	Name        string `gorm:"size:128;index" json:"name"`
	Description string `gorm:"type:text" json:"description"`

	// Status is one of: draft / active / disabled.
	Status string `gorm:"size:16;default:draft;index" json:"status"`

	// Tags is a comma-separated list, e.g. "momentum,mid-cap,weekly".
	Tags string `gorm:"size:512" json:"tags"`

	// CurrentVersion is the pointer to the active StrategyVersion row.
	CurrentVersion int `gorm:"default:1" json:"current_version"`

	// DAGJson is the JSON-encoded DAG of the current version.
	DAGJson string `json:"dag_json"`

	// CronExpression is an optional 5-field cron string. When
	// set and Status=active, the scheduler fires the
	// strategy at the matching wall-clock times in
	// config.Cron.Timezone. Empty means "manual-only".
	CronExpression string `gorm:"size:64" json:"cron_expression"`
}

// TableName returns the explicit table name.
func (Strategy) TableName() string { return "strategies" }

// StrategyVersion is an immutable snapshot of a Strategy's DAG at a point in
// time. Old versions are kept for audit / rollback.
type StrategyVersion struct {
	BaseEntity

	StrategyID uint64 `gorm:"uniqueIndex:uk_strategy_version,priority:1" json:"strategy_id"`
	Version    int    `gorm:"uniqueIndex:uk_strategy_version,priority:2" json:"version"`

	DAGJson    string `json:"dag_json"`
	ChangeNote string `gorm:"size:512" json:"change_note"`

	// SnapshotTakenAt is the wall-clock time the version was published.
	SnapshotTakenAt time.Time `json:"snapshot_taken_at"`
}

// TableName returns the explicit table name.
func (StrategyVersion) TableName() string { return "strategy_versions" }

// NodeDefinition stores per-strategy node metadata. The actual node
// configuration lives in the DAG JSON of the owning Strategy/StrategyVersion;
// this table is a denormalized projection used for queries and inspection.
type NodeDefinition struct {
	BaseEntity

	StrategyID uint64 `gorm:"index" json:"strategy_id"`
	NodeKey    string `gorm:"size:64;uniqueIndex:uk_strategy_node,priority:2" json:"node_key"`
	Type       string `gorm:"size:32;index" json:"type"`
	Subtype    string `gorm:"size:32" json:"subtype"`
	ConfigJson string `json:"config_json"`
}

// TableName returns the explicit table name.
func (NodeDefinition) TableName() string { return "node_definitions" }

// Node type constants — keep in sync with the spec §2.1.
const (
	NodeTypeDataSource = "data_source"
	NodeTypeIndicator  = "indicator"
	NodeTypeFilter     = "filter"
	NodeTypeRank       = "rank"
	NodeTypeDedupe     = "dedupe"
	NodeTypeSessionTag = "session_tag"
	NodeTypePersist    = "persist"
	NodeTypeNotify     = "notify"
)

// Strategy status constants.
const (
	StrategyStatusDraft    = "draft"
	StrategyStatusActive   = "active"
	StrategyStatusDisabled = "disabled"
)
