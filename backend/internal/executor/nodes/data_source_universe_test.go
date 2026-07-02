// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package nodes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeCodes_DedupeOrderStable(t *testing.T) {
	// explicit first, resolved appended, dupes + empties dropped.
	got := mergeCodes(
		[]string{"600519.SH", "000001.SZ"},
		[]string{"000001.SZ", "601318.SH", ""},
	)
	require.Equal(t, []string{"600519.SH", "000001.SZ", "601318.SH"}, got)
}

func TestMergeCodes_EmptyExplicit(t *testing.T) {
	got := mergeCodes(nil, []string{"600519.SH", "600519.SH"})
	require.Equal(t, []string{"600519.SH"}, got)
}
