// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/model"
	"github.com/skyzhao/af/internal/orchestrator"
)

// strategyPort is the slice of the strategy service this package needs.
// Narrowed for testability and to avoid an import cycle on the concrete
// orchestrator.StrategyService.
type strategyPort interface {
	Get(ctx context.Context, id uint64) (*orchestrator.StrategyDetail, error)
	Update(ctx context.Context, id uint64, in orchestrator.UpdateStrategyInput) (*model.Strategy, *model.StrategyVersion, error)
}

// Service is the §11 use-case layer: turn an instruction into a
// validated DAG proposal (preview), then commit it on confirm (apply).
// The LLM only proposes; every mutation goes through ParseDAG
// validation and the strategy service's immutable-versioning Update.
type Service struct {
	db     *gorm.DB
	llm    Client
	strat  strategyPort
}

// NewService wires the AI service.
func NewService(db *gorm.DB, llm Client, strat strategyPort) *Service {
	return &Service{db: db, llm: llm, strat: strat}
}

// PreviewResult is what the preview endpoint returns: the proposed DAG
// (to be echoed back on apply), a human-readable diff, whether anything
// actually changed, and the audit row id.
type PreviewResult struct {
	StrategyID  uint64   `json:"strategy_id"`
	Instruction string   `json:"instruction"`
	CurrentDAG  string   `json:"current_dag"`
	ProposedDAG string   `json:"proposed_dag"`
	Changes     []string `json:"changes"`
	Changed     bool     `json:"changed"`
	AuditID     uint64   `json:"audit_id"`
	LLM         string   `json:"llm"`
}

// Preview runs the LLM, validates the proposal, computes a diff, and
// records a preview audit. It never mutates the strategy.
func (s *Service) Preview(ctx context.Context, strategyID uint64, instruction string) (*PreviewResult, error) {
	if s.llm == nil {
		return nil, apperr.Unavailable("ai: assistant disabled")
	}
	if instruction == "" {
		return nil, apperr.InvalidArg("instruction is required")
	}
	detail, err := s.strat.Get(ctx, strategyID)
	if err != nil {
		return nil, err
	}
	current := detail.CurrentVersionDAG

	proposed, err := s.llm.ProposeDAG(ctx, current, instruction)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "ai: llm propose", err)
	}
	// Validate the proposal — a malformed LLM response must fail here,
	// before the user ever sees an "apply" button that would corrupt
	// the strategy.
	if _, perr := orchestrator.ParseDAG(proposed); perr != nil {
		return nil, apperr.InvalidArg("ai: proposed dag is invalid: " + perr.Error())
	}
	changes := diffDAG(current, proposed)

	res := &PreviewResult{
		StrategyID:  strategyID,
		Instruction: instruction,
		CurrentDAG:  current,
		ProposedDAG: proposed,
		Changes:     changes,
		Changed:     len(changes) > 0,
		LLM:         s.llm.Name(),
	}
	res.AuditID = s.writeAudit(ctx, strategyID, instruction, model.AIDecisionPreview, changes)
	return res, nil
}

// ApplyResult is returned after a confirmed apply.
type ApplyResult struct {
	StrategyID uint64 `json:"strategy_id"`
	Version    int    `json:"version"`
	AuditID    uint64 `json:"audit_id"`
}

// Apply commits a previously-previewed DAG. The proposed DAG is
// re-validated server-side (never trust the client echo) and written
// through the strategy service, creating a new immutable version.
func (s *Service) Apply(ctx context.Context, strategyID uint64, proposedDAG, instruction string) (*ApplyResult, error) {
	if proposedDAG == "" {
		return nil, apperr.InvalidArg("proposed_dag is required")
	}
	if _, perr := orchestrator.ParseDAG(proposedDAG); perr != nil {
		return nil, apperr.InvalidArg("ai: proposed dag is invalid: " + perr.Error())
	}
	// Carry over the existing name/description/tags — StrategyService
	// .Update validates name as required and treats the input as the
	// full strategy, so an AI edit that only touches the DAG must
	// preserve the rest.
	detail, err := s.strat.Get(ctx, strategyID)
	if err != nil {
		return nil, err
	}
	_, ver, err := s.strat.Update(ctx, strategyID, orchestrator.UpdateStrategyInput{
		Name:        detail.Name,
		Description: detail.Description,
		Tags:        detail.Tags,
		DAGJson:     proposedDAG,
		ChangeNote:  "AI: " + instruction,
	})
	if err != nil {
		return nil, err
	}
	auditID := s.writeAudit(ctx, strategyID, instruction, model.AIDecisionApplied, nil)
	v := 0
	if ver != nil {
		v = ver.Version
	}
	return &ApplyResult{StrategyID: strategyID, Version: v, AuditID: auditID}, nil
}

// writeAudit records an AIAudit row best-effort (a logging failure must
// not fail the user action). Returns the row id (0 on failure).
func (s *Service) writeAudit(ctx context.Context, strategyID uint64, instruction, decision string, changes []string) uint64 {
	if s.db == nil {
		return 0
	}
	intent, _ := json.Marshal(map[string]any{"instruction": instruction, "llm": s.llm.Name()})
	diff, _ := json.Marshal(map[string]any{"changes": changes})
	a := &model.AIAudit{
		StrategyID:  strategyID,
		IntentJson:  string(intent),
		Decision:    decision,
		DagDiffJson: string(diff),
	}
	if err := s.db.WithContext(ctx).Create(a).Error; err != nil {
		return 0
	}
	return a.ID
}

// diffDAG produces a human-readable list of node-param changes between
// two ReactFlow DAGs. It is intentionally shallow (per-node params +
// node add/remove) — enough for the user to judge the proposal.
func diffDAG(current, proposed string) []string {
	cur := indexNodes(current)
	prop := indexNodes(proposed)
	out := make([]string, 0)

	for id, pn := range prop {
		cn, ok := cur[id]
		if !ok {
			out = append(out, fmt.Sprintf("新增节点 %s (%s)", id, pn.typ))
			continue
		}
		if cn.params != pn.params {
			out = append(out, fmt.Sprintf("修改节点 %s: %s → %s", id, cn.params, pn.params))
		}
	}
	for id, cn := range cur {
		if _, ok := prop[id]; !ok {
			out = append(out, fmt.Sprintf("删除节点 %s (%s)", id, cn.typ))
		}
	}
	return out
}

type nodeInfo struct {
	typ    string
	params string
}

// indexNodes maps node id → {type, canonical params JSON}. Malformed
// input yields an empty map (diff then reports everything as added,
// which is harmless — preview validation already gates apply).
func indexNodes(dag string) map[string]nodeInfo {
	var rf reactFlow
	out := map[string]nodeInfo{}
	if json.Unmarshal([]byte(dag), &rf) != nil {
		return out
	}
	for _, n := range rf.Nodes {
		var params string
		if n.Data != nil {
			if p, ok := n.Data["params"]; ok {
				b, _ := json.Marshal(p)
				params = string(b)
			}
		}
		out[n.ID] = nodeInfo{typ: n.Type, params: params}
	}
	return out
}
