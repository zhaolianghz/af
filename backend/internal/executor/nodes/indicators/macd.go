// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package indicators

// MACD computes the MACD line, signal line, and histogram for
// each value. Standard defaults: fast=12, slow=26, signal=9.
//
// Returns three same-length series: dif (MACD line), dea
// (signal), and macd (histogram = dif - dea). The first
// slow-1+signal-2 entries are NaN (matching conventional TA
// software).
func MACD(values []float64, fast, slow, signal int) (dif, dea, hist []float64) {
	if fast <= 0 {
		fast = 12
	}
	if slow <= 0 {
		slow = 26
	}
	if signal <= 0 {
		signal = 9
	}
	out := make([]float64, len(values))
	for i := range out {
		out[i] = NaN
	}
	dif = append(dif, out...)
	dea = append(dea, out...)
	hist = append(hist, out...)
	if slow > len(values) {
		return
	}
	emaFast := EMA(values, fast)
	emaSlow := EMA(values, slow)
	// DIF = EMA(fast) - EMA(slow); valid from slow-1 onward.
	for i := slow - 1; i < len(values); i++ {
		if isFinite(emaFast[i]) && isFinite(emaSlow[i]) {
			dif[i] = emaFast[i] - emaSlow[i]
		}
	}
	// DEA = EMA(DIF, signal) over the dif series starting at slow-1.
	difFromSlow := make([]float64, 0, len(values)-slow+1)
	for i := slow - 1; i < len(values); i++ {
		difFromSlow = append(difFromSlow, dif[i])
	}
	deaFromSlow := EMA(difFromSlow, signal)
	for i, v := range deaFromSlow {
		idx := slow - 1 + i
		if idx < len(dea) {
			dea[idx] = v
		}
	}
	// Histogram = DIF - DEA, valid from slow+signal-2 onward.
	for i := 0; i < len(values); i++ {
		if isFinite(dif[i]) && isFinite(dea[i]) {
			hist[i] = dif[i] - dea[i]
		} else {
			hist[i] = NaN
		}
	}
	return dif, dea, hist
}
