// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package dingtalk implements notify.Channel for DingTalk custom
// robot webhooks. It supports the optional HMAC-SHA256 signature
// computed over (timestamp + "\n" + secret) that DingTalk requires
// when "加签" is enabled in the robot settings.
//
// Wire format: {"msgtype":"markdown","markdown":{"title":...,"text":...}}
// — DingTalk's documented schema for the "markdown" message type.
package dingtalk

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
	"net/url"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/notify"
)

const ChannelName = "dingtalk"

// Config configures a DingTalk Channel. WebhookURL must be the base
// URL provided by the DingTalk admin console; when Secret is set, the
// channel appends timestamp + sign as query parameters per the
// official signing scheme.
type Config struct {
	WebhookURL string
	Secret     string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Channel is the DingTalk implementation of notify.Channel.
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

// Send delivers msg to the configured DingTalk webhook.
func (c *Channel) Send(ctx context.Context, msg *notify.Message) error {
	if msg == nil {
		return fmt.Errorf("dingtalk: nil message")
	}

	endpoint := c.cfg.WebhookURL
	// Apply signature query params when a secret is configured.
	if c.cfg.Secret != "" {
		signed, err := signDingTalk(c.cfg.Secret, time.Now().UnixMilli())
		if err != nil {
			return fmt.Errorf("dingtalk: sign: %w", err)
		}
		endpoint = appendSign(endpoint, signed.timestamp, signed.sign)
	}

	body, err := buildPayload(msg)
	if err != nil {
		return fmt.Errorf("dingtalk: encode payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("dingtalk: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dingtalk: non-2xx status=%d body=%s", resp.StatusCode, string(respBody))
	}

	// DingTalk returns {"errcode":0,"errmsg":"ok"} on success. A
	// non-zero errcode in a 2xx response is treated as failure.
	var ack struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &ack); err == nil && ack.ErrCode != 0 {
		return fmt.Errorf("dingtalk: ack errcode=%d msg=%q", ack.ErrCode, ack.ErrMsg)
	}
	return nil
}

type dingTalkMarkdown struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

type dingTalkPayload struct {
	MsgType  string            `json:"msgtype"`
	Markdown dingTalkMarkdown  `json:"markdown"`
	At       map[string]any    `json:"at,omitempty"`
}

func buildPayload(msg *notify.Message) ([]byte, error) {
	text := msg.Content
	if msg.Title != "" {
		// DingTalk's markdown card displays the title in the chat
		// preview, so we don't need to prefix the body.
		text = "**" + msg.Title + "**\n\n" + text
	}
	return json.Marshal(dingTalkPayload{
		MsgType:  "markdown",
		Markdown: dingTalkMarkdown{Title: msg.Title, Text: text},
		At:       map[string]any{"isAtAll": false},
	})
}

type signedURL struct {
	timestamp string
	sign      string
}

// signDingTalk computes DingTalk's signature: HMAC-SHA256(secret,
// timestamp + "\n" + secret) base64-encoded.
func signDingTalk(secret string, tsMs int64) (signedURL, error) {
	stringToSign := strconv.FormatInt(tsMs, 10) + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return signedURL{}, err
	}
	return signedURL{
		timestamp: strconv.FormatInt(tsMs, 10),
		sign:      base64.StdEncoding.EncodeToString(mac.Sum(nil)),
	}, nil
}

// appendSign adds timestamp and sign to the webhook URL. Existing query
// parameters are preserved.
func appendSign(rawURL string, ts, sign string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		// Best-effort: just concatenate. url.Parse is permissive
		// enough that this should not happen in practice.
		sep := "?"
		if len(rawURL) > 0 && rawURL[len(rawURL)-1] == '?' {
			sep = ""
		}
		return rawURL + sep + "timestamp=" + ts + "&sign=" + sign
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String()
}
