// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Executor drives a DAG to completion. It is safe for concurrent
// use across multiple runs.
type Executor struct {
	registry *Registry
	logger   *zap.Logger

	maxConcurrency int
	defaultTimeout time.Duration
}

// ExecutorConfig configures NewExecutor.
type ExecutorConfig struct {
	Registry       *Registry
	Logger         *zap.Logger
	MaxConcurrency int
	DefaultTimeout time.Duration
}

// NewExecutor builds an Executor with safe defaults.
func NewExecutor(cfg ExecutorConfig) *Executor {
	if cfg.Registry == nil {
		cfg.Registry = NewRegistry()
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 4
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 60 * time.Second
	}
	return &Executor{
		registry:       cfg.Registry,
		logger:         cfg.Logger,
		maxConcurrency: cfg.MaxConcurrency,
		defaultTimeout: cfg.DefaultTimeout,
	}
}

// Execute runs the DAG to completion. The returned RunSummary is
// non-nil even on error (partial results live in
// summary.NodeResults).
//
// Execution model:
//
//  1. Topologically sort nodes (deterministic, Kahn's algorithm).
//  2. For each node, track a "pending predecessors" counter.
//  3. An errgroup with SetLimit(MaxConcurrency) spawns a
//     goroutine per ready node. A ready node is one whose counter
//     has reached zero. The pump goroutine below promotes nodes
//     from "pending" to "ready" as predecessors complete.
//  4. On node failure, all transitive descendants are marked
//     skipped (with SkipReasonUpstreamFailed) and not executed.
//  5. On context cancellation, unstarted nodes are marked
//     skipped with SkipReasonCancelled.
//  6. Final status: success when every node succeeded; partial
//     when at least one succeeded and at least one was skipped;
//     failed when a node that was required to succeed returned
//     an error.
func (e *Executor) Execute(ctx context.Context, rc *RunContext, req *RunRequest) (*RunSummary, error) {
	if req == nil || req.DAG == nil {
		return nil, fmt.Errorf("%w: nil request/dag", ErrInvalidDAG)
	}
	if err := req.DAG.Validate(); err != nil {
		return nil, err
	}

	summary := &RunSummary{
		RunID:       rc.RunID,
		StrategyID:  rc.StrategyID,
		DryRun:      req.DryRun,
		NodeResults: make(map[string]NodeResult, len(req.DAG.Nodes)),
		StartedAt:   rc.Now(),
	}

	// Pre-validate that every referenced node type is registered.
	// This is a fast-fail: a single unknown type aborts the
	// whole run with a clear error.
	for _, n := range req.DAG.Nodes {
		if _, ok := e.registry.GetBySubtype(n.Type, n.Subtype); !ok {
			wrapped := fmt.Errorf("%w: type=%q subtype=%q (node %q)", ErrUnknownType, n.Type, n.Subtype, n.ID)
			summary.Status = StatusFailed
			summary.Error = wrapped.Error()
			summary.FinishedAt = rc.Now()
			summary.Duration = summary.FinishedAt.Sub(summary.StartedAt)
			return summary, wrapped
		}
	}

	// pending tracks, per node, the number of predecessors
	// still pending. Atomic via pumpMu.
	var (
		pumpMu    sync.Mutex
		pending   = make(map[string]int, len(req.DAG.Nodes))
		ready     = append([]string(nil), req.DAG.Roots()...)
		completed = make(map[string]string, len(req.DAG.Nodes))
	)

	for _, n := range req.DAG.Nodes {
		pending[n.ID] = len(req.DAG.Predecessors(n.ID))
	}
	sortStrings(ready)

	// resultsMu guards summary.NodeResults and payloads.
	var (
		resultsMu sync.Mutex
		payloads  = make(map[string]map[string]any, len(req.DAG.Nodes))
	)

	recordResult := func(res NodeResult) {
		resultsMu.Lock()
		completed[res.NodeID] = res.Status
		summary.NodeResults[res.NodeID] = res
		if res.Payload != nil {
			payloads[res.NodeID] = res.Payload
		}
		resultsMu.Unlock()
	}

	// nextReady pops the next ready node, or "" if the queue is
	// empty. Called only from the main scheduler goroutine; the
	// pumpMu lock is retained for defense in depth.
	nextReady := func() string {
		pumpMu.Lock()
		defer pumpMu.Unlock()
		if len(ready) == 0 {
			return ""
		}
		head := ready[0]
		ready = ready[1:]
		return head
	}

	// enqueueChildren decrements pending[] for each child of
	// `nodeID` and pushes the ones that just hit zero onto
	// `ready` (sorted, for deterministic order). The completed[]
	// read is guarded by resultsMu because worker goroutines
	// read the same map inside runOneNode.
	enqueueChildren := func(nodeID string) {
		for _, child := range req.DAG.Successors(nodeID) {
			resultsMu.Lock()
			_, alreadyDone := completed[child]
			resultsMu.Unlock()
			if alreadyDone {
				continue
			}
			pumpMu.Lock()
			pending[child]--
			if pending[child] <= 0 {
				ready = insertSorted(ready, child)
			}
			pumpMu.Unlock()
		}
	}

	// onNodeDone records a result and, on failure, cascades a
	// skip through every transitive descendant so they are never
	// dispatched. Runs serially in the scheduler loop. The
	// cascade mutates completed/summary under resultsMu since
	// in-flight workers read those maps concurrently.
	onNodeDone := func(res NodeResult) {
		recordResult(res)
		if res.Status == StatusFailed {
			now := rc.Now()
			resultsMu.Lock()
			for _, id := range collectDescendants(res.NodeID, req.DAG) {
				if _, d := completed[id]; d {
					continue
				}
				completed[id] = StatusSkipped
				summary.NodeResults[id] = NodeResult{
					NodeID:     id,
					Status:     StatusSkipped,
					SkipReason: SkipReasonUpstreamFailed,
					StartedAt:  now,
					FinishedAt: now,
				}
			}
			resultsMu.Unlock()
		}
		enqueueChildren(res.NodeID)
	}

	if rc.Bus != nil {
		rc.Bus.Publish(Event{
			RunID: rc.RunID, Type: EventRunStarted, Timestamp: rc.Now(),
			Data: map[string]any{"strategy_id": rc.StrategyID, "dry_run": req.DryRun},
		})
	}

	// ---------------------------------------------------------------
	// Scheduler (race-free rewrite).
	//
	// The previous implementation used errgroup + a separate
	// coordinator goroutine that only called g.Go AFTER a
	// predecessor finished. The main goroutine could reach
	// g.Wait() while the errgroup counter was still 0, so Wait
	// returned immediately, the derived context was cancelled,
	// and the coordinator skipped every remaining node — a run
	// that did nothing yet reported "success".
	//
	// This version makes the MAIN goroutine the scheduler. Worker
	// goroutines only ever call runOneNode (which locks resultsMu
	// for its reads) and report back on `done`. All mutation of
	// ready / pending / completed / summary happens serially in
	// the main loop, so there is no spawn-vs-wait window: the loop
	// blocks on `done` until every dispatched node reports, and it
	// only exits when inFlight == 0 with nothing left ready.
	maxConc := e.maxConcurrency
	if maxConc < 1 {
		maxConc = 1
	}
	sem := make(chan struct{}, maxConc)
	done := make(chan NodeResult)
	inFlight := 0

	// firstErr captures the first node failure so Execute can
	// return it alongside the summary (parity with the old
	// errgroup error).
	var firstErr error

	// dispatch launches a worker for nodeID. The semaphore is
	// acquired INSIDE the goroutine so dispatching never blocks
	// the scheduler loop; actual concurrent execution is still
	// bounded by maxConc.
	dispatch := func(nodeID string) {
		inFlight++
		go func() {
			sem <- struct{}{}
			// Released in LIFO order after the recover below, so a
			// panicking node still frees its concurrency slot and
			// cannot starve/deadlock the scheduler.
			defer func() { <-sem }()
			// A panic inside a node (or in Bus.Publish during event
			// emission) must never crash the process: gin's Recovery
			// middleware only covers the HTTP handler goroutine, not
			// these worker goroutines. Recover and convert the panic
			// into a failed NodeResult so the scheduler loop stays
			// consistent and the run surfaces the failure instead of
			// taking the whole server down. (Runs as the second defer,
			// i.e. before the semaphore release above.)
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("executor: node panic recovered",
						zap.String("node_id", nodeID),
						zap.Any("panic", r),
						zap.ByteString("stack", debug.Stack()),
					)
					done <- NodeResult{
						NodeID:     nodeID,
						Status:     StatusFailed,
						Error:      fmt.Sprintf("node %q panicked: %v", nodeID, r),
						StartedAt:  rc.Now(),
						FinishedAt: rc.Now(),
					}
				}
			}()
			res := e.runOneNode(ctx, rc, req, nodeID, payloads, &resultsMu, completed)
			done <- res
		}()
	}

	// drainReady dispatches every currently-ready node.
	drainReady := func() {
		for {
			next := nextReady()
			if next == "" {
				return
			}
			dispatch(next)
		}
	}

	// If ctx was already cancelled before we started, mark every
	// node as skipped and skip the scheduling loop entirely.
	if ctx.Err() != nil {
		now := rc.Now()
		for _, n := range req.DAG.Nodes {
			summary.NodeResults[n.ID] = NodeResult{
				NodeID:     n.ID,
				Status:     StatusSkipped,
				SkipReason: SkipReasonCancelled,
				StartedAt:  now,
				FinishedAt: now,
			}
		}
	} else {
		drainReady()
		for inFlight > 0 {
			res := <-done
			inFlight--
			if res.Status == StatusFailed && firstErr == nil {
				firstErr = fmt.Errorf("node %q failed: %s", res.NodeID, res.Error)
			}
			onNodeDone(res)
			drainReady()
		}
	}

	summary.FinishedAt = rc.Now()
	summary.Duration = summary.FinishedAt.Sub(summary.StartedAt)
	summary.Status = aggregateStatus(summary.NodeResults)

	if rc.Bus != nil {
		rc.Bus.Publish(Event{
			RunID: rc.RunID, Type: EventRunCompleted, Timestamp: rc.Now(),
			Data: map[string]any{"status": summary.Status, "duration_ms": summary.Duration.Milliseconds(), "error": summary.Error},
		})
	}

	// If every node was skipped due to upstream failure, return
	// an error. Otherwise, the summary itself is the source of
	// truth and we return nil.
	if summary.Status == StatusFailed && summary.Error == "" {
		summary.Error = "one or more nodes failed"
	}
	if firstErr != nil && !errors.Is(firstErr, context.Canceled) && !errors.Is(firstErr, context.DeadlineExceeded) {
		return summary, firstErr
	}
	return summary, nil
}

// runOneNode executes a single node. It does NOT promote
// children — that's the coordinator's job. The function is safe
// for concurrent use.
func (e *Executor) runOneNode(
	ctx context.Context,
	rc *RunContext,
	req *RunRequest,
	nodeID string,
	payloads map[string]map[string]any,
	resultsMu *sync.Mutex,
	completed map[string]string,
) NodeResult {
	def, _ := req.DAG.NodeByID(nodeID)
	res := NodeResult{
		NodeID:    nodeID,
		StartedAt: rc.Now(),
	}

	// Already-completed check (e.g. an upstream failure
	// cascade).
	resultsMu.Lock()
	if c, done := completed[nodeID]; done {
		resultsMu.Unlock()
		res.Status = c
		if c == StatusSkipped {
			res.SkipReason = SkipReasonUpstreamFailed
		}
		res.FinishedAt = rc.Now()
		res.Duration = res.FinishedAt.Sub(res.StartedAt)
		return res
	}
	resultsMu.Unlock()

	// Context cancellation short-circuit.
	if ctx.Err() != nil {
		res.Status = StatusSkipped
		res.SkipReason = SkipReasonCancelled
		res.FinishedAt = rc.Now()
		res.Duration = res.FinishedAt.Sub(res.StartedAt)
		return res
	}

	// Build the inputs map from completed predecessors.
	resultsMu.Lock()
	inputs := make(map[string]any, len(req.DAG.Predecessors(nodeID))+1)
	for _, predID := range req.DAG.Predecessors(nodeID) {
		if p, ok := payloads[predID]; ok {
			inputs[predID] = p
		}
	}
	// Inject this node's own Params under the reserved "_params" key
	// so each Node implementation can read its own configuration.
	inputs["_params"] = []byte(def.Params)
	// Also expose the node's own type/subtype/position for introspection.
	inputs["_type"] = def.Type
	inputs["_subtype"] = def.Subtype
	inputs["_id"] = def.ID
	resultsMu.Unlock()

	// Resolve the node implementation.
	nodeImpl, _ := e.registry.GetBySubtype(def.Type, def.Subtype)

	// Run under a timeout.
	runCtx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	if rc.Bus != nil {
		rc.Bus.Publish(Event{
			RunID: rc.RunID, NodeID: nodeID, Type: EventNodeStarted, Timestamp: rc.Now(),
			Data: map[string]any{"type": def.Type, "subtype": def.Subtype},
		})
	}

	out, runErr := nodeImpl.Run(runCtx, rc, inputs)
	res.FinishedAt = rc.Now()
	res.Duration = res.FinishedAt.Sub(res.StartedAt)

	if runErr != nil {
		// A run error caused by the PARENT context being cancelled
		// (or timing out) is a skip, not a node failure: the node
		// didn't misbehave, the run was torn down around it. Only
		// classify as failed when the parent ctx is still live.
		// (runCtx has the per-node timeout layered on top, so we
		// check the parent ctx, not runCtx.)
		if ctx.Err() != nil || errors.Is(runErr, context.Canceled) {
			res.Status = StatusSkipped
			res.SkipReason = SkipReasonCancelled
			res.Error = ""
			return res
		}
		res.Status = StatusFailed
		res.Error = runErr.Error()
		if rc.Bus != nil {
			rc.Bus.Publish(Event{
				RunID: rc.RunID, NodeID: nodeID, Type: EventNodeFailed, Timestamp: res.FinishedAt,
				Data: map[string]any{"error": res.Error, "duration_ms": res.Duration.Milliseconds()},
			})
		}
		return res
	}
	res.Status = StatusSuccess
	res.Payload = out
	if rc.Bus != nil {
		rc.Bus.Publish(Event{
			RunID: rc.RunID, NodeID: nodeID, Type: EventNodeSuccess, Timestamp: res.FinishedAt,
			Data: map[string]any{"duration_ms": res.Duration.Milliseconds()},
		})
	}
	return res
}

// collectDescendants returns the IDs of every transitive
// descendant of `start` in BFS order, excluding `start` itself.
func collectDescendants(start string, dag *DAG) []string {
	out := make([]string, 0)
	visited := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		for _, child := range dag.Successors(head) {
			if visited[child] {
				continue
			}
			visited[child] = true
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out
}

// aggregateStatus reduces a map of NodeResult into a single
// RunSummary.Status:
//
//	all success                              -> "success"
//	any failure                              -> "failed"
//	any skip and any success (no failure)    -> "partial"
//	all skip (e.g. everything upstream died) -> "partial" with summary.Error
func aggregateStatus(results map[string]NodeResult) string {
	if len(results) == 0 {
		return StatusSuccess
	}
	hasFailed, hasSkipped := false, false
	for _, r := range results {
		switch r.Status {
		case StatusFailed:
			hasFailed = true
		case StatusSkipped:
			hasSkipped = true
		}
	}
	switch {
	case hasFailed:
		return StatusFailed
	case hasSkipped:
		return "partial"
	default:
		return StatusSuccess
	}
}

// ExecuteToNode runs only the nodes on a path leading to
// `targetNodeID`. Nodes outside the path are recorded as
// skipped with SkipReasonNotInPath.
//
// Used by the trial-run "execute up to this node" endpoint to
// let users preview a single node's output.
func (e *Executor) ExecuteToNode(ctx context.Context, rc *RunContext, req *RunRequest, targetNodeID string) (*RunSummary, error) {
	if req == nil || req.DAG == nil {
		return nil, fmt.Errorf("%w: nil request/dag", ErrInvalidDAG)
	}
	if !req.DAG.HasNode(targetNodeID) {
		return nil, fmt.Errorf("%w: target %q", ErrNodeNotFound, targetNodeID)
	}

	// Compute the closure of nodes from which target is
	// reachable (BFS backwards through Predecessors).
	inPath := make(map[string]bool)
	queue := []string{targetNodeID}
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		if inPath[head] {
			continue
		}
		inPath[head] = true
		queue = append(queue, req.DAG.Predecessors(head)...)
	}

	// Synthesize a sub-DAG containing only inPath nodes/edges.
	sub := &DAG{Nodes: make([]NodeDef, 0, len(inPath)), Edges: make([]Edge, 0)}
	for _, n := range req.DAG.Nodes {
		if inPath[n.ID] {
			sub.Nodes = append(sub.Nodes, n)
		}
	}
	for _, ed := range req.DAG.Edges {
		if inPath[ed.Source] && inPath[ed.Target] {
			sub.Edges = append(sub.Edges, ed)
		}
	}

	summary, err := e.Execute(ctx, rc, &RunRequest{DAG: sub, Inputs: req.Inputs, DryRun: req.DryRun})
	if err != nil && summary == nil {
		return nil, err
	}

	// Append skipped entries for out-of-path nodes.
	now := rc.Now()
	for _, n := range req.DAG.Nodes {
		if inPath[n.ID] {
			continue
		}
		if _, done := summary.NodeResults[n.ID]; done {
			continue
		}
		summary.NodeResults[n.ID] = NodeResult{
			NodeID:     n.ID,
			Status:     StatusSkipped,
			SkipReason: SkipReasonNotInPath,
			StartedAt:  now,
			FinishedAt: now,
		}
	}
	return summary, nil
}
