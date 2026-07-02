// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package openapi serves the OpenAPI 3 spec and a Swagger UI page
// directly from the Go binary. The spec is built at runtime from
// the in-process Spec() builder, so there is no risk of the JSON
// going out of sync with the handler code (the JSON encoder
// guarantees well-formedness).
//
// Two routes are wired (see internal/router/router.go):
//
//	GET /api/v1/openapi.json  raw JSON spec
//	GET /docs                 Swagger UI page (loads the JSON from /api/v1/openapi.json)
package openapi

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed index.html
var indexHTML []byte

// Spec returns a handler that serves the OpenAPI JSON. The
// spec is built lazily on first request and cached.
func Spec() gin.HandlerFunc {
	var cached []byte
	return func(c *gin.Context) {
		if cached == nil {
			b, err := json.Marshal(build())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code":    10000,
					"message": "failed to build openapi spec: " + err.Error(),
				})
				return
			}
			cached = b
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", cached)
	}
}

// Docs returns a handler that serves the Swagger UI page. The
// page itself loads the JSON from /api/v1/openapi.json via
// JavaScript — that fetch goes through CORS (allowed by the
// middleware CORS allow-list when served on the same origin).
func Docs() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	}
}
