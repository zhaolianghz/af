// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Ping responds to GET /api/v1/ping with the text "pong".
// Useful as a lightweight readiness/liveness probe.
func Ping(c *gin.Context) {
	c.String(http.StatusOK, "pong")
}
