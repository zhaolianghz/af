// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import "time"

// TradingCalendar records whether a given calendar date is an A-share
// trading day. This is the source of truth for the scheduler when deciding
// whether to run a daily selection.
type TradingCalendar struct {
	BaseEntity

	Date      time.Time `gorm:"type:date;uniqueIndex" json:"date"`
	IsTrading bool      `gorm:"index" json:"is_trading"`
	IsWeekend bool      `json:"is_weekend"`
	Note      string    `gorm:"size:128" json:"note"`

	// Source records the data source that last updated the row.
	// One of: tushare / akshare / manual.
	Source string `gorm:"size:32" json:"source"`

	SyncedAt time.Time `json:"synced_at"`
}

// TableName returns the explicit table name.
func (TradingCalendar) TableName() string { return "trading_calendar" }

// TradingCalendar source constants.
const (
	CalendarSourceTushare = "tushare"
	CalendarSourceAKShare = "akshare"
	CalendarSourceManual  = "manual"
)
