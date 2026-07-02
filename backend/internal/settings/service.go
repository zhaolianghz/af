// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package settings manages the runtime LLM configuration (§11/§14.9
// settings page): persist the active backend, hot-swap the shared
// ai.Provider, and test a connection before saving.
package settings

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/ai"
	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/model"
)

const settingRowID = 1

// Service owns the LLM setting row + the live Provider.
type Service struct {
	db       *gorm.DB
	provider *ai.Provider
	timeout  time.Duration
}

// NewService wires the service. provider is the shared hot-swappable
// client used by ai.Service / review.Service; timeout bounds LLM HTTP.
func NewService(db *gorm.DB, provider *ai.Provider, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Service{db: db, provider: provider, timeout: timeout}
}

// LLMView is the API shape — api_key is masked, never plaintext.
type LLMView struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider"`
	BaseURL      string `json:"base_url"`
	APIKeySet    bool   `json:"api_key_set"`
	APIKeyMasked string `json:"api_key_masked"`
	Model        string `json:"model"`
	Active       string `json:"active"` // current live backend name
}

// Get returns the persisted setting (masked), or zero-ish defaults when
// no row exists yet.
func (s *Service) Get(ctx context.Context) (*LLMView, error) {
	if s.db == nil {
		return nil, apperr.Unavailable("settings: db not configured")
	}
	var row model.LLMSetting
	err := s.db.WithContext(ctx).First(&row, settingRowID).Error
	v := &LLMView{Active: s.provider.Name()}
	if err == nil {
		v.Enabled = row.Enabled
		v.Provider = row.Provider
		v.BaseURL = row.BaseURL
		v.Model = row.Model
		v.APIKeySet = row.APIKey != ""
		v.APIKeyMasked = mask(row.APIKey)
	}
	return v, nil
}

// SaveInput is the editable payload. APIKey empty + KeepKey=true keeps
// the stored key (so the UI can save without re-typing the secret).
type SaveInput struct {
	Enabled  bool
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
	KeepKey  bool
}

// Save persists the setting, rebuilds the client, and hot-swaps the
// Provider. Validates the provider/connection shape first.
func (s *Service) Save(ctx context.Context, in SaveInput) (*LLMView, error) {
	if s.db == nil {
		return nil, apperr.Unavailable("settings: db not configured")
	}
	var row model.LLMSetting
	_ = s.db.WithContext(ctx).First(&row, settingRowID).Error // ignore not-found
	row.ID = settingRowID

	apiKey := in.APIKey
	if in.KeepKey && apiKey == "" {
		apiKey = row.APIKey // keep existing secret
	}

	// Validate by building a client (also catches missing fields).
	if in.Enabled {
		if _, msg := ai.BuildClient(in.Provider, in.BaseURL, apiKey, in.Model, s.timeout); msg != "" {
			return nil, apperr.InvalidArg(msg)
		}
	}

	row.Enabled = in.Enabled
	row.Provider = in.Provider
	row.BaseURL = in.BaseURL
	row.APIKey = apiKey
	row.Model = in.Model
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "settings: save", err)
	}

	s.apply(&row)
	return s.Get(ctx)
}

// Test builds a throwaway client from the input and runs a tiny
// summarize call to verify credentials/endpoint, without persisting.
func (s *Service) Test(ctx context.Context, in SaveInput) error {
	apiKey := in.APIKey
	if in.KeepKey && apiKey == "" && s.db != nil {
		var row model.LLMSetting
		if s.db.WithContext(ctx).First(&row, settingRowID).Error == nil {
			apiKey = row.APIKey
		}
	}
	client, msg := ai.BuildClient(in.Provider, in.BaseURL, apiKey, in.Model, s.timeout)
	if msg != "" {
		return apperr.InvalidArg(msg)
	}
	tctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if _, err := client.Summarize(tctx, "你是连接测试助手。", "回复 OK 两个字即可。"); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "settings: test connection failed", err)
	}
	return nil
}

// LoadOnStart applies the persisted row to the Provider at startup.
// When no row exists the caller's config-derived client stays active.
func (s *Service) LoadOnStart(ctx context.Context) {
	if s.db == nil {
		return
	}
	var row model.LLMSetting
	if s.db.WithContext(ctx).First(&row, settingRowID).Error != nil {
		return // no saved setting; keep config default
	}
	s.apply(&row)
}

// apply rebuilds and swaps the live client per a setting row.
func (s *Service) apply(row *model.LLMSetting) {
	if !row.Enabled {
		s.provider.Set(nil)
		return
	}
	client, msg := ai.BuildClient(row.Provider, row.BaseURL, row.APIKey, row.Model, s.timeout)
	if msg != "" || client == nil {
		s.provider.Set(nil)
		return
	}
	s.provider.Set(client)
}

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

// =============================================================================
// Multi-provider fallback chain (§11 multi-LLM). The llm_providers table
// is the canonical runtime config: an ordered list tried top-to-bottom,
// so when one provider is down the assistant keeps working on the next.
// The legacy single-row llm_settings is migrated in on first load.
// =============================================================================

// ProviderInput is one editable chain entry. ID round-trips so KeepKey
// can preserve a stored secret across edits/reorders. APIKey empty +
// KeepKey=true keeps the existing row's key.
type ProviderInput struct {
	ID       uint64
	Enabled  bool
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
	KeepKey  bool
}

// ProviderView is the masked API shape for one chain entry.
type ProviderView struct {
	ID           uint64 `json:"id"`
	Priority     int    `json:"priority"`
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider"`
	BaseURL      string `json:"base_url"`
	APIKeySet    bool   `json:"api_key_set"`
	APIKeyMasked string `json:"api_key_masked"`
	Model        string `json:"model"`
}

// ChainView is the full settings response: the ordered provider list +
// the live active backend name (which is the chain's Name()).
type ChainView struct {
	Providers []ProviderView `json:"providers"`
	Active    string         `json:"active"`
}

// GetChain returns the ordered provider list (masked). When the
// llm_providers table is empty but a legacy llm_settings row exists, it
// is surfaced as a single derived entry so the UI shows the migrated
// config.
func (s *Service) GetChain(ctx context.Context) (*ChainView, error) {
	if s.db == nil {
		return nil, apperr.Unavailable("settings: db not configured")
	}
	rows, err := s.loadProviderRows(ctx)
	if err != nil {
		return nil, err
	}
	out := &ChainView{Active: s.provider.Name(), Providers: make([]ProviderView, 0, len(rows))}
	for _, r := range rows {
		out.Providers = append(out.Providers, ProviderView{
			ID:           r.ID,
			Priority:     r.Priority,
			Enabled:      r.Enabled,
			Provider:     r.Provider,
			BaseURL:      r.BaseURL,
			APIKeySet:    r.APIKey != "",
			APIKeyMasked: mask(r.APIKey),
			Model:        r.Model,
		})
	}
	return out, nil
}

// loadProviderRows reads the ordered chain, migrating the legacy single
// row in when the table is empty (best-effort, non-fatal).
func (s *Service) loadProviderRows(ctx context.Context) ([]model.LLMProvider, error) {
	var rows []model.LLMProvider
	if err := s.db.WithContext(ctx).Order("priority").Find(&rows).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "settings: load providers", err)
	}
	if len(rows) > 0 {
		return rows, nil
	}
	// Migrate legacy single row → one chain entry (priority 0).
	var legacy model.LLMSetting
	if s.db.WithContext(ctx).First(&legacy, settingRowID).Error == nil {
		mig := model.LLMProvider{
			Priority: 0, Enabled: legacy.Enabled, Provider: legacy.Provider,
			BaseURL: legacy.BaseURL, APIKey: legacy.APIKey, Model: legacy.Model,
		}
		if err := s.db.WithContext(ctx).Create(&mig).Error; err == nil {
			return []model.LLMProvider{mig}, nil
		}
	}
	return nil, nil
}

// SaveChain replaces the entire provider list with the given ordered
// inputs, validates each, persists them (priority = slice index),
// rebuilds the fallback chain, and hot-swaps the live Provider.
func (s *Service) SaveChain(ctx context.Context, inputs []ProviderInput) (*ChainView, error) {
	if s.db == nil {
		return nil, apperr.Unavailable("settings: db not configured")
	}
	// Preserve secrets for KeepKey entries by id.
	existing := map[uint64]string{}
	var prior []model.LLMProvider
	if s.db.WithContext(ctx).Find(&prior).Error == nil {
		for _, r := range prior {
			existing[r.ID] = r.APIKey
		}
	}

	rows := make([]model.LLMProvider, 0, len(inputs))
	for i, in := range inputs {
		apiKey := in.APIKey
		if in.KeepKey && apiKey == "" {
			apiKey = existing[in.ID]
		}
		// Validate enabled entries by building a client (also fills +
		// checks preset defaults).
		if in.Enabled {
			if _, msg := ai.BuildClient(in.Provider, in.BaseURL, apiKey, in.Model, s.timeout); msg != "" {
				return nil, apperr.InvalidArg(fmt.Sprintf("第 %d 个服务商: %s", i+1, msg))
			}
		}
		rows = append(rows, model.LLMProvider{
			Priority: i, Enabled: in.Enabled, Provider: in.Provider,
			BaseURL: in.BaseURL, APIKey: apiKey, Model: in.Model,
		})
	}

	// Replace-all in a transaction so a partial write can't leave a
	// half-updated chain.
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&model.LLMProvider{}).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			return tx.Create(&rows).Error
		}
		return nil
	}); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "settings: save chain", err)
	}

	s.applyChain(rows)
	return s.GetChain(ctx)
}

// TestProvider builds a throwaway client for one entry and runs a tiny
// summarize to verify creds/endpoint, without persisting. KeepKey pulls
// the stored secret by id.
func (s *Service) TestProvider(ctx context.Context, in ProviderInput) error {
	apiKey := in.APIKey
	if in.KeepKey && apiKey == "" && s.db != nil && in.ID > 0 {
		var row model.LLMProvider
		if s.db.WithContext(ctx).First(&row, in.ID).Error == nil {
			apiKey = row.APIKey
		}
	}
	client, msg := ai.BuildClient(in.Provider, in.BaseURL, apiKey, in.Model, s.timeout)
	if msg != "" {
		return apperr.InvalidArg(msg)
	}
	tctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if _, err := client.Summarize(tctx, "你是连接测试助手。", "回复 OK 两个字即可。"); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "settings: test connection failed", err)
	}
	return nil
}

// applyChain builds a fallback chain from the enabled rows (in priority
// order) and hot-swaps the live Provider. No enabled rows → disabled.
func (s *Service) applyChain(rows []model.LLMProvider) {
	clients := make([]ai.Client, 0, len(rows))
	for _, r := range rows {
		if !r.Enabled {
			continue
		}
		c, msg := ai.BuildClient(r.Provider, r.BaseURL, r.APIKey, r.Model, s.timeout)
		if msg != "" || c == nil {
			continue // skip a misconfigured entry rather than fail the chain
		}
		clients = append(clients, c)
	}
	s.provider.Set(ai.NewChainClient(clients)) // nil when empty → disabled
}

// LoadChainOnStart applies the persisted chain at startup. Prefers the
// llm_providers table; migrates the legacy single row in when empty.
// When neither exists, the config-derived client stays active.
func (s *Service) LoadChainOnStart(ctx context.Context) {
	if s.db == nil {
		return
	}
	rows, err := s.loadProviderRows(ctx)
	if err != nil || len(rows) == 0 {
		return
	}
	s.applyChain(rows)
}
