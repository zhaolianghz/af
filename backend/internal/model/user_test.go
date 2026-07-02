// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserCRUD(t *testing.T) {
	db := newTestDB(t)
	role := &Role{Code: RoleCodeViewer, Name: "Viewer", Permissions: `["strategy:read"]`}
	require.NoError(t, db.Create(role).Error)

	now := newTestTime()
	user := &User{
		Username:     "alice",
		PasswordHash: "bcrypt-hash",
		Email:        "alice@example.com",
		RoleID:       role.ID,
		Status:       UserStatusActive,
		LastLoginAt:  &now,
	}
	require.NoError(t, db.Create(user).Error)

	var got User
	require.NoError(t, db.First(&got, user.ID).Error)
	assert.Equal(t, "alice", got.Username)
	assert.Equal(t, role.ID, got.RoleID)
}

func TestUserPasswordHashHiddenInJSON(t *testing.T) {
	u := User{
		BaseEntity:   BaseEntity{ID: 1},
		Username:     "alice",
		PasswordHash: "secret",
		Email:        "alice@example.com",
		RoleID:       1,
		Status:       UserStatusActive,
	}
	b, err := json.Marshal(u)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	_, has := m["password_hash"]
	assert.False(t, has, "password_hash should not appear in JSON output")
}

func TestUserUniqueUsername(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Create(&User{Username: "bob", Status: UserStatusActive}).Error)
	assert.Error(t, db.Create(&User{Username: "bob", Status: UserStatusActive}).Error)
}

func TestRoleCRUD(t *testing.T) {
	db := newTestDB(t)
	role := &Role{Code: RoleCodeAdmin, Name: "Admin", Permissions: `["*"]`}
	require.NoError(t, db.Create(role).Error)

	var got Role
	require.NoError(t, db.First(&got, role.ID).Error)
	assert.Equal(t, RoleCodeAdmin, got.Code)
}

func TestRoleUniqueCode(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Create(&Role{Code: RoleCodeEditor, Name: "e"}).Error)
	assert.Error(t, db.Create(&Role{Code: RoleCodeEditor, Name: "e2"}).Error)
}

func TestAIAuditCRUD(t *testing.T) {
	db := newTestDB(t)
	s := &Strategy{Code: "A", Name: "a", Status: StrategyStatusDraft, DAGJson: "{}"}
	require.NoError(t, db.Create(s).Error)
	u := &User{Username: "u", Status: UserStatusActive}
	require.NoError(t, db.Create(u).Error)

	audit := &AIAudit{
		UserID:     u.ID,
		StrategyID: s.ID,
		IntentJson: `{"intent":"add momentum filter"}`,
		Decision:   AIDecisionApplied,
		DagDiffJson: `{"added":[{"key":"mom"}],"removed":[]}`,
	}
	require.NoError(t, db.Create(audit).Error)

	var got AIAudit
	require.NoError(t, db.First(&got, audit.ID).Error)
	assert.Equal(t, AIDecisionApplied, got.Decision)
}

var _ = time.Hour
