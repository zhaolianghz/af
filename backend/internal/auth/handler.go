// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// HTTP surface for §12 auth. /auth/login is public; /auth/me and
// /auth/change-password are behind the auth middleware.
package auth

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
)

// Handler is the route registrar for auth.
type Handler struct {
	svc    *Service
	logger *zap.Logger
}

// NewHandler wires the handler.
func NewHandler(svc *Service, l *zap.Logger) *Handler {
	if l == nil {
		l = zap.NewNop()
	}
	return &Handler{svc: svc, logger: l}
}

// RegisterPublic mounts the unauthenticated routes (login).
func (h *Handler) RegisterPublic(r *gin.RouterGroup) {
	r.POST("/auth/login", h.Login)
}

// RegisterProtected mounts the authenticated routes (me, change pw).
func (h *Handler) RegisterProtected(r *gin.RouterGroup) {
	r.GET("/auth/me", h.Me)
	r.POST("/auth/change-password", h.ChangePassword)
}

// PublicRegistrar / ProtectedRegistrar adapt the handler to the
// router's RouteRegistrar interface (RegisterRoutes) for the public vs
// protected mount points.
type publicRegistrar struct{ h *Handler }
type protectedRegistrar struct{ h *Handler }

func (p publicRegistrar) RegisterRoutes(r *gin.RouterGroup)    { p.h.RegisterPublic(r) }
func (p protectedRegistrar) RegisterRoutes(r *gin.RouterGroup) { p.h.RegisterProtected(r) }

// PublicRoutes returns a RouteRegistrar for the login endpoint.
func (h *Handler) PublicRoutes() interface{ RegisterRoutes(*gin.RouterGroup) } {
	return publicRegistrar{h}
}

// ProtectedRoutes returns a RouteRegistrar for me/change-password.
func (h *Handler) ProtectedRoutes() interface{ RegisterRoutes(*gin.RouterGroup) } {
	return protectedRegistrar{h}
}

type loginBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login handles POST /auth/login.
func (h *Handler) Login(c *gin.Context) {
	var b loginBody
	if err := c.ShouldBindJSON(&b); err != nil {
		httpresp.Err(c, apperr.InvalidArg(err.Error()))
		return
	}
	token, u, err := h.svc.Login(c.Request.Context(), b.Username, b.Password)
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, gin.H{
		"token": token,
		"user":  gin.H{"id": u.ID, "username": u.Username, "role_id": u.RoleID},
	})
}

// Me handles GET /auth/me — returns the principal from the token.
func (h *Handler) Me(c *gin.Context) {
	httpresp.OK(c, gin.H{
		"user_id":  c.GetUint64(ctxUserID),
		"username": c.GetString(ctxUsername),
		"role":     c.GetString(ctxRole),
	})
}

type changePwBody struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangePassword handles POST /auth/change-password.
func (h *Handler) ChangePassword(c *gin.Context) {
	var b changePwBody
	if err := c.ShouldBindJSON(&b); err != nil {
		httpresp.Err(c, apperr.InvalidArg(err.Error()))
		return
	}
	if err := h.svc.ChangePassword(c.Request.Context(), UserID(c), b.OldPassword, b.NewPassword); err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, gin.H{"ok": true})
}
