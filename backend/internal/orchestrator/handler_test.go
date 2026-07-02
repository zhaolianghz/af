// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for StrategyHandler — wires the strategy CRUD service
// into Gin routes and verifies status codes + response envelopes.
package orchestrator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/httpresp"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestRouter wires the strategy CRUD routes against a fresh
// in-memory DB.
func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	svc, _ := newSvc(t)
	h := NewStrategyHandler(svc, nil)
	r := gin.New()
	g := r.Group("/api/v1/strategies")
	h.RegisterRoutes(g)
	return r
}

func doRequest(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, path, bodyReader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeOK(t *testing.T, w *httptest.ResponseRecorder) httpresp.OKResponse {
	t.Helper()
	var resp httpresp.OKResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func decodeErr(t *testing.T, w *httptest.ResponseRecorder) httpresp.ErrResponse {
	t.Helper()
	var resp httpresp.ErrResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// =============================================================================
// Create
// =============================================================================

func TestStrategyHandler_Create_201(t *testing.T) {
	r := newTestRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies", map[string]any{
		"code":     "h1",
		"name":     "Handler 1",
		"dag_json": sampleDAG,
	})
	require.Equal(t, http.StatusCreated, w.Code)
	resp := decodeOK(t, w)
	require.Equal(t, 0, resp.Code)
	require.Contains(t, resp.Message, "created")
}

func TestStrategyHandler_Create_400_InvalidJSON(t *testing.T) {
	r := newTestRouter(t)
	req, _ := http.NewRequest("POST", "/api/v1/strategies", bytes.NewReader([]byte(`{not json`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	resp := decodeErr(t, w)
	require.Equal(t, 10001, resp.Code) // CodeInvalidArg
}

func TestStrategyHandler_Create_400_MissingName(t *testing.T) {
	r := newTestRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies", map[string]any{
		"dag_json": sampleDAG,
	})
	require.Equal(t, http.StatusBadRequest, w.Code)
	resp := decodeErr(t, w)
	require.Equal(t, 10001, resp.Code)
}

// =============================================================================
// List
// =============================================================================

func TestStrategyHandler_List_200(t *testing.T) {
	r := newTestRouter(t)
	// Seed two strategies.
	for _, code := range []string{"l1", "l2"} {
		w := doRequest(t, r, "POST", "/api/v1/strategies", map[string]any{
			"code":     code,
			"name":     code,
			"dag_json": sampleDAG,
		})
		require.Equal(t, http.StatusCreated, w.Code)
	}
	w := doRequest(t, r, "GET", "/api/v1/strategies?page=1&page_size=10", nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeOK(t, w)
	require.Equal(t, 0, resp.Code)
}

// =============================================================================
// Detail
// =============================================================================

func TestStrategyHandler_Detail_200(t *testing.T) {
	r := newTestRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies", map[string]any{
		"code":     "d1",
		"name":     "Detail",
		"dag_json": sampleDAG,
	})
	require.Equal(t, http.StatusCreated, w.Code)
	created := decodeOK(t, w)
	strat := created.Data.(map[string]any)["strategy"].(map[string]any)
	id := strat["id"].(float64)
	w2 := doRequest(t, r, "GET", "/api/v1/strategies/"+itoa(int(id)), nil)
	require.Equal(t, http.StatusOK, w2.Code)
}

func TestStrategyHandler_Detail_400_BadID(t *testing.T) {
	r := newTestRouter(t)
	w := doRequest(t, r, "GET", "/api/v1/strategies/abc", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestStrategyHandler_Detail_404(t *testing.T) {
	r := newTestRouter(t)
	w := doRequest(t, r, "GET", "/api/v1/strategies/9999", nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

// =============================================================================
// Update
// =============================================================================

func TestStrategyHandler_Update_200(t *testing.T) {
	r := newTestRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies", map[string]any{
		"code":     "u1",
		"name":     "U1",
		"dag_json": sampleDAG,
	})
	require.Equal(t, http.StatusCreated, w.Code)
	created := decodeOK(t, w)
	id := created.Data.(map[string]any)["strategy"].(map[string]any)["id"].(float64)

	w2 := doRequest(t, r, "PUT", "/api/v1/strategies/"+itoa(int(id)), map[string]any{
		"name":        "U1 updated",
		"description": "new desc",
		"dag_json":    sampleDAG,
	})
	require.Equal(t, http.StatusOK, w2.Code)
}

// =============================================================================
// Delete
// =============================================================================

func TestStrategyHandler_Delete_200(t *testing.T) {
	r := newTestRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies", map[string]any{
		"code":     "del1",
		"name":     "Del1",
		"dag_json": sampleDAG,
	})
	require.Equal(t, http.StatusCreated, w.Code)
	created := decodeOK(t, w)
	id := created.Data.(map[string]any)["strategy"].(map[string]any)["id"].(float64)

	w2 := doRequest(t, r, "DELETE", "/api/v1/strategies/"+itoa(int(id)), nil)
	require.Equal(t, http.StatusOK, w2.Code)
}

// =============================================================================
// Clone
// =============================================================================

func TestStrategyHandler_Clone_201(t *testing.T) {
	r := newTestRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies", map[string]any{
		"code":     "cl1",
		"name":     "Clone1",
		"dag_json": sampleDAG,
	})
	require.Equal(t, http.StatusCreated, w.Code)
	created := decodeOK(t, w)
	id := created.Data.(map[string]any)["strategy"].(map[string]any)["id"].(float64)

	w2 := doRequest(t, r, "POST", "/api/v1/strategies/"+itoa(int(id))+"/clone", map[string]any{
		"new_code": "cl1_copy",
	})
	require.Equal(t, http.StatusCreated, w2.Code)
}

// =============================================================================
// Export / Import
// =============================================================================

func TestStrategyHandler_Export_200(t *testing.T) {
	r := newTestRouter(t)
	w := doRequest(t, r, "POST", "/api/v1/strategies", map[string]any{
		"code":     "ex1",
		"name":     "Export1",
		"dag_json": sampleDAG,
	})
	require.Equal(t, http.StatusCreated, w.Code)
	created := decodeOK(t, w)
	id := created.Data.(map[string]any)["strategy"].(map[string]any)["id"].(float64)

	w2 := doRequest(t, r, "GET", "/api/v1/strategies/"+itoa(int(id))+"/export", nil)
	require.Equal(t, http.StatusOK, w2.Code)
	require.Contains(t, w2.Body.String(), `"code": "ex1"`)
	require.Contains(t, w2.Header().Get("Content-Disposition"), `attachment`)
}

func TestStrategyHandler_Import_201(t *testing.T) {
	r := newTestRouter(t)
	payload := `{"code":"imp1","name":"Imported","dag_json":` + jsonString(sampleDAG) + `}`
	w := doRequest(t, r, "POST", "/api/v1/strategies/import", nil)
	_ = w // not used — see below for body POST
	req, _ := http.NewRequest("POST", "/api/v1/strategies/import", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)
	require.Equal(t, http.StatusCreated, w2.Code)
}

func TestStrategyHandler_Import_WrapperForm_201(t *testing.T) {
	r := newTestRouter(t)
	wrapper := map[string]any{
		"json_str": `{"code":"imp2","name":"Imported 2","dag_json":` + jsonString(sampleDAG) + `}`,
	}
	w := doRequest(t, r, "POST", "/api/v1/strategies/import", wrapper)
	require.Equal(t, http.StatusCreated, w.Code)
}

func TestStrategyHandler_Import_400_Empty(t *testing.T) {
	r := newTestRouter(t)
	req, _ := http.NewRequest("POST", "/api/v1/strategies/import", bytes.NewReader([]byte("")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// itoa is a tiny helper to avoid strconv imports here.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// jsonString JSON-encodes s and returns the result as a Go
// string literal that can be embedded in another JSON document.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
