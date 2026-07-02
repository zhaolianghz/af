// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

// Cross-driver column types — IMPORTANT.
//
// Large JSON / text fields (dag_json, config_json, node_snapshot,
// payload_in/out, intent_json, dag_diff_json, default_params_json) are
// intentionally left WITHOUT a `gorm:"type:..."` tag. GORM then maps an
// unannotated, unsized string to the dialect-appropriate column type:
//   - MySQL    -> longtext   (size-0 string -> longtext branch)
//   - Postgres -> text
//   - SQLite   -> text
// This is what makes the same models migrate cleanly on all three
// drivers. Do NOT add `type:longtext` back — Postgres has no `longtext`
// type and AutoMigrate fails ("type longtext does not exist"). And do
// NOT set the MySQL dialector's DefaultStringSize — that would turn
// these size-0 strings into a bounded varchar and truncate big blobs.
// (Verified against gorm.io/driver/{mysql,postgres,sqlite}@v1.6.0.)

import (
	"gorm.io/gorm"
)

// AllModels returns the slice of every concrete entity type defined in the
// model package. Pass this slice to db.AutoMigrate to ensure all tables are
// created. The order is irrelevant for AutoMigrate because GORM does its own
// dependency analysis.
func AllModels() []any {
	return []any{
		// Strategy domain
		&Strategy{},
		&StrategyVersion{},
		&NodeDefinition{},
		&StrategyTemplate{},

		// Recommendation domain
		&Recommendation{},
		&RecommendationTag{},

		// Run / log domain
		&Run{},
		&RunLog{},

		// Calendar / market
		&TradingCalendar{},

		// Performance
		&PerformanceSnapshot{},

		// Positions (user-tracked holdings)
		&Position{},

		// Review reports (§14.9 auto review)
		&ReviewReport{},

		// Runtime LLM setting (§11/§14.9 settings page)
		&LLMSetting{},
		&LLMProvider{},

		// Datasource health
		&DatasourceHealth{},
		&ProviderSwitchEvent{},

		// User / role / AI audit
		&User{},
		&Role{},
		&AIAudit{},
	}
}

// Migrate is a thin convenience wrapper that calls AutoMigrate on the result
// of AllModels. Callers should prefer this over inlining the slice.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(AllModels()...)
}
