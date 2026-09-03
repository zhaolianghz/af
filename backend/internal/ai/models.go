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

// isChatModel heuristically splits chat-capable models from
// embedding/image/audio/rerank entries in a provider's model list, so
// the settings UI can default the dropdown to useful entries. This
// mirrors the reference implementation's split_models.
func isChatModel(id string) bool {
	id = strings.ToLower(id)
	for _, kw := range []string{"embed", "embedding", "bge-", "rerank", "tts", "whisper",
		"speech", "audio", "dall-e", "dalle", "image", "flux", "stable-diffusion",
		"sd3", "sdxl", "kolors", "wanx", "cogview", "seedream", "jimeng", "midjourney"} {
		if strings.Contains(id, kw) {
			return false
		}
	}
	return true
}

// ListModels fetches GET {base}/models with the given key (OpenAI-
// compatible). Returns the full id list plus the chat-filtered subset.
// Used by the settings page to populate a model dropdown instead of
// forcing the operator to hand-type model names.
func ListModels(ctx context.Context, baseURL, apiKey string, timeout time.Duration) (all []string, chat []string, err error) {
	base := normBase(baseURL)
	if base == "" || apiKey == "" {
		return nil, nil, fmt.Errorf("需要 base_url 和 api_key")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, nil, fmt.Errorf("key 鉴权失败 (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 {
		return nil, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var mr modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, nil, fmt.Errorf("decode: %w", err)
	}
	all = make([]string, 0, len(mr.Data))
	for _, m := range mr.Data {
		if m.ID == "" {
			continue
		}
		all = append(all, m.ID)
		if isChatModel(m.ID) {
			chat = append(chat, m.ID)
		}
	}
	return all, chat, nil
}
