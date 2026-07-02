// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package nodes

import "github.com/skyzhao/af/internal/orchestrator"

// RegisterAll registers every built-in node implementation against
// the provided Registry. Call this once at startup after building
// the registry; the orchestrator package exposes
// orchestrator.NewRegistry().
//
// New built-in nodes must be appended here so the executor can
// resolve them. The order does not matter for execution
// correctness, but is stable so registry dumps are deterministic.
func RegisterAll(reg *orchestrator.Registry) {
	if reg == nil {
		return
	}
	reg.MustRegister(NewDataSourceNode())
	reg.MustRegister(NewIndicatorNode())
	reg.MustRegister(NewFilterNode())
	reg.MustRegister(NewRankNode())
	reg.MustRegister(NewDedupeNode())
	reg.MustRegister(NewSessionTagNode())
	reg.MustRegister(NewPersistNode())
	reg.MustRegister(NewNotifyNode())
}
