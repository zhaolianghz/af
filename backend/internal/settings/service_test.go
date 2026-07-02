// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package settings

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/ai"
	"github.com/skyzhao/af/internal/model"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "s.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	return db
}

func TestSettings_SaveMockSwapsProvider(t *testing.T) {
	db := newDB(t)
	prov := ai.NewProvider(nil) // disabled initially
	svc := NewService(db, prov, 0)
	require.Equal(t, "disabled", prov.Name())

	v, err := svc.Save(context.Background(), SaveInput{Enabled: true, Provider: "mock"})
	require.NoError(t, err)
	require.True(t, v.Enabled)
	require.Equal(t, "mock", v.Provider)
	require.Equal(t, "mock", prov.Name()) // hot-swapped live
}

func TestSettings_OpenAIRequiresFields(t *testing.T) {
	svc := NewService(newDB(t), ai.NewProvider(nil), 0)
	_, err := svc.Save(context.Background(), SaveInput{Enabled: true, Provider: "openai", Model: ""})
	require.Error(t, err) // missing base_url/api_key/model
}

func TestSettings_GetMasksKey(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, ai.NewProvider(nil), 0)
	_, err := svc.Save(context.Background(), SaveInput{
		Enabled: true, Provider: "openai",
		BaseURL: "https://api.deepseek.com/v1", APIKey: "sk-abcd1234efgh5678", Model: "deepseek-chat",
	})
	require.NoError(t, err)
	v, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.True(t, v.APIKeySet)
	require.NotContains(t, v.APIKeyMasked, "1234efgh") // middle hidden
	require.Contains(t, v.APIKeyMasked, "sk-a")
}

func TestSettings_KeepKeyPreservesSecret(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, ai.NewProvider(nil), 0)
	_, err := svc.Save(context.Background(), SaveInput{
		Enabled: true, Provider: "openai",
		BaseURL: "https://api.deepseek.com/v1", APIKey: "sk-secret-original", Model: "deepseek-chat",
	})
	require.NoError(t, err)
	// Save again with empty key + KeepKey → secret retained.
	_, err = svc.Save(context.Background(), SaveInput{
		Enabled: true, Provider: "openai",
		BaseURL: "https://api.deepseek.com/v1", APIKey: "", Model: "deepseek-reasoner", KeepKey: true,
	})
	require.NoError(t, err)
	var row model.LLMSetting
	require.NoError(t, db.First(&row, 1).Error)
	require.Equal(t, "sk-secret-original", row.APIKey)
	require.Equal(t, "deepseek-reasoner", row.Model)
}

func TestSettings_LoadOnStartAppliesSavedRow(t *testing.T) {
	db := newDB(t)
	// Persist a mock setting via one service.
	_, err := NewService(db, ai.NewProvider(nil), 0).
		Save(context.Background(), SaveInput{Enabled: true, Provider: "mock"})
	require.NoError(t, err)
	// A fresh provider/service loads it on start.
	prov := ai.NewProvider(nil)
	NewService(db, prov, 0).LoadOnStart(context.Background())
	require.Equal(t, "mock", prov.Name())
}

// =============================================================================
// Multi-provider chain
// =============================================================================

func TestChain_SaveBuildsOrderedChain(t *testing.T) {
	db := newDB(t)
	prov := ai.NewProvider(nil)
	svc := NewService(db, prov, 0)

	// Two mock backends → chain of 2 (mock needs no creds).
	v, err := svc.SaveChain(context.Background(), []ProviderInput{
		{Enabled: true, Provider: "mock"},
		{Enabled: true, Provider: "mock"},
	})
	require.NoError(t, err)
	require.Len(t, v.Providers, 2)
	require.Equal(t, 0, v.Providers[0].Priority)
	require.Equal(t, 1, v.Providers[1].Priority)
	// Live provider is now a 2-member chain.
	require.Contains(t, prov.Name(), "chain[")
}

func TestChain_SingleEnabledIsNotWrapped(t *testing.T) {
	db := newDB(t)
	prov := ai.NewProvider(nil)
	svc := NewService(db, prov, 0)
	_, err := svc.SaveChain(context.Background(), []ProviderInput{{Enabled: true, Provider: "mock"}})
	require.NoError(t, err)
	require.Equal(t, "mock", prov.Name(), "single backend not wrapped in a chain")
}

func TestChain_DisabledEntrySkipped(t *testing.T) {
	db := newDB(t)
	prov := ai.NewProvider(nil)
	svc := NewService(db, prov, 0)
	// One enabled mock + one disabled (openai, would need creds but is
	// skipped because disabled).
	_, err := svc.SaveChain(context.Background(), []ProviderInput{
		{Enabled: true, Provider: "mock"},
		{Enabled: false, Provider: "openai"},
	})
	require.NoError(t, err)
	require.Equal(t, "mock", prov.Name(), "disabled entry not in the live chain")
}

func TestChain_PresetFillsBaseURLAndModel(t *testing.T) {
	// A glm entry with only a key (no base_url/model) must validate via
	// the preset defaults.
	db := newDB(t)
	svc := NewService(db, ai.NewProvider(nil), 0)
	_, err := svc.SaveChain(context.Background(), []ProviderInput{
		{Enabled: true, Provider: "glm", APIKey: "test-key"},
	})
	require.NoError(t, err, "glm preset should fill base_url + model")
}

func TestChain_EnabledOpenAICompatNeedsKey(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, ai.NewProvider(nil), 0)
	// minimax enabled with no api_key → invalid (preset fills url+model
	// but key is still required).
	_, err := svc.SaveChain(context.Background(), []ProviderInput{
		{Enabled: true, Provider: "minimax"},
	})
	require.Error(t, err)
}

func TestChain_KeepKeyPreservesSecretAcrossReorder(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, ai.NewProvider(nil), 0)
	// Save one openai-compat with a key.
	v, err := svc.SaveChain(context.Background(), []ProviderInput{
		{Enabled: true, Provider: "deepseek", APIKey: "sk-keepme", Model: "deepseek-chat"},
	})
	require.NoError(t, err)
	id := v.Providers[0].ID

	// Re-save with KeepKey + empty key → secret retained.
	_, err = svc.SaveChain(context.Background(), []ProviderInput{
		{ID: id, Enabled: true, Provider: "deepseek", Model: "deepseek-chat", KeepKey: true},
	})
	require.NoError(t, err)
	var rows []model.LLMProvider
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "sk-keepme", rows[0].APIKey)
}

func TestChain_GetMigratesLegacyRow(t *testing.T) {
	db := newDB(t)
	// Seed a legacy single-row setting, no llm_providers yet.
	require.NoError(t, db.Create(&model.LLMSetting{
		BaseEntity: model.BaseEntity{ID: settingRowID},
		Enabled:    true, Provider: "mock",
	}).Error)
	svc := NewService(db, ai.NewProvider(nil), 0)

	v, err := svc.GetChain(context.Background())
	require.NoError(t, err)
	require.Len(t, v.Providers, 1, "legacy row migrated into one chain entry")
	require.Equal(t, "mock", v.Providers[0].Provider)
	// Migration persisted a row in llm_providers.
	var count int64
	require.NoError(t, db.Model(&model.LLMProvider{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestChain_LoadChainOnStart(t *testing.T) {
	db := newDB(t)
	_, err := NewService(db, ai.NewProvider(nil), 0).SaveChain(context.Background(), []ProviderInput{
		{Enabled: true, Provider: "mock"},
		{Enabled: true, Provider: "mock"},
	})
	require.NoError(t, err)
	// Fresh provider loads the chain on start.
	prov := ai.NewProvider(nil)
	NewService(db, prov, 0).LoadChainOnStart(context.Background())
	require.Contains(t, prov.Name(), "chain[")
}

func TestChain_GetMasksKeys(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, ai.NewProvider(nil), 0)
	_, err := svc.SaveChain(context.Background(), []ProviderInput{
		{Enabled: true, Provider: "deepseek", APIKey: "sk-abcd1234efgh5678", Model: "deepseek-chat"},
	})
	require.NoError(t, err)
	v, err := svc.GetChain(context.Background())
	require.NoError(t, err)
	require.True(t, v.Providers[0].APIKeySet)
	require.NotContains(t, v.Providers[0].APIKeyMasked, "1234efgh")
}

func TestChain_EmptyDisablesProvider(t *testing.T) {
	db := newDB(t)
	prov := ai.NewProvider(ai.NewMockClient()) // start enabled
	svc := NewService(db, prov, 0)
	require.Equal(t, "mock", prov.Name())
	// Save an empty chain → provider disabled.
	_, err := svc.SaveChain(context.Background(), []ProviderInput{})
	require.NoError(t, err)
	require.Equal(t, "disabled", prov.Name())
}
