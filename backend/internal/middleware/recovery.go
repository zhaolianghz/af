// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package middleware

import (
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
)

// Recovery turns panics into a 500 JSON response (with request_id)
// and logs the stack.
func Recovery(l *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				rid := c.GetString(httpresp.CtxKeyRequestID)
				l.Error("panic recovered",
					zap.Any("error", r),
					zap.String("request_id", rid),
					zap.String("path", c.Request.URL.Path),
					zap.ByteString("stack", debug.Stack()),
				)
				httpresp.AbortWith(c, apperr.CodeInternal, "internal server error")
			}
		}()
		c.Next()
	}
}
