// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package datasource — validate_test.go
package datasource

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateQuotesWithinThreshold(t *testing.T) {
	t.Parallel()
	pairs := []SourceQuote{
		{Source: "eastmoney", Quote: &Quote{Price: 100.0}},
		{Source: "sina", Quote: &Quote{Price: 100.4}}, // 0.4% off
	}
	out := ValidateQuotes(pairs, 0.005)
	assert.Empty(t, out, "within-threshold pair should not be flagged")
}

func TestValidateQuotesExceedsThreshold(t *testing.T) {
	t.Parallel()
	pairs := []SourceQuote{
		{Source: "eastmoney", Quote: &Quote{Price: 100.0}},
		{Source: "sina", Quote: &Quote{Price: 101.5}}, // 1.5% off
	}
	out := ValidateQuotes(pairs, 0.005)
	require.Len(t, out, 2, "expected symmetric pair: eastmoney->sina and sina->eastmoney")
	assert.Equal(t, "eastmoney", out[0].Source)
	assert.Equal(t, "sina", out[0].Other)
	assert.Equal(t, "sina", out[1].Source)
	assert.Equal(t, "eastmoney", out[1].Other)
}

func TestValidateQuotesExactThreshold(t *testing.T) {
	t.Parallel()
	// 0.5% is the default threshold — it should NOT be flagged
	// (deviation must be > threshold, not >=).
	pairs := []SourceQuote{
		{Source: "eastmoney", Quote: &Quote{Price: 100.0}},
		{Source: "sina", Quote: &Quote{Price: 100.5}},
	}
	out := ValidateQuotes(pairs, 0.005)
	assert.Empty(t, out, "deviation exactly equal to threshold should not be flagged")
}

func TestValidateQuotesDefaultThreshold(t *testing.T) {
	t.Parallel()
	// 0.6% off — should be flagged with the default 0.5%.
	pairs := []SourceQuote{
		{Source: "eastmoney", Quote: &Quote{Price: 100.0}},
		{Source: "sina", Quote: &Quote{Price: 100.6}},
	}
	out := ValidateQuotes(pairs, 0)
	assert.Len(t, out, 2, "zero threshold should fall back to 0.5%")
}

func TestValidateQuotesSkipsZeroPrice(t *testing.T) {
	t.Parallel()
	pairs := []SourceQuote{
		{Source: "eastmoney", Quote: &Quote{Price: 0}}, // missing data
		{Source: "sina", Quote: &Quote{Price: 100.0}},
	}
	out := ValidateQuotes(pairs, 0.001)
	assert.Empty(t, out, "zero price should be skipped (no baseline)")
}

func TestValidateQuotesSkipsNilQuote(t *testing.T) {
	t.Parallel()
	pairs := []SourceQuote{
		{Source: "eastmoney", Quote: nil},
		{Source: "sina", Quote: &Quote{Price: 100.0}},
	}
	out := ValidateQuotes(pairs, 0.001)
	assert.Empty(t, out)
}

func TestValidateQuotesThreeSources(t *testing.T) {
	t.Parallel()
	// Three sources, two pairs to compare.
	pairs := []SourceQuote{
		{Source: "akshare", Quote: &Quote{Price: 100.0}},
		{Source: "eastmoney", Quote: &Quote{Price: 100.3}}, // within
		{Source: "sina", Quote: &Quote{Price: 102.0}},      // 2% vs the others
	}
	out := ValidateQuotes(pairs, 0.005)
	// akshare/sina inconsistent, eastmoney/sina inconsistent. No
	// inconsistency between akshare and eastmoney. So 2 pairs * 2
	// = 4 entries.
	require.Len(t, out, 4)
	for _, e := range out {
		assert.NotEqual(t, "akshare→eastmoney", e.Source+"→"+e.Other)
	}
}

func TestValidateQuotesDeterministicOrder(t *testing.T) {
	t.Parallel()
	// Provide pairs in random order; output should be sorted by
	// source name.
	pairs := []SourceQuote{
		{Source: "sina", Quote: &Quote{Price: 102.0}},
		{Source: "eastmoney", Quote: &Quote{Price: 100.0}},
	}
	out := ValidateQuotes(pairs, 0.001)
	require.Len(t, out, 2)
	assert.Equal(t, "eastmoney", out[0].Source)
	assert.Equal(t, "sina", out[0].Other)
}

func TestCrossSourceValidatorRunsEverySource(t *testing.T) {
	t.Parallel()
	a := newFakeSource("eastmoney", &Quote{Price: 100.0})
	b := newFakeSource("sina", &Quote{Price: 102.0})
	c := newFakeSource("akshare", nil)
	c.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		return nil, errors.New("akshare down")
	})

	v := NewCrossSourceQuoteValidator([]Source{a, b, c}, 0.005)
	out, err := v.Validate(context.Background(), "600519")
	require.NoError(t, err)
	// Two sources produced quotes; they disagree > 0.5%.
	require.Len(t, out, 2)
}

func TestCrossSourceValidatorSkipsFailures(t *testing.T) {
	t.Parallel()
	a := newFakeSource("eastmoney", nil)
	a.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		return nil, errors.New("timeout")
	})
	b := newFakeSource("sina", nil)
	b.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		return nil, errors.New("also down")
	})
	v := NewCrossSourceQuoteValidator([]Source{a, b}, 0.005)
	out, err := v.Validate(context.Background(), "600519")
	require.NoError(t, err)
	assert.Nil(t, out, "with < 2 sources producing data, no comparison is possible")
}

func TestCrossSourceValidatorEmptyCode(t *testing.T) {
	t.Parallel()
	v := NewCrossSourceQuoteValidator(nil, 0.005)
	_, err := v.Validate(context.Background(), "")
	assert.Error(t, err)
}

func TestCrossSourceValidatorDefaultThreshold(t *testing.T) {
	t.Parallel()
	v := NewCrossSourceQuoteValidator(nil, 0)
	assert.Equal(t, 0.005, v.Threshold)
}

func TestCrossSourceValidatorAllMatch(t *testing.T) {
	t.Parallel()
	a := newFakeSource("eastmoney", &Quote{Price: 100.0})
	b := newFakeSource("sina", &Quote{Price: 100.1})
	c := newFakeSource("akshare", &Quote{Price: 100.2})
	v := NewCrossSourceQuoteValidator([]Source{a, b, c}, 0.005)
	out, err := v.Validate(context.Background(), "600519")
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestCrossSourceValidatorRespectsTimeout(t *testing.T) {
	t.Parallel()
	a := newFakeSource("eastmoney", nil)
	a.setQuoteFn(func(ctx context.Context, code string) (*Quote, error) {
		select {
		case <-time.After(500 * time.Millisecond):
			return &Quote{Price: 100.0}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	v := NewCrossSourceQuoteValidator([]Source{a}, 0.005)
	v.Timeout = 50 * time.Millisecond
	// Only one source — comparison can't run anyway.
	_, err := v.Validate(context.Background(), "600519")
	require.NoError(t, err)
}
