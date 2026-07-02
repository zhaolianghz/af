// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package openapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestSpec_ServesValidOpenAPI(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/api/v1/openapi.json", nil)
	c.Request = req
	Spec()(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	// The body must be valid JSON.
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("spec is not valid JSON: %v\nbody: %s", err, w.Body.String())
	}
	if raw["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v, want 3.0.3", raw["openapi"])
	}

	// Every $ref in the spec must point to an existing schema or
	// response — otherwise Swagger UI will render a broken link.
	raw2 := w.Body.String()
	refs := findRefs(raw2)
	schemas, _ := raw["components"].(map[string]any)["schemas"].(map[string]any)
	responses, _ := raw["components"].(map[string]any)["responses"].(map[string]any)
	parameters, _ := raw["components"].(map[string]any)["parameters"].(map[string]any)

	for _, r := range refs {
		if !strings.HasPrefix(r, "#/components/") {
			t.Errorf("external $ref not supported: %q", r)
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(r, "#/components/"), "/", 2)
		if len(parts) != 2 {
			t.Errorf("malformed $ref: %q", r)
			continue
		}
		bucket, name := parts[0], parts[1]
		switch bucket {
		case "schemas":
			if _, ok := schemas[name]; !ok {
				t.Errorf("$ref %q points to missing schema %q", r, name)
			}
		case "responses":
			if _, ok := responses[name]; !ok {
				t.Errorf("$ref %q points to missing response %q", r, name)
			}
		case "parameters":
			if _, ok := parameters[name]; !ok {
				t.Errorf("$ref %q points to missing parameter %q", r, name)
			}
		}
	}

	// Coverage check: every wired HTTP route (per the handlers in
	// the backend) must appear in the spec, otherwise the docs
	// silently drift.
	requiredPaths := []string{
		"/healthz",
		"/api/v1/healthz",
		"/api/v1/ping",
		"/api/v1/strategies",
		"/api/v1/strategies/{id}",
		"/api/v1/strategies/{id}/clone",
		"/api/v1/strategies/{id}/export",
		"/api/v1/strategies/import",
		"/api/v1/strategies/{id}/trial-run",
		"/api/v1/strategies/{id}/trial-run/node/{nodeId}",
		"/api/v1/strategies/templates",
		"/api/v1/strategies/from-template/{code}",
		"/api/v1/runs",
		"/api/v1/runs/{id}",
		"/api/v1/runs/{id}/logs",
		"/api/v1/runs/{id}/retry",
		"/api/v1/runs/{id}/events",
		"/api/v1/recommendations",
		"/api/v1/perf/recommendations/{id}",
		"/api/v1/perf/recommendations/{id}/history",
		"/api/v1/perf/calculate",
		"/api/v1/perf/aggregations",
		"/api/v1/notify/test",
		"/api/v1/notify/health",
		"/api/v1/datasource/health",
	}
	paths, _ := raw["paths"].(map[string]any)
	for _, p := range requiredPaths {
		if _, ok := paths[p]; !ok {
			t.Errorf("path %q is not documented in the spec", p)
		}
	}
}

func TestDocs_ServesSwaggerUIHTML(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/docs", nil)
	c.Request = req
	Docs()(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "swagger-ui") {
		t.Error("page does not load swagger-ui")
	}
	if !strings.Contains(body, "/api/v1/openapi.json") {
		t.Error("page does not reference the spec URL")
	}
}

// findRefs returns every JSON-pointer-style $ref value in a raw
// JSON document. We only need string-literal matches, not full
// JSON walking — the spec is small and the regex is cheap.
func findRefs(s string) []string {
	var out []string
	needle := `"$ref":"`
	i := 0
	for {
		j := strings.Index(s[i:], needle)
		if j < 0 {
			break
		}
		start := i + j + len(needle)
		end := strings.Index(s[start:], `"`)
		if end < 0 {
			break
		}
		out = append(out, s[start:start+end])
		i = start + end + 1
	}
	return out
}
