// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import "time"

// DatasourceHealth records the last-known health of a market data source.
// A3 writes to this table from the datasource adapter layer.
type DatasourceHealth struct {
	BaseEntity

	// Source is one of: eastmoney / sina / akshare / tushare.
	Source string `gorm:"size:32;uniqueIndex" json:"source"`

	// Status is one of: healthy / degraded / down.
	Status string `gorm:"size:16;index" json:"status"`

	LastOK   *time.Time `json:"last_ok,omitempty"`
	LastFail *time.Time `json:"last_fail,omitempty"`

	// FailCount5m is the rolling failure count in the last 5 minutes.
	FailCount5m int `gorm:"default:0" json:"fail_count_5m"`

	LastError string `gorm:"type:text" json:"last_error"`
}

// TableName returns the explicit table name.
func (DatasourceHealth) TableName() string { return "datasource_health" }

// Datasource health status constants.
const (
	HealthStatusHealthy  = "healthy"
	HealthStatusDegraded = "degraded"
	HealthStatusDown     = "down"
)

// Datasource source constants (the unique values that may appear in the
// Source column).
const (
	DataSourceEastMoney = "eastmoney"
	DataSourceSina      = "sina"
	DataSourceAKShare   = "akshare"
	DataSourceTushare   = "tushare"
)

// ProviderSwitchEvent records each manual or automatic switch of the active
// data provider, with a reason for the audit trail.
type ProviderSwitchEvent struct {
	BaseEntity

	FromSource string    `gorm:"size:32" json:"from_source"`
	ToSource   string    `gorm:"size:32" json:"to_source"`
	Reason     string    `gorm:"type:text" json:"reason"`
	SwitchedAt time.Time `gorm:"index" json:"switched_at"`
}

// TableName returns the explicit table name.
func (ProviderSwitchEvent) TableName() string { return "provider_switch_events" }
