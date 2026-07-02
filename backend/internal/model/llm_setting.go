// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package model

// LLMSetting is the single-row runtime LLM configuration edited from the
// settings page (§11/§14.9). One row (ID=1) holds the active backend so
// the operator can switch providers without restarting. Falls back to
// the static config (ai.*) when no row exists.
//
// APIKey is stored as given (single-user, self-hosted). The HTTP layer
// masks it on read — it is never returned in plaintext.
type LLMSetting struct {
	BaseEntity

	// Enabled turns the assistant on/off at runtime.
	Enabled bool `json:"enabled"`
	// Provider: "mock" | "openai" (any OpenAI-compatible gateway:
	// openai / deepseek / custom).
	Provider string `gorm:"size:32" json:"provider"`
	BaseURL  string `gorm:"size:255" json:"base_url"`
	APIKey   string `gorm:"size:255" json:"api_key"`
	Model    string `gorm:"size:128" json:"model"`
}

// TableName returns the explicit table name.
func (LLMSetting) TableName() string { return "llm_settings" }
