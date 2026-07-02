// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package dingtalk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	lastURL  string
	lastBody string
	status   int
	respBody string
}

func newRecordingServer(status int, respBody string) *recordingServer {
	rs := &recordingServer{status: status, respBody: respBody}
	rs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.hits.Add(1)
		rs.lastURL = r.URL.String()
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

func newTestChannel(url, secret string) *Channel {
	return New(Config{
		WebhookURL: url,
		Secret:     secret,
		Timeout:    2 * time.Second,
	}, notify.NewCircuitBreaker(notify.CircuitConfig{}), zap.NewNop())
}

func TestDingTalk_SendSuccess(t *testing.T) {
	rs := newRecordingServer(http.StatusOK, `{"errcode":0,"errmsg":"ok"}`)
	defer rs.Close()

	ch := newTestChannel(rs.URL(), "")
	err := ch.Send(context.Background(), &notify.Message{
		Title:     "标题",
		Content:   "hello dingtalk",
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
	assert.Equal(t, "标题", md["title"])
	// Title should be folded into the body for chat preview.
	body, _ := md["text"].(string)
	assert.Contains(t, body, "**标题**")
	assert.Contains(t, body, "hello dingtalk")
}

func TestDingTalk_SendHTTP4xx(t *testing.T) {
	rs := newRecordingServer(http.StatusUnauthorized, `{"errcode":310000,"errmsg":"invalid"}`)
	defer rs.Close()

	ch := newTestChannel(rs.URL(), "")
	err := ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=401")
}

func TestDingTalk_Send5xx(t *testing.T) {
	rs := newRecordingServer(http.StatusBadGateway, "upstream down")
	defer rs.Close()

	ch := newTestChannel(rs.URL(), "")
	err := ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=502")
}

func TestDingTalk_AckErrorCode(t *testing.T) {
	rs := newRecordingServer(http.StatusOK, `{"errcode":300001,"errmsg":"rate limit"}`)
	defer rs.Close()

	ch := newTestChannel(rs.URL(), "")
	err := ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "errcode=300001")
}

func TestDingTalk_Timeout(t *testing.T) {
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

func TestDingTalk_CtxCancel(t *testing.T) {
	hangSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hangSrv.Close()

	ch := newTestChannel(hangSrv.URL, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ch.Send(ctx, &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
}

func TestDingTalk_WithSecretAppendsSign(t *testing.T) {
	rs := newRecordingServer(http.StatusOK, `{"errcode":0,"errmsg":"ok"}`)
	defer rs.Close()

	secret := "ding-secret-1"
	ch := newTestChannel(rs.URL(), secret)
	require.NoError(t, ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert}))

	// Verify that the URL got timestamp + sign query params.
	assert.Contains(t, rs.lastURL, "timestamp=")
	assert.Contains(t, rs.lastURL, "sign=")

	// Extract the sign value and verify it.
	ts, sign, ok := extractSign(rs.lastURL)
	require.True(t, ok, "URL should have timestamp+sign: %s", rs.lastURL)

	stringToSign := fmt.Sprintf("%s\n%s", ts, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stringToSign))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	assert.Equal(t, expected, sign)
}

func TestDingTalk_IsHealthyAndName(t *testing.T) {
	ch := newTestChannel("http://example.invalid", "")
	assert.Equal(t, "dingtalk", ch.Name())
	assert.True(t, ch.IsHealthy())
}

func TestDingTalk_NilMessage(t *testing.T) {
	ch := newTestChannel("http://example.invalid", "")
	err := ch.Send(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil message")
}

// extractSign pulls timestamp= and sign= from a URL.
func extractSign(rawURL string) (ts, sign string, ok bool) {
	u, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", "", false
	}
	ts = u.URL.Query().Get("timestamp")
	sign = u.URL.Query().Get("sign")
	if ts == "" || sign == "" {
		return "", "", false
	}
	return ts, sign, true
}
