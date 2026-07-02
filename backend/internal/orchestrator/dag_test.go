// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for ParseReactFlowJSON / MarshalDAG / normalizeParams.
// These pin the on-disk shape that the editor writes and the
// executor reads.
package orchestrator

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const sampleReactFlow = `{
  "nodes": [
    {
      "id": "ds1",
      "type": "data_source",
      "position": {"x": 0, "y": 0},
      "data": {
        "subtype": "quote",
        "params": {"stock_codes": ["600000.SH", "000001.SZ"]}
      }
    },
    {
      "id": "f1",
      "type": "filter",
      "position": {"x": 200, "y": 0},
      "data": {
        "subtype": "",
        "params": {"field": "close", "op": ">", "value": 10}
      }
    }
  ],
  "edges": [
    {"id": "e1", "source": "ds1", "target": "f1", "sourceHandle": "out", "targetHandle": "in"}
  ]
}`

func TestParseReactFlowJSON_HappyPath(t *testing.T) {
	d, err := ParseReactFlowJSON(sampleReactFlow)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Nodes) != 2 {
		t.Fatalf("nodes: got %d", len(d.Nodes))
	}
	if d.Nodes[0].ID != "ds1" || d.Nodes[0].Type != "data_source" {
		t.Errorf("node 0: %+v", d.Nodes[0])
	}
	if d.Nodes[0].Subtype != "quote" {
		t.Errorf("node 0 subtype: got %q", d.Nodes[0].Subtype)
	}
	if d.Nodes[0].Position == nil || d.Nodes[0].Position.X != 0 {
		t.Errorf("node 0 position: %+v", d.Nodes[0].Position)
	}
	// Params must be a JSON object — verify the round trip
	// by unmarshalling into a generic map.
	var p map[string]any
	if err := json.Unmarshal(d.Nodes[0].Params, &p); err != nil {
		t.Errorf("params unmarshal: %v", err)
	}
	if codes, ok := p["stock_codes"].([]any); !ok || len(codes) != 2 {
		t.Errorf("stock_codes: %+v", p["stock_codes"])
	}
	// Edges.
	if len(d.Edges) != 1 {
		t.Fatalf("edges: got %d", len(d.Edges))
	}
	if d.Edges[0].SourceHandle != "out" || d.Edges[0].TargetHandle != "in" {
		t.Errorf("edge handles: %+v", d.Edges[0])
	}
}

func TestParseReactFlowJSON_Empty(t *testing.T) {
	if _, err := ParseReactFlowJSON(""); !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("empty: got %v want ErrInvalidDAG", err)
	}
}

func TestParseReactFlowJSON_InvalidJSON(t *testing.T) {
	if _, err := ParseReactFlowJSON("{not json"); !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("bad json: got %v want ErrInvalidDAG", err)
	}
}

func TestParseReactFlowJSON_ParamsAsString(t *testing.T) {
	// Some ReactFlow custom nodes emit params as a JSON
	// string. The parser should accept that and re-decode.
	const doc = `{
      "nodes": [
        {"id": "a", "type": "x", "data": {"subtype": "", "params": "{\"k\":1}"}}
      ],
      "edges": []
    }`
	d, err := ParseReactFlowJSON(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var p map[string]any
	if err := json.Unmarshal(d.Nodes[0].Params, &p); err != nil {
		t.Fatalf("params unmarshal: %v", err)
	}
	if v, ok := p["k"]; !ok || v.(float64) != 1 {
		t.Errorf("params: got %+v", p)
	}
}

func TestParseReactFlowJSON_ParamsAsInvalidString(t *testing.T) {
	const doc = `{
      "nodes": [
        {"id": "a", "type": "x", "data": {"subtype": "", "params": "not json"}}
      ],
      "edges": []
    }`
	_, err := ParseReactFlowJSON(doc)
	if !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("bad params: got %v want ErrInvalidDAG", err)
	}
	if !strings.Contains(err.Error(), "params") {
		t.Errorf("err should mention params: %v", err)
	}
}

func TestParseReactFlowJSON_DefaultsToEmptyParams(t *testing.T) {
	const doc = `{
      "nodes": [{"id": "a", "type": "x", "data": {"subtype": ""}}],
      "edges": []
    }`
	d, err := ParseReactFlowJSON(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if string(d.Nodes[0].Params) != "{}" {
		t.Errorf("params: got %q want {}", d.Nodes[0].Params)
	}
}

func TestParseReactFlowJSON_InvalidatesDAG(t *testing.T) {
	// Two nodes with the same id.
	const doc = `{
      "nodes": [
        {"id": "a", "type": "x", "data": {}},
        {"id": "a", "type": "y", "data": {}}
      ],
      "edges": []
    }`
	_, err := ParseReactFlowJSON(doc)
	if !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("dup id: got %v want ErrInvalidDAG", err)
	}
}

func TestParseDAG_AliasForParseReactFlowJSON(t *testing.T) {
	d1, err := ParseReactFlowJSON(sampleReactFlow)
	if err != nil {
		t.Fatalf("ParseReactFlowJSON: %v", err)
	}
	d2, err := ParseDAG(sampleReactFlow)
	if err != nil {
		t.Fatalf("ParseDAG: %v", err)
	}
	if len(d1.Nodes) != len(d2.Nodes) || len(d1.Edges) != len(d2.Edges) {
		t.Errorf("alias produces different DAG: %v vs %v", d1, d2)
	}
}

func TestMarshalDAG_RoundTrip(t *testing.T) {
	d, err := ParseReactFlowJSON(sampleReactFlow)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := MarshalDAG(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Round-trip back to a DAG and compare node count + edge count.
	d2, err := ParseReactFlowJSON(out)
	if err != nil {
		t.Fatalf("Re-parse: %v", err)
	}
	if len(d2.Nodes) != len(d.Nodes) {
		t.Errorf("node count: got %d want %d", len(d2.Nodes), len(d.Nodes))
	}
	if len(d2.Edges) != len(d.Edges) {
		t.Errorf("edge count: got %d want %d", len(d2.Edges), len(d.Edges))
	}
	// Edge ID should be preserved.
	if d2.Edges[0].ID != d.Edges[0].ID {
		t.Errorf("edge id: got %q want %q", d2.Edges[0].ID, d.Edges[0].ID)
	}
}

func TestMarshalDAG_Nil(t *testing.T) {
	if _, err := MarshalDAG(nil); !errors.Is(err, ErrInvalidDAG) {
		t.Errorf("Marshal(nil): got %v want ErrInvalidDAG", err)
	}
}

func TestMarshalDAG_EmptyParamsOmitted(t *testing.T) {
	d := &DAG{
		Nodes: []NodeDef{{ID: "a", Type: "x", Subtype: ""}},
		Edges: nil,
	}
	out, err := MarshalDAG(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// The output should not have a "data.params": null
	// because rawOrEmpty returns nil for empty params.
	// json.Marshal drops nil fields inside a generic
	// map[string]any — we just check the doc parses.
	if _, err := ParseReactFlowJSON(out); err != nil {
		t.Errorf("Marshal output does not re-parse: %v", err)
	}
}

func TestNormalizeParams(t *testing.T) {
	// Object form passes through.
	in := json.RawMessage(`{"k":1}`)
	got, err := normalizeParams(in)
	if err != nil {
		t.Fatalf("object: %v", err)
	}
	if string(got) != `{"k":1}` {
		t.Errorf("object: got %s", got)
	}
	// String form is decoded.
	in = json.RawMessage(`"{\"k\":1}"`)
	got, err = normalizeParams(in)
	if err != nil {
		t.Fatalf("string: %v", err)
	}
	if string(got) != `{"k":1}` {
		t.Errorf("string: got %s", got)
	}
	// Empty string → {}.
	in = json.RawMessage(`""`)
	got, err = normalizeParams(in)
	if err != nil {
		t.Fatalf("empty string: %v", err)
	}
	if string(got) != "{}" {
		t.Errorf("empty string: got %s", got)
	}
	// Empty raw → {}.
	got, err = normalizeParams(nil)
	if err != nil {
		t.Fatalf("nil: %v", err)
	}
	if string(got) != "{}" {
		t.Errorf("nil: got %s", got)
	}
	// Invalid string form.
	in = json.RawMessage(`"not json"`)
	if _, err := normalizeParams(in); err == nil {
		t.Error("invalid string should error")
	}
	// JSON scalars are accepted (the parser only sanity-checks
	// that the input is valid JSON; structural validation is the
	// DAG's job, not normalizeParams').
	in = json.RawMessage(`123`)
	if _, err := normalizeParams(in); err != nil {
		t.Errorf("scalar should be accepted, got %v", err)
	}
}
