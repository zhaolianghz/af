// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package nodes

import (
	"context"
	"encoding/json"
	"math"
	"sort"

	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/orchestrator"

	"github.com/skyzhao/af/internal/executor/nodes/indicators"
)

// IndicatorNode computes a technical indicator over a K-line
// series produced by a preceding data_source node. Its Subtype
// selects the indicator:
//   "ma"             — simple moving average
//   "ema"            — exponential moving average
//   "macd"           — MACD line / signal / histogram
//   "kdj"            — stochastic KDJ
//   "boll"           — Bollinger Bands
//   "volume_ratio"   — volume / n-bar avg
//   "turnover_rate"  — volume / float shares
type IndicatorNode struct{}

// NewIndicatorNode returns a fresh instance.
func NewIndicatorNode() *IndicatorNode { return &IndicatorNode{} }

func (n *IndicatorNode) Type() string    { return orchestrator.NodeTypeIndicator }
func (n *IndicatorNode) Subtype() string { return "" }
func (n *IndicatorNode) Schema() orchestrator.NodeSchema {
	return orchestrator.NodeSchema{
		Description: "Compute a technical indicator over K-line input.",
		Params: map[string]orchestrator.FieldSchema{
			"subtype":    {Type: "enum", Enum: []string{"ma", "ema", "macd", "kdj", "boll", "volume_ratio", "turnover_rate"}, Required: true},
			"period":     {Type: "number", Default: 5, Description: "Window for MA / EMA (default 5)"},
			"fast":       {Type: "number", Default: 12},
			"slow":       {Type: "number", Default: 26},
			"signal":     {Type: "number", Default: 9},
			"n":          {Type: "number", Default: 9, Description: "KDJ n parameter (default 9)"},
			"m1":         {Type: "number", Default: 3},
			"m2":         {Type: "number", Default: 3},
			"k_stddev":   {Type: "number", Default: 2.0, Description: "BOLL band width multiplier"},
		},
		Inputs: map[string]orchestrator.FieldSchema{
			"klines": {Type: "array", Required: true, Description: "K-line series from a preceding data_source node"},
		},
		Outputs: map[string]orchestrator.FieldSchema{
			"items": {Type: "array", Description: "One row per stock: {stock_code, close, <indicator scalar fields>} — the latest value of each indicator. Consumed by filter/rank/dedupe/persist."},
			"count": {Type: "number", Description: "Number of stocks (rows), not klines."},
		},
	}
}

type indicatorParams struct {
	Subtype string `json:"subtype"`
	Period  int    `json:"period"`
	Fast    int    `json:"fast"`
	Slow    int    `json:"slow"`
	Signal  int    `json:"signal"`
	N       int    `json:"n"`
	M1      int    `json:"m1"`
	M2      int    `json:"m2"`
	KStdDev float64 `json:"k_stddev"`
}

func (n *IndicatorNode) Run(ctx context.Context, rc *orchestrator.RunContext, in map[string]any) (map[string]any, error) {
	var p indicatorParams
	if raw, ok := in["_params"].([]byte); ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, orchestrator.NewParamError("indicator", "params", "invalid json: "+err.Error())
		}
	}
	if p.Subtype == "" {
		return nil, orchestrator.NewParamError("indicator", "subtype", "is required")
	}
	klines := extractKLines(in)
	if len(klines) == 0 {
		return nil, orchestrator.NewParamError("indicator", "klines", "no kline series found in input")
	}
	if p.Subtype != "ma" && p.Subtype != "ema" && p.Subtype != "macd" &&
		p.Subtype != "kdj" && p.Subtype != "boll" &&
		p.Subtype != "volume_ratio" && p.Subtype != "turnover_rate" {
		return nil, orchestrator.NewParamError("indicator", "subtype", "unknown: "+p.Subtype)
	}

	// Group bars by stock so each stock's indicator is computed on
	// its OWN series — not a meaningless concatenation of every
	// stock's closes. Then emit ONE row per stock carrying the
	// LATEST value of each indicator as a scalar, plus stock_code /
	// close, which is exactly the per-stock shape that
	// filter/rank/dedupe/persist consume.
	groups := groupByStock(klines)
	items := make([]any, 0, len(groups))
	for _, g := range groups {
		closes := make([]float64, len(g.bars))
		highs := make([]float64, len(g.bars))
		lows := make([]float64, len(g.bars))
		vols := make([]float64, len(g.bars))
		for i, k := range g.bars {
			closes[i] = k.Close
			highs[i] = k.High
			lows[i] = k.Low
			vols[i] = float64(k.Volume)
		}
		row := map[string]any{
			"stock_code": g.code,
			"close":      lastFinite(closes),
		}
		if g.name != "" {
			row["stock_name"] = g.name
		}
		switch p.Subtype {
		case "ma":
			if p.Period <= 0 {
				p.Period = 5
			}
			row["ma"] = lastFinite(indicators.MA(closes, p.Period))
		case "ema":
			if p.Period <= 0 {
				p.Period = 12
			}
			row["ema"] = lastFinite(indicators.EMA(closes, p.Period))
		case "macd":
			dif, dea, hist := indicators.MACD(closes, p.Fast, p.Slow, p.Signal)
			row["macd_dif"] = lastFinite(dif)
			row["macd_dea"] = lastFinite(dea)
			row["macd_hist"] = lastFinite(hist)
		case "kdj":
			k, d, j := indicators.KDJ(highs, lows, closes, p.N, p.M1, p.M2)
			row["kdj_k"] = lastFinite(k)
			row["kdj_d"] = lastFinite(d)
			row["kdj_j"] = lastFinite(j)
		case "boll":
			mid, upper, lower := indicators.BOLL(closes, p.Period, p.KStdDev)
			row["boll_mid"] = lastFinite(mid)
			row["boll_upper"] = lastFinite(upper)
			row["boll_lower"] = lastFinite(lower)
		case "volume_ratio":
			row["volume_ratio"] = lastFinite(indicators.VolumeRatio(vols, p.Period))
		case "turnover_rate":
			// No float-shares series available yet; emit 0 so the
			// row still carries the field for downstream filters.
			shares := make([]float64, len(vols))
			row["turnover_rate"] = lastFinite(indicators.TurnoverRate(vols, shares))
		}
		items = append(items, row)
	}
	_ = ctx
	_ = rc
	return map[string]any{
		"subtype": p.Subtype,
		"count":   len(items),
		"items":   items,
	}, nil
}

// stockGroup is one stock's bars in chronological order.
type stockGroup struct {
	code string
	name string
	bars []datasource.KLine
}

// groupByStock buckets klines by StockCode (preserving first-seen
// order for determinism) and sorts each bucket by Timestamp so the
// indicator series is chronological.
func groupByStock(kls []datasource.KLine) []stockGroup {
	idx := make(map[string]int)
	groups := make([]stockGroup, 0, 8)
	for _, k := range kls {
		i, ok := idx[k.StockCode]
		if !ok {
			idx[k.StockCode] = len(groups)
			groups = append(groups, stockGroup{code: k.StockCode})
			i = len(groups) - 1
		}
		groups[i].bars = append(groups[i].bars, k)
	}
	for i := range groups {
		sort.SliceStable(groups[i].bars, func(a, b int) bool {
			return groups[i].bars[a].Timestamp.Before(groups[i].bars[b].Timestamp)
		})
	}
	return groups
}

// lastFinite returns the last non-NaN, non-Inf value of s, or 0 if
// none. Indicators warm up with NaN/0 for the first N bars, so the
// LATEST finite value is the screening signal.
func lastFinite(s []float64) float64 {
	for i := len(s) - 1; i >= 0; i-- {
		if !math.IsNaN(s[i]) && !math.IsInf(s[i], 0) {
			return s[i]
		}
	}
	return 0
}

func extractKLines(in map[string]any) []datasource.KLine {
	// Walk every predecessor payload, looking for a "klines"
	// array. The first hit wins.
	for _, v := range in {
		if v == nil {
			continue
		}
		payload, ok := v.(map[string]any)
		if !ok {
			continue
		}
		raw, ok := payload["klines"]
		if !ok {
			continue
		}
		switch arr := raw.(type) {
		case []datasource.KLine:
			return arr
		case []any:
			out := make([]datasource.KLine, 0, len(arr))
			for _, e := range arr {
				km, ok := e.(map[string]any)
				if !ok {
					continue
				}
				out = append(out, datasource.KLine{
					StockCode: stringOf(km["stock_code"]),
					Period:    stringOf(km["period"]),
					Open:      floatOf(km["open"]),
					High:      floatOf(km["high"]),
					Low:       floatOf(km["low"]),
					Close:     floatOf(km["close"]),
					Volume:    int64(floatOf(km["volume"])),
				})
			}
			return out
		}
	}
	return nil
}

func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func floatOf(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

var _ orchestrator.Node = (*IndicatorNode)(nil)
