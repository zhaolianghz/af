// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// scriptClient is a Client whose responses are scripted per call.
type scriptClient struct {
	name       string
	proposeOut string
	proposeErr error
	summarize  string
	summErr    error
	calls      int
}

func (s *scriptClient) Name() string { return s.name }
func (s *scriptClient) ProposeDAG(_ context.Context, _, _ string) (string, error) {
	s.calls++
	return s.proposeOut, s.proposeErr
}
func (s *scriptClient) Summarize(_ context.Context, _, _ string) (string, error) {
	s.calls++
	return s.summarize, s.summErr
}

func TestChain_NilAndSingle(t *testing.T) {
	require.Nil(t, NewChainClient(nil), "no clients → nil (disabled)")
	require.Nil(t, NewChainClient([]Client{nil, nil}), "all-nil → nil")

	single := &scriptClient{name: "solo", proposeOut: "{}"}
	c := NewChainClient([]Client{single})
	require.Equal(t, "solo", c.Name(), "single backend returned directly, not wrapped")
}

func TestChain_FirstSucceeds(t *testing.T) {
	a := &scriptClient{name: "a", proposeOut: `{"nodes":[]}`}
	b := &scriptClient{name: "b", proposeOut: `{"nodes":[]}`}
	c := NewChainClient([]Client{a, b})
	out, err := c.ProposeDAG(context.Background(), "{}", "x")
	require.NoError(t, err)
	require.Equal(t, `{"nodes":[]}`, out)
	require.Equal(t, 1, a.calls)
	require.Equal(t, 0, b.calls, "second backend never touched when first succeeds")
}

func TestChain_FallsThroughOnError(t *testing.T) {
	a := &scriptClient{name: "a", proposeErr: errors.New("429 rate limited")}
	b := &scriptClient{name: "b", proposeOut: `{"nodes":[]}`}
	c := NewChainClient([]Client{a, b})
	out, err := c.ProposeDAG(context.Background(), "{}", "x")
	require.NoError(t, err)
	require.Equal(t, `{"nodes":[]}`, out)
	require.Equal(t, 1, a.calls)
	require.Equal(t, 1, b.calls, "fell through to second backend")
}

func TestChain_FallsThroughOnEmpty(t *testing.T) {
	// An empty (but non-error) response must also fall through — an empty
	// proposal is useless.
	a := &scriptClient{name: "a", proposeOut: "   "}
	b := &scriptClient{name: "b", proposeOut: `{"nodes":[]}`}
	c := NewChainClient([]Client{a, b})
	out, err := c.ProposeDAG(context.Background(), "{}", "x")
	require.NoError(t, err)
	require.Equal(t, `{"nodes":[]}`, out)
	require.Equal(t, 1, b.calls)
}

func TestChain_AllFailAggregates(t *testing.T) {
	a := &scriptClient{name: "deepseek", proposeErr: errors.New("timeout")}
	b := &scriptClient{name: "glm", proposeErr: errors.New("401 unauthorized")}
	c := NewChainClient([]Client{a, b})
	_, err := c.ProposeDAG(context.Background(), "{}", "x")
	require.Error(t, err)
	// Aggregate names every backend + its reason for diagnosis.
	require.Contains(t, err.Error(), "deepseek")
	require.Contains(t, err.Error(), "timeout")
	require.Contains(t, err.Error(), "glm")
	require.Contains(t, err.Error(), "401")
}

func TestChain_SummarizeFallback(t *testing.T) {
	a := &scriptClient{name: "a", summErr: errors.New("down")}
	b := &scriptClient{name: "b", summarize: "复盘内容"}
	c := NewChainClient([]Client{a, b})
	out, err := c.Summarize(context.Background(), "sys", "usr")
	require.NoError(t, err)
	require.Equal(t, "复盘内容", out)
	require.Equal(t, 1, a.calls)
	require.Equal(t, 1, b.calls)
}

func TestChain_RespectsCancelledContext(t *testing.T) {
	a := &scriptClient{name: "a", proposeErr: errors.New("fail")}
	b := &scriptClient{name: "b", proposeOut: `{"nodes":[]}`}
	c := NewChainClient([]Client{a, b})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	_, err := c.ProposeDAG(ctx, "{}", "x")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, a.calls, "cancelled context short-circuits before any call")
}

func TestChain_NameListsMembersInOrder(t *testing.T) {
	a := &scriptClient{name: "deepseek:deepseek-chat"}
	b := &scriptClient{name: "glm:glm-4.5"}
	c := NewChainClient([]Client{a, b})
	require.Equal(t, "chain[deepseek:deepseek-chat>glm:glm-4.5]", c.Name())
}
