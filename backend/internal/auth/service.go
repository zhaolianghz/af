// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package auth implements §12 authentication: password login (bcrypt)
// issuing a JWT, JWT verification middleware, and first-admin
// bootstrap. v1 is a single-admin login gate, but the data model
// (User/Role) and token claims (user_id/role) are multi-user ready.
package auth

import (
	"context"
	crand "crypto/rand"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/model"
)

// Service handles login, token issue/verify, and admin bootstrap.
type Service struct {
	db     *gorm.DB
	secret []byte
	ttl    time.Duration
}

// NewService wires the service. secret signs JWTs; ttl bounds token
// lifetime (default 24h).
func NewService(db *gorm.DB, secret string, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Service{db: db, secret: []byte(secret), ttl: ttl}
}

// Claims is the JWT payload — carries user id + role for future
// per-role enforcement.
type Claims struct {
	UserID   uint64 `json:"uid"`
	Username string `json:"usr"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Login verifies username+password and returns a signed token + the
// user. Generic error on any failure (no user-enumeration leak).
func (s *Service) Login(ctx context.Context, username, password string) (string, *model.User, error) {
	if s.db == nil {
		return "", nil, apperr.Unavailable("auth: db not configured")
	}
	var u model.User
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&u).Error
	if err != nil {
		// Still run a bcrypt compare against a dummy hash to keep timing
		// roughly constant (mild user-enumeration defense).
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinv"), []byte(password))
		return "", nil, apperr.Unauthorized("用户名或密码错误")
	}
	if u.Status != "" && u.Status != "active" {
		return "", nil, apperr.Unauthorized("账号已禁用")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", nil, apperr.Unauthorized("用户名或密码错误")
	}

	role := s.roleCode(ctx, u.RoleID)
	token, err := s.issue(&u, role)
	if err != nil {
		return "", nil, apperr.Wrap(apperr.CodeInternal, "auth: sign token", err)
	}
	now := time.Now().UTC()
	s.db.WithContext(ctx).Model(&u).Update("last_login_at", &now)
	return token, &u, nil
}

// issue signs a JWT for the user.
func (s *Service) issue(u *model.User, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   u.ID,
		Username: u.Username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
			Subject:   u.Username,
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// Verify parses + validates a token, returning its claims.
func (s *Service) Verify(token string) (*Claims, error) {
	var c Claims
	t, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil || !t.Valid {
		return nil, apperr.Unauthorized("登录已过期或无效,请重新登录")
	}
	return &c, nil
}

func (s *Service) roleCode(ctx context.Context, roleID uint64) string {
	if roleID == 0 {
		return model.RoleCodeAdmin
	}
	var r model.Role
	if s.db.WithContext(ctx).First(&r, roleID).Error == nil && r.Code != "" {
		return r.Code
	}
	return model.RoleCodeAdmin
}

// Bootstrap ensures an admin user exists. On first boot (no users) it
// creates one with the given username/password; if password is empty,
// a random one is generated and returned (caller logs it once). Returns
// (createdPassword, error): createdPassword is "" when a user already
// existed or an explicit password was used.
func (s *Service) Bootstrap(ctx context.Context, username, password string) (string, error) {
	if s.db == nil {
		return "", apperr.Unavailable("auth: db not configured")
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.User{}).Count(&count).Error; err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "auth: count users", err)
	}
	if count > 0 {
		return "", nil // already bootstrapped
	}
	if username == "" {
		username = "admin"
	}
	generated := ""
	if password == "" {
		password = randomPassword(16)
		generated = password
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "auth: hash password", err)
	}
	u := &model.User{Username: username, PasswordHash: string(hash), Status: "active"}
	if err := s.db.WithContext(ctx).Create(u).Error; err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "auth: create admin", err)
	}
	return generated, nil
}

// ChangePassword updates a user's password after verifying the old one.
func (s *Service) ChangePassword(ctx context.Context, userID uint64, oldPw, newPw string) error {
	if s.db == nil {
		return apperr.Unavailable("auth: db not configured")
	}
	if len(newPw) < 8 {
		return apperr.InvalidArg("新密码至少 8 位")
	}
	var u model.User
	if err := s.db.WithContext(ctx).First(&u, userID).Error; err != nil {
		return apperr.NotFound("user not found")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPw)) != nil {
		return apperr.Unauthorized("原密码错误")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "auth: hash password", err)
	}
	return s.db.WithContext(ctx).Model(&u).Update("password_hash", string(hash)).Error
}

// randomPassword returns a URL-safe random string of n chars.
func randomPassword(n int) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, n)
	if _, err := crand.Read(b); err != nil {
		// Fallback: time-seeded (bootstrap-only, still non-trivial).
		for i := range b {
			b[i] = alphabet[(int(time.Now().UnixNano())+i)%len(alphabet)]
		}
		return string(b)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
