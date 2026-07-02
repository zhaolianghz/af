// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package wecom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/notify"
)

type recordingServer struct {
	server   *httptest.Server
	hits     atomic.Int32
	lastBody string
	status   int
	respBody string
}

func newRecordingServer(status int, respBody string) *recordingServer {
	rs := &recordingServer{status: status, respBody: respBody}
	rs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		rs.lastBody = string(body)
		w.WriteHeader(rs.status)
		_, _ = w.Write([]byte(rs.respBody))
	}))
	return rs
}

func (rs *recordingServer) Close()      { rs.server.Close() }
func (rs *recordingServer) URL() string { return rs.server.URL }
func (rs *recordingServer) Hits() int32 { return rs.hits.Load() }

func newTestChannel(url string) *Channel {
	return New(Config{
		WebhookURL: url,
		Timeout:    2 * time.Second,
	}, notify.NewCircuitBreaker(notify.CircuitConfig{}), zap.NewNop())
}

func TestWeCom_SendSuccess(t *testing.T) {
	rs := newRecordingServer(http.StatusOK, `{"errcode":0,"errmsg":"ok"}`)
	defer rs.Close()

	ch := newTestChannel(rs.URL())
	err := ch.Send(context.Background(), &notify.Message{
		Title:     "标题",
		Content:   "hello wecom",
		Type:      notify.MsgAlert,
		Timestamp: time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), rs.Hits())

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(rs.lastBody), &payload))
	assert.Equal(t, "markdown", payload["msgtype"])
	md, ok := payload["markdown"].(map[string]any)
	require.True(t, ok)
	body, _ := md["content"].(string)
	assert.Contains(t, body, "## 标题")
	assert.Contains(t, body, "hello wecom")
}

func TestWeCom_SendHTTP4xx(t *testing.T) {
	rs := newRecordingServer(http.StatusBadRequest, `{"errcode":400,"errmsg":"bad"}`)
	defer rs.Close()

	ch := newTestChannel(rs.URL())
	err := ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=400")
}

func TestWeCom_Send5xx(t *testing.T) {
	rs := newRecordingServer(http.StatusInternalServerError, "internal")
	defer rs.Close()

	ch := newTestChannel(rs.URL())
	err := ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=500")
}

func TestWeCom_AckErrorCode(t *testing.T) {
	rs := newRecordingServer(http.StatusOK, `{"errcode":40013,"errmsg":"invalid webhook"}`)
	defer rs.Close()

	ch := newTestChannel(rs.URL())
	err := ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "errcode=40013")
}

func TestWeCom_Timeout(t *testing.T) {
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowSrv.Close()

	ch := New(Config{
		WebhookURL: slowSrv.URL,
		Timeout:    50 * time.Millisecond,
		HTTPClient: &http.Client{Timeout: 50 * time.Millisecond},
	}, nil, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	err := ch.Send(ctx, &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
}

func TestWeCom_CtxCancel(t *testing.T) {
	hangSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hangSrv.Close()

	ch := newTestChannel(hangSrv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ch.Send(ctx, &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
}

func TestWeCom_IsHealthyAndName(t *testing.T) {
	ch := newTestChannel("http://example.invalid")
	assert.Equal(t, "wecom", ch.Name())
	assert.True(t, ch.IsHealthy())
}

func TestWeCom_NilMessage(t *testing.T) {
	ch := newTestChannel("http://example.invalid")
	err := ch.Send(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil message")
}
