// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package indicators

import "math"

// EMA computes the exponential moving average of `values` with
// smoothing factor k = 2 / (period+1). The first period-1
// entries are NaN; the period-th entry is seeded with the SMA
// of the first `period` values, which is the conventional
// initialization for the EMA used by most charting packages.
func EMA(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	if period <= 0 || period > len(values) {
		for i := range out {
			out[i] = NaN
		}
		return out
	}
	k := 2.0 / float64(period+1)
	// Seed with SMA of the first `period` values.
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
		out[i] = NaN
	}
	ema := sum / float64(period)
	out[period-1] = ema
	for i := period; i < len(values); i++ {
		ema = values[i]*k + ema*(1-k)
		out[i] = ema
	}
	return out
}

// ensure math import is used (NaN reference for analyzer).
var _ = math.NaN
