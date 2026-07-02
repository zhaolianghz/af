// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for SessionTagNode — verifies force-override, automatic
// time-based derivation, timezone handling, and the side-effect
// of writing the tag to RunContext.Vars.
package nodes

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

func TestSessionTag_InvalidParamsJSON(t *testing.T) {
	n := NewSessionTagNode()
	in := map[string]any{
		orchestrator.InputKeyParams: []byte(`{not json`),
	}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestSessionTag_Force(t *testing.T) {
	n := NewSessionTagNode()
	rc := orchestrator.NewRunContext(orchestrator.RunContextOptions{
		Clock: func() time.Time { return time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC) },
	})
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, sessionTagParams{Force: "MORNING"}),
	}
	out, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	require.Equal(t, "MORNING", out["session_tag"])
	// Side-effect: stored in RunContext.Vars for downstream nodes.
	v, ok := rc.GetVar(orchestrator.VarKeySessionTag)
	require.True(t, ok)
	require.Equal(t, "MORNING", v)
}

func TestSessionTag_DeriveMorning(t *testing.T) {
	// 09:30 Asia/Shanghai → MORNING.
	n := NewSessionTagNode()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	rc := orchestrator.NewRunContext(orchestrator.RunContextOptions{
		Clock: func() time.Time { return time.Date(2026, 6, 11, 9, 30, 0, 0, loc) },
	})
	out, err := n.Run(context.Background(), rc, nil)
	require.NoError(t, err)
	require.Equal(t, model.SessionTagMorning, out["session_tag"])
}

func TestSessionTag_DeriveAfternoon(t *testing.T) {
	// 14:00 Asia/Shanghai → AFTERNOON.
	n := NewSessionTagNode()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	rc := orchestrator.NewRunContext(orchestrator.RunContextOptions{
		Clock: func() time.Time { return time.Date(2026, 6, 11, 14, 0, 0, 0, loc) },
	})
	out, err := n.Run(context.Background(), rc, nil)
	require.NoError(t, err)
	require.Equal(t, model.SessionTagAfternoon, out["session_tag"])
}

func TestSessionTag_DeriveReview(t *testing.T) {
	// 16:00 Asia/Shanghai → REVIEW.
	n := NewSessionTagNode()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	rc := orchestrator.NewRunContext(orchestrator.RunContextOptions{
		Clock: func() time.Time { return time.Date(2026, 6, 11, 16, 0, 0, 0, loc) },
	})
	out, err := n.Run(context.Background(), rc, nil)
	require.NoError(t, err)
	require.Equal(t, model.SessionTagReview, out["session_tag"])
}

func TestSessionTag_DeriveNoPost(t *testing.T) {
	// 03:00 Asia/Shanghai → NO_POST.
	n := NewSessionTagNode()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	rc := orchestrator.NewRunContext(orchestrator.RunContextOptions{
		Clock: func() time.Time { return time.Date(2026, 6, 11, 3, 0, 0, 0, loc) },
	})
	out, err := n.Run(context.Background(), rc, nil)
	require.NoError(t, err)
	require.Equal(t, model.SessionTagNoPost, out["session_tag"])
}

func TestSessionTag_Boundary_1130_IsMorning(t *testing.T) {
	// 11:30 is the inclusive end of MORNING.
	loc, _ := time.LoadLocation("Asia/Shanghai")
	tag := deriveSessionTag(time.Date(2026, 6, 11, 11, 30, 0, 0, loc))
	require.Equal(t, model.SessionTagMorning, tag)
}

func TestSessionTag_Boundary_1500_IsReview(t *testing.T) {
	// 15:00 sharp: AFTERNOON is exclusive, so REVIEW wins.
	loc, _ := time.LoadLocation("Asia/Shanghai")
	tag := deriveSessionTag(time.Date(2026, 6, 11, 15, 0, 0, 0, loc))
	require.Equal(t, model.SessionTagReview, tag)
}

func TestSessionTag_Boundary_1501_IsReview(t *testing.T) {
	// 15:01 starts REVIEW.
	loc, _ := time.LoadLocation("Asia/Shanghai")
	tag := deriveSessionTag(time.Date(2026, 6, 11, 15, 1, 0, 0, loc))
	require.Equal(t, model.SessionTagReview, tag)
}

func TestSessionTag_UnknownTimezone_FallsBackToUTC(t *testing.T) {
	// Unknown timezone → falls back to UTC; the node
	// must not error.
	n := NewSessionTagNode()
	rc := orchestrator.NewRunContext(orchestrator.RunContextOptions{
		Clock: func() time.Time { return time.Date(2026, 6, 11, 9, 30, 0, 0, time.UTC) },
	})
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, sessionTagParams{Timezone: "Mars/Olympus"}),
	}
	out, err := n.Run(context.Background(), rc, in)
	require.NoError(t, err)
	// In UTC 09:30 is MORNING (boundary 9:00 - 11:30).
	require.Equal(t, model.SessionTagMorning, out["session_tag"])
}
