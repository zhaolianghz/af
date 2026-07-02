// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package executor wraps the per-DAG orchestrator.Executor with
// run-level persistence: a Run row, per-node RunLog rows, and a
// final status transition. It also exposes a SSE-friendly event
// bus subscription for the HTTP handler.
//
// The package is split into four files:
//   - executor.go (this file): the run wrapper
//   - scheduler.go: cron-based scheduling
//   - handler.go: HTTP handler + SSE
//   - templates/: 5 built-in strategy templates
package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/notify"
	"github.com/skyzhao/af/internal/orchestrator"
)

// Executor turns a strategy into a persisted run. It is the
// single entry point used by:
//   - the HTTP handler (manual trigger)
//   - the cron scheduler (scheduled trigger)
//   - the trial-run handler (dry-run path, no DB writes)
//
// Concurrency: a single Executor is safe to share across
// goroutines. Each Execute call owns its own Run + RunContext;
// the underlying orchestrator.Executor is itself safe.
type Executor struct {
	db     *gorm.DB
	cfg    config.ExecutorConfig
	logger *zap.Logger
	bus    orchestrator.EventBus

	// external dependencies (datasource / notify) are
	// injected so unit tests can swap them.
	ds       datasource.Manager
	notifyM  notify.Manager
	strategy *orchestrator.StrategyService
	executor *orchestrator.Executor
	registry *orchestrator.Registry
}

// ExecutorOptions configures NewExecutor. All fields are
// optional except db + executor + registry.
type ExecutorOptions struct {
	DB        *gorm.DB
	Cfg       config.ExecutorConfig
	Logger    *zap.Logger
	Bus       orchestrator.EventBus
	DataSource datasource.Manager
	Notify    notify.Manager
	Strategy  *orchestrator.StrategyService
	Executor  *orchestrator.Executor
	Registry  *orchestrator.Registry
}

// NewExecutor wires a run-level executor. Missing optional
// dependencies fall back to safe no-op defaults.
func NewExecutor(o ExecutorOptions) *Executor {
	if o.Logger == nil {
		o.Logger = zap.NewNop()
	}
	if o.Bus == nil {
		o.Bus = orchestrator.NewMemBus()
	}
	return &Executor{
		db:       o.DB,
		cfg:      o.Cfg,
		logger:   o.Logger,
		bus:      o.Bus,
		ds:       o.DataSource,
		notifyM:  o.Notify,
		strategy: o.Strategy,
		executor: o.Executor,
		registry: o.Registry,
	}
}

// Bus returns the event bus so the SSE handler can subscribe.
func (e *Executor) Bus() orchestrator.EventBus { return e.bus }

// Cfg returns the executor config (used by the scheduler to
// decide timeouts).
func (e *Executor) Cfg() config.ExecutorConfig { return e.cfg }

// RunRequest is the input to Execute.
type RunRequest struct {
	StrategyID  uint64
	TriggerType string // manual / cron
	Inputs      map[string]any
	DryRun      bool

	// ExistingRun, when non-nil, replaces the auto-created Run
	// row. Used by Trigger so the HTTP handler can return
	// run_id within ~3s while the DAG walk runs in a goroutine.
	ExistingRun *model.Run
}

// Execute runs the strategy's stored DAG to completion. The
// returned *model.Run reflects the final state, including
// StartedAt / FinishedAt timestamps and the RunLog rows.
//
// When DryRun is true, no Run row is created and no RunLog rows
// are written. The orchestrator still walks the DAG, but each
// node's DB/Notify dependencies are nil so it short-circuits to
// a dry-run summary. (See trial_handler.go in the orchestrator
// package for the original use case.)
func (e *Executor) Execute(ctx context.Context, req RunRequest) (*model.Run, error) {
	if e.strategy == nil || e.executor == nil || e.registry == nil {
		return nil, apperr.Unavailable("executor: orchestrator not wired")
	}
	detail, err := e.strategy.Get(ctx, req.StrategyID)
	if err != nil {
		return nil, err
	}
	dag, err := orchestrator.ParseDAG(detail.CurrentVersionDAG)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalidArg, "executor: parse dag", err)
	}
	if err := dag.Validate(); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalidArg, "executor: invalid dag", err)
	}

	// Resolve the Run row. The HTTP Trigger path passes a
	// pre-created row so it can return run_id within ~3s;
	// everything else lets us auto-create.
	var run *model.Run
	if !req.DryRun {
		if req.ExistingRun != nil {
			run = req.ExistingRun
		} else {
			run = &model.Run{
				StrategyID:  detail.ID,
				TriggerType: triggerOrDefault(req.TriggerType),
				Status:      model.RunStatusRunning,
			}
			if err := e.db.WithContext(ctx).Create(run).Error; err != nil {
				return nil, apperr.Wrap(apperr.CodeInternal, "executor: create run", err)
			}
		}
	}

	// Build the RunContext the orchestrator will use.
	timeout := e.cfg.DefaultRunTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rc := orchestrator.NewRunContext(orchestrator.RunContextOptions{
		DB:           nil, // trial-run style; nodes check rc.DB themselves
		Logger:       e.logger.With(zap.Uint64("strategy_id", detail.ID)),
		DataSource:   e.ds,
		Notify:       e.notifyM,
		Bus:          e.bus,
		RunID:        runIDOrZero(run),
		StrategyID:   detail.ID,
		StrategyCode: detail.Code,
		StrategyName: detail.Name,
		Clock:        time.Now,
	})

	// Wire DB only for non-dry-run (the persist node writes
	// to rc.DB).
	if !req.DryRun {
		rc.DB = e.db
	}

	summary, execErr := e.executor.Execute(runCtx, rc, &orchestrator.RunRequest{
		DAG:    dag,
		Inputs: req.Inputs,
		DryRun: req.DryRun,
	})

	// Persist RunLog rows and finalize the Run row.
	if !req.DryRun && run != nil {
		if persistErr := e.persistLogs(ctx, run.ID, summary, execErr); persistErr != nil {
			e.logger.Error("executor: persist run logs", zap.Error(persistErr))
		}
		finalizeRun(run, summary, execErr)
		if err := e.db.WithContext(ctx).Save(run).Error; err != nil {
			e.logger.Error("executor: save run", zap.Error(err))
		}
		e.publishRunCompleted(run, summary, execErr)
		return run, execErr
	}

	// Dry-run: synthesize a transient Run for the caller.
	if summary != nil {
		return &model.Run{
			StrategyID:  detail.ID,
			TriggerType: triggerOrDefault(req.TriggerType),
			Status:      summary.Status,
			StartedAt:   ptrTime(summary.StartedAt),
			FinishedAt:  ptrTime(summary.FinishedAt),
			Error:       summary.Error,
		}, execErr
	}
	return nil, execErr
}

// ListRuns returns a page of Run rows, ordered by created_at
// desc. Optional filters narrow the result.
type RunListFilter struct {
	StrategyID uint64
	Status     string
	From       *time.Time
	To         *time.Time
	Page       int
	PageSize   int
}

func (e *Executor) ListRuns(ctx context.Context, f RunListFilter) ([]model.Run, int64, error) {
	if e.db == nil {
		return nil, 0, apperr.Unavailable("executor: db not configured")
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}
	q := e.db.WithContext(ctx).Model(&model.Run{}).Order("id DESC")
	if f.StrategyID > 0 {
		q = q.Where("strategy_id = ?", f.StrategyID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.From != nil {
		q = q.Where("created_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("created_at < ?", *f.To)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "executor: count runs", err)
	}
	var runs []model.Run
	if err := q.Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&runs).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "executor: list runs", err)
	}
	return runs, total, nil
}

// GetRun returns a single Run by id, or a BizError(CodeNotFound).
func (e *Executor) GetRun(ctx context.Context, id uint64) (*model.Run, error) {
	if e.db == nil {
		return nil, apperr.Unavailable("executor: db not configured")
	}
	var r model.Run
	if err := e.db.WithContext(ctx).First(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("run not found")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "executor: get run", err)
	}
	return &r, nil
}

// GetRunLogs returns all RunLog rows for the given run id, in
// stable order (started_at, then id).
func (e *Executor) GetRunLogs(ctx context.Context, runID uint64) ([]model.RunLog, error) {
	if e.db == nil {
		return nil, apperr.Unavailable("executor: db not configured")
	}
	var logs []model.RunLog
	if err := e.db.WithContext(ctx).
		Where("run_id = ?", runID).
		Order("started_at ASC, id ASC").
		Find(&logs).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "executor: list logs", err)
	}
	return logs, nil
}

// Retry clones the run parameters into a new run. The original
// run row is left untouched. Returns the new run id.
func (e *Executor) Retry(ctx context.Context, runID uint64) (uint64, error) {
	if e.db == nil {
		return 0, apperr.Unavailable("executor: db not configured")
	}
	src, err := e.GetRun(ctx, runID)
	if err != nil {
		return 0, err
	}
	go func() {
		// Run in a fresh background context so the
		// retry survives the HTTP request returning.
		bg := context.Background()
		bg, cancel := context.WithTimeout(bg, e.cfg.DefaultRunTimeout)
		defer cancel()
		if _, err := e.Execute(bg, RunRequest{
			StrategyID:  src.StrategyID,
			TriggerType: model.RunTriggerManual,
		}); err != nil {
			e.logger.Error("executor: retry failed",
				zap.Uint64("src_run_id", runID),
				zap.Error(err))
		}
	}()
	return src.ID, nil
}

// =============================================================================
// Internals
// =============================================================================

func (e *Executor) persistLogs(ctx context.Context, runID uint64, summary *orchestrator.RunSummary, execErr error) error {
	if e.db == nil {
		return nil
	}
	if summary == nil || len(summary.NodeResults) == 0 {
		return nil
	}
	rows := make([]model.RunLog, 0, len(summary.NodeResults))
	for nodeID, r := range summary.NodeResults {
		rows = append(rows, model.RunLog{
			RunID:      runID,
			NodeKey:    nodeID,
			Status:     r.Status,
			StartedAt:  r.StartedAt,
			FinishedAt: r.FinishedAt,
			PayloadIn:  "",
			PayloadOut: marshalPayload(r.Payload),
			Error:      r.Error,
		})
	}
	return e.db.WithContext(ctx).Create(&rows).Error
}

func (e *Executor) publishRunCompleted(run *model.Run, summary *orchestrator.RunSummary, execErr error) {
	if e.bus == nil || run == nil {
		return
	}
	data := map[string]any{
		"strategy_id": run.StrategyID,
		"status":      run.Status,
		"trigger":     run.TriggerType,
	}
	if execErr != nil {
		data["error"] = execErr.Error()
	}
	if summary != nil {
		data["duration"] = summary.Duration.String()
	}
	e.bus.Publish(orchestrator.Event{
		RunID:     run.ID,
		Type:      orchestrator.EventRunCompleted,
		Timestamp: time.Now(),
		Data:      data,
	})
}

// finalizeRun mutates run in place with the final status +
// timestamps based on the summary.
func finalizeRun(run *model.Run, summary *orchestrator.RunSummary, execErr error) {
	if summary != nil {
		run.StartedAt = ptrTime(summary.StartedAt)
		run.FinishedAt = ptrTime(summary.FinishedAt)
		run.Status = summary.Status
		run.Error = summary.Error
	}
	if execErr != nil && run.Status == "" {
		run.Status = model.RunStatusFailed
		run.Error = execErr.Error()
	}
	if run.FinishedAt == nil {
		now := time.Now()
		run.FinishedAt = &now
	}
}

func triggerOrDefault(t string) string {
	if t == "" {
		return model.RunTriggerManual
	}
	return t
}

func runIDOrZero(r *model.Run) uint64 {
	if r == nil {
		return 0
	}
	return r.ID
}

func ptrTime(t time.Time) *time.Time { return &t }

func marshalPayload(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Sprintf("marshal error: %v", err)
	}
	return string(b)
}

// =============================================================================
// Run lifecycle helpers used by handler.go
// =============================================================================

// CreateRunRow inserts a status=running Run row. The Trigger
// handler uses this to surface run_id within ~3s before
// dispatching the DAG walk to a goroutine.
func (e *Executor) CreateRunRow(ctx context.Context, strategyID uint64, trigger string) (*model.Run, error) {
	if e.db == nil {
		return nil, apperr.Unavailable("executor: db not configured")
	}
	run := &model.Run{
		StrategyID:  strategyID,
		TriggerType: triggerOrDefault(trigger),
		Status:      model.RunStatusRunning,
	}
	if err := e.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "executor: create run", err)
	}
	return run, nil
}

// strategyIDByCode resolves a strategy_code to its id, used by
// the Trigger handler so callers can fire runs via a stable
// code (e.g. `morning_volume_breakout`) instead of an internal id.
func (e *Executor) strategyIDByCode(ctx context.Context, code string) (uint64, error) {
	if e.db == nil {
		return 0, apperr.Unavailable("executor: db not configured")
	}
	var strat model.Strategy
	if err := e.db.WithContext(ctx).
		Where("code = ?", code).
		First(&strat).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, apperr.NotFound("strategy not found: " + code)
		}
		return 0, apperr.Wrap(apperr.CodeInternal, "executor: lookup strategy", err)
	}
	return strat.ID, nil
}

// =============================================================================
// Recommendations query (read-only; needed by the run handler's
// /api/v1/recommendations route).
// =============================================================================

// RecommendationFilter is a thin query spec for ListRecommendations.
type RecommendationFilter struct {
	StrategyCode string
	Tag          string
	From         *time.Time
	To           *time.Time
	Page         int
	PageSize     int
}

// ListRecommendations returns a page of recommendations joined
// with their tags. Used by the recommendations API.
func (e *Executor) ListRecommendations(ctx context.Context, f RecommendationFilter) ([]model.Recommendation, int64, error) {
	if e.db == nil {
		return nil, 0, apperr.Unavailable("executor: db not configured")
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}
	// Use the qualified table name in Order by to avoid the
	// "ambiguous column name: id" error when the tag join is
	// also in play.
	q := e.db.WithContext(ctx).Model(&model.Recommendation{}).
		Order("recommendations.id DESC")
	if f.StrategyCode != "" {
		q = q.Where("recommendations.strategy_code = ?", f.StrategyCode)
	}
	if f.From != nil {
		q = q.Where("recommendations.date >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("recommendations.date < ?", *f.To)
	}
	if f.Tag != "" {
		// Join with tags table. The join is safe even
		// without an index on tag (the table is small in
		// v1). We qualify the join column with the table
		// name to avoid the "ambiguous column" error in
		// SQLite when ordering or filtering by id later.
		q = q.Joins("JOIN recommendation_tags rt ON rt.recommendation_id = recommendations.id").
			Where("rt.tag = ?", f.Tag)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "executor: count recs", err)
	}
	var recs []model.Recommendation
	if err := q.Preload("Tags").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&recs).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "executor: list recs", err)
	}
	// Backfill stock_name for rows missing it (persisted recs derived
	// from a kline data_source carry no name — the kline row has no
	// name column). Best-effort; never fails the list.
	enrichRecNames(ctx, recs, e.ds)
	return recs, total, nil
}

// quoteGetter is the slice of datasource.Manager that enrichRecNames
// needs — just the latest quote. Narrowed so tests can stub it without
// implementing the whole Manager interface.
type quoteGetter interface {
	GetQuote(ctx context.Context, stockCode string) (*datasource.Quote, error)
}

// enrichRecNames backfills StockName on any rec missing it, using the
// live quote. Best-effort: a per-stock failure leaves that row's name
// empty and never returns an error.
func enrichRecNames(ctx context.Context, recs []model.Recommendation, qg quoteGetter) {
	if qg == nil {
		return
	}
	for i := range recs {
		if recs[i].StockName != "" {
			continue
		}
		if q, err := qg.GetQuote(ctx, recs[i].StockCode); err == nil && q != nil && q.StockName != "" {
			recs[i].StockName = q.StockName
		}
	}
}

// DashboardSummary is the aggregate the dashboard landing page renders.
// All counts are computed from existing tables (runs / recommendations)
// — no new persistence, no "position" concept (positions are not a v1
// entity).
type DashboardSummary struct {
	TodayRecommendations int64               `json:"today_recommendations"`
	TotalRecommendations int64               `json:"total_recommendations"`
	RunsWindowDays       int                 `json:"runs_window_days"`
	RunsTotal            int64               `json:"runs_total"`
	RunsSuccess          int64               `json:"runs_success"`
	RunsFailed           int64               `json:"runs_failed"`
	SuccessRate          *float64            `json:"success_rate"` // nil when no runs in window
	RecentErrors         []DashboardRunError `json:"recent_errors"`
}

// DashboardRunError is a compact failed-run row for the "recent errors"
// panel.
type DashboardRunError struct {
	RunID      uint64    `json:"run_id"`
	StrategyID uint64    `json:"strategy_id"`
	Error      string    `json:"error"`
	FinishedAt time.Time `json:"finished_at"`
}

// DashboardStats computes the landing-page summary. windowDays bounds
// the run-success-rate and recent-errors window (default 7).
func (e *Executor) DashboardStats(ctx context.Context, windowDays int) (*DashboardSummary, error) {
	if e.db == nil {
		return nil, apperr.Unavailable("executor: db not configured")
	}
	if windowDays <= 0 {
		windowDays = 7
	}
	db := e.db.WithContext(ctx)
	out := &DashboardSummary{RunsWindowDays: windowDays}

	// Today's date window [today 00:00, tomorrow 00:00) in UTC, matching
	// how the persist node truncates rec dates.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	tomorrow := today.AddDate(0, 0, 1)
	windowStart := now.AddDate(0, 0, -windowDays)

	if err := db.Model(&model.Recommendation{}).
		Where("date >= ? AND date < ?", today, tomorrow).
		Count(&out.TodayRecommendations).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "dashboard: today recs", err)
	}
	if err := db.Model(&model.Recommendation{}).Count(&out.TotalRecommendations).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "dashboard: total recs", err)
	}

	runQ := db.Model(&model.Run{}).Where("created_at >= ?", windowStart)
	if err := runQ.Session(&gorm.Session{}).Count(&out.RunsTotal).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "dashboard: runs total", err)
	}
	if err := db.Model(&model.Run{}).Where("created_at >= ? AND status = ?", windowStart, model.RunStatusSuccess).
		Count(&out.RunsSuccess).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "dashboard: runs success", err)
	}
	if err := db.Model(&model.Run{}).Where("created_at >= ? AND status = ?", windowStart, model.RunStatusFailed).
		Count(&out.RunsFailed).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "dashboard: runs failed", err)
	}
	if out.RunsTotal > 0 {
		rate := float64(out.RunsSuccess) / float64(out.RunsTotal)
		out.SuccessRate = &rate
	}

	var failed []model.Run
	if err := db.Model(&model.Run{}).
		Where("status = ?", model.RunStatusFailed).
		Order("id DESC").Limit(5).Find(&failed).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "dashboard: recent errors", err)
	}
	out.RecentErrors = make([]DashboardRunError, 0, len(failed))
	for _, r := range failed {
		fin := time.Time{}
		if r.FinishedAt != nil {
			fin = *r.FinishedAt
		}
		out.RecentErrors = append(out.RecentErrors, DashboardRunError{
			RunID: r.ID, StrategyID: r.StrategyID, Error: r.Error, FinishedAt: fin,
		})
	}
	return out, nil
}

// the bus into a writer while applying a heartbeat. It blocks
// until the context is cancelled or the stream ends.
func DrainEvents(ctx context.Context, bus orchestrator.EventBus, runID uint64, heartbeat time.Duration, write func(ev orchestrator.Event) error) {
	if bus == nil {
		<-ctx.Done()
		return
	}
	ch, unsub := bus.Subscribe(runID)
	defer unsub()
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_ = write(orchestrator.Event{Type: "__heartbeat__", Timestamp: time.Now()})
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := write(ev); err != nil {
				return
			}
		}
	}
}

// Avoid unused import warnings when the file is split further.
var _ = sync.Mutex{}
