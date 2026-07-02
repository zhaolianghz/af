// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package httpresp is the single source of truth for the JSON envelope
// used by every HTTP handler in the backend.
//
// Two wire shapes:
//
//   Success:  {"code":0, "message":"ok|created", "data":<payload>}
//   Error:    {"code":<int>, "message":<string>, "request_id":<uuid>}
//
// The request_id field on error responses is always populated from
// the gin context by middleware.RequestID (or the inbound
// X-Request-ID header), so a client can correlate a failure to a
// server log line by grepping logs/request_id=<id>.
package httpresp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/skyzhao/af/internal/apperr"
)

// CtxKeyRequestID matches the key set by middleware.RequestID. Exported
// so middleware in other packages can set/look it up without
// re-declaring the constant.
const CtxKeyRequestID = "request_id"

// OKResponse is the success envelope. Code 0 means OK.
type OKResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// ErrResponse is the error envelope. RequestID is always present on
// error responses (empty string if the request_id middleware is
// somehow missing — defensive, never the norm in production).
type ErrResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// OK writes 200 with the success envelope.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, OKResponse{
		Code:    int(apperr.CodeOK),
		Message: "ok",
		Data:    data,
	})
}

// Created writes 201 with the success envelope.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, OKResponse{
		Code:    int(apperr.CodeOK),
		Message: "created",
		Data:    data,
	})
}

// Err writes a JSON error response with the request id pulled from
// the gin context. If err is a *apperr.BizError, the HTTP status is
// derived from the code; otherwise 500. nil err writes a 200 OK.
func Err(c *gin.Context, err error) {
	if err == nil {
		OK(c, nil)
		return
	}
	rid := c.GetString(CtxKeyRequestID)
	if be, ok := apperr.As(err); ok {
		c.JSON(apperr.HTTPStatus(be.Code), ErrResponse{
			Code:      int(be.Code),
			Message:   be.Message,
			RequestID: rid,
		})
		return
	}
	c.JSON(http.StatusInternalServerError, ErrResponse{
		Code:      int(apperr.CodeInternal),
		Message:   err.Error(),
		RequestID: rid,
	})
}

// AbortWith writes a JSON error and aborts the gin chain. Used by
// middleware (panic recovery, auth) that needs to short-circuit
// downstream handlers.
func AbortWith(c *gin.Context, code apperr.Code, message string) {
	c.AbortWithStatusJSON(apperr.HTTPStatus(code), ErrResponse{
		Code:      int(code),
		Message:   message,
		RequestID: c.GetString(CtxKeyRequestID),
	})
}
