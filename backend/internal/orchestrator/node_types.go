// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package orchestrator

// Reserved input keys (read by every Node from its Run() inputs map).
//
// The Executor injects these before every Node.Run call so the Node
// implementation can read its own Params + introspection metadata
// without having to thread them through a separate parameter list.
const (
	// InputKeyParams holds the node's raw JSON params as []byte.
	InputKeyParams = "_params"
	// InputKeyType holds the node's primary type (e.g. "data_source").
	InputKeyType = "_type"
	// InputKeySubtype holds the node's subtype (e.g. "ma" under
	// "indicator"). Empty string if none.
	InputKeySubtype = "_subtype"
	// InputKeyID holds the node's ID within the DAG.
	InputKeyID = "_id"
)

// NodeType constants are the canonical names of the built-in nodes.
// They are exported so node implementations and tests can reference
// the same string without typos.
const (
	NodeTypeDataSource = "data_source"
	NodeTypeIndicator  = "indicator"
	NodeTypeFilter     = "filter"
	NodeTypeRank       = "rank"
	NodeTypeDedupe     = "dedupe"
	NodeTypeSessionTag = "session_tag"
	NodeTypePersist    = "persist"
	NodeTypeNotify     = "notify"
)

// VarKey constants are the canonical RunContext.Vars keys used
// by built-in nodes to publish values for later nodes to read.
const (
	// VarKeySessionTag is the session tag (MORNING / AFTERNOON /
	// NO_POST / REVIEW) determined by the session_tag node and
	// consumed by the persist node when attaching tags to
	// recommendations.
	VarKeySessionTag = "session_tag"
)
