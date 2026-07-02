// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// HTTP surface for §11 AI strategy editing. Mounted under
// /api/v1/strategies/:id/ai by the router.
//
//	POST /api/v1/strategies/:id/ai/preview  {instruction}            -> proposed DAG + diff
//	POST /api/v1/strategies/:id/ai/apply    {instruction, dag_json}  -> commit (new version)
package ai

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
)

// Handler is the route registrar for the AI assistant.
type Handler struct {
	svc    *Service
	logger *zap.Logger
}

// NewHandler wires the handler. svc may be nil (assistant disabled);
// routes then return 503.
func NewHandler(svc *Service, l *zap.Logger) *Handler {
	if l == nil {
		l = zap.NewNop()
	}
	return &Handler{svc: svc, logger: l}
}

// RegisterRoutes mounts onto the strategies/:id group. The caller wires
// the /strategies/:id/ai prefix.
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("/preview", h.Preview)
	r.POST("/apply", h.Apply)
}

func (h *Handler) strategyID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c, apperr.InvalidArg("id must be uint64"))
		return 0, false
	}
	return id, true
}

type previewBody struct {
	Instruction string `json:"instruction"`
}

// Preview handles POST /strategies/:id/ai/preview.
func (h *Handler) Preview(c *gin.Context) {
	if h.svc == nil {
		httpresp.Err(c, apperr.Unavailable("ai assistant disabled"))
		return
	}
	id, ok := h.strategyID(c)
	if !ok {
		return
	}
	var b previewBody
	if err := c.ShouldBindJSON(&b); err != nil {
		httpresp.Err(c, apperr.InvalidArg(err.Error()))
		return
	}
	res, err := h.svc.Preview(c.Request.Context(), id, b.Instruction)
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, res)
}

type applyBody struct {
	Instruction string `json:"instruction"`
	DAGJson     string `json:"dag_json"`
}

// Apply handles POST /strategies/:id/ai/apply.
func (h *Handler) Apply(c *gin.Context) {
	if h.svc == nil {
		httpresp.Err(c, apperr.Unavailable("ai assistant disabled"))
		return
	}
	id, ok := h.strategyID(c)
	if !ok {
		return
	}
	var b applyBody
	if err := c.ShouldBindJSON(&b); err != nil {
		httpresp.Err(c, apperr.InvalidArg(err.Error()))
		return
	}
	res, err := h.svc.Apply(c.Request.Context(), id, b.DAGJson, b.Instruction)
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, res)
}
