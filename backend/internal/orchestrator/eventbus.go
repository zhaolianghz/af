// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package orchestrator

import (
	"sync"
	"sync/atomic"
	"time"
)

// EventType is a string discriminator for the kinds of run-time
// events the executor publishes. The set is closed; new types go
// through a const here so the SSE handler can map them to event
// names without a free-form string check.
type EventType string

const (
	EventRunStarted   EventType = "run.started"
	EventRunCompleted EventType = "run.completed"
	EventNodeStarted  EventType = "node.started"
	EventNodeSuccess  EventType = "node.success"
	EventNodeFailed   EventType = "node.failed"
	EventNodeSkipped  EventType = "node.skipped"
	EventLog          EventType = "log"
)

// Event is a single point-in-time emission on a run. RunID and
// NodeID are exposed separately (rather than stuffed into Data) so
// the SSE transport can route without parsing JSON.
type Event struct {
	RunID     uint64         `json:"run_id"`
	NodeID    string         `json:"node_id,omitempty"`
	Type      EventType      `json:"type"`
	Timestamp time.Time      `json:"ts"`
	Data      map[string]any `json:"data,omitempty"`
}

// EventBus is the pub/sub contract used by Executor (publisher)
// and the SSE handler (subscriber). Implementations must be safe
// for concurrent use.
//
// Publish is non-blocking: when a subscriber's channel buffer is
// full, the event is dropped and a debug message is logged (we
// don't have a logger here, so we count drops via DroppedCount).
type EventBus interface {
	Subscribe(runID uint64) (<-chan Event, func())
	Publish(evt Event)
	DroppedCount() uint64
}

// NewMemBus returns the in-process bus used by Phase 1 / Phase 2.
// It is replaced with a Redis pub/sub bus in multi-instance
// deployments.
func NewMemBus() EventBus {
	return &memBus{
		subs: make(map[uint64][]*memSub),
	}
}

const memBusChanSize = 64

type memSub struct {
	ch chan Event
}

type memBus struct {
	mu      sync.RWMutex
	subs    map[uint64][]*memSub
	dropped atomic.Uint64
}

func (b *memBus) Subscribe(runID uint64) (<-chan Event, func()) {
	sub := &memSub{ch: make(chan Event, memBusChanSize)}
	b.mu.Lock()
	b.subs[runID] = append(b.subs[runID], sub)
	b.mu.Unlock()
	return sub.ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[runID]
		for i, s := range subs {
			if s == sub {
				b.subs[runID] = append(subs[:i], subs[i+1:]...)
				close(s.ch)
				break
			}
		}
		if len(b.subs[runID]) == 0 {
			delete(b.subs, runID)
		}
	}
}

func (b *memBus) Publish(evt Event) {
	// Hold the read lock across the entire send loop so a
	// concurrent unsubscribe cannot close a subscriber's channel
	// between our snapshot and the send. Unsubscribe closes the
	// channel under the write Lock (mutually exclusive with this
	// RLock), which makes a send-on-closed-channel panic
	// impossible. The per-send is non-blocking (select-default),
	// so a slow consumer can't wedge the publisher or block
	// another goroutine's unsubscribe.
	//
	// The dropped counter is atomic: many publishers can hold the
	// RLock concurrently, so a plain uint64 increment under RLock
	// would race.
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs[evt.RunID] {
		select {
		case s.ch <- evt:
		default:
			b.dropped.Add(1)
		}
	}
}

func (b *memBus) DroppedCount() uint64 {
	return b.dropped.Load()
}
