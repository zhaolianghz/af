// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package nodes

import (
	"context"
	"encoding/json"
	"time"

	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

// SessionTagNode determines the current trading session tag
// (MORNING / AFTERNOON / NO_POST / REVIEW) based on the run's
// clock. It stores the tag in RunContext.Vars under
// orchestrator.VarKeySessionTag so the persist node can attach
// it to recommendations without re-deriving it.
//
// Default session windows (Asia/Shanghai):
//
//	MORNING   : 09:00 - 11:30
//	AFTERNOON : 13:00 - 15:00
//	REVIEW    : 15:00 - 17:00
//	NO_POST   : everything else
//
// The "force" param overrides the auto-detection so tests and
// trial-runs can pin the tag.
type SessionTagNode struct{}

// NewSessionTagNode returns a fresh instance.
func NewSessionTagNode() *SessionTagNode { return &SessionTagNode{} }

func (n *SessionTagNode) Type() string    { return orchestrator.NodeTypeSessionTag }
func (n *SessionTagNode) Subtype() string { return "" }
func (n *SessionTagNode) Schema() orchestrator.NodeSchema {
	return orchestrator.NodeSchema{
		Description: "Tag the current run with a session label (MORNING/AFTERNOON/REVIEW/NO_POST).",
		Params: map[string]orchestrator.FieldSchema{
			"force":    {Type: "enum", Enum: []string{"MORNING", "AFTERNOON", "REVIEW", "NO_POST"}, Description: "Force a specific tag (testing / manual override)"},
			"timezone": {Type: "string", Default: "Asia/Shanghai", Description: "IANA timezone for window calculation"},
		},
		Outputs: map[string]orchestrator.FieldSchema{
			"session_tag": {Type: "string", Description: "MORNING / AFTERNOON / REVIEW / NO_POST"},
		},
	}
}

type sessionTagParams struct {
	Force    string `json:"force"`
	Timezone string `json:"timezone"`
}

func (n *SessionTagNode) Run(ctx context.Context, rc *orchestrator.RunContext, in map[string]any) (map[string]any, error) {
	var p sessionTagParams
	if raw, ok := in[orchestrator.InputKeyParams].([]byte); ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, orchestrator.NewParamError("session_tag", "params", "invalid json: "+err.Error())
		}
	}

	tag := p.Force
	if tag == "" {
		tz := p.Timezone
		if tz == "" {
			tz = "Asia/Shanghai"
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			// Fall back to UTC; do not fail the run.
			loc = time.UTC
		}
		tag = deriveSessionTag(rc.Now().In(loc))
	}

	// Publish to RunContext for downstream nodes.
	rc.SetVar(orchestrator.VarKeySessionTag, tag)

	_ = ctx
	return map[string]any{
		"session_tag": tag,
		"derived_at":  rc.Now(),
	}, nil
}

// deriveSessionTag inspects the local time and returns the matching
// session tag. The boundaries are inclusive of the start and exclusive
// of the end (so 11:30 sharp counts as MORNING; 11:31 falls into
// NO_POST).
func deriveSessionTag(now time.Time) string {
	hm := now.Hour()*60 + now.Minute()
	switch {
	case hm >= 9*60 && hm <= 11*60+30:
		return model.SessionTagMorning
	case hm >= 13*60 && hm < 15*60:
		return model.SessionTagAfternoon
	case hm >= 15*60 && hm < 17*60:
		return model.SessionTagReview
	default:
		return model.SessionTagNoPost
	}
}

var _ orchestrator.Node = (*SessionTagNode)(nil)
