// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/model"
)

// StrategyService is the persistence-layer entry point for the
// /api/v1/strategies endpoints. It owns Strategy + StrategyVersion
// creation, version-on-update semantics, cloning, and import/export.
// ScheduleReloader is the slice of the cron scheduler StrategyService
// needs to sync a strategy's cron entry after an Update. The executor
// package's *Scheduler satisfies it; the interface lives here to avoid
// an orchestrator→executor import cycle.
type ScheduleReloader interface {
	Add(strategyID uint64, expr string) error
	Remove(strategyID uint64)
}

type StrategyService struct {
	db       *gorm.DB
	logger   *zap.Logger
	parser   cron.Parser      // validates cron_expression on Update
	reloader ScheduleReloader // nil = scheduling disabled (trials/tests)
}

// NewStrategyService wires a service against a GORM connection.
// db is required (pass nil only for the "DB disabled" mode used by
// the trial-run endpoint, in which case every method that touches
// the DB will return apperr.Unavailable).
func NewStrategyService(db *gorm.DB, l *zap.Logger) *StrategyService {
	if l == nil {
		l = zap.NewNop()
	}
	return &StrategyService{
		db:     db,
		logger: l,
		parser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// SetScheduleReloader wires the cron scheduler so Update can sync a
// strategy's cron entry (Add/Remove) after status/cron changes.
// Optional: when not called (nil), Update persists but skips
// scheduling (used by trial-run and tests).
func (s *StrategyService) SetScheduleReloader(r ScheduleReloader) {
	s.reloader = r
}

// requireDB is a guard used at the top of every method that
// touches the database.
func (s *StrategyService) requireDB() error {
	if s.db == nil {
		return apperr.Unavailable("strategy service: database not configured")
	}
	return nil
}

// =============================================================================
// Inputs / filters
// =============================================================================

// CreateStrategyInput captures the fields accepted by Create.
type CreateStrategyInput struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	DAGJson     string `json:"dag_json"`
}

// UpdateStrategyInput captures the fields accepted by Update. Status
// and CronExpression are optional: when empty (zero value) the
// existing field is left untouched.
type UpdateStrategyInput struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Tags           string `json:"tags"`
	DAGJson        string `json:"dag_json"`
	ChangeNote     string `json:"change_note"`
	Status         string `json:"status"`          // "" = unchanged; draft|active|disabled
	CronExpression string `json:"cron_expression"` // "" = unchanged
}

// StrategyListFilter scopes a List call.
type StrategyListFilter struct {
	Status        string
	TagsContains  string
	CodeLike      string
	Page          int
	PageSize      int
}

// StrategyDetail is what Get / List rows return. The DAG is
// promoted to a top-level field for the convenience of the
// frontend, and the version metadata is included.
type StrategyDetail struct {
	model.Strategy
	CurrentVersionDAG  string `json:"current_version_dag"`
	CurrentVersionNote string `json:"current_version_note"`
}

// =============================================================================
// Create
// =============================================================================

// Create persists a new strategy + its first StrategyVersion.
// If Code is empty, one is auto-generated.
func (s *StrategyService) Create(ctx context.Context, in CreateStrategyInput) (*model.Strategy, *model.StrategyVersion, error) {
	if err := s.requireDB(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, nil, apperr.InvalidArg("name is required")
	}
	if in.DAGJson == "" {
		return nil, nil, apperr.InvalidArg("dag_json is required")
	}
	if _, err := ParseDAG(in.DAGJson); err != nil {
		return nil, nil, apperr.InvalidArg(fmt.Sprintf("invalid dag_json: %v", err))
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		code = generateCode("strat")
	}

	now := time.Now().UTC()
	strat := &model.Strategy{
		Code:           code,
		Name:           in.Name,
		Description:    in.Description,
		Status:         model.StrategyStatusDraft,
		Tags:           in.Tags,
		CurrentVersion: 1,
		DAGJson:        in.DAGJson,
	}
	ver := &model.StrategyVersion{
		Version:         1,
		DAGJson:         in.DAGJson,
		ChangeNote:      "initial",
		SnapshotTakenAt: now,
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(strat).Error; err != nil {
			return apperr.Wrap(apperr.CodeConflict, "create strategy", err)
		}
		ver.StrategyID = strat.ID
		if err := tx.Create(ver).Error; err != nil {
			return apperr.Wrap(apperr.CodeInternal, "create strategy version", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return strat, ver, nil
}

// =============================================================================
// Get / List
// =============================================================================

// Get returns a strategy by ID, eager-loading the current
// version's DAG into StrategyDetail.CurrentVersionDAG.
func (s *StrategyService) Get(ctx context.Context, id uint64) (*StrategyDetail, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	var strat model.Strategy
	if err := s.db.WithContext(ctx).First(&strat, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, apperr.NotFound(fmt.Sprintf("strategy %d", id))
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "load strategy", err)
	}
	detail := &StrategyDetail{Strategy: strat, CurrentVersionDAG: strat.DAGJson}
	if strat.CurrentVersion > 0 {
		var ver model.StrategyVersion
		if err := s.db.WithContext(ctx).
			Where("strategy_id = ? AND version = ?", strat.ID, strat.CurrentVersion).
			First(&ver).Error; err == nil {
			detail.CurrentVersionDAG = ver.DAGJson
			detail.CurrentVersionNote = ver.ChangeNote
		}
	}
	return detail, nil
}

// List returns a page of strategies plus the total count.
func (s *StrategyService) List(ctx context.Context, f StrategyListFilter) ([]model.Strategy, int64, error) {
	if err := s.requireDB(); err != nil {
		return nil, 0, err
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}
	q := s.db.WithContext(ctx).Model(&model.Strategy{})
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.TagsContains != "" {
		// Tags is a comma-separated string; we use LIKE to
		// match a tag substring.
		q = q.Where("tags LIKE ?", "%"+escapeLike(f.TagsContains)+"%")
	}
	if f.CodeLike != "" {
		q = q.Where("code LIKE ?", "%"+escapeLike(f.CodeLike)+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "count strategies", err)
	}
	var rows []model.Strategy
	if err := q.Order("id DESC").
		Limit(f.PageSize).
		Offset((f.Page - 1) * f.PageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "list strategies", err)
	}
	return rows, total, nil
}

// =============================================================================
// Update
// =============================================================================

// Update mutates the strategy and, when the DAG changes, creates
// a new StrategyVersion (incrementing version) and points the
// strategy at it. Pure metadata changes (name/description/tags
// only, DAG identical) skip the new version.
func (s *StrategyService) Update(ctx context.Context, id uint64, in UpdateStrategyInput) (*model.Strategy, *model.StrategyVersion, error) {
	if err := s.requireDB(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, nil, apperr.InvalidArg("name is required")
	}
	if in.DAGJson != "" {
		if _, err := ParseDAG(in.DAGJson); err != nil {
			return nil, nil, apperr.InvalidArg(fmt.Sprintf("invalid dag_json: %v", err))
		}
	}
	if in.Status != "" {
		switch in.Status {
		case model.StrategyStatusDraft, model.StrategyStatusActive, model.StrategyStatusDisabled:
		default:
			return nil, nil, apperr.InvalidArg("invalid status: " + in.Status)
		}
	}
	// cronExpr is captured here (not inline) so the save step below can
	// set it without re-trimming. "" means "leave unchanged".
	cronExpr := strings.TrimSpace(in.CronExpression)
	if cronExpr != "" {
		if _, err := s.parser.Parse(cronExpr); err != nil {
			return nil, nil, apperr.InvalidArg(fmt.Sprintf("invalid cron_expression %q: %v", cronExpr, err))
		}
	}

	var (
		updated model.Strategy
		ver     *model.StrategyVersion
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var strat model.Strategy
		if err := tx.First(&strat, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return apperr.NotFound(fmt.Sprintf("strategy %d", id))
			}
			return apperr.Wrap(apperr.CodeInternal, "load strategy", err)
		}
		dagChanged := in.DAGJson != "" && in.DAGJson != strat.DAGJson
		strat.Name = in.Name
		if in.Description != "" {
			strat.Description = in.Description
		}
		strat.Tags = in.Tags
		if in.Status != "" {
			strat.Status = in.Status
		}
		if cronExpr != "" {
			strat.CronExpression = cronExpr
		}
		if dagChanged {
			strat.DAGJson = in.DAGJson
			strat.CurrentVersion = strat.CurrentVersion + 1

			now := time.Now().UTC()
			nv := &model.StrategyVersion{
				StrategyID:      strat.ID,
				Version:         strat.CurrentVersion,
				DAGJson:         in.DAGJson,
				ChangeNote:      in.ChangeNote,
				SnapshotTakenAt: now,
			}
			if err := tx.Create(nv).Error; err != nil {
				return apperr.Wrap(apperr.CodeInternal, "create strategy version", err)
			}
			ver = nv
		}
		if err := tx.Save(&strat).Error; err != nil {
			return apperr.Wrap(apperr.CodeInternal, "save strategy", err)
		}
		updated = strat
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	// Reconcile the cron entry with the (possibly) new status/cron.
	// Best-effort: a scheduling failure is logged, not returned, so a
	// scheduler hiccup never blocks a successful content save.
	s.scheduleSync(updated)
	if ver == nil {
		// No new version was created; return a synthetic
		// pointer to the current version for the API.
		ver = &model.StrategyVersion{Version: updated.CurrentVersion, DAGJson: updated.DAGJson}
	}
	return &updated, ver, nil
}

// scheduleSync reconciles a strategy's cron entry with its current
// status/cron after an Update. Active + non-empty cron → Add (which
// replaces any prior entry for this strategy); anything else → Remove.
// No-op when no reloader is wired.
func (s *StrategyService) scheduleSync(strat model.Strategy) {
	if s.reloader == nil {
		return
	}
	if strat.Status == model.StrategyStatusActive && strat.CronExpression != "" {
		if err := s.reloader.Add(strat.ID, strat.CronExpression); err != nil {
			s.logger.Warn("strategy: schedule Add failed",
				zap.Uint64("strategy_id", strat.ID),
				zap.String("cron", strat.CronExpression),
				zap.Error(err))
		}
		return
	}
	s.reloader.Remove(strat.ID)
}

// =============================================================================
// Delete
// =============================================================================

// Delete soft-deletes the strategy. The status field is also set
// to "disabled" so list endpoints can hide it without relying on
// the deleted_at column.
func (s *StrategyService) Delete(ctx context.Context, id uint64) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var strat model.Strategy
		if err := tx.First(&strat, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return apperr.NotFound(fmt.Sprintf("strategy %d", id))
			}
			return apperr.Wrap(apperr.CodeInternal, "load strategy", err)
		}
		strat.Status = model.StrategyStatusDisabled
		if err := tx.Save(&strat).Error; err != nil {
			return apperr.Wrap(apperr.CodeInternal, "disable strategy", err)
		}
		if err := tx.Delete(&strat).Error; err != nil {
			return apperr.Wrap(apperr.CodeInternal, "soft-delete strategy", err)
		}
		return nil
	})
}

// =============================================================================
// Clone
// =============================================================================

// Clone copies a strategy with a new code. All node IDs in the
// DAG are remapped (suffix added) and the edges are re-pointed
// to the new IDs to keep the graph structure intact.
func (s *StrategyService) Clone(ctx context.Context, id uint64, newCode string) (*model.Strategy, *model.StrategyVersion, error) {
	if err := s.requireDB(); err != nil {
		return nil, nil, err
	}
	var (
		cloned   model.Strategy
		newVer   model.StrategyVersion
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var src model.Strategy
		if err := tx.First(&src, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return apperr.NotFound(fmt.Sprintf("strategy %d", id))
			}
			return apperr.Wrap(apperr.CodeInternal, "load source strategy", err)
		}
		srcDAG, err := ParseDAG(src.DAGJson)
		if err != nil {
			return apperr.Wrap(apperr.CodeInternal, "parse source dag", err)
		}
		cloneDAG, idMap := remapDAG(srcDAG)

		cloned = model.Strategy{
			Code:           strings.TrimSpace(newCode),
			Name:           src.Name + " (clone)",
			Description:    src.Description,
			Status:         model.StrategyStatusDraft,
			Tags:           src.Tags,
			CurrentVersion: 1,
			DAGJson:        src.DAGJson,
		}
		if cloned.Code == "" {
			cloned.Code = generateCode("clone")
		}

		// Use the cloned DAG JSON for persistence so the
		// remapping is permanent.
		cloneJSON, _ := MarshalDAG(cloneDAG)
		cloned.DAGJson = cloneJSON

		if err := tx.Create(&cloned).Error; err != nil {
			return apperr.Wrap(apperr.CodeConflict, "create clone", err)
		}
		newVer = model.StrategyVersion{
			StrategyID:      cloned.ID,
			Version:         1,
			DAGJson:         cloneJSON,
			ChangeNote:      fmt.Sprintf("cloned from strategy %d (id map: %v)", id, idMap),
			SnapshotTakenAt: time.Now().UTC(),
		}
		if err := tx.Create(&newVer).Error; err != nil {
			return apperr.Wrap(apperr.CodeInternal, "create clone version", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &cloned, &newVer, nil
}

// remapDAG returns a deep copy of d with every node ID suffixed
// by a deterministic short tag, and a map from old->new IDs.
func remapDAG(d *DAG) (*DAG, map[string]string) {
	idMap := make(map[string]string, len(d.Nodes))
	suffix := generateCode("n") // short random suffix
	out := &DAG{
		Nodes: make([]NodeDef, 0, len(d.Nodes)),
		Edges: make([]Edge, 0, len(d.Edges)),
	}
	for _, n := range d.Nodes {
		newID := n.ID + "_" + suffix
		idMap[n.ID] = newID
		out.Nodes = append(out.Nodes, NodeDef{
			ID:       newID,
			Type:     n.Type,
			Subtype:  n.Subtype,
			Params:   append(json.RawMessage(nil), n.Params...),
			Position: n.Position,
		})
	}
	for _, e := range d.Edges {
		newSrc, ok1 := idMap[e.Source]
		newTgt, ok2 := idMap[e.Target]
		if !ok1 || !ok2 {
			continue
		}
		out.Edges = append(out.Edges, Edge{
			ID:           e.ID + "_" + suffix,
			Source:       newSrc,
			SourceHandle: e.SourceHandle,
			Target:       newTgt,
			TargetHandle: e.TargetHandle,
		})
	}
	return out, idMap
}

// =============================================================================
// Export / Import
// =============================================================================

// ExportStrategy is the JSON shape produced by Export and
// accepted by Import.
type ExportStrategy struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	Status      string `json:"status"`
	DAGJson     string `json:"dag_json"`
}

// Export serializes a strategy + its current DAG to JSON.
func (s *StrategyService) Export(ctx context.Context, id uint64) (string, error) {
	if err := s.requireDB(); err != nil {
		return "", err
	}
	var strat model.Strategy
	if err := s.db.WithContext(ctx).First(&strat, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", apperr.NotFound(fmt.Sprintf("strategy %d", id))
		}
		return "", apperr.Wrap(apperr.CodeInternal, "load strategy", err)
	}
	payload := ExportStrategy{
		Code:        strat.Code,
		Name:        strat.Name,
		Description: strat.Description,
		Tags:        strat.Tags,
		Status:      strat.Status,
		DAGJson:     strat.DAGJson,
	}
	buf, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "marshal export", err)
	}
	return string(buf), nil
}

// Import parses a JSON export and creates a new strategy from
// it. The Code may be empty; if absent or colliding, a new one
// is generated.
func (s *StrategyService) Import(ctx context.Context, jsonStr string) (*model.Strategy, *model.StrategyVersion, error) {
	if err := s.requireDB(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(jsonStr) == "" {
		return nil, nil, apperr.InvalidArg("empty import json")
	}
	var payload ExportStrategy
	if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
		return nil, nil, apperr.InvalidArg(fmt.Sprintf("invalid import json: %v", err))
	}
	if strings.TrimSpace(payload.Name) == "" {
		return nil, nil, apperr.InvalidArg("name is required")
	}
	if _, err := ParseDAG(payload.DAGJson); err != nil {
		return nil, nil, apperr.InvalidArg(fmt.Sprintf("invalid dag_json: %v", err))
	}
	return s.Create(ctx, CreateStrategyInput{
		Code:        payload.Code,
		Name:        payload.Name,
		Description: payload.Description,
		Tags:        payload.Tags,
		DAGJson:     payload.DAGJson,
	})
}

// =============================================================================
// Helpers
// =============================================================================

// generateCode returns "prefix_" + a short random hex string. It
// is intentionally not cryptographically unique — the unique
// index on Strategy.Code is the real conflict guard.
func generateCode(prefix string) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixNano()%1_000_000, hex.EncodeToString(b[:]))
}

// escapeLike escapes the two LIKE metacharacters ('%' and '_')
// so a user-supplied substring cannot broaden the match.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// StrategyIDsFromMap is exported for tests that want to assert
// on the remapping behavior. (Currently unused elsewhere.)
func StrategyIDsFromMap(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
