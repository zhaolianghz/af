// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package ai implements the §11 conversational strategy editor: a
// natural-language instruction is turned into a proposed DAG, shown
// to the user as a diff (preview), and only written on explicit
// confirm (apply). The LLM is OFF the critical path — it only
// proposes; validation + the two-stage commit are deterministic Go.
package ai

import (
	"context"
	"errors"
)

// ErrDisabled is returned by Provider when no LLM backend is active.
var ErrDisabled = errors.New("ai: assistant disabled")

// Client is the pluggable LLM backend. Implementations: mockClient
// (rule-based, no creds) and openAIClient (OpenAI-compatible chat
// completions). The contract is intentionally narrow: given the
// current ReactFlow DAG JSON and a natural-language instruction,
// return a COMPLETE proposed ReactFlow DAG JSON. The caller validates
// the result with orchestrator.ParseDAG before doing anything with it,
// so a malformed LLM response fails safe (no mutation).
type Client interface {
	// ProposeDAG returns a modified DAG JSON. It must return ONLY the
	// JSON document (no prose, no code fences) — implementations strip
	// fences defensively.
	ProposeDAG(ctx context.Context, currentDAG, instruction string) (string, error)
	// Summarize returns a free-form completion for the given system +
	// user prompt. Used by §14.9 review generation (no DAG involved).
	Summarize(ctx context.Context, system, user string) (string, error)
	// Name identifies the backend for the audit log.
	Name() string
}

// systemPrompt instructs an LLM to behave as a DAG editor. Shared by
// the OpenAI client; the mock ignores it.
const systemPrompt = `你是 A 股选股策略的 DAG 编辑器。给你一个 ReactFlow 格式的 DAG JSON 和一句中文修改指令,
你必须返回修改后的【完整】 DAG JSON,只输出 JSON 本身,不要解释、不要 markdown 代码块。

DAG 结构: {"nodes":[{"id","type","position":{"x","y"},"data":{"subtype","params":{...}}}],"edges":[{"id","source","target"}]}
节点类型(type): data_source, indicator, filter, rank, dedupe, session_tag, persist
- indicator 的 subtype: ma/ema/macd/kdj/boll/volume_ratio, params 如 {"subtype":"ma","period":5}
- filter 的 params: {"field","op":"<|>|==|>=|<=","value"}
- rank 的 params: {"field","order":"asc|desc","top"}
保持其余节点和边不变,只按指令改动相关部分。返回的 JSON 必须能被解析且节点 id 唯一。`
