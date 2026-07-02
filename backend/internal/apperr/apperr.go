// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package apperr defines common application error types and codes.
//
// The handler layer converts BizError into a uniform JSON response:
//   { "code": <int>, "message": <string>, "data": <any?> }
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a stable, machine-readable error code.
type Code int

const (
	CodeOK Code = 0

	// Generic
	CodeInternal     Code = 10000
	CodeInvalidArg   Code = 10001
	CodeNotFound     Code = 10002
	CodeConflict     Code = 10003
	CodeUnauthorized Code = 10004
	CodeForbidden    Code = 10005
	CodeUnavailable  Code = 10006
	CodeTimeout      Code = 10007
)

// BizError is a structured application error.
type BizError struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	// Cause is the underlying error (optional, for logging).
	Cause error `json:"-"`
}

func (e *BizError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *BizError) Unwrap() error { return e.Cause }

// New creates a BizError with the given code and message.
func New(code Code, msg string) *BizError {
	return &BizError{Code: code, Message: msg}
}

// Wrap wraps a cause into a BizError.
func Wrap(code Code, msg string, cause error) *BizError {
	return &BizError{Code: code, Message: msg, Cause: cause}
}

// As is a convenience for errors.As.
func As(err error) (*BizError, bool) {
	var be *BizError
	if errors.As(err, &be) {
		return be, true
	}
	return nil, false
}

// Common constructors ------------------------------------------------------

func Internal(msg string, cause error) *BizError {
	return Wrap(CodeInternal, msg, cause)
}
func InvalidArg(msg string) *BizError {
	return New(CodeInvalidArg, msg)
}
func NotFound(msg string) *BizError {
	return New(CodeNotFound, msg)
}
func Conflict(msg string) *BizError {
	return New(CodeConflict, msg)
}
func Unauthorized(msg string) *BizError {
	return New(CodeUnauthorized, msg)
}
func Forbidden(msg string) *BizError {
	return New(CodeForbidden, msg)
}
func Unavailable(msg string) *BizError {
	return New(CodeUnavailable, msg)
}
func Timeout(msg string) *BizError {
	return New(CodeTimeout, msg)
}

// HTTPStatus maps a Code to a sensible HTTP status.
func HTTPStatus(c Code) int {
	switch c {
	case CodeOK:
		return http.StatusOK
	case CodeInvalidArg:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeTimeout:
		return http.StatusGatewayTimeout
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
