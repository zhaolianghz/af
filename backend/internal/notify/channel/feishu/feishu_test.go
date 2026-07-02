// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package feishu

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

// recordingServer spins up an httptest.Server that captures the last
// request and replies with a fixed status/body. It is the test
// replacement for a real Feishu webhook.
type recordingServer struct {
	server      *httptest.Server
	hits        atomic.Int32
	lastBody    string
	lastHeaders http.Header
	status      int
	respBody    string
}

func newRecordingServer(status int, respBody string) *recordingServer {
	rs := &recordingServer{status: status, respBody: respBody}
	rs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		rs.lastBody = string(body)
		rs.lastHeaders = r.Header.Clone()
		w.WriteHeader(rs.status)
		_, _ = w.Write([]byte(rs.respBody))
	}))
	return rs
}

func (rs *recordingServer) Close()       { rs.server.Close() }
func (rs *recordingServer) URL() string  { return rs.server.URL }
func (rs *recordingServer) Hits() int32  { return rs.hits.Load() }

func newTestChannel(url, secret string) *Channel {
	return New(Config{
		WebhookURL: url,
		Secret:     secret,
		Timeout:    2 * time.Second,
	}, notify.NewCircuitBreaker(notify.CircuitConfig{
		FailureThreshold: 3,
		Window:           time.Minute,
		Cooldown:         time.Minute,
	}), zap.NewNop())
}

func TestFeishu_SendSuccess(t *testing.T) {
	rs := newRecordingServer(http.StatusOK, `{"StatusCode":0,"StatusMessage":"success"}`)
	defer rs.Close()

	ch := newTestChannel(rs.URL(), "")
	err := ch.Send(context.Background(), &notify.Message{
		Title:     "t",
		Content:   "hello **world**",
		Type:      notify.MsgAlert,
		Timestamp: time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), rs.Hits())

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(rs.lastBody), &payload))
	assert.Equal(t, "interactive", payload["msg_type"])
	card, ok := payload["card"].(map[string]any)
	require.True(t, ok)
	elements, ok := card["elements"].([]any)
	require.True(t, ok)
	require.Len(t, elements, 1)
	elem := elements[0].(map[string]any)
	assert.Equal(t, "markdown", elem["tag"])
	assert.Equal(t, "hello **world**", elem["content"])
	header, ok := card["header"].(map[string]any)
	require.True(t, ok)
	title := header["title"].(map[string]any)
	assert.Equal(t, "t", title["content"])
}

func TestFeishu_SendHTTP4xxReturnsError(t *testing.T) {
	rs := newRecordingServer(http.StatusForbidden, `{"msg":"forbidden"}`)
	defer rs.Close()

	ch := newTestChannel(rs.URL(), "")
	err := ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=403")
	assert.Contains(t, err.Error(), "forbidden")
}

func TestFeishu_Send5xxReturnsError(t *testing.T) {
	rs := newRecordingServer(http.StatusInternalServerError, "boom")
	defer rs.Close()

	ch := newTestChannel(rs.URL(), "")
	err := ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=500")
}

func TestFeishu_SendAckStatusCodeNonZero(t *testing.T) {
	// 200 HTTP but Feishu ack says the message was rejected.
	rs := newRecordingServer(http.StatusOK, `{"StatusCode":19021,"StatusMessage":"param invalid"}`)
	defer rs.Close()

	ch := newTestChannel(rs.URL(), "")
	err := ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StatusCode=19021")
}

func TestFeishu_TimeoutReturnsError(t *testing.T) {
	// Server that sleeps longer than the channel timeout.
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"StatusCode":0}`))
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

func TestFeishu_CtxCancelPropagates(t *testing.T) {
	hangSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client disconnects.
		<-r.Context().Done()
	}))
	defer hangSrv.Close()

	ch := newTestChannel(hangSrv.URL, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ch.Send(ctx, &notify.Message{Content: "x", Type: notify.MsgAlert})
	require.Error(t, err)
}

func TestFeishu_WithSecretSignsRequest(t *testing.T) {
	rs := newRecordingServer(http.StatusOK, `{"StatusCode":0}`)
	defer rs.Close()

	secret := "test-secret-123"
	ch := newTestChannel(rs.URL(), secret)
	require.NoError(t, ch.Send(context.Background(), &notify.Message{Content: "x", Type: notify.MsgAlert}))

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(rs.lastBody), &payload))
	sign, ok := payload["sign"].(string)
	require.True(t, ok, "sign field should be set when secret is configured")
	assert.NotEmpty(t, sign)

	ts, ok := payload["timestamp"].(string)
	require.True(t, ok)

	// Compute the expected signature: HMAC-SHA256(secret, ts + "\n" + secret)
	stringToSign := ts + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stringToSign))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	assert.Equal(t, expected, sign, "signature must match Feishu's HMAC-SHA256 scheme")
}

func TestFeishu_IsHealthy(t *testing.T) {
	ch := newTestChannel("http://example.invalid", "")
	assert.True(t, ch.IsHealthy())
	assert.Equal(t, "feishu", ch.Name())
}

func TestFeishu_NilMessage(t *testing.T) {
	ch := newTestChannel("http://example.invalid", "")
	err := ch.Send(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil message")
}

func TestFeishu_SignHelper(t *testing.T) {
	// Sanity check on the signFeishu helper used in production.
	ts := int64(1700000000)
	sig, err := signFeishu("s3cret", ts)
	require.NoError(t, err)
	require.NotEmpty(t, sig)

	stringToSign := fmt.Sprintf("%d\n%s", ts, "s3cret")
	mac := hmac.New(sha256.New, []byte("s3cret"))
	_, _ = mac.Write([]byte(stringToSign))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	assert.Equal(t, expected, sig)
}
