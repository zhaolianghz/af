// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package executor — template HTTP handler (template_handler.go).
//
// Endpoints:
//
//	GET  /api/v1/strategies/templates            list all templates
//	POST /api/v1/strategies/from-template/:code  instantiate a strategy from a template
package executor

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
	"github.com/skyzhao/af/internal/executor/templates"
)

// TemplateHandler exposes the template gallery + the
// from-template instantiation endpoint.
type TemplateHandler struct {
	loader *templates.Loader
	logger *zap.Logger
	db     *gorm.DB
}

// NewTemplateHandler wires the handler.
func NewTemplateHandler(loader *templates.Loader, db *gorm.DB, l *zap.Logger) *TemplateHandler {
	if l == nil {
		l = zap.NewNop()
	}
	return &TemplateHandler{loader: loader, db: db, logger: l}
}

// RegisterRoutes mounts the template routes onto the strategies group.
// The caller's prefix is /api/v1, so the actual paths are:
//
//	GET  /strategies/templates
//	POST /strategies/from-template/:code
func (h *TemplateHandler) RegisterRoutes(strat *gin.RouterGroup) {
	strat.GET("/templates", h.List)
	strat.POST("/from-template/:code", h.Instantiate)
}

// List handles GET /strategies/templates.
func (h *TemplateHandler) List(c *gin.Context) {
	if h.loader == nil {
		httpresp.Err(c,apperr.Unavailable("templates not wired"))
		return
	}
	rows, err := h.loader.List(c.Request.Context())
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,gin.H{"items": rows, "total": len(rows)})
}

// Instantiate handles POST /strategies/from-template/:code. The
// new strategy is created in draft status; the user edits + saves
// before activating it (and triggering the scheduler).
func (h *TemplateHandler) Instantiate(c *gin.Context) {
	if h.loader == nil || h.db == nil {
		httpresp.Err(c,apperr.Unavailable("templates not wired"))
		return
	}
	code := c.Param("code")
	if code == "" {
		httpresp.Err(c,apperr.InvalidArg("code is required"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	strat, ver, err := h.loader.Instantiate(ctx, h.db, code)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	h.logger.Info("executor: template instantiated",
		zap.String("template", code),
		zap.Uint64("strategy_id", strat.ID),
		zap.String("strategy_code", strat.Code),
		zap.String("via", c.ClientIP()),
	)
	httpresp.Created(c, gin.H{"strategy": strat, "version": ver})
}