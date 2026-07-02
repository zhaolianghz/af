// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/httpresp"
)

// RequestID injects a request ID into the context and response headers.
// If the client supplied X-Request-ID, that value is reused; otherwise
// a UUIDv4 is generated. The same value is read by httpresp.Err so
// every error response body carries the request_id and the client can
// grep the server logs by that id.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = uuid.NewString()
		}
		c.Set(httpresp.CtxKeyRequestID, rid)
		c.Writer.Header().Set("X-Request-ID", rid)
		c.Next()
	}
}

// Logger returns a Gin middleware that logs each request via zap.
func Logger(l *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		if raw != "" {
			path = path + "?" + raw
		}

		fields := []zap.Field{
			zap.String("request_id", c.GetString("request_id")),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", latency),
			zap.String("client_ip", c.ClientIP()),
			zap.Int("size", c.Writer.Size()),
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}

		switch {
		case c.Writer.Status() >= 500:
			l.Error("http", fields...)
		case c.Writer.Status() >= 400:
			l.Warn("http", fields...)
		default:
			l.Info("http", fields...)
		}
	}
}
