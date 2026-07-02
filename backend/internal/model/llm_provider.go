// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

// LLMProvider is one backend in the ordered LLM fallback chain (§11/
// §14.9). Multiple rows form a priority-ordered list: the assistant
// tries Priority 0 first, and only falls through to the next when a
// backend is unavailable (error / timeout / rate-limit). This is the
// multi-provider successor to the single-row LLMSetting — on first
// boot the old single row is migrated into one LLMProvider (priority 0).
//
// All supported providers are OpenAI-compatible (openai / deepseek /
// minimax / bailian / glm / custom gateways), so Provider is just a
// label; BaseURL + Model drive the actual call. "mock" needs no creds.
//
// APIKey is stored as given (single-user, self-hosted). The HTTP layer
// masks it on read — never returned in plaintext.
type LLMProvider struct {
	BaseEntity

	// Priority orders the chain; lower is tried first (0 = primary).
	Priority int `gorm:"index" json:"priority"`
	// Enabled lets a row stay configured but skipped in the chain.
	Enabled bool `json:"enabled"`
	// Provider is the preset label: mock | openai | deepseek | minimax
	// | bailian | glm | custom. Everything but mock is OpenAI-compatible.
	Provider string `gorm:"size:32" json:"provider"`
	BaseURL  string `gorm:"size:255" json:"base_url"`
	APIKey   string `gorm:"size:255" json:"api_key"`
	Model    string `gorm:"size:128" json:"model"`
}

// TableName returns the explicit table name.
func (LLMProvider) TableName() string { return "llm_providers" }
