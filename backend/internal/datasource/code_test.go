// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package datasource

import "testing"

func TestSplitCode(t *testing.T) {
	t.Parallel()
	type want struct {
		digits string
		market Market
	}
	ok := map[string]want{
		// bare, market inferred from leading digit
		"600519": {"600519", MarketSH},
		"900901": {"900901", MarketSH},
		"000001": {"000001", MarketSZ},
		"300750": {"300750", MarketSZ},
		"430047": {"430047", MarketBJ},
		"830799": {"830799", MarketBJ},
		// suffixed canonical form (templates / DB) — the bug case
		"600519.SH": {"600519", MarketSH},
		"000858.SZ": {"000858", MarketSZ},
		"601318.SH": {"601318", MarketSH},
		"430047.BJ": {"430047", MarketBJ},
		// case-insensitive + whitespace tolerant
		"600519.sh": {"600519", MarketSH},
		" 600519 ":  {"600519", MarketSH},
	}
	for in, w := range ok {
		d, m, err := SplitCode(in)
		if err != nil {
			t.Errorf("SplitCode(%q) unexpected err: %v", in, err)
			continue
		}
		if d != w.digits || m != w.market {
			t.Errorf("SplitCode(%q) = (%q,%q), want (%q,%q)", in, d, m, w.digits, w.market)
		}
	}

	bad := []string{
		"",
		"12345",       // too short
		"7xxxxx",      // non-digit
		"1234567",     // too long, no suffix
		"600519.XX",   // unknown suffix
		"12345.SH",    // wrong-length digits before suffix
		"700000",      // unrecognized prefix (bare)
		"abc.SH",      // non-digit before suffix
	}
	for _, in := range bad {
		if _, _, err := SplitCode(in); err == nil {
			t.Errorf("SplitCode(%q) expected error, got nil", in)
		}
	}
}
