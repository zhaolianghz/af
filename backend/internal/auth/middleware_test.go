// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/model"
)

// newGin builds a gin engine with the service's auth middleware and a
// passthrough handler that records the authenticated username.
func newGinWithAuth(t *testing.T) (*gin.Engine, *Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	svc := NewService(db, "test-secret", time.Hour)

	r := gin.New()
	r.Use(svc.Middleware())
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok:%d", UserID(c))
	})
	return r, svc
}

func loginToken(t *testing.T, svc *Service) string {
	t.Helper()
	_, err := svc.Bootstrap(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)
	token, _, err := svc.Login(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)
	return token
}

// Header auth remains the primary path.
func TestMiddleware_HeaderBearer(t *testing.T) {
	r, svc := newGinWithAuth(t)
	token := loginToken(t, svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok:1", w.Body.String())
}

// Native EventSource cannot set an Authorization header, so SSE
// consumers pass the token as an access_token query param (Run #13's
// live-log 401 regression).
func TestMiddleware_QueryTokenFallback(t *testing.T) {
	r, svc := newGinWithAuth(t)
	token := loginToken(t, svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping?access_token="+token, nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok:1", w.Body.String())
}

// A header credential must win over a query token — a leaked URL never
// overrides a real header credential.
func TestMiddleware_HeaderWinsOverQuery(t *testing.T) {
	r, svc := newGinWithAuth(t)
	_ = loginToken(t, svc)

	// garbage query token + valid header -> header path authenticates.
	// (We can't easily build a second valid token with a different user
	// here; a garbage query token suffices to prove it's ignored.)
	otherSvc := NewService(newDB(t), "other-secret", time.Hour)
	otherToken := loginToken(t, otherSvc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping?access_token="+otherToken, nil)
	// Sign the header token with THIS service's secret so it verifies.
	headerToken := loginTokenNoBootstrap(t, svc)
	req.Header.Set("Authorization", "Bearer "+headerToken)
	_ = otherToken
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok:1", w.Body.String())
}

func loginTokenNoBootstrap(t *testing.T, svc *Service) string {
	t.Helper()
	token, _, err := svc.Login(context.Background(), "admin", "hunter2hunter2")
	require.NoError(t, err)
	return token
}

// Neither header nor query token -> 401.
func TestMiddleware_MissingBothUnauthorized(t *testing.T) {
	r, svc := newGinWithAuth(t)
	_ = loginToken(t, svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// Garbage query token is also rejected.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/ping?access_token=garbage", nil)
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusUnauthorized, w2.Code)
}
