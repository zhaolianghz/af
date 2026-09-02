// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package middleware

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// defaultDevOrigins are the CORS origins allowed when no explicit
// configuration is provided — local frontend dev (Vite: 5173 / CRA: 3000).
var defaultDevOrigins = []string{
	"http://localhost:5173",
	"http://127.0.0.1:5173",
	"http://localhost:3000",
	"http://127.0.0.1:3000",
}

// CORS returns a CORS middleware. allowedOrigins comes from config
// (server.cors_origins or SERVER_CORS_ORIGINS, comma-separated);
// when empty it falls back to the localhost dev defaults. Production
// deployments MUST set their real frontend origin(s) here.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	if len(allowedOrigins) == 0 {
		allowedOrigins = defaultDevOrigins
	}
	cfg := cors.Config{
		AllowOrigins: allowedOrigins,
		AllowMethods: []string{
			"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS",
		},
		AllowHeaders: []string{
			"Origin", "Content-Type", "Content-Length", "Accept-Encoding",
			"Authorization", "X-Requested-With", "X-Request-ID",
		},
		ExposeHeaders:    []string{"Content-Length", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	return cors.New(cfg)
}
