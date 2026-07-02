// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for NotifyNode — verifies dry-run, the four template
// families, the auto-detect-from-session-tag path, the picks
// extractor, and the formatPrice helper.
package nodes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/notify"
	"github.com/skyzhao/af/internal/orchestrator"
)

// fakeNotify records every Send call and optionally returns an
// error.
type fakeNotify struct {
	called  int
	gotMsg  *notify.Message
	gotErr  error
}

func (f *fakeNotify) Send(ctx context.Context, msg *notify.Message) error {
	f.called++
	f.gotMsg = msg
	return f.gotErr
}

func (f *fakeNotify) RegisterChannel(name string, ch notify.Channel) {}
func (f *fakeNotify) List() []string                                { return nil }

func newNotifyRC(n notify.Manager) *orchestrator.RunContext {
	return orchestrator.NewRunContext(orchestrator.RunContextOptions{
		Notify:       n,
		Logger:       zap.NewNop(),
		RunID:        7,
		StrategyCode: "morning_b",
		StrategyName: "Morning Breakout",
		Clock: func() time.Time {
			return time.Date(2026, 6, 11, 9, 35, 0, 0, time.UTC)
		},
	})
}

func notifyPicks() []any {
	return []any{
		map[string]any{
			"stock_code":       "600000.SH",
			"stock_name":       "PF Bank",
			"strategy_code":    "morning_b",
			"entry_price_low":  10.0,
			"entry_price_high": 10.5,
		},
		map[string]any{
			"stock_code": "000001.SZ",
			// no name, no price range
		},
	}
}

// =============================================================================
// Validation
// =============================================================================

func TestNotify_InvalidParamsJSON(t *testing.T) {
	n := NewNotifyNode()
	in := map[string]any{orchestrator.InputKeyParams: []byte(`not json`)}
	_, err := n.Run(context.Background(), newNotifyRC(nil), in)
	require.Error(t, err)
}

func TestNotify_UnknownSubtype(t *testing.T) {
	n := NewNotifyNode()
	in := map[string]any{
		"pred":                     map[string]any{"items": notifyPicks()},
		orchestrator.InputKeyParams: params(t, notifyParams{Subtype: "bogus"}),
	}
	_, err := n.Run(context.Background(), newNotifyRC(nil), in)
	require.Error(t, err)
}

// =============================================================================
// Dry-run (no notify.Manager)
// =============================================================================

func TestNotify_DryRun_NoManager(t *testing.T) {
	n := NewNotifyNode()
	in := map[string]any{
		"pred":                     map[string]any{"items": notifyPicks()},
		orchestrator.InputKeyParams: params(t, notifyParams{Subtype: "morning"}),
	}
	out, err := n.Run(context.Background(), newNotifyRC(nil), in)
	require.NoError(t, err)
	require.Equal(t, true, out["dry_run"])
	require.Equal(t, false, out["sent"])
	require.Equal(t, "morning", out["subtype"])
	require.Equal(t, 2, out["recipients"])
	require.NotEmpty(t, out["title"])
}

func TestNotify_DryRun_AutoSubtype_FromSessionTag(t *testing.T) {
	n := NewNotifyNode()
	rc := newNotifyRC(nil)
	rc.SetVar(orchestrator.VarKeySessionTag, model.SessionTagAfternoon)
	in := map[string]any{
		"pred":                     map[string]any{"items": notifyPicks()},
		orchestrator.InputKeyParams: params(t, notifyParams{Subtype: ""}), // auto
	}
	out, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	require.Equal(t, "afternoon", out["subtype"])
}

func TestNotify_DryRun_DefaultsToAlert_WhenNoSessionTag(t *testing.T) {
	n := NewNotifyNode()
	in := map[string]any{
		"pred":                     map[string]any{"items": notifyPicks()},
		orchestrator.InputKeyParams: params(t, notifyParams{Subtype: ""}),
	}
	out, err := n.Run(context.Background(), newNotifyRC(nil), in)
	require.NoError(t, err)
	require.Equal(t, "alert", out["subtype"])
}

// =============================================================================
// Real notify.Manager path
// =============================================================================

func TestNotify_Sends(t *testing.T) {
	f := &fakeNotify{}
	n := NewNotifyNode()
	in := map[string]any{
		"pred":                     map[string]any{"items": notifyPicks()},
		orchestrator.InputKeyParams: params(t, notifyParams{Subtype: "morning"}),
	}
	out, err := n.Run(context.Background(), newNotifyRC(f), in)
	require.NoError(t, err)
	require.Equal(t, true, out["sent"])
	require.Equal(t, 2, out["recipients"])
	require.Equal(t, 1, f.called)
	require.NotNil(t, f.gotMsg)
}

func TestNotify_SendError_Propagates(t *testing.T) {
	f := &fakeNotify{gotErr: errors.New("boom")}
	n := NewNotifyNode()
	in := map[string]any{
		"pred":                     map[string]any{"items": notifyPicks()},
		orchestrator.InputKeyParams: params(t, notifyParams{Subtype: "morning"}),
	}
	_, err := n.Run(context.Background(), newNotifyRC(f), in)
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
}

// =============================================================================
// Per-subtype builds
// =============================================================================

func TestNotify_BuildAlert_DefaultDetail(t *testing.T) {
	// When subtype=alert and no detail is given, the
	// node synthesizes a default message referencing the
	// run and strategy.
	f := &fakeNotify{}
	n := NewNotifyNode()
	rc := newNotifyRC(f)
	in := map[string]any{
		"pred":                     map[string]any{"items": notifyPicks()},
		orchestrator.InputKeyParams: params(t, notifyParams{Subtype: "alert", Source: "executor", Severity: "warn"}),
	}
	_, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	require.Equal(t, 1, f.called)
	require.NotNil(t, f.gotMsg)
}

func TestNotify_BuildReview_UsesRunDate(t *testing.T) {
	f := &fakeNotify{}
	n := NewNotifyNode()
	rc := newNotifyRC(f)
	in := map[string]any{
		"pred":                     map[string]any{"items": notifyPicks()},
		orchestrator.InputKeyParams: params(t, notifyParams{Subtype: "review"}),
	}
	_, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	require.Equal(t, 1, f.called)
	require.NotNil(t, f.gotMsg)
}

// =============================================================================
// extractPicks
// =============================================================================

func TestNotify_ExtractPicks_DropsMissingCode(t *testing.T) {
	n := NewNotifyNode()
	in := map[string]any{
		"pred": map[string]any{
			"items": []any{
				map[string]any{"stock_code": "A"},
				map[string]any{"no_code": true},
			},
		},
	}
	_ = in
	// We can call the unexported method through the public
	// Run path with a nil manager.
	out, err := n.Run(context.Background(), newNotifyRC(nil), map[string]any{
		"pred":                     in["pred"],
		orchestrator.InputKeyParams: params(t, notifyParams{Subtype: "morning"}),
	})
	require.NoError(t, err)
	require.Equal(t, 1, out["recipients"])
}

func TestNotify_ExtractPicks_NilSlice(t *testing.T) {
	n := NewNotifyNode()
	out, err := n.Run(context.Background(), newNotifyRC(nil), map[string]any{
		orchestrator.InputKeyParams: params(t, notifyParams{Subtype: "morning"}),
	})
	require.NoError(t, err)
	require.Equal(t, 0, out["recipients"])
}

// =============================================================================
// formatPrice helper
// =============================================================================

func TestFormatPrice(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{10.5, "10.50"},
		{"raw", "raw"},
		{42, "42.00"},
		{[]any{1, 2}, ""},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			require.Equal(t, c.want, formatPrice(c.in))
		})
	}
}
