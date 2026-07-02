// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import "time"

// PerformanceSnapshot records the post-hoc performance of a Recommendation.
// One row per (recommendation, snapshot_date) pair.
type PerformanceSnapshot struct {
	BaseEntity

	RecommendationID uint64    `gorm:"uniqueIndex:uk_rec_date,priority:1" json:"recommendation_id"`
	SnapshotDate     time.Time `gorm:"type:date;uniqueIndex:uk_rec_date,priority:2" json:"snapshot_date"`

	// Forward returns (T+1, T+3, T+5) — nullable because a snapshot may be
	// taken before the close is available.
	T1Return *float64 `gorm:"type:decimal(12,6)" json:"t1_return,omitempty"`
	T3Return *float64 `gorm:"type:decimal(12,6)" json:"t3_return,omitempty"`
	T5Return *float64 `gorm:"type:decimal(12,6)" json:"t5_return,omitempty"`

	T1Close *float64 `gorm:"type:decimal(12,4)" json:"t1_close,omitempty"`
	T3Close *float64 `gorm:"type:decimal(12,4)" json:"t3_close,omitempty"`
	T5Close *float64 `gorm:"type:decimal(12,4)" json:"t5_close,omitempty"`

	// CumulativeReturn and MaxDrawdown cover the full evaluation window.
	CumulativeReturn *float64 `gorm:"type:decimal(12,6)" json:"cumulative_return,omitempty"`
	MaxDrawdown      *float64 `gorm:"type:decimal(12,6)" json:"max_drawdown,omitempty"`
	WinRate          *float64 `gorm:"type:decimal(6,4)" json:"win_rate,omitempty"`

	CalculatedAt time.Time `gorm:"index" json:"calculated_at"`
}

// TableName returns the explicit table name.
func (PerformanceSnapshot) TableName() string { return "performance_snapshots" }
