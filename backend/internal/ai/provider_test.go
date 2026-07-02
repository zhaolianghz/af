// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProvider_DisabledWhenNil(t *testing.T) {
	p := NewProvider(nil)
	require.Equal(t, "disabled", p.Name())
	require.Nil(t, p.Current())
	_, err := p.ProposeDAG(context.Background(), "{}", "x")
	require.ErrorIs(t, err, ErrDisabled)
	_, err = p.Summarize(context.Background(), "sys", "usr")
	require.ErrorIs(t, err, ErrDisabled)
}

func TestProvider_HotSwap(t *testing.T) {
	// Start disabled, swap in mock at runtime → name + calls reflect it
	// without rebuilding the Provider (settings-page semantics).
	p := NewProvider(nil)
	require.Equal(t, "disabled", p.Name())

	p.Set(NewMockClient())
	require.Equal(t, "mock", p.Name())
	out, err := p.ProposeDAG(context.Background(), baseDAG, "把 ma 周期改成 10")
	require.NoError(t, err)
	require.Contains(t, out, `"period":10`)

	// Swap back to nil → disabled again.
	p.Set(nil)
	require.Equal(t, "disabled", p.Name())
	_, err = p.ProposeDAG(context.Background(), "{}", "x")
	require.ErrorIs(t, err, ErrDisabled)
}

func TestBuildClient(t *testing.T) {
	// mock needs nothing.
	c, msg := BuildClient("mock", "", "", "", 0)
	require.NotNil(t, c)
	require.Empty(t, msg)
	require.Equal(t, "mock", c.Name())

	// empty provider → mock too.
	c, msg = BuildClient("", "", "", "", 0)
	require.NotNil(t, c)
	require.Empty(t, msg)

	// openai-compatible with incomplete settings → nil + message.
	c, msg = BuildClient("deepseek", "", "", "", 0)
	require.Nil(t, c)
	require.NotEmpty(t, msg)

	// openai-compatible complete → a client.
	c, msg = BuildClient("deepseek", "https://api.deepseek.com/v1", "sk-x", "deepseek-chat", 0)
	require.NotNil(t, c)
	require.Empty(t, msg)
}
