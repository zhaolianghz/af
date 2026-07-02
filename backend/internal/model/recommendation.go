// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import "time"

// Recommendation is the persisted result of a strategy run: a stock pick with
// a price range and a snapshot of the node pipeline that produced it.
//
// See design.md Decision 7 for the full schema rationale.
type Recommendation struct {
	BaseEntity

	RunID uint64 `gorm:"index" json:"run_id"`

	// Date is the trading day the recommendation applies to.
	Date time.Time `gorm:"type:date;index" json:"date"`

	StockCode string `gorm:"size:16;index" json:"stock_code"`
	StockName string `gorm:"size:64" json:"stock_name"`

	EntryPriceLow  float64 `gorm:"type:decimal(12,4)" json:"entry_price_low"`
	EntryPriceHigh float64 `gorm:"type:decimal(12,4)" json:"entry_price_high"`

	StrategyCode string `gorm:"size:64;index" json:"strategy_code"`
	StrategyName string `gorm:"size:128" json:"strategy_name"`

	// NodeSnapshot is a JSON blob capturing the executed node pipeline
	// (input -> output) at the time the recommendation was produced.
	NodeSnapshot string `json:"node_snapshot"`

	// Tags is the eager-loaded slice of session tags (MORNING / AFTERNOON /
	// NO_POST / REVIEW, plus source attribution). It is NOT a column — GORM
	// populates it via the foreignKey relation.
	Tags []RecommendationTag `gorm:"foreignKey:RecommendationID;constraint:OnDelete:CASCADE" json:"tags,omitempty"`
}

// TableName returns the explicit table name.
func (Recommendation) TableName() string { return "recommendations" }

// RecommendationTag attaches a session tag to a recommendation. A given
// recommendation may carry multiple tags from different sources (auto node,
// manual override, AI agent).
type RecommendationTag struct {
	BaseEntity

	RecommendationID uint64 `gorm:"uniqueIndex:uk_rec_tag_source,priority:1" json:"recommendation_id"`
	// Tag holds session tags (MORNING/…) AND user/template tags such
	// as the strategy code. Built-in template tags already exceed 16
	// ("low_valuation_high_dividend" is 27), so size:16 made persist
	// fail with "Data too long for column 'tag'". 64 matches
	// strategy_code's width.
	Tag    string `gorm:"size:64;uniqueIndex:uk_rec_tag_source,priority:2" json:"tag"`
	Source string `gorm:"size:16;uniqueIndex:uk_rec_tag_source,priority:3" json:"source"`
	TaggedAt         time.Time `json:"tagged_at"`
}

// TableName returns the explicit table name.
func (RecommendationTag) TableName() string { return "recommendation_tags" }

// Session tag values (Design Decision 8).
const (
	SessionTagMorning   = "MORNING"
	SessionTagAfternoon = "AFTERNOON"
	SessionTagNoPost    = "NO_POST"
	SessionTagReview    = "REVIEW"
)

// Tag source values.
const (
	TagSourceAutoNode = "AUTO_NODE"
	TagSourceManual   = "MANUAL"
	TagSourceAIAgent  = "AI_AGENT"
)
