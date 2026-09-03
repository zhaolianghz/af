// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/notify"
	"github.com/skyzhao/af/internal/orchestrator"
)

// NotifyNode dispatches a templated message via the injected
// notify.Manager. The Subtype selects the template family:
//
//	"morning"   -> notify.BuildMorningPick
//	"afternoon" -> notify.BuildAfternoonPick
//	"review"    -> notify.BuildDailyReview
//	"alert"     -> notify.BuildAlert
//	""          -> auto-detect from the current session tag
//
// On DB-less / trial-run mode (rc.Notify == nil) the node returns a
// dry-run summary describing what message would have been sent.
type NotifyNode struct{}

// NewNotifyNode returns a fresh instance.
func NewNotifyNode() *NotifyNode { return &NotifyNode{} }

func (n *NotifyNode) Type() string    { return orchestrator.NodeTypeNotify }
func (n *NotifyNode) Subtype() string { return "" }
func (n *NotifyNode) Schema() orchestrator.NodeSchema {
	return orchestrator.NodeSchema{
		Description: "Send the upstream pick list via the configured notification channels.",
		Params: map[string]orchestrator.FieldSchema{
			"subtype":  {Type: "enum", Enum: []string{"morning", "afternoon", "review", "alert", ""}, Description: "Template family (empty = auto from session tag)"},
			"source":   {Type: "string", Default: "executor", Description: "Alert source label (subtype=alert only)"},
			"severity": {Type: "enum", Enum: []string{"info", "warn", "error"}, Default: "error", Description: "Alert severity (subtype=alert only)"},
			"detail":   {Type: "string", Description: "Alert detail text (subtype=alert only)"},
		},
		Inputs: map[string]orchestrator.FieldSchema{
			"<any>": {Type: "array", Description: "Pick list (slice of records with stock_code)"},
		},
		Outputs: map[string]orchestrator.FieldSchema{
			"sent":      {Type: "bool"},
			"dry_run":   {Type: "bool"},
			"channel":   {Type: "string"},
			"recipients": {Type: "number"},
		},
	}
}

type notifyParams struct {
	Subtype  string `json:"subtype"`
	Source   string `json:"source"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

func (n *NotifyNode) Run(ctx context.Context, rc *orchestrator.RunContext, in map[string]any) (map[string]any, error) {
	var p notifyParams
	if raw, ok := in[orchestrator.InputKeyParams].([]byte); ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, orchestrator.NewParamError("notify", "params", "invalid json: "+err.Error())
		}
	}

	// Auto-detect subtype from the current session tag if not set.
	subtype := p.Subtype
	if subtype == "" {
		subtype = n.subtypeForSession(rc)
	}

	picks := n.extractPicks(in)

	msg, err := n.buildMessage(subtype, p, picks, rc)
	if err != nil {
		return nil, err
	}

	// Trial-run / no-notify mode: report dry-run.
	if rc.Notify == nil {
		return map[string]any{
			"dry_run":   true,
			"sent":      false,
			"subtype":   subtype,
			"title":     msg.Title,
			"preview":   msg.Content,
			"recipients": len(picks),
		}, nil
	}

	// No channel configured is a normal deployment state (dingtalk/wecom
	// both disabled), not a run failure. Report a skipped success so the
	// run completes and the UI shows why nothing was sent.
	if !rc.Notify.HasChannel() {
		return map[string]any{
			"sent":       false,
			"skipped":    "no channel configured",
			"subtype":    subtype,
			"recipients": len(picks),
		}, nil
	}

	if err := rc.Notify.Send(ctx, msg); err != nil {
		return nil, fmt.Errorf("notify.Send: %w", err)
	}
	return map[string]any{
		"sent":       true,
		"subtype":    subtype,
		"recipients": len(picks),
	}, nil
}

// subtypeForSession maps the session tag to the matching template
// family. Returns "alert" as a defensive default so the run never
// silently sends nothing.
func (n *NotifyNode) subtypeForSession(rc *orchestrator.RunContext) string {
	v, ok := rc.GetVar(orchestrator.VarKeySessionTag)
	if !ok {
		return "alert"
	}
	switch v {
	case model.SessionTagMorning:
		return "morning"
	case model.SessionTagAfternoon:
		return "afternoon"
	case model.SessionTagReview:
		return "review"
	default:
		return "alert"
	}
}

// extractPicks converts the upstream slice into notify.StockPick
// rows. Items without a stock_code are dropped.
func (n *NotifyNode) extractPicks(in map[string]any) []notify.StockPick {
	items := findFirstSlice(in)
	if items == nil {
		return nil
	}
	out := make([]notify.StockPick, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		code := stringOf(m["stock_code"])
		if code == "" {
			continue
		}
		out = append(out, notify.StockPick{
			Code:      code,
			Name:      stringOf(m["stock_name"]),
			Strategy:  stringOf(m["strategy_code"]),
			EntryLow:  formatPrice(m["entry_price_low"]),
			EntryHigh: formatPrice(m["entry_price_high"]),
		})
	}
	return out
}

func (n *NotifyNode) buildMessage(subtype string, p notifyParams, picks []notify.StockPick, rc *orchestrator.RunContext) (*notify.Message, error) {
	switch subtype {
	case "morning":
		return notify.BuildMorningPick(picks), nil
	case "afternoon":
		return notify.BuildAfternoonPick(picks), nil
	case "review":
		// Daily review uses today's date and a single bucket
		// for the upstream picks. v1: hits = len(picks),
		// gainers = picks, losers = empty.
		date := rc.Now().Format("2006-01-02")
		return notify.BuildDailyReview(date, len(picks), picks, nil), nil
	case "alert":
		detail := p.Detail
		if detail == "" {
			detail = fmt.Sprintf("Run %d on strategy %q produced %d picks", rc.RunID, rc.StrategyCode, len(picks))
		}
		return notify.BuildAlert(p.Source, p.Severity, detail), nil
	}
	return nil, orchestrator.NewParamError("notify", "subtype", "unknown: "+subtype)
}

// formatPrice formats a numeric price as a string with 2 decimals.
// Non-numeric values fall back to the raw value formatted as %v.
func formatPrice(v any) string {
	if v == nil {
		return ""
	}
	if f, ok := toFloatOK(v); ok {
		return strconv.FormatFloat(f, 'f', 2, 64)
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

var _ orchestrator.Node = (*NotifyNode)(nil)
