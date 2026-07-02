// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package orchestrator

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
)

// StrategyHandler exposes the /api/v1/strategies CRUD surface.
type StrategyHandler struct {
	svc    *StrategyService
	logger *zap.Logger
}

// NewStrategyHandler wires a handler against a service.
func NewStrategyHandler(svc *StrategyService, l *zap.Logger) *StrategyHandler {
	if l == nil {
		l = zap.NewNop()
	}
	return &StrategyHandler{svc: svc, logger: l}
}

// SetScheduleReloader wires the cron scheduler so a strategy Update
// (status / cron change) syncs the strategy's cron entry. Passes
// through to the service.
func (h *StrategyHandler) SetScheduleReloader(r ScheduleReloader) {
	if h == nil || h.svc == nil {
		return
	}
	h.svc.SetScheduleReloader(r)
}

// RegisterRoutes mounts the strategy CRUD routes onto the given
// router group. The group prefix is the responsibility of the
// caller (router.Options wires /api/v1/strategies).
func (h *StrategyHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("", h.Create)
	r.GET("", h.List)
	r.GET("/:id", h.Detail)
	r.PUT("/:id", h.Update)
	r.DELETE("/:id", h.Delete)
	r.POST("/:id/clone", h.Clone)
	r.GET("/:id/export", h.Export)
	r.POST("/import", h.Import)
}

// Create handles POST /strategies.
func (h *StrategyHandler) Create(c *gin.Context) {
	var in CreateStrategyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpresp.Err(c,apperrInvalid(err))
		return
	}
	strat, ver, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.Created(c,gin.H{"strategy": strat, "version": ver})
}

// List handles GET /strategies?status=...&tags_contains=...&code_like=...&page=1&page_size=20.
func (h *StrategyHandler) List(c *gin.Context) {
	f := StrategyListFilter{
		Status:       c.Query("status"),
		TagsContains: c.Query("tags_contains"),
		CodeLike:     c.Query("code_like"),
		Page:         atoiDefault(c.Query("page"), 1),
		PageSize:     atoiDefault(c.Query("page_size"), 20),
	}
	rows, total, err := h.svc.List(c.Request.Context(), f)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,gin.H{"items": rows, "total": total, "page": f.Page, "page_size": f.PageSize})
}

// Detail handles GET /strategies/:id.
func (h *StrategyHandler) Detail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperrInvalidStr("id must be uint64"))
		return
	}
	d, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,d)
}

// Update handles PUT /strategies/:id.
func (h *StrategyHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperrInvalidStr("id must be uint64"))
		return
	}
	var in UpdateStrategyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpresp.Err(c,apperrInvalid(err))
		return
	}
	strat, ver, err := h.svc.Update(c.Request.Context(), id, in)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,gin.H{"strategy": strat, "version": ver})
}

// Delete handles DELETE /strategies/:id.
func (h *StrategyHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperrInvalidStr("id must be uint64"))
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,gin.H{"id": id, "status": "disabled"})
}

// Clone handles POST /strategies/:id/clone.
func (h *StrategyHandler) Clone(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperrInvalidStr("id must be uint64"))
		return
	}
	var body struct {
		NewCode string `json:"new_code"`
	}
	_ = c.ShouldBindJSON(&body) // body is optional
	strat, ver, err := h.svc.Clone(c.Request.Context(), id, body.NewCode)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.Created(c,gin.H{"strategy": strat, "version": ver})
}

// Export handles GET /strategies/:id/export — returns the JSON
// as a downloadable attachment.
func (h *StrategyHandler) Export(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperrInvalidStr("id must be uint64"))
		return
	}
	s, err := h.svc.Export(c.Request.Context(), id)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="strategy-`+strconv.FormatUint(id, 10)+`.json"`)
	c.Data(http.StatusOK, "application/json", []byte(s))
}

// Import handles POST /strategies/import — accepts either a
// raw JSON body or a {json_str: "..."} wrapper.
func (h *StrategyHandler) Import(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil || len(raw) == 0 {
		httpresp.Err(c,apperrInvalidStr("empty body"))
		return
	}
	// Try the wrapper form first.
	var wrapper struct {
		JSONStr string `json:"json_str"`
	}
	if err := tryUnmarshal(raw, &wrapper); err == nil && wrapper.JSONStr != "" {
		raw = []byte(wrapper.JSONStr)
	}
	strat, ver, err := h.svc.Import(c.Request.Context(), string(raw))
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.Created(c,gin.H{"strategy": strat, "version": ver})
}

// =============================================================================
// Local helpers
// =============================================================================

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func tryUnmarshal(b []byte, v any) error {
	return json.Unmarshal(b, v)
}

// apperrInvalid wraps a generic JSON-parse error as 400.
func apperrInvalid(err error) error { return apperr.InvalidArg(err.Error()) }
func apperrInvalidStr(s string) error {
	return apperr.InvalidArg(s)
}
