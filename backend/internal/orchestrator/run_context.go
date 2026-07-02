// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package orchestrator

import (
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/notify"
)

// RunContext carries everything a node needs at execution time.
// It is constructed once per run (or trial-run) and shared — read-
// only, with the exception of Vars — across all node invocations.
//
// All external-system dependencies (DB, DataSource, Notify) are
// optional. Trial-runs pass nil for DB and Notify, and the nodes
// detect this and short-circuit.
type RunContext struct {
	DB         *gorm.DB
	Logger     *zap.Logger
	DataSource datasource.Manager
	Notify     notify.Manager
	Bus        EventBus

	RunID        uint64
	StrategyID   uint64
	StrategyCode string
	StrategyName string

	StartedAt time.Time
	clock     func() time.Time

	// Vars is a per-run scratch space. Nodes publish values
	// here (e.g. session_tag node writes Vars["session_tag"]) for
	// later nodes to read. Guarded by mu.
	Vars map[string]any
	mu   sync.RWMutex
}

// RunContextOptions configures NewRunContext. Zero values get
// safe defaults.
type RunContextOptions struct {
	DB           *gorm.DB
	Logger       *zap.Logger
	DataSource   datasource.Manager
	Notify       notify.Manager
	Bus          EventBus
	RunID        uint64
	StrategyID   uint64
	StrategyCode string
	StrategyName string
	StartedAt    time.Time
	// Clock is injectable for deterministic tests. nil = time.Now.
	Clock func() time.Time
}

// NewRunContext builds a RunContext with sensible defaults.
func NewRunContext(o RunContextOptions) *RunContext {
	if o.Logger == nil {
		o.Logger = zap.NewNop()
	}
	if o.Bus == nil {
		o.Bus = NewMemBus()
	}
	if o.StartedAt.IsZero() {
		o.StartedAt = time.Now()
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
	return &RunContext{
		DB:           o.DB,
		Logger:       o.Logger,
		DataSource:   o.DataSource,
		Notify:       o.Notify,
		Bus:          o.Bus,
		RunID:        o.RunID,
		StrategyID:   o.StrategyID,
		StrategyCode: o.StrategyCode,
		StrategyName: o.StrategyName,
		StartedAt:    o.StartedAt,
		clock:        o.Clock,
		Vars:         make(map[string]any),
	}
}

// Now returns the run's current time. Injected Clock takes
// precedence over time.Now so tests can pin time.
func (rc *RunContext) Now() time.Time {
	if rc.clock != nil {
		return rc.clock()
	}
	return time.Now()
}

// SetVar writes a value to Vars.
func (rc *RunContext) SetVar(key string, v any) {
	rc.mu.Lock()
	rc.Vars[key] = v
	rc.mu.Unlock()
}

// GetVar reads a value from Vars, returning (v, true) if present.
func (rc *RunContext) GetVar(key string) (any, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	v, ok := rc.Vars[key]
	return v, ok
}
