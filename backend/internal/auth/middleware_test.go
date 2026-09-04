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

// newGinWithAuth builds a gin engine with the service's auth middleware
// and a passthrough handler that records the authenticated user id.
// The /runs/1/events route mirrors the real SSE route shape so the
// query-token fallback's path scoping is exercised.
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
	ok := func(c *gin.Context) {
		c.String(http.StatusOK, "ok:%d", UserID(c))
	}
	r.GET("/ping", ok)
	r.GET("/api/v1/runs/1/events", ok) // SSE-shaped route
	r.POST("/api/v1/runs/1/events", ok)
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

// Native EventSource cannot set an Authorization header, so the SSE
// route accepts the token as an access_token query param (Run #13's
// live-log 401 regression).
func TestMiddleware_QueryTokenFallback_OnSSERoute(t *testing.T) {
	r, svc := newGinWithAuth(t)
	token := loginToken(t, svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/1/events?access_token="+token, nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok:1", w.Body.String())
}

// The query fallback is scoped to the SSE route: any other GET
// (different path) must NOT authenticate via ?access_token — tokens in
// URLs leak to logs/history/Referer, so the channel stays SSE-only.
func TestMiddleware_QueryTokenRejected_OnOtherRoutes(t *testing.T) {
	r, svc := newGinWithAuth(t)
	token := loginToken(t, svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping?access_token="+token, nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// ...and scoped to GET: a POST to the same events path can't ride the
// query token either.
func TestMiddleware_QueryTokenRejected_OnNonGET(t *testing.T) {
	r, svc := newGinWithAuth(t)
	token := loginToken(t, svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/1/events?access_token="+token, nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// A header credential must win over a query token — a leaked URL never
// overrides a real header credential.
func TestMiddleware_HeaderWinsOverQuery(t *testing.T) {
	r, svc := newGinWithAuth(t)
	_ = loginToken(t, svc)

	otherSvc := NewService(newDB(t), "other-secret", time.Hour)
	otherToken := loginToken(t, otherSvc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/1/events?access_token="+otherToken, nil)
	headerToken := loginTokenNoBootstrap(t, svc)
	req.Header.Set("Authorization", "Bearer "+headerToken)
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

// Neither header nor (on the SSE route) query token -> 401.
func TestMiddleware_MissingBothUnauthorized(t *testing.T) {
	r, svc := newGinWithAuth(t)
	_ = loginToken(t, svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/1/events", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// Garbage query token is also rejected.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/runs/1/events?access_token=garbage", nil)
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusUnauthorized, w2.Code)
}

// A non-Bearer Authorization header (proxy-injected Basic, lowercase
// bearer, …) must not BLOCK the SSE query fallback — the header just
// isn't used, and the access_token param still authenticates.
func TestMiddleware_NonBearerHeader_DoesNotBlockSSEFallback(t *testing.T) {
	r, svc := newGinWithAuth(t)
	token := loginToken(t, svc)

	for _, h := range []string{"Basic dXNlcjpwYXNz", "bearer " + token, "Bearer"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/1/events?access_token="+token, nil)
		req.Header.Set("Authorization", h)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "header %q should not block SSE query fallback", h)
	}
}
