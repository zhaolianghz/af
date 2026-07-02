// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/database"
)

// Health responds to GET /healthz with a status payload.
//
// In addition to the process status, it pings the database (when one is
// configured) and reports it as ok|down|disabled. Redis is intentionally
// out-of-scope for this iteration.
type Health struct {
	Version string
	Started time.Time
	DB      *gorm.DB // optional; when nil, the DB section is omitted
}

// NewHealth builds a Health with no DB.
func NewHealth(version string) *Health {
	return &Health{Version: version, Started: time.Now()}
}

// NewHealthWithDB builds a Health that pings the given DB.
func NewHealthWithDB(version string, db *gorm.DB) *Health {
	return &Health{Version: version, Started: time.Now(), DB: db}
}

// Healthz is the GET /healthz handler.
//
// Response shape:
//
//	{
//	  "status":  "ok",
//	  "version": "0.1.0",
//	  "ts":      "2025-01-15T10:30:00Z",
//	  "uptime":  "1m23s",
//	  "db":      "ok"            // omitted when no DB is configured
//	}
func (h *Health) Healthz(c *gin.Context) {
	body := gin.H{
		"status":  "ok",
		"version": h.Version,
		"ts":      time.Now().UTC().Format(time.RFC3339),
		"uptime":  time.Since(h.Started).String(),
	}

	if h.DB != nil {
		if err := database.Ping(h.DB); err != nil {
			body["db"] = "down"
			body["status"] = "degraded"
		} else {
			body["db"] = "ok"
		}
	}

	c.JSON(http.StatusOK, body)
}
