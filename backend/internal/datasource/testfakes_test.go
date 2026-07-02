// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — testfakes_test.go
//
// Shared fakes used by manager_test.go, breaker_test.go, and
// validate_test.go. Living in their own file keeps each test
// file focused on assertions.
package datasource

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/skyzhao/af/internal/model"
)

// fakeSource is a programmable Source. The exported setters let tests
// swap in a custom call behavior before each call. Call counters let
// tests assert on routing / fallback behavior.
type fakeSource struct {
	name    string
	healthy atomic.Bool

	mu      sync.Mutex
	quoteFn func(ctx context.Context, code string) (*Quote, error)
	klineFn func(ctx context.Context, code, period string, start, end time.Time) ([]KLine, error)
	fundFn  func(ctx context.Context, code string) (*Fundamental, error)
	newsFn  func(ctx context.Context, code string, limit int) ([]News, error)

	quoteCalls atomic.Int64
	klineCalls atomic.Int64
	fundCalls  atomic.Int64
	newsCalls  atomic.Int64
}

func newFakeSource(name string, q *Quote) *fakeSource {
	f := &fakeSource{name: name}
	f.healthy.Store(true)
	f.quoteFn = func(ctx context.Context, code string) (*Quote, error) {
		if q == nil {
			return nil, errors.New("no quote configured")
		}
		out := *q
		out.StockCode = code
		out.Source = name
		return &out, nil
	}
	f.klineFn = func(ctx context.Context, code, period string, start, end time.Time) ([]KLine, error) {
		return nil, ErrNotImplemented
	}
	f.fundFn = func(ctx context.Context, code string) (*Fundamental, error) {
		return nil, ErrNotImplemented
	}
	f.newsFn = func(ctx context.Context, code string, limit int) ([]News, error) {
		return nil, ErrNotImplemented
	}
	return f
}

func (f *fakeSource) Name() string { return f.name }
func (f *fakeSource) IsHealthy() bool {
	return f.healthy.Load()
}

func (f *fakeSource) GetQuote(ctx context.Context, code string) (*Quote, error) {
	f.quoteCalls.Add(1)
	f.mu.Lock()
	fn := f.quoteFn
	f.mu.Unlock()
	return fn(ctx, code)
}

func (f *fakeSource) GetKLine(ctx context.Context, code, period string, start, end time.Time) ([]KLine, error) {
	f.klineCalls.Add(1)
	f.mu.Lock()
	fn := f.klineFn
	f.mu.Unlock()
	return fn(ctx, code, period, start, end)
}

func (f *fakeSource) GetFundamental(ctx context.Context, code string) (*Fundamental, error) {
	f.fundCalls.Add(1)
	f.mu.Lock()
	fn := f.fundFn
	f.mu.Unlock()
	return fn(ctx, code)
}

func (f *fakeSource) GetNews(ctx context.Context, code string, limit int) ([]News, error) {
	f.newsCalls.Add(1)
	f.mu.Lock()
	fn := f.newsFn
	f.mu.Unlock()
	return fn(ctx, code, limit)
}

func (f *fakeSource) setQuoteFn(fn func(ctx context.Context, code string) (*Quote, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quoteFn = fn
}

func (f *fakeSource) setKLineFn(fn func(ctx context.Context, code, period string, start, end time.Time) ([]KLine, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.klineFn = fn
}

func (f *fakeSource) setFundFn(fn func(ctx context.Context, code string) (*Fundamental, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fundFn = fn
}

func (f *fakeSource) setNewsFn(fn func(ctx context.Context, code string, limit int) ([]News, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.newsFn = fn
}

// recordingRepo is a HealthRepo that captures every event for tests
// to assert on.
type recordingRepo struct {
	mu         sync.Mutex
	successes  []string
	failures   []string
	switches   []string
	healthRows map[string]*model.DatasourceHealth
}

func newRecordingRepo() *recordingRepo {
	return &recordingRepo{healthRows: map[string]*model.DatasourceHealth{}}
}

func (r *recordingRepo) RecordSuccess(ctx context.Context, source string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.successes = append(r.successes, source)
	row, ok := r.healthRows[source]
	if !ok {
		row = &model.DatasourceHealth{Source: source}
		r.healthRows[source] = row
	}
	row.Status = model.HealthStatusHealthy
	return nil
}

func (r *recordingRepo) RecordFailure(ctx context.Context, source, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = append(r.failures, source+":"+errMsg)
	row, ok := r.healthRows[source]
	if !ok {
		row = &model.DatasourceHealth{Source: source}
		r.healthRows[source] = row
	}
	row.Status = model.HealthStatusDegraded
	row.LastError = errMsg
	return nil
}

func (r *recordingRepo) RecordSwitch(ctx context.Context, from, to, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.switches = append(r.switches, from+"->"+to+":"+reason)
	return nil
}

func (r *recordingRepo) GetHealth(ctx context.Context, source string) (*model.DatasourceHealth, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row, ok := r.healthRows[source]; ok {
		copy := *row
		return &copy, nil
	}
	return nil, nil
}

func (r *recordingRepo) ListAll(ctx context.Context) ([]model.DatasourceHealth, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.DatasourceHealth, 0, len(r.healthRows))
	for _, r := range r.healthRows {
		out = append(out, *r)
	}
	return out, nil
}
