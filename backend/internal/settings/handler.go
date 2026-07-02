// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// HTTP surface for the §11/§14.9 LLM settings page. Mounted under
// /api/v1/settings by the router.
//
//	GET  /api/v1/settings/llm        current setting (api_key masked)
//	PUT  /api/v1/settings/llm        save + hot-swap the active backend
//	POST /api/v1/settings/llm/test   test a connection without saving
package settings

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
)

// Handler is the route registrar for settings.
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

// RegisterRoutes mounts onto the given group (caller wires /settings).
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	// Multi-provider fallback chain (current).
	r.GET("/llm/providers", h.GetProviders)
	r.PUT("/llm/providers", h.SaveProviders)
	r.POST("/llm/providers/test", h.TestProvider)
	// Legacy single-provider routes (kept for backward compatibility).
	r.GET("/llm", h.GetLLM)
	r.PUT("/llm", h.SaveLLM)
	r.POST("/llm/test", h.TestLLM)
}

// providerBody is one chain entry in the PUT /llm/providers payload.
type providerBody struct {
	ID       uint64 `json:"id"`
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	KeepKey  bool   `json:"keep_key"`
}

func (b providerBody) toInput() ProviderInput {
	return ProviderInput{
		ID: b.ID, Enabled: b.Enabled, Provider: b.Provider, BaseURL: b.BaseURL,
		APIKey: b.APIKey, Model: b.Model, KeepKey: b.KeepKey,
	}
}

// GetProviders handles GET /settings/llm/providers — the ordered chain.
func (h *Handler) GetProviders(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("settings not wired"))
		return
	}
	v, err := h.svc.GetChain(c.Request.Context())
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, v)
}

// SaveProviders handles PUT /settings/llm/providers — replace the chain.
func (h *Handler) SaveProviders(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("settings not wired"))
		return
	}
	var body struct {
		Providers []providerBody `json:"providers"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		httpresp.Err(c, apperr.InvalidArg(err.Error()))
		return
	}
	inputs := make([]ProviderInput, 0, len(body.Providers))
	for _, b := range body.Providers {
		inputs = append(inputs, b.toInput())
	}
	v, err := h.svc.SaveChain(c.Request.Context(), inputs)
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, v)
}

// TestProvider handles POST /settings/llm/providers/test — test one entry.
func (h *Handler) TestProvider(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("settings not wired"))
		return
	}
	var b providerBody
	if err := c.ShouldBindJSON(&b); err != nil {
		httpresp.Err(c, apperr.InvalidArg(err.Error()))
		return
	}
	if err := h.svc.TestProvider(c.Request.Context(), b.toInput()); err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, gin.H{"ok": true})
}

func (h *Handler) ok() bool { return h.svc != nil }

type llmBody struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	KeepKey  bool   `json:"keep_key"`
}

func (b llmBody) toInput() SaveInput {
	return SaveInput{
		Enabled: b.Enabled, Provider: b.Provider, BaseURL: b.BaseURL,
		APIKey: b.APIKey, Model: b.Model, KeepKey: b.KeepKey,
	}
}

// GetLLM handles GET /settings/llm.
func (h *Handler) GetLLM(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("settings not wired"))
		return
	}
	v, err := h.svc.Get(c.Request.Context())
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, v)
}

// SaveLLM handles PUT /settings/llm.
func (h *Handler) SaveLLM(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("settings not wired"))
		return
	}
	var b llmBody
	if err := c.ShouldBindJSON(&b); err != nil {
		httpresp.Err(c, apperr.InvalidArg(err.Error()))
		return
	}
	v, err := h.svc.Save(c.Request.Context(), b.toInput())
	if err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, v)
}

// TestLLM handles POST /settings/llm/test.
func (h *Handler) TestLLM(c *gin.Context) {
	if !h.ok() {
		httpresp.Err(c, apperr.Unavailable("settings not wired"))
		return
	}
	var b llmBody
	if err := c.ShouldBindJSON(&b); err != nil {
		httpresp.Err(c, apperr.InvalidArg(err.Error()))
		return
	}
	if err := h.svc.Test(c.Request.Context(), b.toInput()); err != nil {
		httpresp.Err(c, err)
		return
	}
	httpresp.OK(c, gin.H{"ok": true})
}
