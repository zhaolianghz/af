// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for Executor: linear DAGs, diamond DAGs, error cascade,
// context cancellation, single-node trial, and the event bus
// hookup.
//
// KNOWN ISSUE — production-code race
// -----------------------------------
// Executor.Execute uses errgroup.WithContext and a separate
// coordinator goroutine. The coordinator's loop only adds the
// "next" node to the errgroup after a predecessor completes, so
// the errgroup's counter can drop to zero between two adjacent
// steps. When that happens, errgroup cancels its derived
// context, the coordinator observes gctx.Err() on its next
// iteration, and the remaining nodes are never scheduled.
// Symptom: NodeResults is empty / partially empty and Status is
// "success" (or "partial").
//
// This is a real race in the production code. Even single-node
// DAGs hit it: the main goroutine reaches g.Wait() before the
// coordinator has had a chance to call g.Go, the errgroup
// counter is 0, g.Wait() returns immediately, and gctx is
// cancelled before the coordinator's first iteration can do
// useful work.
//
// The barrier-based single-node test below correctly
// identifies this race — it fails today with "Execute returned
// before any node was called". All Execute/ExecuteToNode
// integration tests are skipped with t.Skip() until the
// production race is fixed. Re-enable by removing the t.Skip
// calls and the `_Skipped_Race` suffix from the test names.
package orchestrator

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// recordingNode runs successfully and records its own ID in
// the order it was called. The Payload it returns is a small
// map so downstream nodes can read inputs by predecessor ID.
//
// `started`, if non-nil, receives the node's ID right before
// it returns. This is the synchronization point tests use to
// wait until g.Go has actually been called by the coordinator
// (avoiding the spawn-vs-wait race — see file header).
type recordingNode struct {
	typ     string
	delay   time.Duration
	failOn  bool
	payload map[string]any
	called  *[]string
	started chan string
}

// recordMu guards the shared *called slice that diamond/parallel
// tests pass to multiple recordingNodes. Before the executor ran
// nodes concurrently the append was single-threaded; now that the
// scheduler genuinely parallelizes independent branches, the
// shared slice needs a lock (test-only).
var recordMu sync.Mutex

func (r *recordingNode) Type() string    { return r.typ }
func (r *recordingNode) Subtype() string { return "" }
func (r *recordingNode) Schema() NodeSchema {
	return NodeSchema{Description: "recording " + r.typ}
}
func (r *recordingNode) Run(ctx context.Context, rc *RunContext, in map[string]any) (map[string]any, error) {
	if r.started != nil {
		// Non-blocking send: we only need the first signal.
		select {
		case r.started <- r.typ:
		default:
		}
	}
	if r.called != nil {
		recordMu.Lock()
		*r.called = append(*r.called, r.typ)
		recordMu.Unlock()
	}
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if r.failOn {
		return nil, errors.New("forced failure: " + r.typ)
	}
	out := r.payload
	if out == nil {
		out = map[string]any{"from": r.typ}
	}
	return out, nil
}

func newLinearDAG(ids ...string) *DAG {
	nodes := make([]NodeDef, len(ids))
	edges := make([]Edge, 0, len(ids)-1)
	for i, id := range ids {
		nodes[i] = NodeDef{ID: id, Type: id}
		if i > 0 {
			edges = append(edges, Edge{ID: "e" + id, Source: ids[i-1], Target: id})
		}
	}
	return &DAG{Nodes: nodes, Edges: edges}
}

func newDiamondDAG() *DAG {
	return &DAG{
		Nodes: []NodeDef{
			{ID: "a", Type: "a"},
			{ID: "b", Type: "b"},
			{ID: "c", Type: "c"},
			{ID: "d", Type: "d"},
		},
		Edges: []Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "a", Target: "c"},
			{ID: "e3", Source: "b", Target: "d"},
			{ID: "e4", Source: "c", Target: "d"},
		},
	}
}

func newReg(nodes ...Node) *Registry {
	r := NewRegistry()
	for _, n := range nodes {
		if err := r.Register(n); err != nil {
			panic(err)
		}
	}
	return r
}

func newExecRC(reg *Registry, bus EventBus) (*Executor, *RunContext) {
	e := NewExecutor(ExecutorConfig{
		Registry:       reg,
		Logger:         zap.NewNop(),
		MaxConcurrency: 4,
		DefaultTimeout: 1 * time.Second,
	})
	rc := NewRunContext(RunContextOptions{
		Logger: zap.NewNop(),
		Bus:    bus,
		RunID:  1,
	})
	return e, rc
}

// =============================================================================
// Happy-path execution (single-node DAGs only — see file header for race)
// =============================================================================

func TestExecutor_SingleNode_RaceDocumented(t *testing.T) {
	// TODO: re-enable when the spawn-vs-wait race in
	// Executor.Execute is fixed (see file header). When
	// the race is fixed, this test should pass with no
	// changes — the barrier correctly waits for the node
	// to be scheduled before checking the result.
	var called []string
	started := make(chan string, 1)
	reg := newReg(&recordingNode{typ: "a", called: &called, started: started})
	bus := NewMemBus()
	e, rc := newExecRC(reg, bus)

	// Run Execute in a goroutine and wait for the node's Run
	// to be called before checking the result. This dodges
	// the spawn-vs-wait race (see file header).
	type res struct {
		s   *RunSummary
		err error
	}
	done := make(chan res, 1)
	go func() {
		s, err := e.Execute(context.Background(), rc, &RunRequest{DAG: newLinearDAG("a")})
		done <- res{s, err}
	}()

	select {
	case id := <-started:
		if id != "a" {
			t.Errorf("started: got %q want a", id)
		}
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Execute: %v", r.err)
		}
		t.Fatal("Execute returned before any node was called (race not avoided)")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: no node called")
	}

	var summary *RunSummary
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Execute: %v (status=%q error=%q)", r.err, r.s.Status, r.s.Error)
		}
		summary = r.s
	case <-time.After(2 * time.Second):
		t.Fatal("Execute didn't return")
	}

	if summary.Status != StatusSuccess {
		t.Errorf("Status: got %q want %q (error=%q)", summary.Status, StatusSuccess, summary.Error)
	}
	if len(summary.NodeResults) != 1 {
		t.Errorf("NodeResults: got %d", len(summary.NodeResults))
	}
	r, ok := summary.NodeResults["a"]
	if !ok {
		t.Fatalf("missing result for a")
	}
	if r.Status != StatusSuccess {
		t.Errorf("a: status %q", r.Status)
	}
	if got := called; len(got) != 1 || got[0] != "a" {
		t.Errorf("call order: %v", got)
	}
}

func TestExecutor_Linear_Skipped_Race(t *testing.T) {
	// TODO: re-enable when the coordinator race in
	// Executor.Execute (see file header) is fixed.
	var called []string
	reg := newReg(
		&recordingNode{typ: "a", called: &called},
		&recordingNode{typ: "b", called: &called},
		&recordingNode{typ: "c", called: &called},
	)
	bus := NewMemBus()
	e, rc := newExecRC(reg, bus)
	dag := newLinearDAG("a", "b", "c")
	summary, err := e.Execute(context.Background(), rc, &RunRequest{DAG: dag})
	if err != nil {
		t.Fatalf("Execute: %v (status=%q error=%q)", err, summary.Status, summary.Error)
	}
	if summary.Status != StatusSuccess {
		t.Errorf("Status: got %q want %q (error=%q)", summary.Status, StatusSuccess, summary.Error)
	}
	if len(summary.NodeResults) != 3 {
		t.Errorf("NodeResults: got %d", len(summary.NodeResults))
	}
	for _, id := range []string{"a", "b", "c"} {
		r, ok := summary.NodeResults[id]
		if !ok {
			t.Errorf("missing result for %s", id)
			continue
		}
		if r.Status != StatusSuccess {
			t.Errorf("%s: status %q", id, r.Status)
		}
	}
	if got := called; len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("call order: %v", got)
	}
}

func TestExecutor_Diamond_Skipped_Race(t *testing.T) {
	// TODO: re-enable when the coordinator race in
	// Executor.Execute (see file header) is fixed.
	var called []string
	reg := newReg(
		&recordingNode{typ: "a", called: &called},
		&recordingNode{typ: "b", called: &called},
		&recordingNode{typ: "c", called: &called},
		&recordingNode{typ: "d", called: &called},
	)
	bus := NewMemBus()
	e, rc := newExecRC(reg, bus)
	summary, err := e.Execute(context.Background(), rc, &RunRequest{DAG: newDiamondDAG()})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if summary.Status != StatusSuccess {
		t.Errorf("Status: got %q want %q", summary.Status, StatusSuccess)
	}
	if len(summary.NodeResults) != 4 {
		t.Errorf("NodeResults: got %d", len(summary.NodeResults))
	}
	// a must run first; d must run last.
	if len(called) >= 4 {
		if called[0] != "a" || called[3] != "d" {
			t.Errorf("call order: %v", called)
		}
	}
}

func TestExecutor_InputsPropagate_Skipped_Race(t *testing.T) {
	// b reads a's payload (under the key "a") and echoes it
	// in its own payload under "got".
	bNode := &captureNode{typ: "b"}
	aNode := &recordingNode{typ: "a", payload: map[string]any{"value": 42}}
	reg := newReg(aNode, bNode)
	e, rc := newExecRC(reg, NewMemBus())
	_, err := e.Execute(context.Background(), rc, &RunRequest{DAG: newLinearDAG("a", "b")})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if bNode.got["a"] == nil {
		t.Fatalf("b did not receive a's payload; got=%+v", bNode.got)
	}
	mp, ok := bNode.got["a"].(map[string]any)
	if !ok {
		t.Fatalf("a payload type: %T", bNode.got["a"])
	}
	if mp["value"] != 42 {
		t.Errorf("a payload value: got %v want 42", mp["value"])
	}
}

type captureNode struct {
	typ string
	got map[string]any
}

func (c *captureNode) Type() string    { return c.typ }
func (c *captureNode) Subtype() string { return "" }
func (c *captureNode) Schema() NodeSchema {
	return NodeSchema{Description: "capture " + c.typ}
}
func (c *captureNode) Run(ctx context.Context, rc *RunContext, in map[string]any) (map[string]any, error) {
	c.got = make(map[string]any, len(in))
	for k, v := range in {
		// Strip reserved keys for the assertion.
		if k == InputKeyParams || k == InputKeyType || k == InputKeySubtype || k == InputKeyID {
			continue
		}
		c.got[k] = v
	}
	return map[string]any{"ok": true}, nil
}

// =============================================================================
// Error cascade
// =============================================================================

func TestExecutor_ErrorCascade_Skipped_Race(t *testing.T) {
	// a → b (fails) → c
	//          ↘ d
	reg := newReg(
		&recordingNode{typ: "a"},
		&recordingNode{typ: "b", failOn: true},
		&recordingNode{typ: "c"},
		&recordingNode{typ: "d"},
	)
	e, rc := newExecRC(reg, NewMemBus())
	dag := &DAG{
		Nodes: []NodeDef{
			{ID: "a", Type: "a"},
			{ID: "b", Type: "b"},
			{ID: "c", Type: "c"},
			{ID: "d", Type: "d"},
		},
		Edges: []Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "c"},
			{ID: "e3", Source: "a", Target: "d"},
		},
	}
	summary, err := e.Execute(context.Background(), rc, &RunRequest{DAG: dag})
	if err == nil {
		t.Error("expected error from failed run")
	}
	if summary == nil {
		t.Fatal("summary should be non-nil even on failure")
	}
	if summary.Status != StatusFailed {
		t.Errorf("Status: got %q want %q", summary.Status, StatusFailed)
	}
	// b is failed; c is skipped; d is success.
	if summary.NodeResults["b"].Status != StatusFailed {
		t.Errorf("b: got %q want %q", summary.NodeResults["b"].Status, StatusFailed)
	}
	if summary.NodeResults["c"].Status != StatusSkipped {
		t.Errorf("c: got %q want %q", summary.NodeResults["c"].Status, StatusSkipped)
	}
	if summary.NodeResults["c"].SkipReason != SkipReasonUpstreamFailed {
		t.Errorf("c skip reason: got %q", summary.NodeResults["c"].SkipReason)
	}
	if summary.NodeResults["d"].Status != StatusSuccess {
		t.Errorf("d: got %q want success", summary.NodeResults["d"].Status)
	}
}

// =============================================================================
// Unknown type
// =============================================================================

func TestExecutor_UnknownType(t *testing.T) {
	reg := newReg(&recordingNode{typ: "known"})
	e, rc := newExecRC(reg, NewMemBus())
	summary, err := e.Execute(context.Background(), rc, &RunRequest{
		DAG: &DAG{
			Nodes: []NodeDef{
				{ID: "a", Type: "known"},
				{ID: "b", Type: "ghost"},
			},
			Edges: []Edge{{Source: "a", Target: "b"}},
		},
	})
	if !errors.Is(err, ErrUnknownType) {
		t.Errorf("err: got %v want ErrUnknownType", err)
	}
	if summary == nil || summary.Status != StatusFailed {
		t.Errorf("summary should be non-nil and failed, got %+v", summary)
	}
	if summary.Error == "" {
		t.Error("summary.Error should be populated")
	}
}

// =============================================================================
// Nil DAG
// =============================================================================

func TestExecutor_NilRequest(t *testing.T) {
	e, rc := newExecRC(NewRegistry(), NewMemBus())
	if _, err := e.Execute(context.Background(), rc, nil); !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("Execute(nil): got %v want ErrInvalidDAG", err)
	}
}

func TestExecutor_InvalidDAG(t *testing.T) {
	e, rc := newExecRC(NewRegistry(), NewMemBus())
	// Empty DAG.
	_, err := e.Execute(context.Background(), rc, &RunRequest{DAG: &DAG{}})
	if !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("empty DAG: got %v want ErrInvalidDAG", err)
	}
}

// =============================================================================
// Event bus hooks
// =============================================================================

func TestExecutor_PublishesRunEvents_RaceDocumented(t *testing.T) {
	// TODO: re-enable when the spawn-vs-wait race in
	// Executor.Execute is fixed.
	started := make(chan string, 1)
	reg := newReg(&recordingNode{typ: "a", started: started})
	bus := NewMemBus()
	e, rc := newExecRC(reg, bus)
	ch, unsub := bus.Subscribe(rc.RunID)
	defer unsub()

	// Run Execute in a goroutine and wait for the node to be
	// called before we begin collecting events. The barrier
	// ensures the coordinator has scheduled the work (see
	// file header for the spawn-vs-wait race).
	type res struct {
		err error
	}
	done := make(chan res, 1)
	go func() {
		_, err := e.Execute(context.Background(), rc, &RunRequest{DAG: newLinearDAG("a")})
		done <- res{err}
	}()

	select {
	case <-started:
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Execute: %v", r.err)
		}
		t.Fatal("Execute returned before any node was called (race not avoided)")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: no node called")
	}

	if err := (<-done).err; err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Expect at least RunStarted + NodeStarted + NodeSuccess + RunCompleted.
	types := map[EventType]int{}
	timeout := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case evt := <-ch:
			types[evt.Type]++
		case <-timeout:
			break loop
		}
	}
	if types[EventRunStarted] == 0 {
		t.Error("no EventRunStarted")
	}
	if types[EventRunCompleted] == 0 {
		t.Error("no EventRunCompleted")
	}
	if types[EventNodeStarted] == 0 {
		t.Error("no EventNodeStarted")
	}
	if types[EventNodeSuccess] == 0 {
		t.Error("no EventNodeSuccess")
	}
}

// =============================================================================
// Context cancellation
// =============================================================================

func TestExecutor_ContextAlreadyCancelled(t *testing.T) {
	reg := newReg(&recordingNode{typ: "a"})
	e, rc := newExecRC(reg, NewMemBus())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	summary, err := e.Execute(ctx, rc, &RunRequest{DAG: newLinearDAG("a")})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Node was skipped because the context was already done.
	r, ok := summary.NodeResults["a"]
	if !ok {
		t.Fatal("missing result for a")
	}
	if r.Status != StatusSkipped || r.SkipReason != SkipReasonCancelled {
		t.Errorf("a: status=%q reason=%q", r.Status, r.SkipReason)
	}
}

func TestExecutor_ContextCancelledMidRun_Skipped_Race(t *testing.T) {
	// a runs forever; we cancel mid-run. Children are skipped.
	a := &recordingNode{typ: "a", delay: 500 * time.Millisecond}
	b := &recordingNode{typ: "b"}
	reg := newReg(a, b)
	e, rc := newExecRC(reg, NewMemBus())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	summary, err := e.Execute(ctx, rc, &RunRequest{DAG: newLinearDAG("a", "b")})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// a was cancelled.
	if r := summary.NodeResults["a"]; r.Status != StatusSkipped {
		t.Errorf("a: status=%q", r.Status)
	}
	// b was skipped.
	if r := summary.NodeResults["b"]; r.Status != StatusSkipped {
		t.Errorf("b: status=%q", r.Status)
	}
}

// =============================================================================
// ExecuteToNode
// =============================================================================

func TestExecutor_ExecuteToNode_Skipped_Race(t *testing.T) {
	var called atomic.Int32
	a := &countingNode{typ: "a", counter: &called}
	b := &countingNode{typ: "b", counter: &called}
	c := &countingNode{typ: "c", counter: &called}
	d := &countingNode{typ: "d", counter: &called}
	reg := newReg(a, b, c, d)
	e, rc := newExecRC(reg, NewMemBus())

	summary, err := e.ExecuteToNode(context.Background(), rc,
		&RunRequest{DAG: newDiamondDAG()}, "c")
	if err != nil {
		t.Fatalf("ExecuteToNode: %v", err)
	}
	if got := called.Load(); got != 2 {
		t.Errorf("called: got %d want 2", got)
	}
	if r := summary.NodeResults["b"]; r.Status != StatusSkipped || r.SkipReason != SkipReasonNotInPath {
		t.Errorf("b: status=%q reason=%q", r.Status, r.SkipReason)
	}
	if r := summary.NodeResults["d"]; r.Status != StatusSkipped || r.SkipReason != SkipReasonNotInPath {
		t.Errorf("d: status=%q reason=%q", r.Status, r.SkipReason)
	}
}

func TestExecutor_ExecuteToNode_UnknownTarget(t *testing.T) {
	reg := newReg(&recordingNode{typ: "a"})
	e, rc := newExecRC(reg, NewMemBus())
	_, err := e.ExecuteToNode(context.Background(), rc,
		&RunRequest{DAG: newLinearDAG("a")}, "ghost")
	if !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("unknown target: got %v want ErrNodeNotFound", err)
	}
}

// countingNode increments a counter on every call.
type countingNode struct {
	typ     string
	counter *atomic.Int32
}

func (c *countingNode) Type() string    { return c.typ }
func (c *countingNode) Subtype() string { return "" }
func (c *countingNode) Schema() NodeSchema {
	return NodeSchema{Description: "counting " + c.typ}
}
func (c *countingNode) Run(ctx context.Context, rc *RunContext, in map[string]any) (map[string]any, error) {
	c.counter.Add(1)
	return map[string]any{"from": c.typ}, nil
}

// =============================================================================
// collectDescendants (indirectly covered by error cascade test).
// =============================================================================

func TestCollectDescendants(t *testing.T) {
	dag := &DAG{
		Nodes: []NodeDef{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}},
		Edges: []Edge{
			{Source: "a", Target: "b"},
			{Source: "b", Target: "c"},
			{Source: "c", Target: "d"},
			{Source: "a", Target: "e"},
		},
	}
	// BFS from "a": a's children are b then e (in edge
	// order); then b's child c; then c's child d. The BFS
	// queue pops e before c, so the order is b, e, c, d.
	got := collectDescendants("a", dag)
	want := []string{"b", "e", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d (got=%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestAggregateStatus(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]NodeResult
		want string
	}{
		{name: "all success", in: map[string]NodeResult{
			"a": {Status: StatusSuccess},
			"b": {Status: StatusSuccess},
		}, want: StatusSuccess},
		{name: "all skipped", in: map[string]NodeResult{
			"a": {Status: StatusSkipped},
		}, want: "partial"},
		{name: "mix success+skipped", in: map[string]NodeResult{
			"a": {Status: StatusSuccess},
			"b": {Status: StatusSkipped},
		}, want: "partial"},
		{name: "any failure", in: map[string]NodeResult{
			"a": {Status: StatusSuccess},
			"b": {Status: StatusFailed},
		}, want: StatusFailed},
		{name: "empty", in: map[string]NodeResult{}, want: StatusSuccess},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateStatus(tc.in); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestNewExecutor_Defaults(t *testing.T) {
	e := NewExecutor(ExecutorConfig{})
	if e.maxConcurrency != 4 {
		t.Errorf("maxConcurrency: got %d want 4", e.maxConcurrency)
	}
	if e.defaultTimeout != 60*time.Second {
		t.Errorf("defaultTimeout: got %v want 60s", e.defaultTimeout)
	}
	if e.registry == nil {
		t.Error("registry should default to empty")
	}
	if e.logger == nil {
		t.Error("logger should default to nop")
	}
}

// =============================================================================
// ExecuteToNode — error paths + single-node happy path (C2a coverage).
// Multi-node DAGs hit the documented goroutine race, so we only assert
// the error branches and a 1-node target here.
// =============================================================================

func TestExecuteToNode_NilRequest(t *testing.T) {
	e, rc := newExecRC(NewRegistry(), NewMemBus())
	if _, err := e.ExecuteToNode(context.Background(), rc, nil, "a"); !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("ExecuteToNode(nil): got %v, want ErrInvalidDAG", err)
	}
}

func TestExecuteToNode_TargetNotFound(t *testing.T) {
	e, rc := newExecRC(NewRegistry(), NewMemBus())
	req := &RunRequest{DAG: newLinearDAG("a")}
	if _, err := e.ExecuteToNode(context.Background(), rc, req, "zzz"); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("ExecuteToNode(bad target): got %v, want ErrNodeNotFound", err)
	}
}

// =============================================================================
// Worker panic recovery (#6): a panicking node must not crash the
// process; it is converted into a failed NodeResult and the run ends
// with status "failed". Without the recover in dispatch(), the panic
// would propagate out of the worker goroutine and terminate the test
// binary (and, in production, the whole server).
// =============================================================================

type panicNode struct {
	typ string
}

func (p *panicNode) Type() string    { return p.typ }
func (p *panicNode) Subtype() string { return "" }
func (p *panicNode) Schema() NodeSchema {
	return NodeSchema{Description: "panicking " + p.typ}
}
func (p *panicNode) Run(ctx context.Context, rc *RunContext, in map[string]any) (map[string]any, error) {
	panic("forced panic from " + p.typ)
}

func TestExecutor_NodePanicRecovered(t *testing.T) {
	reg := newReg(&panicNode{typ: "a"})
	e, rc := newExecRC(reg, NewMemBus())

	summary, err := e.Execute(context.Background(), rc, &RunRequest{DAG: newLinearDAG("a")})

	// The panic must surface as a failed node (firstErr), not a crash.
	if err == nil {
		t.Error("expected error from panicked node")
	}
	if summary == nil {
		t.Fatal("summary should be non-nil even on panic")
	}
	if summary.Status != StatusFailed {
		t.Errorf("Status: got %q want %q", summary.Status, StatusFailed)
	}
	r, ok := summary.NodeResults["a"]
	if !ok {
		t.Fatal("missing result for panicked node a")
	}
	if r.Status != StatusFailed {
		t.Errorf("a: status %q want %q", r.Status, StatusFailed)
	}
	if r.Error == "" {
		t.Error("a: expected non-empty error describing the panic")
	}
}

// TestExecutor_NodePanicDoesNotDeadlockConcurrency verifies that a
// panicking node releases its concurrency slot, so a subsequent ready
// node can still be dispatched. Without the deferred semaphore release,
// enough panics would leak all slots and wedge the scheduler.
func TestExecutor_NodePanicDoesNotDeadlockConcurrency(t *testing.T) {
	// maxConcurrency=1: one slot. Two roots: a panics, b must still
	// be able to run after a's slot is freed.
	e := NewExecutor(ExecutorConfig{
		Registry:       newReg(&panicNode{typ: "a"}, &recordingNode{typ: "b"}),
		Logger:         zap.NewNop(),
		MaxConcurrency: 1,
		DefaultTimeout: 1 * time.Second,
	})
	rc := NewRunContext(RunContextOptions{Logger: zap.NewNop(), Bus: NewMemBus(), RunID: 1})

	done := make(chan struct{})
	go func() {
		defer close(done)
		summary, err := e.Execute(context.Background(), rc, &RunRequest{DAG: &DAG{
			Nodes: []NodeDef{{ID: "a", Type: "a"}, {ID: "b", Type: "b"}},
		}})
		if err == nil {
			t.Errorf("expected error from panicked node a")
		}
		if summary == nil || summary.NodeResults["b"].Status != StatusSuccess {
			t.Errorf("b should still succeed after a's panic; got %+v", summary)
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Execute deadlocked: panicked node leaked its concurrency slot")
	}
}
