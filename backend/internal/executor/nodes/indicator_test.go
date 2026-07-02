// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for IndicatorNode — verifies parameter validation, all
// seven subtypes, default values, and the NaN-to-zero conversion
// in floatSeriesToMap.
package nodes

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/orchestrator"
)

func klines(n int) []datasource.KLine {
	out := make([]datasource.KLine, 0, n)
	for i := 0; i < n; i++ {
		c := float64(i + 1)
		out = append(out, datasource.KLine{
			StockCode: "600000.SH",
			Open:      c, High: c, Low: c, Close: c, Volume: 1000,
		})
	}
	return out
}

func indicatorIn(t *testing.T, kls []datasource.KLine, p indicatorParams) map[string]any {
	t.Helper()
	return map[string]any{
		"pred": map[string]any{"klines": kls},
		orchestrator.InputKeyParams: params(t, p),
	}
}

// =============================================================================
// Validation
// =============================================================================

func TestIndicator_InvalidParamsJSON(t *testing.T) {
	n := NewIndicatorNode()
	in := map[string]any{orchestrator.InputKeyParams: []byte(`not json`)}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestIndicator_MissingSubtype(t *testing.T) {
	n := NewIndicatorNode()
	in := indicatorIn(t, klines(20), indicatorParams{Subtype: ""})
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestIndicator_NoKlines(t *testing.T) {
	n := NewIndicatorNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, indicatorParams{Subtype: "ma"}),
	}
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

func TestIndicator_UnknownSubtype(t *testing.T) {
	n := NewIndicatorNode()
	in := indicatorIn(t, klines(20), indicatorParams{Subtype: "bogus"})
	_, err := n.Run(context.Background(), newFilterRC(), in)
	require.Error(t, err)
}

// =============================================================================
// MA
// =============================================================================

func TestIndicator_MA(t *testing.T) {
	n := NewIndicatorNode()
	in := indicatorIn(t, klines(20), indicatorParams{Subtype: "ma", Period: 5})
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, "ma", out["subtype"])
	// One row per stock (the helper uses a single stock_code).
	require.Equal(t, 1, out["count"])
	items := out["items"].([]any)
	require.Len(t, items, 1)
	row := items[0].(map[string]any)
	require.Equal(t, "600000.SH", row["stock_code"])
	// Latest close is 20; latest MA(5) of 1..20 = avg(16..20) = 18.
	require.Equal(t, 20.0, row["close"])
	require.Equal(t, 18.0, row["ma"])
}

func TestIndicator_MA_DefaultPeriod(t *testing.T) {
	n := NewIndicatorNode()
	in := indicatorIn(t, klines(20), indicatorParams{Subtype: "ma"})
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	row := out["items"].([]any)[0].(map[string]any)
	// Default period 5; latest = avg(16..20) = 18.
	require.Equal(t, 18.0, row["ma"])
}

func TestIndicator_PerStockRows(t *testing.T) {
	// Two stocks interleaved must produce two independent rows,
	// each computed on its OWN series (not the concatenation).
	kls := []datasource.KLine{
		{StockCode: "AAA", Close: 10}, {StockCode: "BBB", Close: 100},
		{StockCode: "AAA", Close: 11}, {StockCode: "BBB", Close: 101},
		{StockCode: "AAA", Close: 12}, {StockCode: "BBB", Close: 102},
	}
	n := NewIndicatorNode()
	in := map[string]any{
		"pred":                      map[string]any{"klines": kls},
		orchestrator.InputKeyParams: params(t, indicatorParams{Subtype: "ma", Period: 2}),
	}
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	require.Equal(t, 2, out["count"])
	byCode := map[string]map[string]any{}
	for _, it := range out["items"].([]any) {
		r := it.(map[string]any)
		byCode[r["stock_code"].(string)] = r
	}
	// AAA latest MA(2) = avg(11,12)=11.5; BBB = avg(101,102)=101.5.
	require.Equal(t, 11.5, byCode["AAA"]["ma"])
	require.Equal(t, 101.5, byCode["BBB"]["ma"])
	require.Equal(t, 12.0, byCode["AAA"]["close"])
	require.Equal(t, 102.0, byCode["BBB"]["close"])
}

// =============================================================================
// EMA / MACD
// =============================================================================

func TestIndicator_EMA(t *testing.T) {
	n := NewIndicatorNode()
	in := indicatorIn(t, klines(20), indicatorParams{Subtype: "ema", Period: 12})
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	row := out["items"].([]any)[0].(map[string]any)
	require.Contains(t, row, "ema")
	require.NotZero(t, row["ema"])
}

func TestIndicator_MACD(t *testing.T) {
	n := NewIndicatorNode()
	in := indicatorIn(t, klines(30), indicatorParams{
		Subtype: "macd", Fast: 12, Slow: 26, Signal: 9,
	})
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	row := out["items"].([]any)[0].(map[string]any)
	// Per-stock scalar fields the macd template filters on.
	require.Contains(t, row, "macd_dif")
	require.Contains(t, row, "macd_dea")
	require.Contains(t, row, "macd_hist")
}

// =============================================================================
// KDJ / BOLL
// =============================================================================

func TestIndicator_KDJ(t *testing.T) {
	n := NewIndicatorNode()
	in := indicatorIn(t, klines(30), indicatorParams{
		Subtype: "kdj", N: 9, M1: 3, M2: 3,
	})
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	row := out["items"].([]any)[0].(map[string]any)
	require.Contains(t, row, "kdj_k")
	require.Contains(t, row, "kdj_d")
	require.Contains(t, row, "kdj_j")
}

func TestIndicator_BOLL(t *testing.T) {
	n := NewIndicatorNode()
	in := indicatorIn(t, klines(30), indicatorParams{
		Subtype: "boll", Period: 20, KStdDev: 2.0,
	})
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	row := out["items"].([]any)[0].(map[string]any)
	require.Contains(t, row, "boll_mid")
	require.Contains(t, row, "boll_upper")
	require.Contains(t, row, "boll_lower")
}

// =============================================================================
// Volume ratio / turnover rate
// =============================================================================

func TestIndicator_VolumeRatio(t *testing.T) {
	n := NewIndicatorNode()
	in := indicatorIn(t, klines(20), indicatorParams{Subtype: "volume_ratio", Period: 5})
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	row := out["items"].([]any)[0].(map[string]any)
	require.Contains(t, row, "volume_ratio")
}

func TestIndicator_TurnoverRate(t *testing.T) {
	n := NewIndicatorNode()
	in := indicatorIn(t, klines(20), indicatorParams{Subtype: "turnover_rate"})
	out, err := n.Run(context.Background(), newFilterRC(), in)
	require.NoError(t, err)
	row := out["items"].([]any)[0].(map[string]any)
	require.Contains(t, row, "turnover_rate")
}

// =============================================================================
// extractKLines
// =============================================================================

func TestExtractKLines_FromTypedSlice(t *testing.T) {
	kls := []datasource.KLine{
		{StockCode: "A", Close: 1.0},
		{StockCode: "B", Close: 2.0},
	}
	in := map[string]any{
		"pred": map[string]any{"klines": kls},
	}
	got := extractKLines(in)
	require.Len(t, got, 2)
}

func TestExtractKLines_FromAnySlice(t *testing.T) {
	in := map[string]any{
		"pred": map[string]any{
			"klines": []any{
				map[string]any{"close": 1.0, "open": 0.5, "high": 1.5, "low": 0.5, "volume": 100},
			},
		},
	}
	got := extractKLines(in)
	require.Len(t, got, 1)
	require.Equal(t, 1.0, got[0].Close)
}

func TestExtractKLines_NilWhenAbsent(t *testing.T) {
	in := map[string]any{"pred": map[string]any{"other": []any{1}}}
	require.Nil(t, extractKLines(in))
}

// =============================================================================
// lastFinite
// =============================================================================

func TestLastFinite_ReturnsLastValue(t *testing.T) {
	require.Equal(t, 2.0, lastFinite([]float64{1, 0, 2}))
}

func TestLastFinite_SkipsTrailingNaNInf(t *testing.T) {
	require.Equal(t, 5.0, lastFinite([]float64{5, math.NaN(), math.Inf(1)}))
}

func TestLastFinite_EmptyOrAllNaN(t *testing.T) {
	require.Equal(t, 0.0, lastFinite(nil))
	require.Equal(t, 0.0, lastFinite([]float64{math.NaN(), math.NaN()}))
}
