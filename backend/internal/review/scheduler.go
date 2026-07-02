// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package review

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/skyzhao/af/internal/model"
	"go.uber.org/zap"
)

// SchedulerConfig configures the §14.9 review cron. Empty cron strings
// disable that cadence; the manual POST endpoints still work.
type SchedulerConfig struct {
	DailyCron  string // e.g. "30 15 * * 1-5" (15:30 Mon-Fri)
	WeeklyCron string // e.g. "0 20 * * 0"     (20:00 Sun)
	Timezone   string // default Asia/Shanghai
	Logger     *zap.Logger
}

// Scheduler owns the daily + weekly review cron entries.
type Scheduler struct {
	svc    *Service
	cfg    SchedulerConfig
	cron   *cron.Cron
	parser cron.Parser
	logger *zap.Logger

	mu     sync.Mutex
	active bool
}

// NewScheduler builds the scheduler.
func NewScheduler(svc *Service, cfg SchedulerConfig) (*Scheduler, error) {
	if svc == nil {
		return nil, fmt.Errorf("review: scheduler: nil service")
	}
	l := cfg.Logger
	if l == nil {
		l = zap.NewNop()
	}
	tz := cfg.Timezone
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("review: scheduler: load timezone %q: %w", tz, err)
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	return &Scheduler{
		svc:    svc,
		cfg:    cfg,
		cron:   cron.New(cron.WithParser(parser), cron.WithLocation(loc)),
		parser: parser,
		logger: l,
	}, nil
}

// Start registers the configured cron entries and begins firing.
func (s *Scheduler) Start() error {
	add := func(expr, kind string, fire func(context.Context, time.Time) (*model.ReviewReport, error)) error {
		if expr == "" {
			return nil
		}
		sched, err := s.parser.Parse(expr)
		if err != nil {
			return fmt.Errorf("review: scheduler: parse %q: %w", expr, err)
		}
		s.cron.Schedule(sched, cron.FuncJob(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if _, err := fire(ctx, time.Now()); err != nil {
				s.logger.Error("review: scheduled generate failed",
					zap.String("kind", kind), zap.Error(err))
				return
			}
			s.logger.Info("review: generated", zap.String("kind", kind))
		}))
		return nil
	}
	if err := add(s.cfg.DailyCron, "daily", s.svc.GenerateDaily); err != nil {
		return err
	}
	if err := add(s.cfg.WeeklyCron, "weekly", s.svc.GenerateWeekly); err != nil {
		return err
	}
	s.mu.Lock()
	s.active = true
	s.mu.Unlock()
	s.cron.Start()
	s.logger.Info("review scheduler started",
		zap.String("daily", s.cfg.DailyCron), zap.String("weekly", s.cfg.WeeklyCron))
	return nil
}

// Stop halts the cron, waiting for any in-flight job (bounded by ctx).
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return nil
	}
	s.active = false
	s.mu.Unlock()
	done := s.cron.Stop()
	select {
	case <-done.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
