// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package auth

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
)

// Context keys for the authenticated principal.
const (
	ctxUserID   = "auth_user_id"
	ctxUsername = "auth_username"
	ctxRole     = "auth_role"
)

// Middleware verifies the Bearer token on protected routes and stashes
// the principal (user id / username / role) in the gin context for
// downstream handlers + future per-role enforcement.
//
// Native EventSource cannot set an Authorization header (a long-standing
// W3C gap), so SSE consumers fall back to an `access_token` query
// parameter — the same escape hatch GitHub's API and Django Channels
// use. Prefer the header whenever present; only read the query param
// when the header is absent so a stolen-URL token never overrides a
// real header credential.
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		token := ""
		if strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		} else if h == "" {
			token = c.Query("access_token")
		}
		if token == "" {
			httpresp.Err(c, apperr.Unauthorized("缺少登录凭证"))
			c.Abort()
			return
		}
		claims, err := s.Verify(token)
		if err != nil {
			httpresp.Err(c, err)
			c.Abort()
			return
		}
		c.Set(ctxUserID, claims.UserID)
		c.Set(ctxUsername, claims.Username)
		c.Set(ctxRole, claims.Role)
		c.Next()
	}
}

// UserID returns the authenticated user id from the context (0 if none).
func UserID(c *gin.Context) uint64 {
	if v, ok := c.Get(ctxUserID); ok {
		if id, ok := v.(uint64); ok {
			return id
		}
	}
	return 0
}

// Role returns the authenticated role code from the context ("" if none).
func Role(c *gin.Context) string {
	if v, ok := c.Get(ctxRole); ok {
		if r, ok := v.(string); ok {
			return r
		}
	}
	return ""
}

// RequireRole returns middleware that only lets requests carrying one of
// the given role codes through. It must be mounted AFTER Service.Middleware
// (which populates the role). Requests without a matching role get 403.
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		if !allowed[Role(c)] {
			httpresp.Err(c, apperr.Forbidden("当前角色无权访问该资源"))
			c.Abort()
			return
		}
		c.Next()
	}
}
