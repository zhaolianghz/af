// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for the node registration helper — verifies that
// RegisterAll installs every built-in node into a fresh registry
// and that nil-registries are handled defensively.
package nodes

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/orchestrator"
)

func TestRegisterAll_RegistersEveryBuiltin(t *testing.T) {
	reg := orchestrator.NewRegistry()
	RegisterAll(reg)

	// All eight built-in types must be resolvable.
	types := []string{
		orchestrator.NodeTypeDataSource,
		orchestrator.NodeTypeIndicator,
		orchestrator.NodeTypeFilter,
		orchestrator.NodeTypeRank,
		orchestrator.NodeTypeDedupe,
		orchestrator.NodeTypeSessionTag,
		orchestrator.NodeTypePersist,
		orchestrator.NodeTypeNotify,
	}
	for _, typ := range types {
		// Subtype "" resolves to the catch-all per type.
		n, ok := reg.GetBySubtype(typ, "")
		require.True(t, ok, "missing type %q", typ)
		require.NotNil(t, n)
	}
}

func TestRegisterAll_NilRegistry_NoOp(t *testing.T) {
	// Should not panic.
	RegisterAll(nil)
}
