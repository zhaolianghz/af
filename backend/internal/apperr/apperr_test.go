// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package apperr

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewAndWrap(t *testing.T) {
	e := New(CodeInvalidArg, "bad")
	assert.Equal(t, CodeInvalidArg, e.Code)
	assert.Equal(t, "bad", e.Message)
	assert.Equal(t, "[10001] bad", e.Error())

	cause := errors.New("root")
	w := Wrap(CodeInternal, "boom", cause)
	assert.Equal(t, cause, w.Unwrap())
	assert.Contains(t, w.Error(), "root")
}

func TestAs(t *testing.T) {
	e := NotFound("nope")
	var target *BizError
	if !errors.As(e, &target) {
		t.Fatal("expected errors.As to find *BizError")
	}
	assert.Equal(t, CodeNotFound, target.Code)
}

func TestHTTPStatus(t *testing.T) {
	cases := map[Code]int{
		CodeOK:           http.StatusOK,
		CodeInvalidArg:   http.StatusBadRequest,
		CodeUnauthorized: http.StatusUnauthorized,
		CodeForbidden:    http.StatusForbidden,
		CodeNotFound:     http.StatusNotFound,
		CodeConflict:     http.StatusConflict,
		CodeTimeout:      http.StatusGatewayTimeout,
		CodeUnavailable:  http.StatusServiceUnavailable,
		CodeInternal:     http.StatusInternalServerError,
		Code(99999):      http.StatusInternalServerError,
	}
	for c, want := range cases {
		assert.Equal(t, want, HTTPStatus(c), "code %d", c)
	}
}

func TestConvenienceConstructors(t *testing.T) {
	assert.Equal(t, CodeInternal, Internal("x", nil).Code)
	assert.Equal(t, CodeInvalidArg, InvalidArg("x").Code)
	assert.Equal(t, CodeNotFound, NotFound("x").Code)
	assert.Equal(t, CodeConflict, Conflict("x").Code)
	assert.Equal(t, CodeUnauthorized, Unauthorized("x").Code)
	assert.Equal(t, CodeForbidden, Forbidden("x").Code)
	assert.Equal(t, CodeUnavailable, Unavailable("x").Code)
	assert.Equal(t, CodeTimeout, Timeout("x").Code)
}
