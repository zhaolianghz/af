// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package executor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/executor/templates"
	"github.com/skyzhao/af/internal/model"
)

func init() { gin.SetMode(gin.TestMode) }

func newTmplTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := model.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newTmplRouter(t *testing.T, h *TemplateHandler) *gin.Engine {
	t.Helper()
	r := gin.New()
	g := r.Group("/api/v1/strategies")
	h.RegisterRoutes(g)
	return r
}

func TestTemplateHandler_List_503_WhenNoLoader(t *testing.T) {
	h := NewTemplateHandler(nil, nil, zap.NewNop())
	r := newTmplRouter(t, h)
	req, _ := http.NewRequest("GET", "/api/v1/strategies/templates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestTemplateHandler_List_200(t *testing.T) {
	db := newTmplTestDB(t)
	loader, err := templates.NewLoader(db, zap.NewNop())
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	h := NewTemplateHandler(loader, db, zap.NewNop())
	r := newTmplRouter(t, h)
	req, _ := http.NewRequest("GET", "/api/v1/strategies/templates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Total < 1 {
		t.Errorf("total = %d, want >= 1 (bundled templates)", resp.Data.Total)
	}
}

func TestTemplateHandler_Instantiate_503_WhenNotWired(t *testing.T) {
	h := NewTemplateHandler(nil, nil, zap.NewNop())
	r := newTmplRouter(t, h)
	req, _ := http.NewRequest("POST", "/api/v1/strategies/from-template/morning_volume_breakout", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestTemplateHandler_Instantiate_404_UnknownCode(t *testing.T) {
	db := newTmplTestDB(t)
	loader, _ := templates.NewLoader(db, zap.NewNop())
	h := NewTemplateHandler(loader, db, zap.NewNop())
	r := newTmplRouter(t, h)
	req, _ := http.NewRequest("POST", "/api/v1/strategies/from-template/does-not-exist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestTemplateHandler_Instantiate_201(t *testing.T) {
	db := newTmplTestDB(t)
	loader, _ := templates.NewLoader(db, zap.NewNop())
	codes := loader.ListCodes()
	if len(codes) == 0 {
		t.Skip("no bundled templates to instantiate")
	}
	h := NewTemplateHandler(loader, db, zap.NewNop())
	r := newTmplRouter(t, h)
	req, _ := http.NewRequest("POST", "/api/v1/strategies/from-template/"+codes[0], nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Strategy map[string]any `json:"strategy"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Strategy["id"] == nil {
		t.Errorf("no strategy id in response: %s", w.Body.String())
	}
}
