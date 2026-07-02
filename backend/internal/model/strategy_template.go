// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

// StrategyTemplate is a built-in or user-saved template that
// produces a DAG_JSON + default config when "used". A7-BE2 fills
// in the 5 built-in rows; users can also create their own
// (BuiltIn=false).
type StrategyTemplate struct {
	BaseEntity

	// Code is the unique business code (e.g. "morning_volume_breakout").
	Code string `gorm:"size:64;uniqueIndex:uk_tpl_code_deleted,priority:1" json:"code"`

	Name        string `gorm:"size:128" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	Industry    string `gorm:"size:64;index" json:"industry"`

	// DefaultParamsJSON is a free-form JSON blob with the
	// template's default node Params, applied when the template
	// is instantiated.
	DefaultParamsJSON string `json:"default_params_json"`

	// AIExplanation is a hard-coded 100-200 character
	// description written by the system author. It is shown in
	// the template gallery and is NOT a live LLM call.
	AIExplanation string `gorm:"type:text" json:"ai_explanation"`

	// BuiltIn distinguishes system-shipped templates from
	// user-saved ones.
	BuiltIn bool `gorm:"default:false" json:"built_in"`

	// DAGJson is the pre-baked ReactFlow DAG. When the user
	// clicks "Use template", a new Strategy row is created with
	// this DAG (after a node-ID remap).
	DAGJson string `json:"dag_json"`
}

// TableName returns the explicit table name.
func (StrategyTemplate) TableName() string { return "strategy_templates" }
