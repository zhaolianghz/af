// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package ai

import (
	"context"
	"fmt"
	"strings"
)

// chainClient is a Client that wraps an ordered list of backends and
// tries them in priority order: the first one that succeeds wins, and
// any that fail (network error, HTTP 4xx/5xx, timeout, rate-limit, empty
// response) are skipped so the next backend gets a turn. This is the
// LLM analogue of the datasource manager's source fallback — when one
// model/provider is unavailable, the others keep the assistant working.
//
// It implements Client, so it drops into the existing hot-swappable
// Provider with zero changes to ai.Service / review.Service.
//
// Note on cost: this is SEQUENTIAL, not parallel — a healthy first
// backend means exactly one upstream call. Later backends are only hit
// when earlier ones fail, so the common case costs one provider's tokens.
type chainClient struct {
	backends []namedClient
}

// namedClient pairs a Client with the human label used in logs / audit
// (e.g. "deepseek:deepseek-chat"). The label is the inner client's
// Name() captured at build time.
type namedClient struct {
	name   string
	client Client
}

// NewChainClient builds a fallback chain from an ordered slice of
// clients (index 0 = highest priority). nil entries are dropped. With
// zero usable clients it returns nil, so callers treat "no backends" as
// "disabled" exactly like a nil Client.
func NewChainClient(clients []Client) Client {
	bs := make([]namedClient, 0, len(clients))
	for _, c := range clients {
		if c == nil {
			continue
		}
		bs = append(bs, namedClient{name: c.Name(), client: c})
	}
	if len(bs) == 0 {
		return nil
	}
	// A single backend needs no chain wrapper — return it directly so
	// Name() and behavior are identical to the non-chained case.
	if len(bs) == 1 {
		return bs[0].client
	}
	return &chainClient{backends: bs}
}

// Name lists the chain members in order, e.g.
// "chain[deepseek:deepseek-chat>glm:glm-4.5>minimax:MiniMax-M2]".
func (c *chainClient) Name() string {
	names := make([]string, len(c.backends))
	for i, b := range c.backends {
		names[i] = b.name
	}
	return "chain[" + strings.Join(names, ">") + "]"
}

// ProposeDAG tries each backend in order, returning the first success.
// A backend "fails" on any non-nil error OR an empty result (an empty
// proposal is useless and should fall through to the next model). The
// returned error aggregates every backend's failure for diagnosis.
func (c *chainClient) ProposeDAG(ctx context.Context, currentDAG, instruction string) (string, error) {
	var errs []string
	for _, b := range c.backends {
		// Honor a cancelled/expired parent context before trying the
		// next backend — no point burning the remaining providers if
		// the caller already gave up.
		if err := ctx.Err(); err != nil {
			return "", err
		}
		out, err := b.client.ProposeDAG(ctx, currentDAG, instruction)
		if err == nil && strings.TrimSpace(out) != "" {
			return out, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %s", b.name, reason(err, out)))
	}
	return "", fmt.Errorf("ai: all %d backends failed: %s", len(c.backends), strings.Join(errs, "; "))
}

// Summarize tries each backend in order, returning the first non-empty
// success. Same fallback semantics as ProposeDAG.
func (c *chainClient) Summarize(ctx context.Context, system, user string) (string, error) {
	var errs []string
	for _, b := range c.backends {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		out, err := b.client.Summarize(ctx, system, user)
		if err == nil && strings.TrimSpace(out) != "" {
			return out, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %s", b.name, reason(err, out)))
	}
	return "", fmt.Errorf("ai: all %d backends failed: %s", len(c.backends), strings.Join(errs, "; "))
}

// reason renders a backend's failure for the aggregate error: the error
// text, or "empty response" when it returned nothing without erroring.
func reason(err error, out string) string {
	if err != nil {
		return err.Error()
	}
	if strings.TrimSpace(out) == "" {
		return "empty response"
	}
	return "ok"
}
