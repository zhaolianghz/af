// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package indicators

// TurnoverRate computes turnover_rate = volume / float_shares.
// Returns NaN where float_shares <= 0 (avoid divide-by-zero).
//
// float_shares is constant per strategy; volume is the per-bar
// series. The two slices must be equal length.
func TurnoverRate(volumes, floatShares []float64) []float64 {
	L := len(volumes)
	out := make([]float64, L)
	for i := range out {
		out[i] = NaN
	}
	if len(floatShares) != L {
		return out
	}
	for i := 0; i < L; i++ {
		fs := floatShares[i]
		if fs <= 0 {
			out[i] = NaN
			continue
		}
		out[i] = volumes[i] / fs
	}
	return out
}
