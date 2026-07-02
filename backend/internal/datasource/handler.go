// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — handler.go
//
// HTTP handlers exposed under /api/v1/datasource. The only one
// required by tasks.md §3.8 + §3.11 is the health endpoint, which
// reports the per-source breaker state and the latest DatasourceHealth
// rows. Additional routes (quote / kline / etc.) can be added in a
// later iteration.
package datasource

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// HealthHandler is the GET /api/v1/datasource/health handler. It
// returns a JSON document describing every configured source's
// breaker state plus the most recent DatasourceHealth row.
type HealthHandler struct {
	manager Manager
	repo    HealthRepo
	logger  *zap.Logger
}

// NewHealthHandler returns a HealthHandler bound to the given manager
// + repo. The repo may be nil, in which case the handler omits the
// "db" section.
func NewHealthHandler(mgr Manager, repo HealthRepo, logger *zap.Logger) *HealthHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &HealthHandler{manager: mgr, repo: repo, logger: logger}
}

// HealthResponse is the JSON document returned by HealthHandler.
type HealthResponse struct {
	Sources []string          `json:"sources"`
	Breakers []BreakerSnapshot `json:"breakers"`
	DB       []map[string]interface{} `json:"db,omitempty"`
}

// Handle implements gin.HandlerFunc.
func (h *HealthHandler) Handle(c *gin.Context) {
	resp := HealthResponse{
		Sources:  h.manager.ListSources(),
		Breakers: h.manager.BreakerSnapshots(),
	}
	if h.repo != nil {
		rows, err := h.repo.ListAll(c.Request.Context())
		if err != nil {
			h.logger.Warn("list datasource health failed", zap.Error(err))
		} else {
			// Reduce the model row to a small map so the public
			// API does not leak GORM-specific fields.
			out := make([]map[string]interface{}, 0, len(rows))
			for _, r := range rows {
				out = append(out, map[string]interface{}{
					"source":        r.Source,
					"status":        r.Status,
					"fail_count_5m": r.FailCount5m,
					"last_ok":       r.LastOK,
					"last_fail":     r.LastFail,
					"last_error":    r.LastError,
				})
			}
			resp.DB = out
		}
	}
	c.JSON(http.StatusOK, resp)
}
