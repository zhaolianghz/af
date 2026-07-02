// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for RunContext. Covers defaults, Now() with
// injectable Clock, and the Vars mutex.
package orchestrator

import (
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewRunContext_Defaults(t *testing.T) {
	rc := NewRunContext(RunContextOptions{})
	if rc.Logger == nil {
		t.Error("Logger should default to nop, got nil")
	}
	if rc.Bus == nil {
		t.Error("Bus should default to NewMemBus, got nil")
	}
	if rc.Vars == nil {
		t.Error("Vars should be initialized")
	}
	if rc.StartedAt.IsZero() {
		t.Error("StartedAt should default to time.Now()")
	}
	if rc.clock == nil {
		t.Error("clock should default to time.Now")
	}
}

func TestNewRunContext_OverrideClock(t *testing.T) {
	fixed := time.Date(2026, 6, 11, 9, 35, 0, 0, time.UTC)
	rc := NewRunContext(RunContextOptions{
		Clock: func() time.Time { return fixed },
	})
	if got := rc.Now(); !got.Equal(fixed) {
		t.Errorf("Now: got %v want %v", got, fixed)
	}
}

func TestNewRunContext_OverrideLogger(t *testing.T) {
	l := zap.NewExample()
	rc := NewRunContext(RunContextOptions{Logger: l})
	if rc.Logger != l {
		t.Errorf("Logger: got %p want %p", rc.Logger, l)
	}
}

func TestRunContext_StartedAtOverride(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rc := NewRunContext(RunContextOptions{StartedAt: fixed})
	if !rc.StartedAt.Equal(fixed) {
		t.Errorf("StartedAt: got %v want %v", rc.StartedAt, fixed)
	}
}

func TestRunContext_SetGetVar(t *testing.T) {
	rc := NewRunContext(RunContextOptions{})
	rc.SetVar("foo", "bar")
	v, ok := rc.GetVar("foo")
	if !ok {
		t.Fatal("GetVar: not found")
	}
	if v.(string) != "bar" {
		t.Errorf("GetVar: got %v want bar", v)
	}
	if _, ok := rc.GetVar("missing"); ok {
		t.Error("GetVar(missing) should return false")
	}
}

func TestRunContext_VarsConcurrent(t *testing.T) {
	rc := NewRunContext(RunContextOptions{})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			rc.SetVar("k", i)
		}(i)
		go func() {
			defer wg.Done()
			_, _ = rc.GetVar("k")
		}()
	}
	wg.Wait()
}

func TestRunContext_NowNilClock(t *testing.T) {
	// A RunContext with clock explicitly nil should fall
	// through to time.Now.
	rc := &RunContext{}
	got := rc.Now()
	if time.Since(got) > time.Second {
		t.Errorf("Now() with nil clock: returned time too far in the past: %v", got)
	}
}
