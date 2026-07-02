// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// HTTP surface for §14.9 review reports. Mounted under
// /api/v1/reviews by the router.
//
//	GET  /api/v1/reviews?kind=&limit=   list recent reports
//	GET  /api/v1/reviews/:id            one report
//	POST /api/v1/reviews/generate       {kind:"daily"|"weekly"} manual trigger
package review

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
	"github.com/skyzhao/af/internal/model"
)

// timeNow is indirected for testability.
var timeNow = time.Now

// Handler is the route registrar for reviews.
type Handler struct {
	svc    *Service
	logger *zap.Logger
}

// NewHandler wires the handler. svc may be nil → 503.
func NewHandler(svc *Service, l *zap.Logger) *Handler {
	if l == nil {
		l = zap.NewNop()
	}
	return &Handler{svc: svc, logger: l}
}

// RegisterRoutes mounts onto the given group (caller wires /reviews).
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.List)
	r.GET("/:id", h.Get)
	r.POST("/generate", h.Generate)
}

func (h *Handler) ok() bool { return h.svc != nil }

// List handles GET /reviews?kind=&limit=.
func (h *Handler) List(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("reviews not wired"))
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	rows, err := h.svc.List(c.Request.Context(), c.Query("kind"), limit)
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, gin.H{"items": rows})
}

// Get handles GET /reviews/:id.
func (h *Handler) Get(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("reviews not wired"))
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c, apperr.InvalidArg("id must be uint64"))
		return
	}
	r, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, r)
}

type generateBody struct {
	Kind string `json:"kind"`
}

// Generate handles POST /reviews/generate — manual daily/weekly run.
func (h *Handler) Generate(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("reviews not wired"))
		return
	}
	var b generateBody
	_ = c.ShouldBindJSON(&b)
	now := timeNow()
	var (
		rep *model.ReviewReport
		err error
	)
	switch b.Kind {
	case model.ReviewKindWeekly:
		rep, err = h.svc.GenerateWeekly(c.Request.Context(), now)
	case model.ReviewKindDaily, "":
		rep, err = h.svc.GenerateDaily(c.Request.Context(), now)
	default:
		httpresp.Err(c, apperr.InvalidArg("kind must be daily or weekly"))
		return
	}
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.Created(c, rep)
}
