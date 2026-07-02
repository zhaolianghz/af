// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

// PersistNode writes the upstream pick list to model.Recommendation
// (one row per item) and attaches the current RunContext's session
// tag via model.RecommendationTag in the same transaction.
//
// The upstream payload's "items" slice (or whatever the first slice
// in the predecessor payloads is) is treated as the pick list. Each
// item is expected to be a map with at least "stock_code"; optional
// fields "stock_name", "entry_price_low", "entry_price_high" are
// copied if present.
//
// On DB-less / trial-run mode (rc.DB == nil), the node returns a
// dry-run summary without writing anything.
type PersistNode struct{}

// NewPersistNode returns a fresh instance.
func NewPersistNode() *PersistNode { return &PersistNode{} }

func (n *PersistNode) Type() string    { return orchestrator.NodeTypePersist }
func (n *PersistNode) Subtype() string { return "" }
func (n *PersistNode) Schema() orchestrator.NodeSchema {
	return orchestrator.NodeSchema{
		Description: "Persist the upstream pick list as Recommendations + session tags.",
		Params: map[string]orchestrator.FieldSchema{
			"extra_tags": {Type: "array", Description: "Additional tags to attach beyond the session tag"},
			"date":       {Type: "string", Description: "Override the recommendation date (YYYY-MM-DD); default is today"},
		},
		Inputs: map[string]orchestrator.FieldSchema{
			"<any>": {Type: "array", Description: "Upstream pick list (slice of records with stock_code)"},
		},
		Outputs: map[string]orchestrator.FieldSchema{
			"persisted":         {Type: "number"},
			"recommendation_ids": {Type: "array"},
			"dry_run":           {Type: "bool"},
		},
	}
}

type persistParams struct {
	ExtraTags []string `json:"extra_tags"`
	Date      string   `json:"date"`
}

func (n *PersistNode) Run(ctx context.Context, rc *orchestrator.RunContext, in map[string]any) (map[string]any, error) {
	var p persistParams
	if raw, ok := in[orchestrator.InputKeyParams].([]byte); ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, orchestrator.NewParamError("persist", "params", "invalid json: "+err.Error())
		}
	}

	items := findFirstSlice(in)
	if items == nil {
		return map[string]any{"persisted": 0, "recommendation_ids": []uint64{}, "skipped_reason": "no items"}, nil
	}

	// Determine the recommendation date.
	recDate := rc.Now().UTC().Truncate(24 * time.Hour)
	if p.Date != "" {
		if t, err := time.Parse("2006-01-02", p.Date); err == nil {
			recDate = t
		}
	}

	// Collect tags: session_tag from RunContext + any extras.
	tags := make([]string, 0, len(p.ExtraTags)+1)
	if v, ok := rc.GetVar(orchestrator.VarKeySessionTag); ok {
		if s, ok := v.(string); ok && s != "" {
			tags = append(tags, s)
		}
	}
	tags = append(tags, p.ExtraTags...)

	// Trial-run / DB-less mode: no writes, just report.
	if rc.DB == nil {
		return map[string]any{
			"dry_run":            true,
			"persisted":          len(items),
			"recommendation_ids": []uint64{},
			"tags":               tags,
		}, nil
	}

	ids, err := n.writeRecommendations(ctx, rc, items, tags, recDate)
	if err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}

	return map[string]any{
		"persisted":          len(ids),
		"recommendation_ids": ids,
		"tags":               tags,
		"date":               recDate.Format("2006-01-02"),
	}, nil
}

func (n *PersistNode) writeRecommendations(ctx context.Context, rc *orchestrator.RunContext, items []any, tags []string, date time.Time) ([]uint64, error) {
	ids := make([]uint64, 0, len(items))
	tx := rc.DB.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		code := stringOf(m["stock_code"])
		if code == "" {
			continue
		}
		// Entry prices: use explicit entry_price_low/high if an
		// upstream node set them; otherwise fall back to the row's
		// latest close (the reference price at recommendation time).
		// Without this, every recommendation persisted 0/0 because no
		// node in the bundled templates emits entry_price_* — only
		// close flows through from the indicator rows.
		entryLow := floatOf(m["entry_price_low"])
		entryHigh := floatOf(m["entry_price_high"])
		if entryLow == 0 && entryHigh == 0 {
			if close := floatOf(m["close"]); close > 0 {
				entryLow, entryHigh = close, close
			}
		}
		rec := &model.Recommendation{
			RunID:          rc.RunID,
			Date:           date,
			StockCode:      code,
			StockName:      stringOf(m["stock_name"]),
			EntryPriceLow:  entryLow,
			EntryPriceHigh: entryHigh,
			StrategyCode:   rc.StrategyCode,
			StrategyName:   rc.StrategyName,
			NodeSnapshot:   marshalSnapshot(m),
		}
		if err := tx.Create(rec).Error; err != nil {
			return nil, err
		}
		ids = append(ids, rec.ID)
		if err := attachTags(tx, rec.ID, tags); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	committed = true
	return ids, nil
}

func attachTags(tx *gorm.DB, recID uint64, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([]model.RecommendationTag, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		rows = append(rows, model.RecommendationTag{
			RecommendationID: recID,
			Tag:              t,
			Source:           model.TagSourceAutoNode,
			TaggedAt:         now,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

func marshalSnapshot(m map[string]any) string {
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

var _ orchestrator.Node = (*PersistNode)(nil)
