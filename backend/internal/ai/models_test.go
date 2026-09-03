// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormBase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://api.deepseek.com/v1", "https://api.deepseek.com/v1"},
		{"https://api.deepseek.com/v1/", "https://api.deepseek.com/v1"},
		{" https://api.deepseek.com/v1 ", "https://api.deepseek.com/v1"},
		// Full endpoints pasted from provider docs — the suffix must go.
		{"https://api.deepseek.com/v1/chat/completions", "https://api.deepseek.com/v1"},
		{"https://api.siliconflow.cn/v1/embeddings", "https://api.siliconflow.cn/v1"},
		{"https://x.example.com/v1/models", "https://x.example.com/v1"},
		{"https://x.example.com/v1/models/", "https://x.example.com/v1"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, normBase(c.in), "normBase(%q)", c.in)
	}
}

func TestListModels(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "deepseek-chat"},
				{"id": "deepseek-reasoner"},
				{"id": "text-embedding-3-small"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Pasting the full /chat/completions endpoint must still work.
	models, err := ListModels(context.Background(), srv.URL+"/v1/chat/completions", "sk-test", 5*time.Second)
	require.NoError(t, err)
	// Full list, unfiltered — embedding models stay selectable.
	assert.Equal(t, []string{"deepseek-chat", "deepseek-reasoner", "text-embedding-3-small"}, models)

	// Bad key → 401 surfaces as an auth error.
	_, err = ListModels(context.Background(), srv.URL+"/v1", "sk-wrong", 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "鉴权失败")
}

func TestListModelsRequiresKey(t *testing.T) {
	_, err := ListModels(context.Background(), "https://x.example.com/v1", "", time.Second)
	require.Error(t, err)
}
