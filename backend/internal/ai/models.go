// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// normBase canonicalizes a user-pasted OpenAI-compatible base URL.
// Operators frequently paste the FULL endpoint copied from provider
// docs ("https://api.deepseek.com/v1/chat/completions") — appending
// "/chat/completions" to that would 404. Strip the known suffixes so
// the stored base is always the bare ".../v1" root. Also drops
// trailing slashes and whitespace.
func normBase(base string) string {
	b := strings.TrimRight(strings.TrimSpace(base), "/")
	for _, suf := range []string{"/chat/completions", "/embeddings", "/models"} {
		if strings.HasSuffix(b, suf) {
			b = strings.TrimSuffix(b, suf)
		}
	}
	return strings.TrimRight(b, "/")
}

// modelsResponse is the OpenAI-compatible GET /models payload
// (data: [{id: "deepseek-chat"}, ...]).
type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// ListModels fetches GET {base}/models with the given key (OpenAI-
// compatible) and returns the provider's full model-id list — no
// filtering: the list is the provider's menu, the operator picks.
// Used by the settings page to populate a model dropdown instead of
// forcing the operator to hand-type model names.
func ListModels(ctx context.Context, baseURL, apiKey string, timeout time.Duration) ([]string, error) {
	base := normBase(baseURL)
	if base == "" || apiKey == "" {
		return nil, fmt.Errorf("需要 base_url 和 api_key")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("key 鉴权失败 (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var mr modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := make([]string, 0, len(mr.Data))
	for _, m := range mr.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}
