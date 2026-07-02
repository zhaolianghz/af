// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — validate.go
//
// Cross-source consistency checks. design.md §3.5 says: every source
// should agree on the same stock's price within 0.5%. Pairs that
// disagree by more than the threshold are reported (and, in a later
// iteration, written to the alerts table).
//
// The validator in this round is a pure function over a set of
// (source, quote) pairs plus the configured threshold. It does not
// call into the manager — the caller fetches the quotes first and
// hands them in. That keeps the validator easy to unit test and
// decoupled from the network.
package datasource

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// Inconsistency is a single (source, reference, deviation, reason)
// report produced by CrossSourceQuoteValidator.
type Inconsistency struct {
	Source   string  `json:"source"`
	Other    string  `json:"other"`
	Price    float64 `json:"price"`
	OtherPx  float64 `json:"other_price"`
	Deviation float64 `json:"deviation"` // absolute relative deviation
	Threshold float64 `json:"threshold"`
}

// ValidateQuotes takes a slice of (source_name, quote) pairs and
// returns a slice of inconsistencies (one per source/other pair that
// disagrees by more than threshold). The threshold is the relative
// price difference (0.005 == 0.5 %).
//
// Quotes whose price is <= 0 are skipped (the relative deviation is
// undefined). The pairs are compared symmetrically: if A disagrees
// with B, two Inconsistency entries are produced (one with
// Source=A, Other=B, one with Source=B, Other=A). This makes the
// output easier to feed into a UI without duplicating logic.
func ValidateQuotes(pairs []SourceQuote, threshold float64) []Inconsistency {
	if threshold <= 0 {
		threshold = 0.005
	}
	out := make([]Inconsistency, 0)
	// Sort by source name for deterministic output.
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Source < pairs[j].Source })

	for i := 0; i < len(pairs); i++ {
		if pairs[i].Quote == nil || pairs[i].Quote.Price <= 0 {
			continue
		}
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].Quote == nil || pairs[j].Quote.Price <= 0 {
				continue
			}
			pa := pairs[i].Quote.Price
			pb := pairs[j].Quote.Price
			// Relative deviation: |a-b| / max(a,b)
			denom := math.Max(pa, pb)
			if denom == 0 {
				continue
			}
			dev := math.Abs(pa-pb) / denom
			if dev > threshold {
				out = append(out, Inconsistency{
					Source:    pairs[i].Source,
					Other:     pairs[j].Source,
					Price:     pa,
					OtherPx:   pb,
					Deviation: dev,
					Threshold: threshold,
				})
				out = append(out, Inconsistency{
					Source:    pairs[j].Source,
					Other:     pairs[i].Source,
					Price:     pb,
					OtherPx:   pa,
					Deviation: dev,
					Threshold: threshold,
				})
			}
		}
	}
	return out
}

// SourceQuote is a (source_name, quote) pair fed to ValidateQuotes.
type SourceQuote struct {
	Source string
	Quote  *Quote
}

// CrossSourceQuoteValidator pulls a quote from every source in
// sources and runs ValidateQuotes. It is intentionally separate from
// the manager's normal call path: it bypasses the cache and the
// breakers so we observe the freshest possible value from every
// source.
type CrossSourceQuoteValidator struct {
	Sources   []Source
	Threshold float64
	Timeout   time.Duration
}

// NewCrossSourceQuoteValidator constructs a validator with the given
// threshold (0.005 by default if <= 0).
func NewCrossSourceQuoteValidator(sources []Source, threshold float64) *CrossSourceQuoteValidator {
	if threshold <= 0 {
		threshold = 0.005
	}
	return &CrossSourceQuoteValidator{
		Sources:   sources,
		Threshold: threshold,
		Timeout:   10 * time.Second,
	}
}

// Validate fetches a quote for stockCode from every source and
// returns the inconsistency list. Failures on individual sources are
// silently ignored (a source that cannot produce a quote is treated
// as "no data", not as "inconsistent").
func (v *CrossSourceQuoteValidator) Validate(ctx context.Context, stockCode string) ([]Inconsistency, error) {
	if stockCode == "" {
		return nil, fmt.Errorf("datasource: CrossSourceQuoteValidator: empty stockCode")
	}
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pairs := make([]SourceQuote, 0, len(v.Sources))
	for _, s := range v.Sources {
		q, err := s.GetQuote(cctx, stockCode)
		if err != nil {
			// skip — a source that fails is not a consistency
			// signal
			continue
		}
		pairs = append(pairs, SourceQuote{Source: s.Name(), Quote: q})
	}
	if len(pairs) < 2 {
		return nil, nil
	}
	return ValidateQuotes(pairs, v.Threshold), nil
}
