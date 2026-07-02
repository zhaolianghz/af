// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package indicators

import "math"

// BOLL computes Bollinger Bands. mid is the n-bar SMA; upper
// and lower are mid ± k*sigma, where sigma is the population
// standard deviation over the same window.
//
// Standard defaults: n=20, k=2.0.
func BOLL(values []float64, n int, k float64) (mid, upper, lower []float64) {
	if n <= 0 {
		n = 20
	}
	if k <= 0 {
		k = 2.0
	}
	L := len(values)
	out := make([]float64, L)
	for i := range out {
		out[i] = NaN
	}
	mid = append(mid, out...)
	upper = append(upper, out...)
	lower = append(lower, out...)
	if n > L {
		return
	}
	mid = MA(values, n)
	for i := n - 1; i < L; i++ {
		mean := mid[i]
		if !isFinite(mean) {
			continue
		}
		var sq float64
		for j := i - n + 1; j <= i; j++ {
			d := values[j] - mean
			sq += d * d
		}
		sigma := math.Sqrt(sq / float64(n))
		upper[i] = mean + k*sigma
		lower[i] = mean - k*sigma
	}
	return mid, upper, lower
}
