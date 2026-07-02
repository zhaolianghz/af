// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package templates ships 5 built-in strategy templates that ship
// in the binary via go:embed.
//
//	Code                        Industry  Description
//	─────────────────────────────────────────────────────────────────
//	morning_volume_breakout     通用       早盘 9:30-10:00 涨幅 2-5% + 量比 > 2
//	afternoon_main_inflow       通用       14:00-14:50 量比 > 1.5 + 涨幅 > 0 (v1 近似)
//	macd_golden_cross           通用       MACD 金叉 + 站上 5 日均线
//	dragon_tiger_institutional  通用       龙虎榜新闻筛选 (v1 近似, 无机构净买入字段)
//	low_valuation_high_dividend 通用       PE < 15 + 股息率 > 3% + ROE > 10%
//
// Each template is a ReactFlow native DAG JSON document. The
// Loader produces a *Template that the handler can use to
// instantiate a real Strategy row.
package templates

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed *.json
var bundledFS embed.FS

// Template is the runtime representation of a strategy template.
// DAGJson is the ReactFlow-native JSON document (the same format
// orchestrator.ParseDAG accepts). AIExplanation is a hard-coded
// 100-200 character description shown in the gallery.
type Template struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Industry      string `json:"industry"`
	BuiltIn       bool   `json:"built_in"`
	AIExplanation string `json:"ai_explanation"`
	DAGJson       string `json:"dag_json"`
}

// ListItem is the wire-format template entry — drops DAGJson for
// the gallery view.
type ListItem struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Industry      string `json:"industry"`
	BuiltIn       bool   `json:"built_in"`
	AIExplanation string `json:"ai_explanation"`
}

// Loader resolves templates by code. The bundled set is loaded
// once at startup; user-saved templates (BuiltIn=false) are
// looked up from the strategy_templates table when db is wired.
type Loader struct {
	logger *zap.Logger
	db     *gorm.DB

	mu       sync.RWMutex
	bundled  map[string]*Template // code -> Template
}

// NewLoader builds a Loader with the bundled set already loaded.
func NewLoader(db *gorm.DB, l *zap.Logger) (*Loader, error) {
	if l == nil {
		l = zap.NewNop()
	}
	ldr := &Loader{logger: l, db: db, bundled: make(map[string]*Template)}
	if err := ldr.loadBundled(); err != nil {
		return nil, err
	}
	return ldr, nil
}

// loadBundled walks the embed.FS and parses every *.json file.
// Each file's basename (sans .json) is the template code.
func (l *Loader) loadBundled() error {
	entries, err := bundledFS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("templates: read embed: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := bundledFS.ReadFile(e.Name())
		if err != nil {
			return fmt.Errorf("templates: read %s: %w", e.Name(), err)
		}
		tpl := &Template{BuiltIn: true}
		if err := json.Unmarshal(raw, tpl); err != nil {
			return fmt.Errorf("templates: parse %s: %w", e.Name(), err)
		}
		// Sanity-check the DAG parses.
		if _, err := orchestrator.ParseDAG(tpl.DAGJson); err != nil {
			return fmt.Errorf("templates: invalid dag in %s: %w", e.Name(), err)
		}
		l.bundled[tpl.Code] = tpl
	}
	return nil
}

// List returns the gallery view (DAGJson stripped). Bundled
// templates are always listed; user-saved ones are pulled from
// the DB when available.
func (l *Loader) List(ctx context.Context) ([]ListItem, error) {
	out := make([]ListItem, 0, len(l.bundled))
	l.mu.RLock()
	for _, t := range l.bundled {
		out = append(out, ListItem{
			Code: t.Code, Name: t.Name, Description: t.Description,
			Industry: t.Industry, BuiltIn: t.BuiltIn, AIExplanation: t.AIExplanation,
		})
	}
	l.mu.RUnlock()

	if l.db != nil {
		var rows []model.StrategyTemplate
		if err := l.db.WithContext(ctx).Where("built_in = ?", false).Find(&rows).Error; err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "templates: list user-saved", err)
		}
		for _, r := range rows {
			// Skip user-saved rows that shadow a bundled
			// code (they take precedence; we already
			// include the bundled entry, so dedup).
			if _, ok := l.lookupBundled(r.Code); ok {
				continue
			}
			out = append(out, ListItem{
				Code: r.Code, Name: r.Name, Description: r.Description,
				Industry: r.Industry, BuiltIn: r.BuiltIn, AIExplanation: r.AIExplanation,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].BuiltIn != out[j].BuiltIn {
			return out[i].BuiltIn
		}
		return out[i].Code < out[j].Code
	})
	return out, nil
}

// Get returns the Template by code. Looks up user-saved first
// (so admins can override a bundled template's DAG), then bundled.
func (l *Loader) Get(ctx context.Context, code string) (*Template, error) {
	if l.db != nil {
		var row model.StrategyTemplate
		if err := l.db.WithContext(ctx).
			Where("code = ?", code).
			First(&row).Error; err == nil {
			return &Template{
				Code: row.Code, Name: row.Name, Description: row.Description,
				Industry: row.Industry, BuiltIn: row.BuiltIn,
				AIExplanation: row.AIExplanation, DAGJson: row.DAGJson,
			}, nil
		} else if err != gorm.ErrRecordNotFound {
			return nil, apperr.Wrap(apperr.CodeInternal, "templates: lookup", err)
		}
	}
	if t, ok := l.lookupBundled(code); ok {
		return t, nil
	}
	return nil, apperr.NotFound("template not found: " + code)
}

// lookupBundled is the in-memory lookup helper.
func (l *Loader) lookupBundled(code string) (*Template, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	t, ok := l.bundled[code]
	return t, ok
}

// ListCodes returns the bundled template codes in stable order.
// Useful for startup logs and test assertions.
func (l *Loader) ListCodes() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]string, 0, len(l.bundled))
	for code := range l.bundled {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// Instantiate creates a new model.Strategy + model.StrategyVersion
// from the template identified by code. The new strategy's Code is
// derived from the template code (suffixed with a timestamp) so
// the unique constraint is satisfied.
//
// Returns the new strategy + its v1 version.
func (l *Loader) Instantiate(ctx context.Context, db *gorm.DB, code string) (*model.Strategy, *model.StrategyVersion, error) {
	tpl, err := l.Get(ctx, code)
	if err != nil {
		return nil, nil, err
	}
	if db == nil {
		return nil, nil, apperr.Unavailable("templates: db not configured")
	}
	if _, err := orchestrator.ParseDAG(tpl.DAGJson); err != nil {
		return nil, nil, apperr.Wrap(apperr.CodeInternal, "templates: parse dag", err)
	}

	now := time.Now().UTC()
	strat := &model.Strategy{
		Code:        generateCode(code),
		Name:        tpl.Name + " (from template)",
		Description: tpl.Description,
		Status:      model.StrategyStatusDraft,
		Tags:        tpl.Industry,
		CurrentVersion: 1,
		DAGJson:     tpl.DAGJson,
	}
	ver := &model.StrategyVersion{
		Version:         1,
		DAGJson:         tpl.DAGJson,
		ChangeNote:      "instantiated from template " + code,
		SnapshotTakenAt: now,
	}

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(strat).Error; err != nil {
			return apperr.Wrap(apperr.CodeConflict, "templates: create strategy", err)
		}
		ver.StrategyID = strat.ID
		if err := tx.Create(ver).Error; err != nil {
			return apperr.Wrap(apperr.CodeInternal, "templates: create version", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	l.logger.Info("templates: instantiated",
		zap.String("template", code),
		zap.Uint64("strategy_id", strat.ID),
		zap.String("code", strat.Code))
	return strat, ver, nil
}

// SyncToDB upserts every bundled template into the
// strategy_templates table. Called at startup so the gallery can
// serve a consistent view across instances. No-op when db is nil.
func (l *Loader) SyncToDB(ctx context.Context) error {
	if l.db == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, t := range l.bundled {
		row := model.StrategyTemplate{
			Code:             t.Code,
			Name:             t.Name,
			Description:      t.Description,
			Industry:         t.Industry,
			DefaultParamsJSON: "{}",
			AIExplanation:    t.AIExplanation,
			BuiltIn:          true,
			DAGJson:          t.DAGJson,
		}
		// Upsert by code. We do not touch the soft-delete
		// column here because the user might have soft-
		// deleted a bundled template on purpose.
		err := l.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "code"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name", "description", "industry",
				"ai_explanation", "built_in", "dag_json",
			}),
		}).Create(&row).Error
		if err != nil {
			l.logger.Warn("templates: sync failed",
				zap.String("code", t.Code), zap.Error(err))
		}
	}
	return nil
}

// generateCode produces a unique strategy code derived from the
// template code. The suffix is the unix second count to avoid
// collisions across rapid "Use template" clicks.
func generateCode(templateCode string) string {
	if !strings.HasPrefix(templateCode, "tpl_") {
		templateCode = "tpl_" + templateCode
	}
	return fmt.Sprintf("%s_%d", templateCode, time.Now().Unix())
}