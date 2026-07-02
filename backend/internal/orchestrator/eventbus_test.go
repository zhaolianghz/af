// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for the in-memory EventBus. Covers subscribe/publish,
// multi-subscriber fan-out, drop on full buffer, and cleanup.
package orchestrator

import (
	"sync"
	"testing"
	"time"
)

func TestEventBus_PublishReceive(t *testing.T) {
	bus := NewMemBus()
	ch, unsub := bus.Subscribe(42)
	defer unsub()

	bus.Publish(Event{RunID: 42, Type: EventRunStarted, Timestamp: time.Now()})

	select {
	case evt := <-ch:
		if evt.RunID != 42 {
			t.Errorf("RunID: got %d", evt.RunID)
		}
		if evt.Type != EventRunStarted {
			t.Errorf("Type: got %q", evt.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventBus_FanOutToMultipleSubscribers(t *testing.T) {
	bus := NewMemBus()
	ch1, u1 := bus.Subscribe(1)
	ch2, u2 := bus.Subscribe(1)
	defer u1()
	defer u2()

	bus.Publish(Event{RunID: 1, Type: EventNodeStarted})
	bus.Publish(Event{RunID: 1, Type: EventNodeSuccess})

	for i, ch := range []<-chan Event{ch1, ch2} {
		var got []EventType
		timeout := time.After(200 * time.Millisecond)
		for len(got) < 2 {
			select {
			case evt := <-ch:
				got = append(got, evt.Type)
			case <-timeout:
				t.Fatalf("sub %d: only got %d events", i, len(got))
			}
		}
		if got[0] != EventNodeStarted || got[1] != EventNodeSuccess {
			t.Errorf("sub %d: ordering wrong: %v", i, got)
		}
	}
}

func TestEventBus_OnlyMatchingRunID(t *testing.T) {
	bus := NewMemBus()
	ch, unsub := bus.Subscribe(7)
	defer unsub()

	// Different run ID — should not be delivered.
	bus.Publish(Event{RunID: 8, Type: EventRunStarted})
	// Matching run ID — should be delivered.
	bus.Publish(Event{RunID: 7, Type: EventRunStarted})

	select {
	case evt := <-ch:
		if evt.RunID != 7 {
			t.Errorf("RunID: got %d want 7", evt.RunID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected one event, got none")
	}
	// And there should be no second event.
	select {
	case evt := <-ch:
		t.Errorf("unexpected second event: %+v", evt)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEventBus_DropOnFullBuffer(t *testing.T) {
	bus := NewMemBus()
	ch, unsub := bus.Subscribe(1)
	defer unsub()

	// Fill the buffer + 1 more so one is dropped.
	for i := 0; i < memBusChanSize+5; i++ {
		bus.Publish(Event{RunID: 1, Type: EventNodeStarted})
	}
	if got := bus.DroppedCount(); got != 5 {
		t.Errorf("DroppedCount: got %d want 5", got)
	}
	// Drain the channel.
	drained := 0
	timeout := time.After(50 * time.Millisecond)
drain:
	for {
		select {
		case <-ch:
			drained++
		case <-timeout:
			break drain
		}
	}
	if drained != memBusChanSize {
		t.Errorf("drained: got %d want %d", drained, memBusChanSize)
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewMemBus()
	ch1, u1 := bus.Subscribe(1)
	ch2, u2 := bus.Subscribe(1)

	u1()
	bus.Publish(Event{RunID: 1, Type: EventRunStarted})

	// ch1 should be closed; ch2 should receive the event.
	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("ch1 should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("ch1 should be closed promptly")
	}
	select {
	case <-ch2:
		// good
	case <-time.After(100 * time.Millisecond):
		t.Error("ch2 should have received event")
	}
	u2()
}

func TestEventBus_UnsubscribeIdempotent(t *testing.T) {
	bus := NewMemBus()
	_, unsub := bus.Subscribe(1)
	unsub()
	unsub() // must not panic
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := NewMemBus()
	ch, unsub := bus.Subscribe(1)
	defer unsub()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				bus.Publish(Event{RunID: 1, Type: EventNodeStarted})
			}
		}()
	}
	wg.Wait()
	// We don't assert exact received count (drops are OK
	// here); we just want no panics.
	count := 0
	timeout := time.After(100 * time.Millisecond)
loop:
	for {
		select {
		case <-ch:
			count++
		case <-timeout:
			break loop
		}
	}
	if count == 0 {
		t.Error("expected at least some events delivered")
	}
}
