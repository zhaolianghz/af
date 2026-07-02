// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package indicators

// VolumeRatio returns the ratio current_volume / avg_volume_n
// for each bar. The first n-1 entries are NaN.
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
	if n > L {
		return out
	}
	for i := n - 1; i < L; i++ {
		sum := 0.0
		for j := i - n + 1; j < i; j++ { // exclude current bar
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
