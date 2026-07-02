// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for StrategyService (Create / Get / List / Update /
// Delete / Clone / Export / Import). Uses an in-memory SQLite
// DB so each test gets a clean schema.
package orchestrator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/model"
)

// newTestDB returns a fresh in-memory-style SQLite *gorm.DB
// with all model tables migrated. Each test gets its own
// connection and its own on-disk file, fully isolated.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "orchestrator_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	return db
}

const sampleDAG = `{
  "nodes": [
    {"id": "ds1", "type": "data_source", "data": {"subtype": "quote", "params": {"stock_codes": ["600000.SH"]}}},
    {"id": "f1", "type": "filter", "data": {"params": {"field": "close", "op": ">", "value": 10}}}
  ],
  "edges": [
    {"id": "e1", "source": "ds1", "target": "f1", "sourceHandle": "out", "targetHandle": "in"}
  ]
}`

func newSvc(t *testing.T) (*StrategyService, *gorm.DB) {
	t.Helper()
	db := newTestDB(t)
	return NewStrategyService(db, nil), db
}

// =============================================================================
// Create
// =============================================================================

func TestStrategyService_Create_HappyPath(t *testing.T) {
	svc, db := newSvc(t)
	strat, ver, err := svc.Create(context.Background(), CreateStrategyInput{
		Code:        "morning_breakout",
		Name:        "Morning Volume Breakout",
		Description: "Long upper shadow + volume",
		Tags:        "momentum,morning",
		DAGJson:     sampleDAG,
	})
	require.NoError(t, err)
	require.NotNil(t, strat)
	require.NotNil(t, ver)
	require.Equal(t, "morning_breakout", strat.Code)
	require.Equal(t, 1, strat.CurrentVersion)
	require.Equal(t, model.StrategyStatusDraft, strat.Status)
	require.Equal(t, 1, ver.Version)
	require.Equal(t, "initial", ver.ChangeNote)
	require.NotZero(t, ver.SnapshotTakenAt)
	// Row exists in DB.
	var count int64
	db.Model(&model.Strategy{}).Where("id = ?", strat.ID).Count(&count)
	require.Equal(t, int64(1), count)
	db.Model(&model.StrategyVersion{}).Where("strategy_id = ?", strat.ID).Count(&count)
	require.Equal(t, int64(1), count)
}

func TestStrategyService_Create_GeneratesCode(t *testing.T) {
	svc, _ := newSvc(t)
	strat, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Name:    "Uncoded",
		DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(strat.Code, "strat_"), "code should have auto-generated prefix, got %q", strat.Code)
}

func TestStrategyService_Create_RejectsMissingName(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Create(context.Background(), CreateStrategyInput{
		DAGJson: sampleDAG,
	})
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok, "expected BizError, got %T", err)
	require.Equal(t, apperr.CodeInvalidArg, be.Code)
}

func TestStrategyService_Create_RejectsMissingDAG(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Name: "no dag",
	})
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeInvalidArg, be.Code)
}

func TestStrategyService_Create_RejectsInvalidDAG(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Name:    "bad dag",
		DAGJson: `{"nodes": [], "edges": []}`,
	})
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeInvalidArg, be.Code)
	require.Contains(t, be.Message, "invalid dag_json")
}

func TestStrategyService_Create_NoDB(t *testing.T) {
	svc := NewStrategyService(nil, nil)
	_, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Name:    "x",
		DAGJson: sampleDAG,
	})
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeUnavailable, be.Code)
}

// =============================================================================
// Get
// =============================================================================

func TestStrategyService_Get_Found(t *testing.T) {
	svc, _ := newSvc(t)
	strat, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code:    "s1",
		Name:    "S1",
		DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	d, err := svc.Get(context.Background(), strat.ID)
	require.NoError(t, err)
	require.Equal(t, "S1", d.Name)
	require.Equal(t, sampleDAG, d.CurrentVersionDAG)
}

func TestStrategyService_Get_NotFound(t *testing.T) {
	svc, _ := newSvc(t)
	_, err := svc.Get(context.Background(), 9999)
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeNotFound, be.Code)
}

// =============================================================================
// List
// =============================================================================

func TestStrategyService_List_FilterByStatus(t *testing.T) {
	svc, db := newSvc(t)
	s1, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "a", Name: "A", DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	_, _, err = svc.Create(context.Background(), CreateStrategyInput{
		Code: "b", Name: "B", DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	// Flip s1 to active.
	require.NoError(t, db.Model(s1).Update("status", model.StrategyStatusActive).Error)

	rows, total, err := svc.List(context.Background(), StrategyListFilter{Status: model.StrategyStatusActive})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	require.Equal(t, s1.ID, rows[0].ID)
}

func TestStrategyService_List_FilterByCode(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Create(context.Background(), CreateStrategyInput{Code: "morning_a", Name: "A", DAGJson: sampleDAG})
	require.NoError(t, err)
	_, _, err = svc.Create(context.Background(), CreateStrategyInput{Code: "afternoon_b", Name: "B", DAGJson: sampleDAG})
	require.NoError(t, err)

	rows, _, err := svc.List(context.Background(), StrategyListFilter{CodeLike: "morning"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "morning_a", rows[0].Code)
}

func TestStrategyService_List_FilterByTags(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "x1", Name: "X1", Tags: "momentum,mid", DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	_, _, err = svc.Create(context.Background(), CreateStrategyInput{
		Code: "x2", Name: "X2", Tags: "value,low", DAGJson: sampleDAG,
	})
	require.NoError(t, err)

	rows, _, err := svc.List(context.Background(), StrategyListFilter{TagsContains: "momentum"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "x1", rows[0].Code)
}

func TestStrategyService_List_FilterEscapesLike(t *testing.T) {
	// % and _ are LIKE metacharacters — they must be
	// escaped so a user-supplied substring cannot broaden
	// the match.
	svc, _ := newSvc(t)
	_, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "abc", Name: "ABC", Tags: "x", DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	// Querying for "%" should NOT match everything.
	rows, _, err := svc.List(context.Background(), StrategyListFilter{TagsContains: "%"})
	require.NoError(t, err)
	require.Len(t, rows, 0)
}

func TestStrategyService_List_Pagination(t *testing.T) {
	svc, _ := newSvc(t)
	for i := 0; i < 5; i++ {
		_, _, err := svc.Create(context.Background(), CreateStrategyInput{
			Code:    "p" + string(rune('a'+i)),
			Name:    "P",
			DAGJson: sampleDAG,
		})
		require.NoError(t, err)
	}
	rows, total, err := svc.List(context.Background(), StrategyListFilter{Page: 1, PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, rows, 2)
}

// =============================================================================
// Update
// =============================================================================

func TestStrategyService_Update_MetadataOnly_NoNewVersion(t *testing.T) {
	svc, _ := newSvc(t)
	s, v1, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "u1", Name: "U1", DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	_, v2, err := svc.Update(context.Background(), s.ID, UpdateStrategyInput{
		Name:        "U1 renamed",
		Description: "new desc",
		DAGJson:     sampleDAG, // identical → no new version
	})
	require.NoError(t, err)
	// Update returns a synthetic version pointer — its
	// version number should match current, but no new row.
	require.Equal(t, 1, v2.Version)
	require.Equal(t, 1, v1.Version)
}

func TestStrategyService_Update_DAGChanged_CreatesNewVersion(t *testing.T) {
	svc, db := newSvc(t)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "u2", Name: "U2", DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	newDAG := strings.Replace(sampleDAG, `"close"`, `"open"`, 1)
	_, v, err := svc.Update(context.Background(), s.ID, UpdateStrategyInput{
		Name:       "U2",
		DAGJson:    newDAG,
		ChangeNote: "swap close->open",
	})
	require.NoError(t, err)
	require.Equal(t, 2, v.Version)
	require.Equal(t, "swap close->open", v.ChangeNote)
	// Persisted version is 2.
	var reloaded model.Strategy
	require.NoError(t, db.First(&reloaded, s.ID).Error)
	require.Equal(t, 2, reloaded.CurrentVersion)
	// Two version rows exist.
	var count int64
	db.Model(&model.StrategyVersion{}).Where("strategy_id = ?", s.ID).Count(&count)
	require.Equal(t, int64(2), count)
}

func TestStrategyService_Update_NotFound(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Update(context.Background(), 9999, UpdateStrategyInput{
		Name:    "x",
		DAGJson: sampleDAG,
	})
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeNotFound, be.Code)
}

// fakeReloader records Add/Remove calls for scheduleSync tests.
type fakeReloader struct {
	added   map[uint64]string
	removed map[uint64]bool
}

func (f *fakeReloader) Add(id uint64, expr string) error {
	if f.added == nil {
		f.added = map[uint64]string{}
	}
	f.added[id] = expr
	return nil
}
func (f *fakeReloader) Remove(id uint64) {
	if f.removed == nil {
		f.removed = map[uint64]bool{}
	}
	f.removed[id] = true
}

func TestStrategyService_Update_StatusAndCron(t *testing.T) {
	svc, db := newSvc(t)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "sc1", Name: "SC1", DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	updated, _, err := svc.Update(context.Background(), s.ID, UpdateStrategyInput{
		Name: "SC1", DAGJson: sampleDAG,
		Status:         model.StrategyStatusActive,
		CronExpression: "30 15 * * 1-5",
	})
	require.NoError(t, err)
	require.Equal(t, model.StrategyStatusActive, updated.Status)
	require.Equal(t, "30 15 * * 1-5", updated.CronExpression)
	// Persisted to DB, not just in-memory.
	var reloaded model.Strategy
	require.NoError(t, db.First(&reloaded, s.ID).Error)
	require.Equal(t, model.StrategyStatusActive, reloaded.Status)
	require.Equal(t, "30 15 * * 1-5", reloaded.CronExpression)
}

func TestStrategyService_Update_RejectsInvalidStatus(t *testing.T) {
	svc, _ := newSvc(t)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{Code: "is", Name: "IS", DAGJson: sampleDAG})
	require.NoError(t, err)
	_, _, err = svc.Update(context.Background(), s.ID, UpdateStrategyInput{
		Name: "IS", DAGJson: sampleDAG, Status: "garbage",
	})
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeInvalidArg, be.Code)
}

func TestStrategyService_Update_RejectsInvalidCron(t *testing.T) {
	svc, _ := newSvc(t)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{Code: "ic", Name: "IC", DAGJson: sampleDAG})
	require.NoError(t, err)
	_, _, err = svc.Update(context.Background(), s.ID, UpdateStrategyInput{
		Name: "IC", DAGJson: sampleDAG, Status: model.StrategyStatusActive, CronExpression: "not a cron",
	})
	require.Error(t, err)
}

func TestStrategyService_Update_ScheduleReload(t *testing.T) {
	svc, _ := newSvc(t)
	fr := &fakeReloader{}
	svc.SetScheduleReloader(fr)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{Code: "sr", Name: "SR", DAGJson: sampleDAG})
	require.NoError(t, err)

	// active + cron -> reloader.Add.
	_, _, err = svc.Update(context.Background(), s.ID, UpdateStrategyInput{
		Name: "SR", DAGJson: sampleDAG, Status: model.StrategyStatusActive, CronExpression: "0 16 * * 1-5",
	})
	require.NoError(t, err)
	require.Equal(t, "0 16 * * 1-5", fr.added[s.ID])
	require.False(t, fr.removed[s.ID])

	// disable -> reloader.Remove.
	_, _, err = svc.Update(context.Background(), s.ID, UpdateStrategyInput{
		Name: "SR", DAGJson: sampleDAG, Status: model.StrategyStatusDisabled,
	})
	require.NoError(t, err)
	require.True(t, fr.removed[s.ID])
}

// =============================================================================
// Delete (soft-delete)
// =============================================================================

func TestStrategyService_Delete(t *testing.T) {
	svc, db := newSvc(t)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "d1", Name: "D1", DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	require.NoError(t, svc.Delete(context.Background(), s.ID))

	// Direct lookup with Unscoped shows the row still exists
	// but its status is disabled and DeletedAt is set.
	var reloaded model.Strategy
	require.NoError(t, db.Unscoped().First(&reloaded, s.ID).Error)
	require.Equal(t, model.StrategyStatusDisabled, reloaded.Status)
	require.NotNil(t, reloaded.DeletedAt)
	require.True(t, reloaded.DeletedAt.Valid)
}

func TestStrategyService_Delete_NotFound(t *testing.T) {
	svc, _ := newSvc(t)
	err := svc.Delete(context.Background(), 9999)
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeNotFound, be.Code)
}

// =============================================================================
// Clone
// =============================================================================

func TestStrategyService_Clone(t *testing.T) {
	svc, db := newSvc(t)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code: "src", Name: "Source", DAGJson: sampleDAG,
	})
	require.NoError(t, err)
	clone, ver, err := svc.Clone(context.Background(), s.ID, "cloned_code")
	require.NoError(t, err)
	require.Equal(t, "cloned_code", clone.Code)
	require.Equal(t, 1, clone.CurrentVersion)
	require.Equal(t, "Source (clone)", clone.Name)
	require.Equal(t, 1, ver.Version)
	require.NotEqual(t, s.ID, clone.ID)

	// The clone's DAG should have remapped node IDs.
	originalDAG, _ := ParseDAG(s.DAGJson)
	cloneDAG, _ := ParseDAG(clone.DAGJson)
	require.Equal(t, len(originalDAG.Nodes), len(cloneDAG.Nodes))
	// Old IDs must not appear in the clone.
	for _, n := range originalDAG.Nodes {
		require.NotContains(t, cloneDAG.Nodes, n.ID, "original node id %q should be remapped", n.ID)
	}
	// The clone row's DAGJson reflects the remapping.
	require.NotEqual(t, s.DAGJson, clone.DAGJson)
	// Both rows exist.
	var count int64
	db.Model(&model.Strategy{}).Unscoped().Count(&count)
	require.Equal(t, int64(2), count)
}

func TestStrategyService_Clone_NotFound(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Clone(context.Background(), 9999, "x")
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeNotFound, be.Code)
}

// =============================================================================
// Export / Import
// =============================================================================

func TestStrategyService_Export_Import_RoundTrip(t *testing.T) {
	svc, _ := newSvc(t)
	s, _, err := svc.Create(context.Background(), CreateStrategyInput{
		Code:        "exp",
		Name:        "Exportable",
		Description: "round trip",
		Tags:        "tag1,tag2",
		DAGJson:     sampleDAG,
	})
	require.NoError(t, err)
	out, err := svc.Export(context.Background(), s.ID)
	require.NoError(t, err)
	require.Contains(t, out, `"code": "exp"`)
	require.Contains(t, out, `"name": "Exportable"`)

	// Re-import into a fresh service (simulates moving the
	// strategy to another environment).
	svc2, _ := newSvc(t)
	imp, _, err := svc2.Import(context.Background(), out)
	require.NoError(t, err)
	require.Equal(t, "Exportable", imp.Name)
	require.Equal(t, "tag1,tag2", imp.Tags)
}

func TestStrategyService_Import_Empty(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Import(context.Background(), "")
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeInvalidArg, be.Code)
}

func TestStrategyService_Import_BadJSON(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Import(context.Background(), `{not json`)
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeInvalidArg, be.Code)
}

func TestStrategyService_Import_MissingName(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Import(context.Background(), `{"code":"x","dag_json":"`+sampleDAG+`"}`)
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeInvalidArg, be.Code)
}

func TestStrategyService_Import_BadDAG(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Import(context.Background(),
		`{"code":"x","name":"n","dag_json":"{\"nodes\":[],\"edges\":[]}"}`)
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeInvalidArg, be.Code)
}

func TestStrategyService_Export_NotFound(t *testing.T) {
	svc, _ := newSvc(t)
	_, err := svc.Export(context.Background(), 9999)
	require.Error(t, err)
	be, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeNotFound, be.Code)
}

// =============================================================================
// Helpers
// =============================================================================

func TestGenerateCode_Unique(t *testing.T) {
	// 1000 calls should produce 1000 distinct codes (the
	// unique index is the real conflict guard, but we
	// also want low collision rate in-process).
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		c := generateCode("x")
		if _, dup := seen[c]; dup {
			t.Fatalf("duplicate code: %s", c)
		}
		seen[c] = struct{}{}
	}
}

func TestEscapeLike(t *testing.T) {
	cases := map[string]string{
		"plain":   "plain",
		"%":       "\\%",
		"_":       "\\_",
		"a%b_c":   "a\\%b\\_c",
		`back\sl`: `back\\sl`,
	}
	for in, want := range cases {
		if got := escapeLike(in); got != want {
			t.Errorf("escapeLike(%q): got %q want %q", in, got, want)
		}
	}
}

func TestStrategyIDsFromMap(t *testing.T) {
	got := StrategyIDsFromMap(map[string]string{
		"a": "1",
		"c": "3",
		"b": "2",
	})
	want := []string{"a", "b", "c"}
	require.Equal(t, want, got)
}
