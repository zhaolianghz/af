// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package perf computes and persists post-hoc performance snapshots for
// recommendations: T+1, T+3, T+5 forward returns, plus window-level
// cumulative return, max drawdown, and win rate.
//
// The package is split into three layers:
//
//   - calculator.go: pure math (no I/O). Easily unit-tested.
//   - service.go:    fetches K-line, applies the calculator, persists
//                    snapshots. Idempotent on (rec_id, snapshot_date).
//   - handler.go:    exposes the HTTP surface for §9.4 (manual trigger,
//                    per-rec history, aggregations).
//
// Conventions:
//   - All return / drawdown / win-rate values are nullable floats. A nil
//     pointer means "no data" — never treat that as zero.
//   - The "entry" used for returns is the midpoint of the
//     recommendation's (low, high) price band.
//   - Trading-day math goes through calendar.Service so weekends and
//     holidays are handled consistently.
package perf

import "math"

// TReturn computes (exit / entry) - 1, the open-to-close style
// forward return.
//
// Returns nil when entry is non-positive or non-finite. We deliberately
// do NOT clamp negative returns — a stop-out at limit-down (≈ -10 %) is
// a meaningful data point, not noise.
func TReturn(entry, exit float64) *float64 {
	if !isFinitePositive(entry) || !isFinite(exit) {
		return nil
	}
	r := (exit / entry) - 1.0
	if !isFinite(r) {
		return nil
	}
	return &r
}

// MaxDrawdown computes the worst peak-to-trough drop over a series of
// equity values, where the first element is the entry (= 1.0 in pure-
// return terms, or the entry price in raw-price terms). Returns nil
// when there are fewer than 2 points or every value is non-finite.
//
// The convention is:
//
//	peak  = running max of the path
//	dd_i  = (peak_i - value_i) / peak_i
//	mdd   = max(dd_i) over all i
//
// For a single rec, the input is typically [entry, t1, t3, t5]; the
// returned number is the worst dip from the best seen so far within
// the T+1..T+5 window.
func MaxDrawdown(path []float64) *float64 {
	if len(path) < 2 {
		return nil
	}
	peak := math.NaN()
	var mdd float64
	for _, v := range path {
		if !isFinite(v) {
			continue
		}
		if math.IsNaN(peak) || v > peak {
			peak = v
		}
		if isFinitePositive(peak) {
			dd := (peak - v) / peak
			if dd > mdd {
				mdd = dd
			}
		}
	}
	if math.IsNaN(mdd) {
		return nil
	}
	return &mdd
}

// WinRate computes the fraction of strictly-positive returns in the
// slice. NaN / Inf entries are skipped. Returns nil when no finite,
// non-NaN entry is present.
//
// For a single recommendation's snapshot, the canonical use is to pass
// [t1, t3, t5] — a "1-of-3 positive" wins. For the aggregate query,
// pass the per-rec T+5 return across many recommendations.
func WinRate(returns []float64) *float64 {
	if len(returns) == 0 {
		return nil
	}
	var total, wins int
	for _, r := range returns {
		if !isFinite(r) {
			continue
		}
		total++
		if r > 0 {
			wins++
		}
	}
	if total == 0 {
		return nil
	}
	rate := float64(wins) / float64(total)
	return &rate
}

// AverageReturn computes the arithmetic mean of the (finite) input
// slice. Returns nil when no finite entry is present.
func AverageReturn(returns []float64) *float64 {
	if len(returns) == 0 {
		return nil
	}
	var sum float64
	var n int
	for _, r := range returns {
		if !isFinite(r) {
			continue
		}
		sum += r
		n++
	}
	if n == 0 {
		return nil
	}
	avg := sum / float64(n)
	return &avg
}

// MedianReturn computes the median of the (finite) input. Returns nil
// when the slice has no finite entries. The input is sorted in place
// by the caller's contract — pass a copy if the caller needs the
// original ordering preserved.
func MedianReturn(returns []float64) *float64 {
	cleaned := make([]float64, 0, len(returns))
	for _, r := range returns {
		if isFinite(r) {
			cleaned = append(cleaned, r)
		}
	}
	n := len(cleaned)
	if n == 0 {
		return nil
	}
	// In-place sort; cleaned is a private copy so this is safe.
	sortFloats(cleaned)
	if n%2 == 1 {
		m := cleaned[n/2]
		return &m
	}
	m := (cleaned[n/2-1] + cleaned[n/2]) / 2.0
	return &m
}

// isFinite reports whether v is neither NaN nor ±Inf.
func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// isFinitePositive reports whether v is a finite, strictly-positive
// number. We treat zero as "no data" because a zero entry would
// produce an infinite return.
func isFinitePositive(v float64) bool {
	return isFinite(v) && v > 0
}

// sortFloats sorts a []float64 in ascending order. Kept as a tiny
// helper so we don't pull sort into the production binary just for
// MedianReturn.
func sortFloats(a []float64) {
	// Insertion sort is fine for the small slices we deal with
	// (typically length <= a few hundred).
	for i := 1; i < len(a); i++ {
		v := a[i]
		j := i - 1
		for j >= 0 && a[j] > v {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = v
	}
}
