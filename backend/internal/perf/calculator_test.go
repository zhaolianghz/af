// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package perf

import (
	"math"
	"testing"
)

func TestTReturn(t *testing.T) {
	cases := []struct {
		name      string
		entry, exit float64
		wantNil   bool
		wantApprox float64
	}{
		{"normal positive", 10.0, 11.0, false, 0.10},
		{"normal negative", 10.0, 9.0, false, -0.10},
		{"unchanged", 10.0, 10.0, false, 0.0},
		{"limit-down", 10.0, 9.0, false, -0.10},
		{"zero entry", 0.0, 9.0, true, 0},
		{"negative entry", -1.0, 9.0, true, 0},
		{"nan entry", math.NaN(), 9.0, true, 0},
		{"inf exit", 10.0, math.Inf(1), true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TReturn(c.entry, c.exit)
			if c.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %v, got nil", c.wantApprox)
			}
			if math.Abs(*got-c.wantApprox) > 1e-9 {
				t.Fatalf("want %v, got %v", c.wantApprox, *got)
			}
		})
	}
}

func TestMaxDrawdown(t *testing.T) {
	cases := []struct {
		name    string
		path    []float64
		wantNil bool
		want    float64
	}{
		{"no dd", []float64{10, 11, 12, 13}, false, 0.0},
		{"classic 50% dd", []float64{100, 50, 100}, false, 0.5},
		{"entry dip", []float64{10, 9, 11, 12}, false, 0.10},
		{"rebound then dip", []float64{10, 12, 8, 14}, false, 4.0 / 12.0},
		{"single point", []float64{10}, true, 0},
		{"empty", []float64{}, true, 0},
		{"nan in input skipped", []float64{10, math.NaN(), 12, 8}, false, 1.0 / 3.0},
		{"negative ignored as peak", []float64{-5, -10}, false, 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MaxDrawdown(c.path)
			if c.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %v, got nil", c.want)
			}
			if math.Abs(*got-c.want) > 1e-9 {
				t.Fatalf("want %v, got %v", c.want, *got)
			}
		})
	}
}

func TestWinRate(t *testing.T) {
	cases := []struct {
		name     string
		input    []float64
		wantNil  bool
		want     float64
	}{
		{"all positive", []float64{0.01, 0.05, 0.10}, false, 1.0},
		{"all negative", []float64{-0.01, -0.05, -0.10}, false, 0.0},
		{"mixed", []float64{0.01, -0.05, 0.10}, false, 2.0 / 3.0},
		{"zero is not a win", []float64{0.0, 0.0}, false, 0.0},
		{"all nan", []float64{math.NaN(), math.NaN()}, true, 0},
		{"empty", []float64{}, true, 0},
		{"nan skipped", []float64{math.NaN(), 0.05, math.NaN(), -0.02}, false, 0.5},
		{"inf skipped", []float64{math.Inf(1), -0.01, 0.02}, false, 0.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := WinRate(c.input)
			if c.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %v, got nil", c.want)
			}
			if math.Abs(*got-c.want) > 1e-9 {
				t.Fatalf("want %v, got %v", c.want, *got)
			}
		})
	}
}

func TestAverageReturn(t *testing.T) {
	if r := AverageReturn(nil); r != nil {
		t.Fatalf("nil input should return nil, got %v", *r)
	}
	in := []float64{0.1, -0.05, 0.2, math.NaN(), math.Inf(1)}
	r := AverageReturn(in)
	if r == nil {
		t.Fatal("expected non-nil")
	}
	// average of 0.1, -0.05, 0.2 (3 finite values) = 0.25 / 3
	if math.Abs(*r-0.25/3.0) > 1e-9 {
		t.Fatalf("want %v, got %v", 0.25/3.0, *r)
	}
}

func TestMedianReturn(t *testing.T) {
	if r := MedianReturn(nil); r != nil {
		t.Fatalf("nil input should return nil, got %v", *r)
	}
	// odd length
	r := MedianReturn([]float64{0.01, 0.05, -0.02, 0.10, -0.05})
	if r == nil || math.Abs(*r-0.01) > 1e-9 {
		t.Fatalf("odd median: want 0.01, got %v", r)
	}
	// even length
	r = MedianReturn([]float64{0.05, -0.02, 0.10, -0.05})
	if r == nil || math.Abs(*r-0.015) > 1e-9 {
		t.Fatalf("even median: want 0.015, got %v", r)
	}
	// nan stripped
	r = MedianReturn([]float64{math.NaN(), 0.10, math.NaN(), -0.05, 0.20})
	if r == nil || math.Abs(*r-0.10) > 1e-9 {
		t.Fatalf("nan-stripped median: want 0.10, got %v", r)
	}
}

func TestSingleRecWinRate(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name    string
		t1, t3, t5 *float64
		wantNil bool
		want    float64
	}{
		{"all positive", f(0.01), f(0.05), f(0.10), false, 1.0},
		{"all negative", f(-0.01), f(-0.05), f(-0.10), false, 0.0},
		{"only t1 positive", f(0.05), f(-0.10), f(-0.10), false, 1.0},
		{"only t5 positive", f(-0.05), f(-0.10), f(0.10), false, 1.0},
		{"none known", nil, nil, nil, true, 0},
		{"partial unknown — no positives", nil, f(-0.05), nil, true, 0},
		{"partial unknown — t1 positive", f(0.05), nil, nil, false, 1.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := singleRecWinRate(c.t1, c.t3, c.t5)
			if c.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %v, got nil", c.want)
			}
			if math.Abs(*got-c.want) > 1e-9 {
				t.Fatalf("want %v, got %v", c.want, *got)
			}
		})
	}
}
