// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package feishu implements the notify.Channel interface for the
// Feishu (Lark) incoming webhook. It supports the optional HMAC-SHA256
// signature header required when the webhook is created with "签名校验"
// enabled in the Feishu admin console.
//
// Wire format: Feishu's "interactive" message card. The payload embeds
// the message Content as a markdown element and the message Title as
// the card header.
//
// Concurrency: a single *Channel is safe for use from multiple
// goroutines. The HTTP client is shared.
package feishu

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/notify"
)

// ChannelName is the registry key used by the manager.
const ChannelName = "feishu"

// Config configures a Feishu Channel. WebhookURL is required; Secret is
// optional — leave it empty to skip signature generation. Timeout is
// the per-call HTTP timeout; the manager also wraps each attempt in a
// context timeout, so the effective deadline is min(Timeout, ctx
// deadline). HTTPClient is optional; nil falls back to
// http.DefaultClient.
type Config struct {
	WebhookURL string
	Secret     string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Channel is the Feishu implementation of notify.Channel.
type Channel struct {
	cfg    Config
	client *http.Client
	// breaker is the per-channel circuit breaker. We expose it as a
	// field so callers (and tests) can inspect state via IsHealthy.
	breaker *notify.CircuitBreaker
	logger  *zap.Logger
}

// New constructs a Feishu Channel. The provided circuit breaker is
// shared with the caller; this is intentional so a manager that owns
// the breaker can also see Feishu's health.
func New(cfg Config, breaker *notify.CircuitBreaker, logger *zap.Logger) *Channel {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}
	if breaker == nil {
		breaker = notify.NewCircuitBreaker(notify.CircuitConfig{})
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Channel{cfg: cfg, client: cfg.HTTPClient, breaker: breaker, logger: logger}
}

// Name returns the channel's registry name.
func (c *Channel) Name() string { return ChannelName }

// IsHealthy reports whether the circuit breaker is closed.
func (c *Channel) IsHealthy() bool { return c.breaker.IsHealthy()}

// Send posts the message to the Feishu webhook. It returns a non-nil
// error for any non-2xx response or transport-level failure.
func (c *Channel) Send(ctx context.Context, msg *notify.Message) error {
	if msg == nil {
		return fmt.Errorf("feishu: nil message")
	}
	body, err := buildPayload(msg, c.cfg.Secret, time.Now())
	if err != nil {
		return fmt.Errorf("feishu: encode payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("feishu: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("feishu: http: %w", err)
	}
	defer resp.Body.Close()

	// Read a small amount of the body for diagnostics. Feishu returns
	// at most a few hundred bytes of error context.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("feishu: non-2xx status=%d body=%s", resp.StatusCode, string(respBody))
	}

	// Feishu returns {"StatusCode":0,"StatusMessage":"success"} on
	// success. A non-zero StatusCode in a 2xx response is treated as a
	// failure so retry logic kicks in.
	var ack struct {
		StatusCode    int    `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if err := json.Unmarshal(respBody, &ack); err == nil && ack.StatusCode != 0 {
		return fmt.Errorf("feishu: ack StatusCode=%d msg=%q", ack.StatusCode, ack.StatusMessage)
	}
	return nil
}

// feishuPayload is the request body shape. We model it as an inline
// struct rather than a top-level type to keep the package's exported
// surface small.
type feishuPayload struct {
	Timestamp string         `json:"timestamp"`
	Sign      string         `json:"sign,omitempty"`
	MsgType   string         `json:"msg_type"`
	Card      map[string]any `json:"card"`
}

func buildPayload(msg *notify.Message, secret string, now time.Time) ([]byte, error) {
	elements := []map[string]any{
		{"tag": "markdown", "content": msg.Content},
	}
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"elements": elements,
	}
	if msg.Title != "" {
		card["header"] = map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": msg.Title,
			},
			"template": "blue",
		}
	}
	payload := feishuPayload{
		Timestamp: fmt.Sprintf("%d", now.Unix()),
		MsgType:   "interactive",
		Card:      card,
	}
	if secret != "" {
		sign, err := signFeishu(secret, now.Unix())
		if err != nil {
			return nil, err
		}
		payload.Sign = sign
	}
	return json.Marshal(payload)
}

// signFeishu implements Feishu's signature scheme:
//   key = secret
//   string_to_sign = timestamp + "\n" + secret
//   HMAC-SHA256(key, string_to_sign) → base64
// See https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot
func signFeishu(secret string, ts int64) (string, error) {
	stringToSign := fmt.Sprintf("%d\n%s", ts, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}
