// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for the SSE /events handler.
//
// End-to-end SSE streaming is hard to test in-process (DrainEvents
// blocks on the bus; cancellation propagation through gin is racy).
// Here we cover the parts that are easy to verify in-process:
//   - HTTP error paths (404 missing run, 400 bad id)
//   - The wire-format helpers (writeSSE / writeEvent)
//
// For the full streaming behaviour, see the orchestrator-level
// eventbus tests and the integration smoke tests in the frontend
// (e2e/run-detail.spec.ts mocks EventSource).
package executor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skyzhao/af/internal/orchestrator"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// HTTP error paths
// =============================================================================

func TestSSE_RejectsMissingRun(t *testing.T) {
	h, _, _ := newTestHandler(t)
	router := buildRouter(h)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := httpGet(srv.URL + "/api/v1/runs/9999/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 404, resp.StatusCode)
}

func TestSSE_RejectsInvalidID(t *testing.T) {
	h, _, _ := newTestHandler(t)
	router := buildRouter(h)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := httpGet(srv.URL + "/api/v1/runs/not-a-number/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 400, resp.StatusCode)
}

// httpGet is a thin wrapper so the test file doesn't import net/http.
func httpGet(url string) (*http.Response, error) {
	return http.DefaultClient.Get(url)
}

// =============================================================================
// Wire-format helpers (writeSSE / writeEvent)
// =============================================================================

func TestWriteSSE_EmitsEventAndDataLines(t *testing.T) {
	rec := httptest.NewRecorder()
	require.NoError(t, writeSSE(rec, "ready", `{"run_id":1}`))
	out := rec.Body.String()
	require.Equal(t, "event: ready\ndata: {\"run_id\":1}\n\n", out)
}

func TestWriteSSE_OmitsEventLineForEmptyName(t *testing.T) {
	rec := httptest.NewRecorder()
	require.NoError(t, writeSSE(rec, "", "hello"))
	out := rec.Body.String()
	require.Equal(t, "data: hello\n\n", out)
}

func TestWriteEvent_NamedEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	ev := orchestrator.Event{
		RunID:     7,
		Type:      orchestrator.EventNodeSuccess,
		NodeID:    "n1",
		Timestamp: time.Unix(0, 1737000000000000000),
		Data:      map[string]any{"status": "ok"},
	}
	require.NoError(t, writeEvent(rec, ev))
	out := rec.Body.String()
	// Expect: event, id (unix-nano), data lines.
	require.True(t, strings.HasPrefix(out, "event: node.success\n"))
	require.Contains(t, out, "id: 1737000000000000000\n")
	require.Contains(t, out, "data: {")
	require.Contains(t, out, "\"status\":\"ok\"")
	require.True(t, strings.HasSuffix(out, "\n\n"))
}

func TestWriteEvent_HeartbeatEmitsAsSpecialEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	ev := orchestrator.Event{
		Type:      "__heartbeat__",
		Timestamp: time.Unix(0, 1737000000000000000),
	}
	require.NoError(t, writeEvent(rec, ev))
	out := rec.Body.String()
	require.True(t, strings.HasPrefix(out, "event: heartbeat\n"))
	// Heartbeat has no `id:` line and uses a different payload.
	require.NotContains(t, out, "id: ")
	require.Contains(t, out, "data: {")
}
