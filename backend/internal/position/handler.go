// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// HTTP surface for the §10 positions ledger. Mounted under
// /api/v1/positions by the router.
//
//	GET    /api/v1/positions            list open positions + live P&L + summary
//	POST   /api/v1/positions            mark/add a position (manual cost+qty)
//	PATCH  /api/v1/positions/:id        edit cost / quantity / note
//	DELETE /api/v1/positions/:id        close (soft-delete) a position
package position

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
)

// Handler is the route registrar for positions.
type Handler struct {
	svc    *Service
	logger *zap.Logger
}

// NewHandler wires the handler. svc may be nil; routes then 503.
func NewHandler(svc *Service, l *zap.Logger) *Handler {
	if l == nil {
		l = zap.NewNop()
	}
	return &Handler{svc: svc, logger: l}
}

// RegisterRoutes mounts the routes onto the given group (caller wires
// the /positions prefix).
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.List)
	r.POST("", h.Create)
	r.PATCH("/:id", h.Update)
	r.DELETE("/:id", h.Close)
}

func (h *Handler) ok() bool { return h.svc != nil }

// List handles GET /positions — open positions + live P&L + a summary.
func (h *Handler) List(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("positions not wired"))
		return
	}
	views, err := h.svc.List(c.Request.Context())
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, gin.H{"items": views, "summary": Summarize(views)})
}

type createBody struct {
	StockCode              string  `json:"stock_code"`
	StockName              string  `json:"stock_name"`
	CostPrice              float64 `json:"cost_price"`
	Quantity               int64   `json:"quantity"`
	OpenedAt               string  `json:"opened_at"` // YYYY-MM-DD, optional
	SourceRecommendationID uint64  `json:"source_recommendation_id"`
	Note                   string  `json:"note"`
}

// Create handles POST /positions — mark a recommendation as bought or
// add a holding manually.
func (h *Handler) Create(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("positions not wired"))
		return
	}
	var b createBody
	if err := c.ShouldBindJSON(&b); err != nil {
		httpresp.Err(c, apperr.InvalidArg(err.Error()))
		return
	}
	in := CreateInput{
		StockCode:              b.StockCode,
		StockName:              b.StockName,
		CostPrice:              b.CostPrice,
		Quantity:               b.Quantity,
		SourceRecommendationID: b.SourceRecommendationID,
		Note:                   b.Note,
	}
	if b.OpenedAt != "" {
		if t, err := time.Parse("2006-01-02", b.OpenedAt); err == nil {
			in.OpenedAt = &t
		} else {
			httpresp.Err(c, apperr.InvalidArg("opened_at must be YYYY-MM-DD"))
			return
		}
	}
	p, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.Created(c, p)
}

type updateBody struct {
	CostPrice *float64 `json:"cost_price"`
	Quantity  *int64   `json:"quantity"`
	Note      *string  `json:"note"`
}

// Update handles PATCH /positions/:id.
func (h *Handler) Update(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("positions not wired"))
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c, apperr.InvalidArg("id must be uint64"))
		return
	}
	var b updateBody
	if err := c.ShouldBindJSON(&b); err != nil {
		httpresp.Err(c, apperr.InvalidArg(err.Error()))
		return
	}
	p, err := h.svc.Update(c.Request.Context(), id, UpdateInput{
		CostPrice: b.CostPrice, Quantity: b.Quantity, Note: b.Note,
	})
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, p)
}

// Close handles DELETE /positions/:id.
func (h *Handler) Close(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("positions not wired"))
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c, apperr.InvalidArg("id must be uint64"))
		return
	}
	if err := h.svc.Close(c.Request.Context(), id); err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, gin.H{"closed": id})
}
