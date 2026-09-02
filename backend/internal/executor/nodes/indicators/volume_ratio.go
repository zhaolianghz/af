// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package indicators

// VolumeRatio returns the ratio current_volume / avg(prev n volumes)
// for each bar — the conventional 量比 semantics where the baseline
// is the mean of the n bars BEFORE the current one (current bar
// excluded). The first n entries are NaN (warm-up needs n prior bars).
//
// If n <= 0, defaults to 5.
func VolumeRatio(volumes []float64, n int) []float64 {
	if n <= 0 {
		n = 5
	}
	L := len(volumes)
	out := make([]float64, L)
	for i := range out {
		out[i] = NaN
	}
	for i := n; i < L; i++ {
		sum := 0.0
		for j := i - n; j < i; j++ { // the n bars before the current one
			sum += volumes[j]
		}
		avg := sum / float64(n)
		if avg == 0 {
			out[i] = NaN
		} else {
			out[i] = volumes[i] / avg
		}
	}
	return out
}
