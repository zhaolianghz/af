// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package auth

import (
	"net/http"
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
// W3C gap), so the ONE endpoint that must be consumed via EventSource —
// the SSE run-events stream — accepts an `access_token` query parameter
// as a fallback. The fallback is deliberately narrow:
//
//   - GET + the /runs/:id/events path only (sseRoute prefix check) —
//     tokens riding in URLs leak to access logs, history and Referer,
//     so no other route or method may authenticate this way;
//   - header credentials always win — a stolen-URL token can never
//     override a real header;
//   - a non-Bearer Authorization header (e.g. a proxy-injected Basic
//     credential) does not block the SSE fallback, it just isn't used.
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		token := ""
		if strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		} else if sseRequest(c) {
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

// sseRequest reports whether this request targets the run-events SSE
// stream — the one route structurally required to authenticate without
// an Authorization header. The events route is registered as
// POST-free GET-only under /api/v1/runs/:id/events; the suffix+prefix
// match works regardless of the configured api base path, and the
// method is re-checked here for defense in depth.
func sseRequest(c *gin.Context) bool {
	if c.Request == nil || c.Request.Method != http.MethodGet {
		return false
	}
	p := c.Request.URL.Path
	return strings.HasSuffix(p, "/events") && strings.Contains(p, "/runs/")
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
