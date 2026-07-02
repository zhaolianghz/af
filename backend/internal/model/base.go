// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package model defines the GORM entity types for the A-Stock Selector System.
//
// All entities embed BaseEntity which provides the standard primary key and
// timestamp columns. Entities that need soft-delete embed SoftDelete directly.
package model

import (
	"time"

	"gorm.io/gorm"
)

// BaseEntity provides the common columns (id / created_at / updated_at) for
// every table. Models should embed this anonymously.
//
// ID is uint64 so that high-volume tables (runs, recommendations, run logs)
// have plenty of headroom; small tables (users, roles, ai_audits) pay no
// real cost for the wider type.
type BaseEntity struct {
	ID        uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName returns the default table name derived from the type name.
// Concrete models may override this to give an explicit table name.
func (BaseEntity) TableName() string { return "" }

// Now returns the current UTC time, used to keep timestamp generation
// consistent in tests and seed code.
func Now() time.Time { return time.Now().UTC() }
