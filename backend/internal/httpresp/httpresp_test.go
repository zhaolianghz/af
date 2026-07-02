// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package httpresp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/skyzhao/af/internal/apperr"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// helper: build a gin context with an optional request id, run fn, return response.
func runCtx(t *testing.T, rid string, fn func(c *gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if rid != "" {
		c.Set(CtxKeyRequestID, rid)
	}
	req := httptest.NewRequest("GET", "/x", nil)
	c.Request = req
	fn(c)
	return w
}

func TestOK(t *testing.T) {
	w := runCtx(t, "rid-1", func(c *gin.Context) {
		OK(c, map[string]string{"k": "v"})
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body OKResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != 0 || body.Message != "ok" {
		t.Errorf("body = %+v", body)
	}
	d, _ := body.Data.(map[string]any)
	if d["k"] != "v" {
		t.Errorf("data = %+v", body.Data)
	}
}

func TestCreated(t *testing.T) {
	w := runCtx(t, "", func(c *gin.Context) {
		Created(c, nil)
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
}

func TestErr_BizError_IncludesRequestID(t *testing.T) {
	w := runCtx(t, "abc-123", func(c *gin.Context) {
		Err(c, apperr.NotFound("strategy 42 not found"))
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	var body ErrResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != int(apperr.CodeNotFound) {
		t.Errorf("code = %d, want %d", body.Code, apperr.CodeNotFound)
	}
	if body.Message != "strategy 42 not found" {
		t.Errorf("message = %q", body.Message)
	}
	if body.RequestID != "abc-123" {
		t.Errorf("request_id = %q, want abc-123", body.RequestID)
	}
}

func TestErr_GenericError_500(t *testing.T) {
	w := runCtx(t, "rid-2", func(c *gin.Context) {
		Err(c, errors.New("db connection lost"))
	})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	var body ErrResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Code != int(apperr.CodeInternal) {
		t.Errorf("code = %d, want %d", body.Code, apperr.CodeInternal)
	}
	if body.RequestID != "rid-2" {
		t.Errorf("request_id = %q, want rid-2", body.RequestID)
	}
}

func TestErr_NoRequestID_EmptyString(t *testing.T) {
	w := runCtx(t, "", func(c *gin.Context) {
		Err(c, apperr.InvalidArg("bad input"))
	})
	var body ErrResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.RequestID != "" {
		t.Errorf("request_id = %q, want empty (no middleware)", body.RequestID)
	}
	// field MUST still be present in JSON, not omitted — the whole
	// point is client tooling can rely on it.
	raw, _ := json.Marshal(body)
	if !contains(string(raw), `"request_id":""`) {
		t.Errorf("raw json missing request_id key: %s", string(raw))
	}
}

func TestErr_NilBecomesOK(t *testing.T) {
	w := runCtx(t, "", func(c *gin.Context) {
		Err(c, nil)
	})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (nil err should fall through to OK)", w.Code)
	}
}

func TestAbortWith(t *testing.T) {
	var aborted bool
	w := runCtx(t, "rid-abort", func(c *gin.Context) {
		AbortWith(c, apperr.CodeUnauthorized, "login required")
		aborted = c.IsAborted()
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if !aborted {
		t.Error("chain should be aborted")
	}
	var body ErrResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.RequestID != "rid-abort" {
		t.Errorf("request_id = %q", body.RequestID)
	}
}

// contains is a tiny helper so the test doesn't import strings just
// for one check.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
