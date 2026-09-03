// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package notify provides a multi-channel notification subsystem.
//
// The package is built around a small set of abstractions:
//
//   - Channel: a single concrete delivery target (Feishu, DingTalk, WeCom, ...).
//     Channels are stateless HTTP webhooks — the only state is the circuit
//     breaker they wrap.
//   - Manager: a router that picks a primary channel and falls back to
//     others in order when the primary fails (after retries are exhausted).
//   - Retry / CircuitBreaker: shared reliability helpers used by the manager
//     and exposed for direct use from cron jobs and the strategy engine.
//
// All HTTP traffic in tests must use httptest.NewServer. No code under
// internal/notify/ should make real network calls.
package notify

import (
	"context"
	"errors"
	"time"
)

// Message is the unit of work the manager and channels exchange.
//
// Content is expected to be a Markdown body. Title is optional and may
// be empty for cards that do not have a title slot (WeCom, for example).
// Meta is a free-form bag of business data (stock codes, strategy IDs, ...)
// that channels may or may not use when serializing their payload.
type Message struct {
	Title     string         // 消息标题（可空）
	Content   string         // 消息主体（markdown 文本）
	Type      MessageType    // 类型
	Meta      map[string]any // 业务附加字段（股票、方案等）
	Timestamp time.Time      // 消息构造时间
}

// MessageType is the high-level category of a message. Templates always
// set this so downstream code can filter or route on it.
type MessageType string

const (
	// MsgMorning is the pre-open / morning recommendation card.
	MsgMorning MessageType = "morning"
	// MsgAfternoon is the closing / afternoon recommendation card.
	MsgAfternoon MessageType = "afternoon"
	// MsgReview is the daily / weekly review report card.
	MsgReview MessageType = "review"
	// MsgAlert is the system alert (data-source down, run failed, ...).
	MsgAlert MessageType = "alert"
)

// Channel is a single delivery target. Implementations live in
// internal/notify/channel/<name>/ so they can evolve independently.
type Channel interface {
	// Name returns the channel's registered name (feishu / dingtalk / wecom).
	Name() string
	// Send delivers a message. It MUST honor ctx cancellation/deadline and
	// return a non-nil error for any HTTP non-2xx response. The retry and
	// circuit-breaker wrappers live above this method; the channel itself
	// does not retry.
	Send(ctx context.Context, msg *Message) error
	// IsHealthy reports whether the channel's circuit breaker is closed
	// (i.e. ready to accept new traffic). It is used by /api/v1/notify/health
	// and by debug tooling.
	IsHealthy() bool
}

// Manager picks a primary channel, retries it, then walks fallbacks.
type Manager interface {
	// Send delivers msg via the configured primary channel; if that fails
	// (after retries are exhausted) it walks the fallback channels in
	// order. ErrAllChannelsFailed is returned only when every candidate
	// channel has been tried and failed.
	Send(ctx context.Context, msg *Message) error
	// RegisterChannel associates a name with a Channel implementation.
	// Re-registering an existing name overwrites the previous entry; the
	// circuit breaker is not reset.
	RegisterChannel(name string, ch Channel)
	// List returns the registered channel names in registration order.
	List() []string
	// HasChannel reports whether any channel is registered. Implementations
	// must be safe for concurrent use.
	HasChannel() bool
}

// ErrAllChannelsFailed is returned by Manager.Send when every candidate
// (primary + all fallbacks) has been tried and failed. It is wrapped via
// fmt.Errorf("%w"), so callers should use errors.Is to detect it.
var ErrAllChannelsFailed = errors.New("notify: all channels failed")

// StockPick is the row shape used by the message templates. Fields are
// strings (not numeric) because prices are displayed verbatim and the
// notification layer never does arithmetic on them.
type StockPick struct {
	Code      string // 股票代码
	Name      string // 股票名称
	Strategy  string // 命中的策略 / 模板
	EntryLow  string // 建议买入区间下限
	EntryHigh string // 建议买入区间上限
}
