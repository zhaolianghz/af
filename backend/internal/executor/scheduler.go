// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Scheduler wraps robfig/cron/v3 with the A7-BE2 semantics:
// each active strategy's cron expression is registered, and the
// scheduler consults the trading calendar before firing. Non-
// trading days and outside-session windows are skipped silently
// (logged at debug level).
package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/calendar"
	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

// SchedulerConfig configures NewScheduler. ScheduleField lets
// the operator pick which column drives cron — the
// Strategy.CronExpression column is the default.
type SchedulerConfig struct {
	DB            *gorm.DB
	Logger        *zap.Logger
	Cron          config.CronConfig
	Executor      *Executor
	Calendar      *calendar.Service
	Strategy      *orchestrator.StrategyService
	// Location is the timezone for cron parsing. Defaults to
	// config.Cron.Timezone.
	Location *time.Location
	// BaseCtx is the parent context for cron-fired runs. Wire the
	// server root context so graceful shutdown cancels in-flight
	// cron runs instead of leaking them past process exit. Defaults
	// to context.Background().
	BaseCtx context.Context
}

// Scheduler owns the cron runner. It is safe to call Add /
// Remove concurrently; the underlying cron.AddFunc uses its
// own internal lock.
type Scheduler struct {
	cfg    SchedulerConfig
	c      *cron.Cron
	parser cron.Parser

	// active tracks strategy_id -> cron entry id so we can
	// remove an old entry when an admin updates a strategy.
	mu     sync.Mutex
	active map[uint64]cron.EntryID

	// onFire is invoked from the cron goroutine. Tests
	// override this to assert behavior without running the
	// full Execute path.
	onFire func(strategyID uint64, trigger string)
}

// NewScheduler builds a Scheduler. Call Start to begin firing.
func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.BaseCtx == nil {
		cfg.BaseCtx = context.Background()
	}
	if cfg.Location == nil {
		loc, err := time.LoadLocation(cfg.Cron.Timezone)
		if err != nil {
			return nil, fmt.Errorf("scheduler: load timezone: %w", err)
		}
		cfg.Location = loc
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	runner := cron.New(cron.WithParser(parser), cron.WithLocation(cfg.Location))
	return &Scheduler{
		cfg:    cfg,
		c:      runner,
		parser: parser,
		active: make(map[uint64]cron.EntryID),
	}, nil
}

// Start begins firing scheduled jobs. It returns immediately;
// the cron goroutine runs in the background.
func (s *Scheduler) Start() {
	s.c.Start()
}

// Stop blocks until any in-flight job has completed, then halts
// the cron runner.
func (s *Scheduler) Stop(ctx context.Context) error {
	stopCtx := s.c.Stop()
	select {
	case <-stopCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LoadFromDB scans the strategies table and registers every
// active strategy that has a CronExpression set. Existing
// entries for the same strategy_id are replaced.
func (s *Scheduler) LoadFromDB(ctx context.Context) error {
	if s.cfg.DB == nil {
		return nil
	}
	var rows []model.Strategy
	if err := s.cfg.DB.WithContext(ctx).
		Where("status = ?", model.StrategyStatusActive).
		Where("cron_expression <> ''").
		Find(&rows).Error; err != nil {
		return fmt.Errorf("scheduler: load strategies: %w", err)
	}
	for _, row := range rows {
		if err := s.Add(row.ID, row.CronExpression); err != nil {
			s.cfg.Logger.Warn("scheduler: register strategy failed",
				zap.Uint64("strategy_id", row.ID),
				zap.String("cron", row.CronExpression),
				zap.Error(err))
		}
	}
	return nil
}

// Add registers a new cron entry for the given strategy. If an
// entry already exists for strategyID, it is removed first so
// the new expression takes effect immediately.
func (s *Scheduler) Add(strategyID uint64, expr string) error {
	if expr == "" {
		return errors.New("scheduler: empty cron expression")
	}
	sched, err := s.parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("scheduler: parse %q: %w", expr, err)
	}
	s.mu.Lock()
	if old, ok := s.active[strategyID]; ok {
		s.c.Remove(old)
	}
	s.mu.Unlock()

	entry := s.c.Schedule(sched, cron.FuncJob(func() {
		s.fire(strategyID)
	}))
	s.mu.Lock()
	s.active[strategyID] = entry
	s.mu.Unlock()
	return nil
}

// Remove cancels the cron entry for a strategy.
func (s *Scheduler) Remove(strategyID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.active[strategyID]; ok {
		s.c.Remove(old)
		delete(s.active, strategyID)
	}
}

// ActiveEntries returns the registered strategy ids, for
// debugging / /api/v1/runs/scheduled introspection.
func (s *Scheduler) ActiveEntries() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint64, 0, len(s.active))
	for id := range s.active {
		out = append(out, id)
	}
	return out
}

// SetOnFire replaces the fire hook. Useful in tests.
func (s *Scheduler) SetOnFire(fn func(strategyID uint64, trigger string)) {
	s.onFire = fn
}

// =============================================================================
// Internals
// =============================================================================

// fire is the actual cron callback. It performs the trading-day
// + session checks and then either dispatches to the run-level
// executor or records a skip.
func (s *Scheduler) fire(strategyID uint64) {
	now := time.Now()
	logger := s.cfg.Logger.With(
		zap.Uint64("strategy_id", strategyID),
		zap.Time("fire_at", now),
	)

	// Trading-day check.
	if s.cfg.Calendar != nil && !s.cfg.Calendar.IsTradingDay(now) {
		logger.Debug("scheduler: skipping non-trading day")
		return
	}
	// Session window check (only when the calendar is wired).
	if s.cfg.Calendar != nil && !s.cfg.Calendar.IsInTradingSession(now) {
		logger.Debug("scheduler: skipping outside trading session")
		return
	}
	if s.onFire != nil {
		s.onFire(strategyID, model.RunTriggerCron)
		return
	}
	if s.cfg.Executor == nil {
		logger.Warn("scheduler: executor not wired; dropping fire")
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("scheduler: cron run panicked", zap.Any("panic", r))
			}
		}()
		ctx, cancel := context.WithTimeout(s.cfg.BaseCtx, s.cfgExecutorTimeout())
		defer cancel()
		if _, err := s.cfg.Executor.Execute(ctx, RunRequest{
			StrategyID:  strategyID,
			TriggerType: model.RunTriggerCron,
		}); err != nil {
			logger.Error("scheduler: cron run failed", zap.Error(err))
		}
	}()
}

func (s *Scheduler) cfgExecutorTimeout() time.Duration {
	// The executor carries its own DefaultRunTimeout; we
	// double it for the scheduling wrapper to account for
	// queue / DB latency around the run.
	if s.cfg.Executor == nil {
		return 5 * time.Minute
	}
	return s.cfg.Executor.Cfg().DefaultRunTimeout
}
