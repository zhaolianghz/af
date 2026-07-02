// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package orchestrator

import (
	"encoding/json"
	"fmt"
)

// ParseReactFlowJSON decodes a ReactFlow native JSON document into
// an internal DAG.
//
// ReactFlow's on-disk shape is:
//
//	{
//	  "nodes": [
//	    {
//	      "id": "a",
//	      "type": "indicator",
//	      "position": {"x": 0, "y": 0},
//	      "data": { "subtype": "ma", "params": { "period": 5 } }
//	    },
//	    ...
//	  ],
//	  "edges": [
//	    { "id": "e1", "source": "a", "target": "b", "sourceHandle": "out", "targetHandle": "in" },
//	    ...
//	  ]
//	}
//
// The "type" and "data.subtype" fields are the values passed to
// Registry.Get / GetBySubtype. data.params may be either a JSON
// object or a string; in the latter case it is JSON-decoded again.
func ParseReactFlowJSON(raw string) (*DAG, error) {
	if raw == "" {
		return nil, fmt.Errorf("%w: empty json", ErrInvalidDAG)
	}

	// reactFlowDoc is the on-disk shape. The internal DAG is
	// produced by the translator below.
	var doc struct {
		Nodes []struct {
			ID       string          `json:"id"`
			Type     string          `json:"type"`
			Position *Position       `json:"position"`
			Data     struct {
				Subtype string          `json:"subtype"`
				Params  json.RawMessage `json:"params"`
			} `json:"data"`
		} `json:"nodes"`
		Edges []struct {
			ID           string `json:"id"`
			Source       string `json:"source"`
			SourceHandle string `json:"sourceHandle"`
			Target       string `json:"target"`
			TargetHandle string `json:"targetHandle"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("%w: parse json: %v", ErrInvalidDAG, err)
	}

	dag := &DAG{
		Nodes: make([]NodeDef, 0, len(doc.Nodes)),
		Edges: make([]Edge, 0, len(doc.Edges)),
	}
	for _, n := range doc.Nodes {
		params, err := normalizeParams(n.Data.Params)
		if err != nil {
			return nil, fmt.Errorf("%w: node %q params: %v", ErrInvalidDAG, n.ID, err)
		}
		dag.Nodes = append(dag.Nodes, NodeDef{
			ID:       n.ID,
			Type:     n.Type,
			Subtype:  n.Data.Subtype,
			Params:   params,
			Position: n.Position,
		})
	}
	for _, e := range doc.Edges {
		dag.Edges = append(dag.Edges, Edge{
			ID:           e.ID,
			Source:       e.Source,
			SourceHandle: e.SourceHandle,
			Target:       e.Target,
			TargetHandle: e.TargetHandle,
		})
	}

	if err := dag.Validate(); err != nil {
		return nil, err
	}
	return dag, nil
}

// ParseDAG is the alias used by the executor for clarity. The
// internal canonical format is the ReactFlow native format; this
// function is the entry point.
func ParseDAG(raw string) (*DAG, error) { return ParseReactFlowJSON(raw) }

// MarshalDAG renders a DAG back into the ReactFlow native JSON
// form. Round-tripping a DAG through MarshalDAG → ParseDAG must
// produce an equal DAG (modulo unknown fields being dropped).
func MarshalDAG(d *DAG) (string, error) {
	if d == nil {
		return "", fmt.Errorf("%w: nil dag", ErrInvalidDAG)
	}
	doc := struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}{
		Nodes: make([]map[string]any, 0, len(d.Nodes)),
		Edges: make([]map[string]any, 0, len(d.Edges)),
	}
	for _, n := range d.Nodes {
		entry := map[string]any{
			"id":   n.ID,
			"type": n.Type,
			"data": map[string]any{
				"subtype": n.Subtype,
				"params":  rawOrEmpty(n.Params),
			},
		}
		if n.Position != nil {
			entry["position"] = map[string]any{"x": n.Position.X, "y": n.Position.Y}
		}
		doc.Nodes = append(doc.Nodes, entry)
	}
	for _, e := range d.Edges {
		entry := map[string]any{
			"source": e.Source,
			"target": e.Target,
		}
		if e.ID != "" {
			entry["id"] = e.ID
		}
		if e.SourceHandle != "" {
			entry["sourceHandle"] = e.SourceHandle
		}
		if e.TargetHandle != "" {
			entry["targetHandle"] = e.TargetHandle
		}
		doc.Edges = append(doc.Edges, entry)
	}
	buf, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("%w: marshal: %v", ErrInvalidDAG, err)
	}
	return string(buf), nil
}

func rawOrEmpty(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	// Re-marshal to drop the RawMessage wrapper type, so the
	// output is plain JSON.
	var v any
	if err := json.Unmarshal(r, &v); err != nil {
		// Fall back to string.
		return string(r)
	}
	return v
}

// normalizeParams accepts either a JSON object/array or a JSON
// string containing JSON. The latter is what some ReactFlow
// custom-node implementations emit. We always return a RawMessage
// that is itself valid JSON (object or array).
func normalizeParams(p json.RawMessage) (json.RawMessage, error) {
	if len(p) == 0 {
		return json.RawMessage("{}"), nil
	}
	// Try to unmarshal as a string; if that works, decode the
	// inner string as JSON.
	var asString string
	if err := json.Unmarshal(p, &asString); err == nil {
		inner := json.RawMessage(asString)
		if len(inner) == 0 {
			return json.RawMessage("{}"), nil
		}
		// Sanity-check the inner JSON.
		var probe any
		if err := json.Unmarshal(inner, &probe); err != nil {
			return nil, fmt.Errorf("params string is not valid json: %v", err)
		}
		return inner, nil
	}
	// Otherwise it should already be an object/array; sanity-check.
	var probe any
	if err := json.Unmarshal(p, &probe); err != nil {
		return nil, fmt.Errorf("params is not valid json: %v", err)
	}
	return p, nil
}
