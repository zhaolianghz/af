// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package notify

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeChannel is a test double. It tracks how many times Send was
// called, lets tests inject a sequence of return values, and exposes
// a configurable IsHealthy state.
type fakeChannel struct {
	name    string
	healthy atomic.Bool
	calls   atomic.Int32
	// returns is a queue; each Send pops one and returns it. If the
	// queue is empty the last element is returned forever.
	returns []error
	// isHealthy is the function used by IsHealthy. If nil, defaults
	// to atomic.Bool.Load.
}

func newFake(name string, returns ...error) *fakeChannel {
	f := &fakeChannel{name: name, returns: returns}
	f.healthy.Store(true)
	if f.returns == nil {
		// Make sure the queue always has at least one element so the
		// "default last value" logic has something to return.
		f.returns = []error{nil}
	}
	return f
}

func (f *fakeChannel) Name() string { return f.name }

func (f *fakeChannel) Send(ctx context.Context, msg *Message) error {
	idx := int(f.calls.Add(1)) - 1
	if idx < len(f.returns) {
		return f.returns[idx]
	}
	return f.returns[len(f.returns)-1]
}

func (f *fakeChannel) IsHealthy() bool { return f.healthy.Load() }

func (f *fakeChannel) Calls() int32 { return f.calls.Load() }

func TestManager_PrimarySuccess(t *testing.T) {
	primary := newFake("primary")
	fallback := newFake("fallback", errors.New("should not be called"))
	mgr := NewManager(ManagerOptions{
		Primary:  "primary",
		Fallback: []string{"fallback"},
		Retry:    RetryConfig{Attempts: 1, InitialBackoff: time.Millisecond},
	})
	mgr.RegisterChannel(primary.Name(), primary)
	mgr.RegisterChannel(fallback.Name(), fallback)

	err := mgr.Send(context.Background(), &Message{Type: MsgAlert, Content: "hi"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), primary.Calls())
	assert.Equal(t, int32(0), fallback.Calls(), "fallback must not be tried when primary succeeds")
}

func TestManager_PrimaryFailsFallbackSucceeds(t *testing.T) {
	primary := newFake("primary", errors.New("primary down"))
	fallback := newFake("fallback") // succeeds
	mgr := NewManager(ManagerOptions{
		Primary:  "primary",
		Fallback: []string{"fallback"},
		Retry:    RetryConfig{Attempts: 1, InitialBackoff: time.Millisecond},
		Timeout:  100 * time.Millisecond,
	})
	mgr.RegisterChannel(primary.Name(), primary)
	mgr.RegisterChannel(fallback.Name(), fallback)

	err := mgr.Send(context.Background(), &Message{Type: MsgAlert, Content: "hi"})
	require.NoError(t, err, "fallback should pick up the slack")
	assert.Equal(t, int32(1), primary.Calls())
	assert.Equal(t, int32(1), fallback.Calls())
}

func TestManager_PrimaryFailsFallbackFails(t *testing.T) {
	primary := newFake("primary", errors.New("primary down"))
	fallback := newFake("fallback", errors.New("fallback down"))
	mgr := NewManager(ManagerOptions{
		Primary:  "primary",
		Fallback: []string{"fallback"},
		Retry:    RetryConfig{Attempts: 1, InitialBackoff: time.Millisecond},
		Timeout:  100 * time.Millisecond,
	})
	mgr.RegisterChannel(primary.Name(), primary)
	mgr.RegisterChannel(fallback.Name(), fallback)

	err := mgr.Send(context.Background(), &Message{Type: MsgAlert, Content: "hi"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAllChannelsFailed), "err should wrap ErrAllChannelsFailed")
	assert.Equal(t, int32(1), primary.Calls())
	assert.Equal(t, int32(1), fallback.Calls())
}

func TestManager_NoChannelsRegistered(t *testing.T) {
	mgr := NewManager(ManagerOptions{Primary: "primary"})
	err := mgr.Send(context.Background(), &Message{Type: MsgAlert, Content: "hi"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAllChannelsFailed))
}

func TestManager_PrimaryUnhealthySkipped(t *testing.T) {
	primary := newFake("primary")
	primary.healthy.Store(false) // breaker open
	fallback := newFake("fallback")
	mgr := NewManager(ManagerOptions{
		Primary:  "primary",
		Fallback: []string{"fallback"},
		Retry:    RetryConfig{Attempts: 1, InitialBackoff: time.Millisecond},
		Timeout:  100 * time.Millisecond,
	})
	mgr.RegisterChannel(primary.Name(), primary)
	mgr.RegisterChannel(fallback.Name(), fallback)

	err := mgr.Send(context.Background(), &Message{Type: MsgAlert, Content: "hi"})
	require.NoError(t, err)
	assert.Equal(t, int32(0), primary.Calls(), "unhealthy channel must not be called")
	assert.Equal(t, int32(1), fallback.Calls())
}

func TestManager_PrimaryUnknownSkipped(t *testing.T) {
	mgr := NewManager(ManagerOptions{
		Primary:  "does-not-exist",
		Fallback: []string{"fallback"},
		Retry:    RetryConfig{Attempts: 1, InitialBackoff: time.Millisecond},
		Timeout:  100 * time.Millisecond,
	})
	fallback := newFake("fallback")
	mgr.RegisterChannel(fallback.Name(), fallback)

	err := mgr.Send(context.Background(), &Message{Type: MsgAlert, Content: "hi"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), fallback.Calls())
}

func TestManager_NilMessageReturnsError(t *testing.T) {
	mgr := NewManager(ManagerOptions{Primary: "primary"})
	primary := newFake("primary")
	mgr.RegisterChannel(primary.Name(), primary)

	err := mgr.Send(context.Background(), nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAllChannelsFailed))
	assert.Equal(t, int32(0), primary.Calls(), "nil message must not touch channels")
}

func TestManager_ListAndRegister(t *testing.T) {
	mgr := NewManager(ManagerOptions{Primary: "a"})
	mgr.RegisterChannel("a", newFake("a"))
	mgr.RegisterChannel("b", newFake("b"))
	mgr.RegisterChannel("", newFake("ignored"))
	mgr.RegisterChannel("c", nil)

	names := mgr.List()
	assert.ElementsMatch(t, []string{"a", "b"}, names)
}

func TestManager_RetryExhaustsThenFallsBack(t *testing.T) {
	// 3 transient errors then a final error → retry exhausted.
	primary := newFake("primary", errors.New("e1"), errors.New("e2"), errors.New("e3"))
	fallback := newFake("fallback")
	mgr := NewManager(ManagerOptions{
		Primary:  "primary",
		Fallback: []string{"fallback"},
		Retry:    RetryConfig{Attempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond},
		Timeout:  100 * time.Millisecond,
	})
	mgr.RegisterChannel(primary.Name(), primary)
	mgr.RegisterChannel(fallback.Name(), fallback)

	err := mgr.Send(context.Background(), &Message{Type: MsgAlert, Content: "hi"})
	require.NoError(t, err, "fallback should rescue the message")
	assert.Equal(t, int32(3), primary.Calls(), "primary should be retried 3 times")
	assert.Equal(t, int32(1), fallback.Calls())
}

func TestManager_ChannelRegistryIsConcurrencySafe(t *testing.T) {
	mgr := NewManager(ManagerOptions{Primary: "primary"})
	go mgr.RegisterChannel("primary", newFake("primary"))
	go mgr.RegisterChannel("primary", newFake("primary"))
	go mgr.List()
	_ = mgr
}

func TestBuildHealth_AllUnconfigured(t *testing.T) {
	snap := BuildHealth(map[string]*CircuitBreaker{})
	assert.False(t, snap.Healthy)
	assert.Equal(t, "unconfigured", snap.Channels["feishu"].State)
	assert.False(t, snap.Channels["feishu"].Registered)
}

func TestBuildHealth_AllHealthy(t *testing.T) {
	breakers := map[string]*CircuitBreaker{
		"feishu":   NewCircuitBreaker(CircuitConfig{}),
		"dingtalk": NewCircuitBreaker(CircuitConfig{}),
		"wecom":    NewCircuitBreaker(CircuitConfig{}),
	}
	snap := BuildHealth(breakers)
	assert.True(t, snap.Healthy)
	for name, ch := range snap.Channels {
		assert.True(t, ch.Registered, name)
		assert.True(t, ch.Healthy, name)
	}
}

func TestBuildHealth_OneOpen(t *testing.T) {
	feishu := NewCircuitBreaker(CircuitConfig{FailureThreshold: 1, Window: time.Minute, Cooldown: time.Minute})
	ok, _ := feishu.Allow()
	require.True(t, ok)
	feishu.OnFailure()
	require.Equal(t, StateOpen, feishu.State())

	breakers := map[string]*CircuitBreaker{
		"feishu":   feishu,
		"dingtalk": NewCircuitBreaker(CircuitConfig{}),
		"wecom":    NewCircuitBreaker(CircuitConfig{}),
	}
	snap := BuildHealth(breakers)
	assert.False(t, snap.Healthy, "any open breaker → overall unhealthy")
	assert.Equal(t, "open", snap.Channels["feishu"].State)
	assert.False(t, snap.Channels["feishu"].Healthy)
}

func TestNewTestPingHandler_NilManager(t *testing.T) {
	h, err := NewTestPingHandler(nil, nil)
	require.Error(t, err)
	assert.Nil(t, h)
}
