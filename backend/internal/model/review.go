// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import "time"

// ReviewReport is a generated daily/weekly performance review (§14.9).
// The body is an LLM (or mock) summary of the period's recommendations
// and their T+N outcomes; the structured metrics are stored alongside
// so the UI can render headline numbers without re-parsing the prose.
type ReviewReport struct {
	BaseEntity

	// Kind is "daily" | "weekly".
	Kind string `gorm:"size:16;index" json:"kind"`
	// PeriodStart / PeriodEnd bound the reviewed window (inclusive
	// start, exclusive end), stored as dates.
	PeriodStart time.Time `gorm:"type:date;index" json:"period_start"`
	PeriodEnd   time.Time `gorm:"type:date" json:"period_end"`

	// RecommendationCount is how many recs fell in the window.
	RecommendationCount int `json:"recommendation_count"`
	// WinRateT5 is the share of recs with t5_return > 0 among those
	// with a known T+5 (nil when none have reached T+5).
	WinRateT5 *float64 `gorm:"type:decimal(12,6)" json:"win_rate_t5"`
	// AvgT5Return is the mean T+5 return among recs with one (nil when
	// none).
	AvgT5Return *float64 `gorm:"type:decimal(12,6)" json:"avg_t5_return"`

	// Summary is the generated narrative review.
	Summary string `json:"summary"`
	// LLM identifies the backend that produced the summary.
	LLM string `gorm:"size:64" json:"llm"`
}

// TableName returns the explicit table name.
func (ReviewReport) TableName() string { return "review_reports" }

// Review kind constants.
const (
	ReviewKindDaily  = "daily"
	ReviewKindWeekly = "weekly"
)
