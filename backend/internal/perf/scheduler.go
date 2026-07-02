// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package perf — scheduler.go
//
// §9.7 end-of-day backfill cron. Wires a single robfig/cron entry
// (configured by PerfConfig.BackfillCron) that, on every fire, walks
// the most-recent lookback window and computes a snapshot for any
// recommendation that doesn't yet have one.
//
// The scheduler is intentionally minimal — it owns exactly one cron
// entry, not the per-strategy table the executor's scheduler manages.
// Adding / removing entries dynamically isn't part of §9.
package perf

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// SchedulerConfig configures NewScheduler. The zero value disables
// the cron; callers should populate Cron + Timezone for a real
// schedule.
type SchedulerConfig struct {
	// Cron is the robfig/cron expression. Empty disables the
	// scheduler entirely; the route is still mounted but no
	// background fire happens.
	Cron string
	// Timezone is the IANA name used to interpret Cron. Defaults
	// to Asia/Shanghai (the A-share calendar).
	Timezone string
	// Lookback is the time window Service.Backfill scans on each
	// fire. The default in main.go is cfg.Perf.KlineLookback
	// (168h).
	Lookback time.Duration
	// Logger is required (zap.NewNop() is wired in if nil).
	Logger *zap.Logger
	// onFire replaces the default Service.Backfill call. Used by
	// tests to assert fire() side effects without running the full
	// compute path.
	onFire func(ctx context.Context)
}

// Scheduler owns the §9.7 cron runner. Safe for concurrent Start /
// Stop / Active calls; the underlying cron.AddFunc uses its own
// lock so fire() can race with Stop without panic.
type Scheduler struct {
	svc    *Service
	cfg    SchedulerConfig
	cron   *cron.Cron
	parser cron.Parser
	logger *zap.Logger

	mu     sync.Mutex
	active bool
	entry  cron.EntryID
}

// NewScheduler builds a Scheduler. The runner is created
// unconditionally so Stop can be a no-op for the "never started"
// path; Cron==" disables the schedule at Start time.
func NewScheduler(svc *Service, cfg SchedulerConfig) (*Scheduler, error) {
	if svc == nil {
		return nil, fmt.Errorf("perf: scheduler: nil service")
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
		return nil, fmt.Errorf("perf: scheduler: load timezone %q: %w", tz, err)
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	runner := cron.New(cron.WithParser(parser), cron.WithLocation(loc))
	return &Scheduler{
		svc:    svc,
		cfg:    cfg,
		cron:   runner,
		parser: parser,
		logger: l,
	}, nil
}

// Start registers the cron entry (if Cron is non-empty) and begins
// firing. Returns nil when Cron is empty — the route is still
// available, the operator can hit POST /perf/calculate manually.
func (s *Scheduler) Start() error {
	if s.cfg.Cron == "" {
		s.logger.Info("perf scheduler: cron disabled (empty expression)")
		return nil
	}
	sched, err := s.parser.Parse(s.cfg.Cron)
	if err != nil {
		return fmt.Errorf("perf: scheduler: parse %q: %w", s.cfg.Cron, err)
	}
	s.mu.Lock()
	s.entry = s.cron.Schedule(sched, cron.FuncJob(func() {
		s.fire(context.Background())
	}))
	s.active = true
	s.mu.Unlock()
	s.cron.Start()
	s.logger.Info("perf scheduler started",
		zap.String("cron", s.cfg.Cron),
		zap.Duration("lookback", s.cfg.Lookback),
	)
	return nil
}

// Stop blocks until any in-flight fire has completed (robfig's
// Stop() returns a channel that closes when the runner has drained),
// then halts. ctx bounds the wait — the underlying goroutine is
// independent of our context.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return nil
	}
	s.active = false
	s.mu.Unlock()
	stopCtx := s.cron.Stop()
	select {
	case <-stopCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Active reports whether the cron is currently firing. Used in
// startup logs and the /api/v1/perf/health surface.
func (s *Scheduler) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// fire is the cron callback. It runs the configured backfill hook
// (default: Service.Backfill over the configured lookback). Errors
// are logged, never returned — the cron goroutine has no caller to
// report to.
func (s *Scheduler) fire(ctx context.Context) {
	logger := s.logger.With(zap.Time("fire_at", time.Now()))
	if s.cfg.onFire != nil {
		s.cfg.onFire(ctx)
		return
	}
	if s.svc == nil {
		logger.Warn("perf scheduler: nil service; skipping fire")
		return
	}
	processed, errs, err := s.svc.Backfill(ctx, s.cfg.Lookback)
	if err != nil {
		logger.Error("perf scheduler: backfill failed", zap.Error(err))
		return
	}
	logger.Info("perf scheduler: backfill done",
		zap.Int("processed", processed),
		zap.Int("errored", errs),
	)
}
