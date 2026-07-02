// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Integration tests for the trial-run contract: every built-in
// node must behave predictably when invoked with a DB-less /
// Notify-less / DataSource-less RunContext — exactly the
// configuration the trial-run endpoint constructs in
// `backend/internal/orchestrator/trial_handler.go:runTrial`.
//
// WHY THIS FILE EXISTS
// --------------------
// The FE1 plan called for a single end-to-end trial-run test
// covering all 8 node types. The Executor's documented
// coordinator race (see
// `backend/internal/orchestrator/executor_test.go` file header)
// prevents reliable multi-node DAG runs today, so going through
// `Executor.Execute` would either flake or require t.Skip().
//
// Instead, this file exercises each node's Run() directly with
// the same RunContext shape that `runTrial` builds. This pins
// the actual contract that matters for trial-run: **in dry-run
// mode, every node type returns a valid payload and never
// panics on a nil DB / Notify / DataSource.** When the
// coordinator race is fixed, a thin wrapper around these cases
// will give the full executor-level coverage.
package nodes

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

// trialRunRC mirrors the RunContext constructed by
// orchestrator.(*TrialRunHandler).runTrial: nil DB, nil Notify,
// nil DataSource, fresh bus. We inject a pinned clock so
// assertions on time-derived fields stay deterministic.
func trialRunRC() *orchestrator.RunContext {
	return orchestrator.NewRunContext(orchestrator.RunContextOptions{
		DB:           nil,
		Notify:       nil,
		DataSource:   nil,
		Logger:       zap.NewNop(),
		StrategyID:   1,
		StrategyCode: "trial_strat",
		StrategyName: "Trial Strategy",
	})
}

// =============================================================================
// Registry wiring — verifies RegisterAll installs every node the
// trial-run contract relies on.
// =============================================================================

func TestTrialRun_RegisterAll_ResolvesEveryNodeType(t *testing.T) {
	reg := orchestrator.NewRegistry()
	RegisterAll(reg)

	for _, typ := range []string{
		orchestrator.NodeTypeDataSource,
		orchestrator.NodeTypeIndicator,
		orchestrator.NodeTypeFilter,
		orchestrator.NodeTypeRank,
		orchestrator.NodeTypeDedupe,
		orchestrator.NodeTypeSessionTag,
		orchestrator.NodeTypePersist,
		orchestrator.NodeTypeNotify,
	} {
		n, ok := reg.GetBySubtype(typ, "")
		require.True(t, ok, "RegisterAll missing type %q", typ)
		require.NotNil(t, n)
		require.Equal(t, typ, n.Type())
	}
}

// =============================================================================
// All 8 node types × trial-run contract
// =============================================================================
//
// For each node, the trial-run expectation is one of:
//   - "dry-run summary" — the node detects nil deps and returns
//     {dry_run: true, ...}. Required for: data_source, persist, notify.
//   - "pure transform" — the node doesn't touch external systems
//     at all; it succeeds regardless. Required for: filter, rank,
//     dedupe, session_tag.
//   - "needs data" — the node rejects empty input with a
//     ParamError. Required for: indicator (no klines available
//     in a context without a data_source predecessor).

func TestTrialRun_AllNodeTypes_PredictableBehavior(t *testing.T) {
	ctx := context.Background()
	rc := trialRunRC()

	// Pick list shape that data_source/persist/notify expect to
	// flow through the DAG. Helps exercise the "with upstream
	// payload" branch of each node.
	picks := []any{
		map[string]any{
			"stock_code":       "600000.SH",
			"stock_name":       "Test",
			"entry_price_low":  10.0,
			"entry_price_high": 10.5,
		},
	}

	t.Run("data_source returns dry_run=true", func(t *testing.T) {
		n := NewDataSourceNode()
		in := map[string]any{
			orchestrator.InputKeyParams: params(t, map[string]any{
				"subtype":     "kline",
				"stock_codes": []string{"600000.SH"},
				"period":      "1d",
				"days":        30,
			}),
		}
		out, err := n.Run(ctx, rc, in)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.Equal(t, true, out["dry_run"])
		require.Equal(t, "kline", out["subtype"])
	})

	t.Run("persist returns dry_run=true with upstream items", func(t *testing.T) {
		n := NewPersistNode()
		in := map[string]any{
			"pred":                     map[string]any{"items": picks},
			orchestrator.InputKeyParams: params(t, map[string]any{}),
		}
		out, err := n.Run(ctx, rc, in)
		require.NoError(t, err)
		require.Equal(t, true, out["dry_run"])
		require.Equal(t, len(picks), out["persisted"])
		// recommendation_ids is empty in dry-run (no DB write).
		ids, ok := out["recommendation_ids"].([]uint64)
		require.True(t, ok, "recommendation_ids should be []uint64, got %T", out["recommendation_ids"])
		require.Empty(t, ids)
	})

	t.Run("notify returns dry_run=true with preview", func(t *testing.T) {
		// Pin a session tag so subtypeForSession returns "morning"
		// deterministically. Otherwise the test would depend on
		// wall-clock time.
		rcWithSession := trialRunRC()
		rcWithSession.SetVar(orchestrator.VarKeySessionTag, model.SessionTagMorning)

		n := NewNotifyNode()
		in := map[string]any{
			"pred":                     map[string]any{"items": picks},
			orchestrator.InputKeyParams: params(t, map[string]any{}),
		}
		out, err := n.Run(ctx, rcWithSession, in)
		require.NoError(t, err)
		require.Equal(t, true, out["dry_run"])
		require.Equal(t, false, out["sent"])
		require.Equal(t, "morning", out["subtype"])
		require.Equal(t, len(picks), out["recipients"])
	})

	t.Run("filter succeeds with no upstream (empty result)", func(t *testing.T) {
		n := NewFilterNode()
		in := map[string]any{
			orchestrator.InputKeyParams: params(t, map[string]any{
				"field": "close",
				"op":    ">",
				"value": 10,
			}),
		}
		out, err := n.Run(ctx, rc, in)
		require.NoError(t, err)
		require.Equal(t, 0, out["matched"])
		require.Equal(t, 0, out["dropped"])
	})

	t.Run("rank succeeds with no upstream (empty result)", func(t *testing.T) {
		n := NewRankNode()
		in := map[string]any{
			orchestrator.InputKeyParams: params(t, map[string]any{
				"field": "chg_pct",
				"order": "desc",
				"top":   10,
			}),
		}
		out, err := n.Run(ctx, rc, in)
		require.NoError(t, err)
		require.Equal(t, 0, out["count"])
	})

	t.Run("dedupe succeeds with no upstream (empty result)", func(t *testing.T) {
		n := NewDedupeNode()
		in := map[string]any{
			orchestrator.InputKeyParams: params(t, map[string]any{"key": "stock_code"}),
		}
		out, err := n.Run(ctx, rc, in)
		require.NoError(t, err)
		require.Equal(t, 0, out["count"])
	})

	t.Run("session_tag returns a valid tag", func(t *testing.T) {
		n := NewSessionTagNode()
		in := map[string]any{
			orchestrator.InputKeyParams: params(t, map[string]any{
				"force": model.SessionTagReview,
			}),
		}
		out, err := n.Run(ctx, rc, in)
		require.NoError(t, err)
		require.Equal(t, model.SessionTagReview, out["session_tag"])

		// And the tag is published to RunContext.Vars so downstream
		// persist nodes can pick it up.
		v, ok := rc.GetVar(orchestrator.VarKeySessionTag)
		require.True(t, ok)
		require.Equal(t, model.SessionTagReview, v)
	})

	t.Run("indicator without klines returns ParamError (not panic)", func(t *testing.T) {
		// Indicator is the one node type that cannot run without
		// real input data — there's no meaningful "dry-run" for a
		// numeric series. The contract is: return a ParamError
		// explaining what's missing, never a panic / nil deref.
		n := NewIndicatorNode()
		in := map[string]any{
			orchestrator.InputKeyParams: params(t, map[string]any{
				"subtype": "ma",
				"period":  5,
			}),
		}
		_, err := n.Run(ctx, rc, in)
		require.Error(t, err)
		var pe *orchestrator.ParamError
		require.True(t, errors.As(err, &pe), "expected ParamError, got %T", err)
		require.Contains(t, pe.Error(), "klines")
	})
}

// =============================================================================
// Nil-safety — guards against future regressions where someone
// adds a DB/Notify/DataSource dereference without a nil check.
// =============================================================================

func TestTrialRun_Persist_NilDB_NoPanic(t *testing.T) {
	// Force the "items present" path so the node reaches the
	// rc.DB check rather than the earlier "no items" shortcut.
	n := NewPersistNode()
	rc := trialRunRC() // DB explicitly nil
	in := map[string]any{
		"pred":                     map[string]any{"items": pickListForTrial()},
		orchestrator.InputKeyParams: params(t, map[string]any{}),
	}
	require.NotPanics(t, func() {
		out, err := n.Run(context.Background(), rc, in)
		require.NoError(t, err)
		require.Equal(t, true, out["dry_run"])
	})
}

func TestTrialRun_Notify_NilManager_NoPanic(t *testing.T) {
	n := NewNotifyNode()
	rc := trialRunRC() // Notify explicitly nil
	in := map[string]any{
		"pred":                     map[string]any{"items": pickListForTrial()},
		orchestrator.InputKeyParams: params(t, map[string]any{"subtype": "alert"}),
	}
	require.NotPanics(t, func() {
		out, err := n.Run(context.Background(), rc, in)
		require.NoError(t, err)
		require.Equal(t, true, out["dry_run"])
	})
}

func TestTrialRun_DataSource_NilManager_NoPanic(t *testing.T) {
	n := NewDataSourceNode()
	rc := trialRunRC() // DataSource explicitly nil
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, map[string]any{
			"subtype":     "quote",
			"stock_codes": []string{"600000.SH"},
		}),
	}
	require.NotPanics(t, func() {
		out, err := n.Run(context.Background(), rc, in)
		require.NoError(t, err)
		require.Equal(t, true, out["dry_run"])
	})
}

// =============================================================================
// Helpers
// =============================================================================

func pickListForTrial() []any {
	return []any{
		map[string]any{
			"stock_code": "600000.SH",
		},
		map[string]any{
			"stock_code": "000001.SZ",
		},
	}
}
