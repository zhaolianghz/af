// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package wecom implements notify.Channel for WeCom (企业微信) group
// robot webhooks. The supported payload is the "markdown" type, which
// renders msg.Content as a Markdown block inside the group message.
//
// WeCom does not require any signature header for the basic markdown
// type — its signing scheme only applies to the "text" message type —
// so this implementation is stateless from an auth perspective.
package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/notify"
)

const ChannelName = "wecom"

// Config configures a WeCom Channel. WebhookURL is required.
type Config struct {
	WebhookURL string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Channel is the WeCom implementation of notify.Channel.
type Channel struct {
	cfg     Config
	client  *http.Client
	breaker *notify.CircuitBreaker
	logger  *zap.Logger
}

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

func (c *Channel) Name() string    { return ChannelName }
func (c *Channel) IsHealthy() bool { return c.breaker.IsHealthy() }

// Send delivers msg to the configured WeCom webhook.
func (c *Channel) Send(ctx context.Context, msg *notify.Message) error {
	if msg == nil {
		return fmt.Errorf("wecom: nil message")
	}

	body, err := buildPayload(msg)
	if err != nil {
		return fmt.Errorf("wecom: encode payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("wecom: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("wecom: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wecom: non-2xx status=%d body=%s", resp.StatusCode, string(respBody))
	}

	// WeCom returns {"errcode":0,"errmsg":"ok"} on success.
	var ack struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &ack); err == nil && ack.ErrCode != 0 {
		return fmt.Errorf("wecom: ack errcode=%d msg=%q", ack.ErrCode, ack.ErrMsg)
	}
	return nil
}

type wecomMarkdown struct {
	Content string `json:"content"`
}

type wecomPayload struct {
	MsgType  string        `json:"msgtype"`
	Markdown wecomMarkdown `json:"markdown"`
}

func buildPayload(msg *notify.Message) ([]byte, error) {
	// WeCom markdown has no title field at the top level, so we fold
	// the title into the body to keep it visible in the chat preview.
	content := msg.Content
	if msg.Title != "" {
		content = "## " + msg.Title + "\n\n" + content
	}
	return json.Marshal(wecomPayload{
		MsgType:  "markdown",
		Markdown: wecomMarkdown{Content: content},
	})
}
