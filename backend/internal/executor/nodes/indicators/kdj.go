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
// K = recursive SMA of RSV with weight m1 (seeded at 50)
// D = recursive SMA of K   with weight m2 (seeded at 50)
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
	// K/D use the standard KDJ recursive SMA (Wilder-style),
	// NOT the rolling arithmetic mean:
	//   K[i] = (m1-1)/m1·K[i-1] + 1/m1·RSV[i],  seeded K=50
	//   D[i] = (m2-1)/m2·D[i-1] + 1/m2·K[i],    seeded D=50
	// The previous implementation called the rolling MA on the
	// NaN-prefixed RSV series; MA seeds its running sum with the
	// first m1 values (all NaN during the RSV warm-up), and
	// NaN+x=NaN is permanent, so K/D/J were NaN for EVERY bar.
	// The indicator node then mapped that to 0 via lastFinite,
	// silently corrupting every KDJ-based filter.
	//
	// RSV is finite on exactly the contiguous suffix [n-1, L),
	// so we seed K/D at index n-1 and iterate forward. k/d/j
	// were initialized to all-NaN above; positions before n-1
	// stay NaN (genuine warm-up), the rest become finite.
	seed := 50.0
	kw := float64(m1-1) / float64(m1)
	kr := 1.0 / float64(m1)
	dw := float64(m2-1) / float64(m2)
	dr := 1.0 / float64(m2)
	k[n-1] = seed
	d[n-1] = seed
	for i := n; i < L; i++ {
		k[i] = kw*k[i-1] + kr*rsv[i]
	}
	for i := n; i < L; i++ {
		d[i] = dw*d[i-1] + dr*k[i]
	}
	for i := n - 1; i < L; i++ {
		j[i] = 3*k[i] - 2*d[i]
	}
	return k, d, j
}

// SMA is a simple moving average used by KDJ to seed K and D.
// Identical to MA but kept under a separate name so callers
// don't have to import math.
func SMA(values []float64, period int) []float64 {
	return MA(values, period)
}
