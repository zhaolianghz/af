// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import "time"

// User is the principal for the system. Authentication is implemented in a
// later milestone; this iteration only defines the storage model.
type User struct {
	BaseEntity

	Username     string `gorm:"size:64;uniqueIndex" json:"username"`
	PasswordHash string `gorm:"size:255" json:"-"`
	Email        string `gorm:"size:128" json:"email"`
	RoleID       uint64 `gorm:"index" json:"role_id"`

	// Status is one of: active / disabled.
	Status      string     `gorm:"size:16;default:active;index" json:"status"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

// TableName returns the explicit table name.
func (User) TableName() string { return "users" }

// User status constants.
const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

// Role groups users and bundles a set of permissions. Permissions are stored
// as a JSON array string (e.g. `["strategy:read","strategy:write"]`).
type Role struct {
	BaseEntity

	Code        string `gorm:"size:32;uniqueIndex" json:"code"` // admin / editor / viewer
	Name        string `gorm:"size:64" json:"name"`
	Permissions string `gorm:"type:text" json:"permissions"`
}

// TableName returns the explicit table name.
func (Role) TableName() string { return "roles" }

// Role code constants.
const (
	RoleCodeAdmin  = "admin"
	RoleCodeEditor = "editor"
	RoleCodeViewer = "viewer"
)

// AIAudit records each invocation of the AI agent on a strategy. It captures
// the intent the user expressed, the system's decision, and the diff that was
// applied to the strategy DAG (if any).
type AIAudit struct {
	BaseEntity

	UserID      uint64 `gorm:"index" json:"user_id"`
	StrategyID  uint64 `gorm:"index" json:"strategy_id"`
	IntentJson  string `json:"intent_json"`

	// Decision is one of: applied / rejected / preview.
	Decision    string `gorm:"size:16" json:"decision"`
	DagDiffJson string `json:"dag_diff_json"`
}

// TableName returns the explicit table name.
func (AIAudit) TableName() string { return "ai_audits" }

// AIAudit decision constants.
const (
	AIDecisionApplied  = "applied"
	AIDecisionRejected = "rejected"
	AIDecisionPreview  = "preview"
)
