// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package indicators implements the pure math for the technical-
// analysis node subtypes (MA / EMA / MACD / KDJ / BOLL /
// volume_ratio / turnover_rate). Every function here takes a
// []float64 series and returns a same-length series with NaN
// (here represented by 0 with a leading-NA convention) for
// indices that have not accumulated enough history.
//
// The functions are deliberately array-only — they do not
// import the orchestrator or datasource packages. Tests in this
// package pin the math against hand-calculated fixtures.
package indicators

import "math"

// NaN is the sentinel value returned for "not enough history"
// entries. The indicator node converts NaN to 0 before adding
// the series to the KLine output, so downstream consumers see a
// clean float64.
var NaN = math.NaN()

// isFinite returns false for NaN/Inf.
func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// MA computes a simple moving average of window `period` over
// `values`. The first period-1 entries are NaN.
func MA(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	if period <= 0 || period > len(values) {
		for i := range out {
			out[i] = NaN
		}
		return out
	}
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
		out[i] = NaN
	}
	out[period-1] = sum / float64(period)
	for i := period; i < len(values); i++ {
		sum += values[i] - values[i-period]
		out[i] = sum / float64(period)
	}
	return out
}
