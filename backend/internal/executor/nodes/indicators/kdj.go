// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package indicators

// KDJ computes the stochastic K, D, J oscillators. Standard
// defaults: n=9, m1=3, m2=3.
//
// high/low/close are equal-length series; high[i]/low[i] define
// the rolling n-bar window's extremes, close[i] is the current
// bar's close.
//
// RSV = (close - lowest_low_n) / (highest_high_n - lowest_low_n) * 100
// K = SMA(RSV, m1)
// D = SMA(K, m2)
// J = 3*K - 2*D
func KDJ(high, low, close []float64, n, m1, m2 int) (k, d, j []float64) {
	if n <= 0 {
		n = 9
	}
	if m1 <= 0 {
		m1 = 3
	}
	if m2 <= 0 {
		m2 = 3
	}
	L := len(close)
	out := make([]float64, L)
	for i := range out {
		out[i] = NaN
	}
	k = append(k, out...)
	d = append(d, out...)
	j = append(j, out...)
	if n > L {
		return
	}

	rsv := make([]float64, L)
	for i := range rsv {
		rsv[i] = NaN
	}
	for i := n - 1; i < L; i++ {
		hh := high[i-n+1]
		ll := low[i-n+1]
		for k2 := i - n + 2; k2 <= i; k2++ {
			if high[k2] > hh {
				hh = high[k2]
			}
			if low[k2] < ll {
				ll = low[k2]
			}
		}
		if hh == ll {
			rsv[i] = 50.0
		} else {
			rsv[i] = (close[i] - ll) / (hh - ll) * 100.0
		}
	}
	// K is SMA(RSV, m1) seeded at the first non-NaN rsv.
	// Standard practice: K is seeded with 50 (or the first
	// RSV) and updated with the recurrence K = (m1-1)/m1*K_prev
	// + 1/m1*RSV. We use the SMA form which is equivalent for
	// a uniform m1.
	k = SMA(rsv, m1)
	d = SMA(k, m2)
	for i := 0; i < L; i++ {
		if isFinite(k[i]) && isFinite(d[i]) {
			j[i] = 3*k[i] - 2*d[i]
		} else {
			j[i] = NaN
		}
	}
	return k, d, j
}

// SMA is a simple moving average used by KDJ to seed K and D.
// Identical to MA but kept under a separate name so callers
// don't have to import math.
func SMA(values []float64, period int) []float64 {
	return MA(values, period)
}
