// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package ai

import (
	"context"
	"sync"
	"time"
)

// Provider is a hot-swappable Client wrapper. The active backend can be
// replaced at runtime (from the settings page) without restarting the
// server. It implements Client, so ai.Service / review.Service take it
// transparently. A nil inner client means "AI disabled" — calls return
// ErrDisabled so handlers surface a clean 503.
type Provider struct {
	mu  sync.RWMutex
	cur Client
}

// NewProvider builds a Provider around an initial client (may be nil).
func NewProvider(initial Client) *Provider {
	return &Provider{cur: initial}
}

// Set atomically swaps the active client (nil disables).
func (p *Provider) Set(c Client) {
	p.mu.Lock()
	p.cur = c
	p.mu.Unlock()
}

// Current returns the active client (may be nil).
func (p *Provider) Current() Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cur
}

// Name reports the active backend, or "disabled".
func (p *Provider) Name() string {
	c := p.Current()
	if c == nil {
		return "disabled"
	}
	return c.Name()
}

// ProposeDAG delegates to the active client.
func (p *Provider) ProposeDAG(ctx context.Context, currentDAG, instruction string) (string, error) {
	c := p.Current()
	if c == nil {
		return "", ErrDisabled
	}
	return c.ProposeDAG(ctx, currentDAG, instruction)
}

// Summarize delegates to the active client.
func (p *Provider) Summarize(ctx context.Context, system, user string) (string, error) {
	c := p.Current()
	if c == nil {
		return "", ErrDisabled
	}
	return c.Summarize(ctx, system, user)
}

// Preset is a known OpenAI-compatible provider's default endpoint +
// model, so the UI/operator only has to pick a name and supply a key.
// A custom/unknown provider supplies its own base_url + model.
type Preset struct {
	BaseURL string
	Model   string
}

// Presets maps a provider label to its OpenAI-compatible defaults. All
// of these speak /chat/completions, so the single openAIClient handles
// every one — only base_url + model differ. "mock" and "custom" carry
// no defaults (mock needs none; custom is user-supplied).
//
// Verified endpoints (2026-06):
//   - deepseek: https://api.deepseek.com/v1
//   - openai:   https://api.openai.com/v1
//   - minimax:  https://api.minimax.io/v1            (OpenAI-compatible)
//   - bailian:  https://dashscope.aliyuncs.com/compatible-mode/v1  (阿里云百炼)
//   - glm:      https://open.bigmodel.cn/api/paas/v4 (智谱 GLM)
var Presets = map[string]Preset{
	"deepseek": {BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"},
	"openai":   {BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"},
	"minimax":  {BaseURL: "https://api.minimax.io/v1", Model: "MiniMax-Text-01"},
	"bailian":  {BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus"},
	"glm":      {BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4.5"},
}

// ResolveEndpoint fills in a preset's base_url/model when the caller
// left them blank, so a row that just says provider=glm + api_key works.
// Explicit base_url/model always win (custom gateways, pinned models).
func ResolveEndpoint(provider, baseURL, model string) (string, string) {
	if p, ok := Presets[provider]; ok {
		if baseURL == "" {
			baseURL = p.BaseURL
		}
		if model == "" {
			model = p.Model
		}
	}
	return baseURL, model
}

// BuildClient constructs a Client from a provider name + connection
// settings. "mock" needs nothing; everything else is treated as
// OpenAI-compatible (openai / deepseek / minimax / bailian / glm /
// custom gateways). Preset providers fill in base_url + model defaults
// when omitted. Returns nil + an error message when the resolved
// openai-compatible settings are still incomplete.
func BuildClient(provider, baseURL, apiKey, model string, timeout time.Duration) (Client, string) {
	switch provider {
	case "", "mock":
		return NewMockClient(), ""
	default: // openai-compatible
		baseURL = normBase(baseURL) // tolerate pasted full endpoints (.../chat/completions)
		baseURL, model = ResolveEndpoint(provider, baseURL, model)
		if baseURL == "" || apiKey == "" || model == "" {
			return nil, "openai-compatible provider needs base_url, api_key and model"
		}
		return NewOpenAIClient(baseURL, apiKey, model, timeout), ""
	}
}
