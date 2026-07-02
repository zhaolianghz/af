// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package datasource

import (
	"fmt"
	"strings"
)

// Market is the canonical exchange identifier for an A-share code.
type Market string

const (
	MarketSH Market = "SH" // Shanghai
	MarketSZ Market = "SZ" // Shenzhen
	MarketBJ Market = "BJ" // Beijing
)

// SplitCode parses an A-share stock code into its 6-digit numeric
// part and exchange Market. It accepts BOTH canonical app forms:
//
//	"600519.SH"  → ("600519", MarketSH)   // suffixed (templates, DB)
//	"600519"     → ("600519", MarketSH)   // bare (market inferred)
//
// The suffix is authoritative when present. For a bare code the
// market is inferred from the leading digit:
//
//	6, 9          → SH
//	0, 3          → SZ
//	4, 8          → BJ
//
// Returning the split form lets every upstream adapter build its
// own wire format (eastmoney "1.600519", sina "sh600519", …)
// without each one re-implementing — and re-breaking — the parse.
// Before this helper, eastmoney.toSecID and sina.sinaPrefix both
// hard-required len==6, so every bundled template (which ships
// suffixed codes like "600519.SH") failed at the data_source node
// with "expected 6-digit code".
func SplitCode(stockCode string) (digits string, market Market, err error) {
	s := strings.TrimSpace(strings.ToUpper(stockCode))
	if s == "" {
		return "", "", fmt.Errorf("empty stock code")
	}

	// Suffix form: NNNNNN.SH / .SZ / .BJ — the suffix wins.
	if dot := strings.LastIndexByte(s, '.'); dot >= 0 {
		digits = s[:dot]
		suffix := s[dot+1:]
		if len(digits) != 6 || !allDigits(digits) {
			return "", "", fmt.Errorf("expected 6-digit code before suffix, got %q", stockCode)
		}
		switch Market(suffix) {
		case MarketSH, MarketSZ, MarketBJ:
			return digits, Market(suffix), nil
		default:
			return "", "", fmt.Errorf("unknown exchange suffix %q in %q", suffix, stockCode)
		}
	}

	// Bare form: infer the market from the leading digit.
	if len(s) != 6 || !allDigits(s) {
		return "", "", fmt.Errorf("expected 6-digit code, got %q", stockCode)
	}
	switch s[0] {
	case '6', '9':
		return s, MarketSH, nil
	case '0', '3':
		return s, MarketSZ, nil
	case '4', '8':
		return s, MarketBJ, nil
	}
	return "", "", fmt.Errorf("unrecognized A-share prefix in %q", stockCode)
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
