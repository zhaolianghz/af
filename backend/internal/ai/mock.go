// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// mockClient is a deterministic, rule-based stand-in for a real LLM.
// It understands a handful of common Chinese instructions so the whole
// §11 pipeline (preview → diff → apply → audit) works end to end with
// zero credentials. Swap in openAIClient (config ai.provider=openai)
// for genuine natural-language understanding.
type mockClient struct{}

// NewMockClient returns the rule-based client.
func NewMockClient() Client { return &mockClient{} }

func (m *mockClient) Name() string { return "mock" }

// reactFlow is the minimal mutable shape we parse/edit/re-emit. We keep
// data.params as a raw map so we can tweak individual fields without a
// full typed model of every node kind.
type reactFlow struct {
	Nodes []struct {
		ID       string                 `json:"id"`
		Type     string                 `json:"type"`
		Position map[string]any         `json:"position"`
		Data     map[string]any         `json:"data"`
	} `json:"nodes"`
	Edges []map[string]any `json:"edges"`
}

var reMAPeriod = regexp.MustCompile(`(?:ma|均线|周期).*?(\d+)`)
var reTopN = regexp.MustCompile(`(?:top|取|前|保留).*?(\d+)`)

// ProposeDAG applies a best-effort rule transform. Unrecognized
// instructions return the DAG unchanged with a marker so the caller's
// diff shows "no change" rather than erroring.
func (m *mockClient) ProposeDAG(_ context.Context, currentDAG, instruction string) (string, error) {
	var rf reactFlow
	if err := json.Unmarshal([]byte(currentDAG), &rf); err != nil {
		return "", fmt.Errorf("mock: parse current dag: %w", err)
	}
	instr := strings.ToLower(instruction)

	changed := false
	switch {
	// "把 MA 周期改成 10" / "均线周期 20"
	case strings.Contains(instr, "ma") || strings.Contains(instruction, "均线") || strings.Contains(instruction, "周期"):
		if mm := reMAPeriod.FindStringSubmatch(instr); mm != nil {
			if n, err := strconv.Atoi(mm[1]); err == nil {
				changed = m.setIndicatorPeriod(&rf, "ma", n) || changed
			}
		}
	// "top 取 5" / "保留前 3 只"
	case strings.Contains(instr, "top") || strings.Contains(instruction, "保留") || strings.Contains(instruction, "取前") || strings.Contains(instruction, "前"):
		if mm := reTopN.FindStringSubmatch(instr); mm != nil {
			if n, err := strconv.Atoi(mm[1]); err == nil {
				changed = m.setRankTop(&rf, n) || changed
			}
		}
	}

	out, err := json.Marshal(rf)
	if err != nil {
		return "", fmt.Errorf("mock: marshal: %w", err)
	}
	if !changed {
		// Return unchanged; the service diff will report "无变化" and
		// the user learns the mock didn't understand the instruction.
		return currentDAG, nil
	}
	return string(out), nil
}

func (m *mockClient) setIndicatorPeriod(rf *reactFlow, subtype string, period int) bool {
	changed := false
	for i := range rf.Nodes {
		n := &rf.Nodes[i]
		if n.Type != "indicator" || n.Data == nil {
			continue
		}
		params, _ := n.Data["params"].(map[string]any)
		if params == nil {
			continue
		}
		if st, _ := params["subtype"].(string); st == subtype {
			params["period"] = period
			changed = true
		}
	}
	return changed
}

func (m *mockClient) setRankTop(rf *reactFlow, top int) bool {
	changed := false
	for i := range rf.Nodes {
		n := &rf.Nodes[i]
		if n.Type != "rank" || n.Data == nil {
			continue
		}
		params, _ := n.Data["params"].(map[string]any)
		if params == nil {
			continue
		}
		params["top"] = top
		changed = true
	}
	return changed
}

// Summarize returns a deterministic, template-based "review" for the
// mock backend. It echoes the structured data the caller already
// embedded in the user prompt rather than inventing prose — enough to
// prove the §14.9 pipeline without an LLM. Swap to provider=openai for
// genuine narrative summaries.
func (m *mockClient) Summarize(_ context.Context, _ string, user string) (string, error) {
	return "【自动复盘 · mock】\n\n" + user +
		"\n\n（这是规则式占位摘要。配置 ai.provider=openai 可生成真正的自然语言复盘。）", nil
}
