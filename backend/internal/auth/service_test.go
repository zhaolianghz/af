// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/model"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	return db
}

func TestBootstrap_GeneratesPasswordOnEmptyDB(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, "test-secret", time.Hour)

	pw, err := svc.Bootstrap(context.Background(), "admin", "")
	require.NoError(t, err)
	require.NotEmpty(t, pw, "empty AdminPassword should yield a generated one")

	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestBootstrap_ExplicitPasswordNotReturned(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, "test-secret", time.Hour)

	pw, err := svc.Bootstrap(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)
	require.Empty(t, pw, "explicit password should not be echoed back")
}

func TestBootstrap_Idempotent(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, "test-secret", time.Hour)

	_, err := svc.Bootstrap(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)

	// Second call must not create a second user nor generate a password.
	pw, err := svc.Bootstrap(context.Background(), "admin", "")
	require.NoError(t, err)
	require.Empty(t, pw)

	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestLogin_SuccessIssuesVerifiableToken(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, "test-secret", time.Hour)
	_, err := svc.Bootstrap(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)

	token, u, err := svc.Login(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Equal(t, "admin", u.Username)

	claims, err := svc.Verify(token)
	require.NoError(t, err)
	require.Equal(t, u.ID, claims.UserID)
	require.Equal(t, "admin", claims.Username)
	require.Equal(t, model.RoleCodeAdmin, claims.Role)
}

func TestLogin_WrongPassword(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, "test-secret", time.Hour)
	_, err := svc.Bootstrap(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)

	_, _, err = svc.Login(context.Background(), "admin", "wrong")
	require.Error(t, err)
}

func TestLogin_UnknownUser(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, "test-secret", time.Hour)

	_, _, err := svc.Login(context.Background(), "ghost", "whatever")
	require.Error(t, err)
}

func TestVerify_RejectsForeignSecret(t *testing.T) {
	db := newDB(t)
	_, err := NewService(db, "secret-A", time.Hour).Bootstrap(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)

	token, _, err := NewService(db, "secret-A", time.Hour).Login(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)

	// A service with a different secret must reject the token.
	_, err = NewService(db, "secret-B", time.Hour).Verify(token)
	require.Error(t, err)
}

func TestVerify_RejectsExpiredToken(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, "test-secret", -time.Minute) // already expired
	_, err := svc.Bootstrap(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)

	// ttl <= 0 is clamped to 24h by NewService, so force a real expiry by
	// signing with a tiny positive ttl and waiting is too slow. Instead use
	// a service whose ttl is small but positive.
	short := NewService(db, "test-secret", time.Nanosecond)
	token, _, err := short.Login(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond)
	_, err = short.Verify(token)
	require.Error(t, err)
}

func TestChangePassword(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, "test-secret", time.Hour)
	_, err := svc.Bootstrap(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)
	_, u, err := svc.Login(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)

	// Wrong old password rejected.
	require.Error(t, svc.ChangePassword(context.Background(), u.ID, "wrong", "newpassword123"))
	// Too-short new password rejected.
	require.Error(t, svc.ChangePassword(context.Background(), u.ID, "hunter2hunter2", "short"))
	// Happy path.
	require.NoError(t, svc.ChangePassword(context.Background(), u.ID, "hunter2hunter2", "newpassword123"))

	// Old password no longer works; new one does.
	_, _, err = svc.Login(context.Background(), "admin", "hunter2hunter2")
	require.Error(t, err)
	_, _, err = svc.Login(context.Background(), "admin", "newpassword123")
	require.NoError(t, err)
}
