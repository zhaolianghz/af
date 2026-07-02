// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import "time"

// Position is a user-tracked holding. A position is created by
// "marking" a recommendation as bought (or added manually); the
// cost price and quantity are maintained by hand — the system does
// NOT execute trades. Current price / P&L are computed on read from
// the live datasource, never stored.
//
// v1 scope: a simple long-only holding ledger. No partial sells, no
// average-down history — editing cost/quantity overwrites. Closing a
// position soft-deletes it (DeletedAt).
type Position struct {
	BaseEntity

	StockCode string `gorm:"size:16;index" json:"stock_code"`
	StockName string `gorm:"size:64" json:"stock_name"`

	// CostPrice is the per-share cost the user paid (hand-entered).
	CostPrice float64 `gorm:"type:decimal(12,4)" json:"cost_price"`
	// Quantity is the number of shares held (hand-entered).
	Quantity int64 `json:"quantity"`

	// OpenedAt is the trade date the user opened the position.
	OpenedAt time.Time `gorm:"type:date;index" json:"opened_at"`

	// SourceRecommendationID links back to the recommendation this was
	// marked from (0 when added manually). Not a FK constraint — the
	// recommendation may be pruned independently.
	SourceRecommendationID uint64 `gorm:"index" json:"source_recommendation_id"`

	// Note is a freeform user memo (optional).
	Note string `gorm:"size:255" json:"note"`
}

// TableName returns the explicit table name.
func (Position) TableName() string { return "positions" }
