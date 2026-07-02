// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package ai

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

// A minimal valid 2-node DAG: data_source(kline) -> indicator(ma,5).
const baseDAG = `{"nodes":[` +
	`{"id":"ds","type":"data_source","position":{"x":0,"y":0},"data":{"subtype":"kline","params":{"subtype":"kline","stock_codes":["600519.SH"],"period":"1d","days":60}}},` +
	`{"id":"ind","type":"indicator","position":{"x":240,"y":0},"data":{"subtype":"ma","params":{"subtype":"ma","period":5}}}` +
	`],"edges":[{"id":"e1","source":"ds","target":"ind"}]}`

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ai.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	return db
}

// fakeStrat is an in-memory strategyPort.
type fakeStrat struct {
	dag        string
	updatedDAG string
	version    int
}

func (f *fakeStrat) Get(_ context.Context, _ uint64) (*orchestrator.StrategyDetail, error) {
	return &orchestrator.StrategyDetail{CurrentVersionDAG: f.dag}, nil
}

func (f *fakeStrat) Update(_ context.Context, _ uint64, in orchestrator.UpdateStrategyInput) (*model.Strategy, *model.StrategyVersion, error) {
	f.updatedDAG = in.DAGJson
	f.version++
	return &model.Strategy{}, &model.StrategyVersion{Version: f.version}, nil
}

func TestAI_PreviewChangesMAPeriod(t *testing.T) {
	db := newDB(t)
	fs := &fakeStrat{dag: baseDAG}
	svc := NewService(db, NewMockClient(), fs)

	res, err := svc.Preview(context.Background(), 1, "把 ma 周期改成 10")
	require.NoError(t, err)
	require.True(t, res.Changed, "should detect a change")
	require.NotEmpty(t, res.Changes)
	require.Contains(t, res.ProposedDAG, `"period":10`)
	require.NotZero(t, res.AuditID)

	// Preview must NOT mutate the strategy.
	require.Empty(t, fs.updatedDAG)

	// Audit row written with decision=preview.
	var audits []model.AIAudit
	require.NoError(t, db.Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, model.AIDecisionPreview, audits[0].Decision)
}

func TestAI_PreviewNoChangeOnUnknownInstruction(t *testing.T) {
	svc := NewService(newDB(t), NewMockClient(), &fakeStrat{dag: baseDAG})
	res, err := svc.Preview(context.Background(), 1, "给我泡杯咖啡")
	require.NoError(t, err)
	require.False(t, res.Changed)
	require.Empty(t, res.Changes)
}

func TestAI_ApplyCommitsAndAudits(t *testing.T) {
	db := newDB(t)
	fs := &fakeStrat{dag: baseDAG}
	svc := NewService(db, NewMockClient(), fs)

	prev, err := svc.Preview(context.Background(), 1, "ma 周期 20")
	require.NoError(t, err)

	res, err := svc.Apply(context.Background(), 1, prev.ProposedDAG, "ma 周期 20")
	require.NoError(t, err)
	require.Equal(t, 1, res.Version)
	require.Contains(t, fs.updatedDAG, `"period":20`)

	var audits []model.AIAudit
	require.NoError(t, db.Order("id").Find(&audits).Error)
	require.Len(t, audits, 2) // preview + applied
	require.Equal(t, model.AIDecisionApplied, audits[1].Decision)
}

func TestAI_ApplyRejectsInvalidDAG(t *testing.T) {
	svc := NewService(newDB(t), NewMockClient(), &fakeStrat{dag: baseDAG})
	_, err := svc.Apply(context.Background(), 1, `{"nodes":`, "broken")
	require.Error(t, err) // invalid JSON DAG must be rejected, no mutation
}

func TestAI_PreviewValidatesProposal(t *testing.T) {
	// An LLM that returns garbage must fail preview, not surface an
	// "apply" the user could commit.
	svc := NewService(newDB(t), badLLM{}, &fakeStrat{dag: baseDAG})
	_, err := svc.Preview(context.Background(), 1, "anything")
	require.Error(t, err)
}

type badLLM struct{}

func (badLLM) Name() string { return "bad" }
func (badLLM) ProposeDAG(_ context.Context, _, _ string) (string, error) {
	return `not json at all`, nil
}
func (badLLM) Summarize(_ context.Context, _, _ string) (string, error) { return "", nil }

// errProposeLLM fails the ProposeDAG call itself (network/timeout).
type errProposeLLM struct{}

func (errProposeLLM) Name() string { return "errpropose" }
func (errProposeLLM) ProposeDAG(_ context.Context, _, _ string) (string, error) {
	return "", context.DeadlineExceeded
}
func (errProposeLLM) Summarize(_ context.Context, _, _ string) (string, error) { return "", nil }

func TestAI_PreviewDisabledWhenNilLLM(t *testing.T) {
	// nil Client → assistant disabled → Unavailable, before touching the
	// strategy or LLM.
	svc := NewService(newDB(t), nil, &fakeStrat{dag: baseDAG})
	_, err := svc.Preview(context.Background(), 1, "ma 周期 10")
	require.Error(t, err)
}

func TestAI_PreviewEmptyInstruction(t *testing.T) {
	svc := NewService(newDB(t), NewMockClient(), &fakeStrat{dag: baseDAG})
	_, err := svc.Preview(context.Background(), 1, "")
	require.Error(t, err)
}

func TestAI_PreviewLLMErrorWrapped(t *testing.T) {
	// ProposeDAG failing (not just returning garbage) must surface as an
	// error, not a usable preview.
	svc := NewService(newDB(t), errProposeLLM{}, &fakeStrat{dag: baseDAG})
	_, err := svc.Preview(context.Background(), 1, "anything")
	require.Error(t, err)
}

func TestAI_ApplyEmptyProposedDAG(t *testing.T) {
	svc := NewService(newDB(t), NewMockClient(), &fakeStrat{dag: baseDAG})
	_, err := svc.Apply(context.Background(), 1, "", "instr")
	require.Error(t, err)
}

func TestAI_ApplyPreservesNameDescTags(t *testing.T) {
	// An AI edit that only touches the DAG must carry over the existing
	// name/description/tags (StrategyService.Update treats input as the
	// full strategy and requires name).
	db := newDB(t)
	fs := &fakeStratFull{
		detail: &orchestrator.StrategyDetail{
			CurrentVersionDAG: baseDAG,
		},
	}
	// Stamp identity fields on the detail (Strategy is embedded).
	fs.detail.Name = "我的策略"
	fs.detail.Description = "desc"
	fs.detail.Tags = "a,b"
	svc := NewService(db, NewMockClient(), fs)

	prev, err := svc.Preview(context.Background(), 1, "ma 周期 20")
	require.NoError(t, err)
	_, err = svc.Apply(context.Background(), 1, prev.ProposedDAG, "ma 周期 20")
	require.NoError(t, err)
	require.Equal(t, "我的策略", fs.gotUpdate.Name)
	require.Equal(t, "desc", fs.gotUpdate.Description)
	require.Equal(t, "a,b", fs.gotUpdate.Tags)
	require.Contains(t, fs.gotUpdate.ChangeNote, "AI:")
}

// fakeStratFull captures the Update input so we can assert carry-over.
type fakeStratFull struct {
	detail    *orchestrator.StrategyDetail
	gotUpdate orchestrator.UpdateStrategyInput
}

func (f *fakeStratFull) Get(_ context.Context, _ uint64) (*orchestrator.StrategyDetail, error) {
	return f.detail, nil
}
func (f *fakeStratFull) Update(_ context.Context, _ uint64, in orchestrator.UpdateStrategyInput) (*model.Strategy, *model.StrategyVersion, error) {
	f.gotUpdate = in
	return &model.Strategy{}, &model.StrategyVersion{Version: 2}, nil
}

func TestAI_DiffDetectsAddAndRemove(t *testing.T) {
	// addNode appends a filter; the diff must report it as 新增.
	withFilter := `{"nodes":[` +
		`{"id":"ds","type":"data_source","position":{"x":0,"y":0},"data":{"subtype":"kline","params":{}}},` +
		`{"id":"ind","type":"indicator","position":{"x":240,"y":0},"data":{"subtype":"ma","params":{"period":5}}},` +
		`{"id":"filt","type":"filter","position":{"x":480,"y":0},"data":{"params":{"field":"close","op":">","value":10}}}` +
		`],"edges":[]}`
	added := diffDAG(baseDAG, withFilter)
	require.NotEmpty(t, added)
	require.Contains(t, joinLines(added), "新增节点 filt")

	removed := diffDAG(withFilter, baseDAG)
	require.Contains(t, joinLines(removed), "删除节点 filt")
}

func joinLines(xs []string) string {
	out := ""
	for _, x := range xs {
		out += x + "\n"
	}
	return out
}
