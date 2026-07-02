// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// runHandler is a small helper that mounts a single handler on a
// fresh Gin engine and returns the response recorder.
func runHandler(t *testing.T, method, path string, h gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, path, h)
	req := httptest.NewRequest(method, path, bytes.NewReader(nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestNewHealthHandler_Empty(t *testing.T) {
	h := NewHealthHandler(nil)
	w := runHandler(t, "GET", "/notify/health", h)
	assert.Equal(t, http.StatusOK, w.Code)

	var snap HealthSnapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snap))
	assert.False(t, snap.Healthy)
	for _, name := range []string{"feishu", "dingtalk", "wecom"} {
		ch, ok := snap.Channels[name]
		require.True(t, ok, "channel %s should be present", name)
		assert.False(t, ch.Registered)
	}
}

func TestNewHealthHandler_AllHealthy(t *testing.T) {
	breakers := map[string]*CircuitBreaker{
		"feishu":   NewCircuitBreaker(CircuitConfig{}),
		"dingtalk": NewCircuitBreaker(CircuitConfig{}),
		"wecom":    NewCircuitBreaker(CircuitConfig{}),
	}
	h := NewHealthHandler(breakers)
	w := runHandler(t, "GET", "/notify/health", h)
	require.Equal(t, http.StatusOK, w.Code)

	var snap HealthSnapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snap))
	assert.True(t, snap.Healthy)
	for _, name := range []string{"feishu", "dingtalk", "wecom"} {
		assert.True(t, snap.Channels[name].Registered, name)
		assert.True(t, snap.Channels[name].Healthy, name)
	}
}

func TestNewHealthHandler_OneOpen(t *testing.T) {
	br := NewCircuitBreaker(CircuitConfig{
		FailureThreshold: 1,
		Window:           time.Hour,
		Cooldown:         time.Hour,
	})
	ok, _ := br.Allow()
	require.True(t, ok)
	br.OnFailure()
	require.Equal(t, StateOpen, br.State())

	breakers := map[string]*CircuitBreaker{"feishu": br}
	h := NewHealthHandler(breakers)
	w := runHandler(t, "GET", "/notify/health", h)

	var snap HealthSnapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snap))
	assert.False(t, snap.Healthy)
	assert.Equal(t, "open", snap.Channels["feishu"].State)
	assert.False(t, snap.Channels["feishu"].Healthy)
}

func TestNewTestPingHandler_Success(t *testing.T) {
	mgr := NewManager(ManagerOptions{Primary: "primary"})
	mgr.RegisterChannel("primary", newFake("primary"))

	h, err := NewTestPingHandler(mgr, zap.NewNop())
	require.NoError(t, err)
	w := runHandler(t, "POST", "/notify/test", h)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, true, body["ok"])
}

func TestNewTestPingHandler_AllChannelsFailReturns502(t *testing.T) {
	mgr := NewManager(ManagerOptions{Primary: "primary", Retry: RetryConfig{Attempts: 1}})
	// Register a primary that always fails.
	bad := &alwaysFailChannel{name: "primary"}
	mgr.RegisterChannel("primary", bad)

	h, err := NewTestPingHandler(mgr, zap.NewNop())
	require.NoError(t, err)
	w := runHandler(t, "POST", "/notify/test", h)
	assert.Equal(t, http.StatusBadGateway, w.Code)
}

type alwaysFailChannel struct{ name string }

func (a *alwaysFailChannel) Name() string                                       { return a.name }
func (a *alwaysFailChannel) Send(ctx context.Context, msg *Message) error        { return assert.AnError }
func (a *alwaysFailChannel) IsHealthy() bool                                    { return true }
