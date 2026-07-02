// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package openapi

// =============================================================================
// OpenAPI 3.0 spec — built at runtime from Go data structures.
//
// Why Go data instead of a hand-written JSON file?
//   - The JSON encoder guarantees the spec is always well-formed.
//   - Adding a new endpoint = adding a struct literal in one place,
//     not editing a 600+ line JSON file (which is what got us here
//     in the first place — silent trailing-brace typos).
//   - Schemas are referenced by Go type name, so renaming a struct
//     surfaces as a compile error, not a stale $ref at runtime.
// =============================================================================

// ref is a JSON reference object (not used directly; we use $ref in schema).
type ref struct {
	Ref string `json:"$ref"`
}

// schema is the common shape for all OpenAPI 3.0 schema objects.
type schema struct {
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Description          string             `json:"description,omitempty"`
	Enum                 []any              `json:"enum,omitempty"`
	Example              any                `json:"example,omitempty"`
	Default              any                `json:"default,omitempty"`
	Minimum              any                `json:"minimum,omitempty"`
	Maximum              any                `json:"maximum,omitempty"`
	Properties           map[string]*schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *schema            `json:"items,omitempty"`
	AdditionalProperties any                `json:"additionalProperties,omitempty"`
	OneOf                []*schema          `json:"oneOf,omitempty"`
	Ref                  string             `json:"$ref,omitempty"`
}

type parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Required    bool    `json:"required,omitempty"`
	Description string  `json:"description,omitempty"`
	Schema      *schema `json:"schema,omitempty"`
}

type requestBody struct {
	Required bool             `json:"required,omitempty"`
	Content  map[string]*mtRo `json:"content,omitempty"`
}

// mtRo = media-type response object.
type mtRo struct {
	Schema   *schema        `json:"schema,omitempty"`
	Examples map[string]any `json:"examples,omitempty"`
}

type response struct {
	Description string             `json:"description"`
	Content     map[string]*mtRo   `json:"content,omitempty"`
	Ref         string             `json:"$ref,omitempty"`
}

type operation struct {
	Tags        []string             `json:"tags,omitempty"`
	Summary     string               `json:"summary,omitempty"`
	Description string               `json:"description,omitempty"`
	Parameters  []*parameter         `json:"parameters,omitempty"`
	RequestBody *requestBody         `json:"requestBody,omitempty"`
	Responses   map[string]*response `json:"responses"`
}

type pathItem struct {
	Get        *operation   `json:"get,omitempty"`
	Post       *operation   `json:"post,omitempty"`
	Put        *operation   `json:"put,omitempty"`
	Delete     *operation   `json:"delete,omitempty"`
	Patch      *operation   `json:"patch,omitempty"`
	Parameters []*parameter `json:"parameters,omitempty"`
}

type info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type spec struct {
	OpenAPI    string               `json:"openapi"`
	Info       *info                `json:"info"`
	Servers    []*server            `json:"servers,omitempty"`
	Tags       []*tag               `json:"tags,omitempty"`
	Paths      map[string]*pathItem `json:"paths"`
	Components map[string]any       `json:"components"`
}

// =============================================================================
// Schema builders — return $ref by name, or full schema for inline use.
// =============================================================================

func sRef(name string) *schema { return &schema{Ref: "#/components/schemas/" + name} }

func sStr(desc, ex string) *schema {
	return &schema{Type: "string", Description: desc, Example: ex}
}

func sInt(desc, format string) *schema {
	return &schema{Type: "integer", Format: format, Description: desc}
}

func sNum(desc, format string) *schema {
	return &schema{Type: "number", Format: format, Description: desc}
}

func sBool(desc string) *schema {
	return &schema{Type: "boolean", Description: desc}
}

func sArrOf(s *schema) *schema {
	return &schema{Type: "array", Items: s}
}

func sObjMap() *schema {
	return &schema{Type: "object", AdditionalProperties: true}
}

func sEnum(desc string, vals ...any) *schema {
	return &schema{Type: "string", Description: desc, Enum: vals}
}

// sProps builds a schema with named properties. Required is the
// list of property names that are mandatory.
func sProps(required []string, props map[string]*schema) *schema {
	return &schema{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

// =============================================================================
// Common responses
// =============================================================================

func errRespSchema() *schema {
	return sProps(
		[]string{"code", "message", "request_id"},
		map[string]*schema{
			"code":       sInt("Stable error code. 10000-10007 are the canonical apperr codes.", "int32"),
			"message":    sStr("Human-readable error message.", "strategy 42 not found"),
			"request_id": sStr("Always present — correlate with server logs.", "550e8400-e29b-41d4-a716-446655440000"),
		},
	)
}

func okEnvelope() *schema {
	return sProps(
		[]string{"code", "message"},
		map[string]*schema{
			"code":    sInt("", "int32"),
			"message": sStr("", ""),
			"data":    sObjMap(),
		},
	)
}

func errRespCode(code int, msg string) *response {
	return &response{
		Description: msg,
		Content:     map[string]*mtRo{"application/json": {Schema: errRespSchema()}},
	}
}

func okResp() *response {
	return &response{
		Description: "OK",
		Content:     map[string]*mtRo{"application/json": {Schema: okEnvelope()}},
	}
}

func okRespWithStatus(_ int, desc string) *response {
	return &response{
		Description: desc,
		Content:     map[string]*mtRo{"application/json": {Schema: okEnvelope()}},
	}
}

// =============================================================================
// Spec assembly
// =============================================================================

func build() *spec {
	p := &parameter{
		Name:        "X-Request-ID",
		In:          "header",
		Required:    false,
		Description: "Optional caller-supplied id. If absent, server generates a UUIDv4. Always echoed back in X-Request-ID response header and the request_id field of any error body.",
		Schema:      sStr("", ""),
	}
	page := &parameter{
		Name:   "page",
		In:     "query",
		Schema: &schema{Type: "integer", Minimum: 1, Default: 1},
	}
	pageSize := &parameter{
		Name:   "page_size",
		In:     "query",
		Schema: &schema{Type: "integer", Minimum: 1, Maximum: 200, Default: 20},
	}

	intID := &schema{Type: "integer", Format: "int64"}
	pID := func(name string) *parameter {
		return &parameter{Name: name, In: "path", Required: true, Schema: intID}
	}
	pStr := func(name, desc string) *parameter {
		return &parameter{Name: name, In: "path", Required: true, Schema: sStr(desc, "")}
	}
	pQ := func(name string, sc *schema) *parameter {
		return &parameter{Name: name, In: "query", Schema: sc}
	}

	return &spec{
		OpenAPI: "3.0.3",
		Info: &info{
			Title:       "AF Selector API",
			Version:     "0.1.0",
			Description: "A-Stock Selector backend — strategy CRUD, trial-runs, scheduled run lifecycle, SSE event stream, and post-hoc performance tracking. All error responses include a `request_id` field that matches the `X-Request-ID` response header and the `request_id` field in the server's zap log lines — share it when filing a bug.",
		},
		Servers: []*server{{URL: "http://localhost:8080", Description: "Local dev"}},
		Tags: []*tag{
			{Name: "Health", Description: "Liveness / readiness probes"},
			{Name: "Notify", Description: "A4 notification channel test + health"},
			{Name: "Datasource", Description: "A3 external datasource health"},
			{Name: "Strategies", Description: "A7 strategy CRUD + DAG versioning"},
			{Name: "Trial Run", Description: "A7 dry-run execution of a strategy DAG"},
			{Name: "Templates", Description: "Built-in strategy templates + instantiation"},
			{Name: "Runs", Description: "Run lifecycle + per-node logs + SSE event stream"},
			{Name: "Recommendations", Description: "Persisted run output (post-persist-node rows)"},
			{Name: "Perf", Description: "§9 post-hoc T+N returns, drawdown, win-rate, aggregations"},
		},
		Paths:      buildPaths(p, page, pageSize, intID, pID, pStr, pQ),
		Components: buildComponents(),
	}
}

func buildPaths(p, page, pageSize *parameter, intID *schema,
	pID func(string) *parameter, pStr func(string, string) *parameter,
	pQ func(string, *schema) *parameter) map[string]*pathItem {

	paths := map[string]*pathItem{}

	// ---- Health ----
	healthResp := &response{Description: "Service status", Content: map[string]*mtRo{"application/json": {Schema: healthSchema()}}}
	paths["/healthz"] = &pathItem{
		Get: &operation{
			Tags:        []string{"Health"},
			Summary:     "Liveness probe (root-level, legacy alias)",
			Description: "Same handler as /api/v1/healthz. Kept at root for backward compatibility with load balancers and k8s probes.",
			Responses:   map[string]*response{"200": healthResp},
		},
	}
	paths["/api/v1/healthz"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Health"},
			Summary:    "Liveness probe (canonical path under /api/v1)",
			Parameters: []*parameter{p},
			Responses:  map[string]*response{"200": healthResp},
		},
	}
	paths["/api/v1/ping"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Health"},
			Summary:    "Lightweight readiness probe — returns text 'pong'",
			Parameters: []*parameter{p},
			Responses:  map[string]*response{"200": {Description: "Always 200 with body 'pong'", Content: map[string]*mtRo{"text/plain": {Schema: sStr("", "pong")}}}},
		},
	}

	// ---- Notify ----
	paths["/api/v1/notify/test"] = &pathItem{
		Post: &operation{
			Tags:       []string{"Notify"},
			Summary:    "Send a test notification through the configured channel(s)",
			Parameters: []*parameter{p},
			RequestBody: &requestBody{Required: true, Content: map[string]*mtRo{
				"application/json": {Schema: sRef("NotifyTestRequest")},
			}},
			Responses: map[string]*response{
				"200": okRespWithStatus(200, "Notification accepted (delivery is async)"),
				"400": errRespCode(10001, "Invalid request"),
				"503": errRespCode(10006, "Notify channel unavailable"),
			},
		},
	}
	paths["/api/v1/notify/health"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Notify"},
			Summary:    "Notification channel health",
			Parameters: []*parameter{p},
			Responses:  map[string]*response{"200": {Description: "Channel health", Content: map[string]*mtRo{"application/json": {Schema: sRef("NotifyHealthResponse")}}}},
		},
	}

	// ---- Datasource ----
	paths["/api/v1/datasource/health"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Datasource"},
			Summary:    "External datasource (e.g. Tushare) health",
			Parameters: []*parameter{p},
			Responses:  map[string]*response{"200": {Description: "Datasource health", Content: map[string]*mtRo{"application/json": {Schema: sRef("DatasourceHealthResponse")}}}},
		},
	}

	// ---- Strategies ----
	pageMeta := sRef("PageMeta")
	paths["/api/v1/strategies"] = &pathItem{
		Post: &operation{
			Tags:       []string{"Strategies"},
			Summary:    "Create a new strategy (initial version 1)",
			Parameters: []*parameter{p},
			RequestBody: &requestBody{Required: true, Content: map[string]*mtRo{
				"application/json": {Schema: sRef("StrategyInput")},
			}},
			Responses: map[string]*response{
				"201": okRespWithStatus(201, "Created"),
				"400": errRespCode(10001, "Invalid request"),
				"409": errRespCode(10003, "Strategy code already exists"),
			},
		},
		Get: &operation{
			Tags:       []string{"Strategies"},
			Summary:    "List strategies (paginated, filterable)",
			Parameters: []*parameter{p, page, pageSize, pQ("status", sEnum("", "draft", "active", "disabled")), pQ("tags_contains", sStr("", "")), pQ("code_like", sStr("", ""))},
			Responses:  map[string]*response{"200": {Description: "Paginated list", Content: map[string]*mtRo{"application/json": {Schema: pageMeta}}}},
		},
	}

	paths["/api/v1/strategies/{id}"] = &pathItem{
		Parameters: []*parameter{p, pID("id")},
		Get: &operation{
			Tags:      []string{"Strategies"},
			Summary:   "Get a strategy (with its current DAG)",
			Responses: map[string]*response{"200": okResp(), "404": errRespCode(10002, "Strategy not found")},
		},
		Put: &operation{
			Tags:       []string{"Strategies"},
			Summary:    "Update a strategy (creates a new immutable version if dag_json changes)",
			Parameters: []*parameter{p, pID("id")},
			RequestBody: &requestBody{Required: true, Content: map[string]*mtRo{
				"application/json": {Schema: sRef("StrategyUpdate")},
			}},
			Responses: map[string]*response{"200": okResp(), "400": errRespCode(10001, "Invalid request"), "404": errRespCode(10002, "Strategy not found")},
		},
		Delete: &operation{
			Tags:      []string{"Strategies"},
			Summary:   "Soft-delete a strategy (status -> disabled)",
			Responses: map[string]*response{"200": okResp(), "404": errRespCode(10002, "Strategy not found")},
		},
	}

	paths["/api/v1/strategies/{id}/clone"] = &pathItem{
		Post: &operation{
			Tags:       []string{"Strategies"},
			Summary:    "Clone a strategy under a new code",
			Parameters: []*parameter{p, pID("id")},
			RequestBody: &requestBody{Content: map[string]*mtRo{
				"application/json": {Schema: sProps([]string{}, map[string]*schema{
					"new_code": sStr("", ""),
				})},
			}},
			Responses: map[string]*response{"201": okRespWithStatus(201, "Created"), "404": errRespCode(10002, "Strategy not found"), "409": errRespCode(10003, "Target code already exists")},
		},
	}

	paths["/api/v1/strategies/{id}/export"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Strategies"},
			Summary:    "Export a strategy as a downloadable JSON file",
			Parameters: []*parameter{p, pID("id")},
			Responses:  map[string]*response{"200": {Description: "JSON attachment", Content: map[string]*mtRo{"application/json": {Schema: sStr("JSON body as string", "")}}}, "404": errRespCode(10002, "Strategy not found")},
		},
	}

	importBody := &schema{OneOf: []*schema{
		sRef("StrategyInput"),
		sProps([]string{"json_str"}, map[string]*schema{
			"json_str": sStr("JSON-encoded strategy as a string", ""),
		}),
	}}
	paths["/api/v1/strategies/import"] = &pathItem{
		Post: &operation{
			Tags:        []string{"Strategies"},
			Summary:     "Import a strategy (raw JSON or {json_str: '...'} wrapper)",
			Parameters:  []*parameter{p},
			RequestBody: &requestBody{Required: true, Content: map[string]*mtRo{"application/json": {Schema: importBody}}},
			Responses:   map[string]*response{"201": okRespWithStatus(201, "Created"), "400": errRespCode(10001, "Invalid request"), "409": errRespCode(10003, "Strategy code already exists")},
		},
	}

	paths["/api/v1/strategies/{id}/trial-run"] = &pathItem{
		Post: &operation{
			Tags:       []string{"Trial Run"},
			Summary:    "Dry-run a strategy DAG end-to-end (no DB writes, no notifications)",
			Parameters: []*parameter{p, pID("id")},
			RequestBody: &requestBody{Content: map[string]*mtRo{
				"application/json": {Schema: sProps([]string{}, map[string]*schema{
					"inputs": sObjMap(),
				})},
			}},
			Responses: map[string]*response{"200": {Description: "Trial-run summary", Content: map[string]*mtRo{"application/json": {Schema: okEnvelope()}}}, "400": errRespCode(10001, "Invalid request"), "404": errRespCode(10002, "Strategy not found")},
		},
	}

	paths["/api/v1/strategies/{id}/trial-run/node/{nodeId}"] = &pathItem{
		Post: &operation{
			Tags:       []string{"Trial Run"},
			Summary:    "Dry-run up to (and including) a specific DAG node",
			Parameters: []*parameter{p, pID("id"), pStr("nodeId", "")},
			Responses:  map[string]*response{"200": {Description: "Trial-run summary", Content: map[string]*mtRo{"application/json": {Schema: okEnvelope()}}}, "400": errRespCode(10001, "Invalid request"), "404": errRespCode(10002, "Strategy or node not found")},
		},
	}

	templatesListSchema := &schema{
		Type: "object",
		Properties: map[string]*schema{
			"items": sArrOf(sRef("StrategyTemplate")),
			"total": sInt("", "int32"),
		},
	}
	paths["/api/v1/strategies/templates"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Templates"},
			Summary:    "List built-in strategy templates",
			Parameters: []*parameter{p},
			Responses: map[string]*response{
				"200": {
					Description: "Templates",
					Content:     map[string]*mtRo{"application/json": {Schema: templatesListSchema}},
				},
			},
		},
	}

	paths["/api/v1/strategies/from-template/{code}"] = &pathItem{
		Post: &operation{
			Tags:       []string{"Templates"},
			Summary:    "Instantiate a strategy from a built-in template (status=draft)",
			Parameters: []*parameter{p, pStr("code", "")},
			Responses:  map[string]*response{"201": okRespWithStatus(201, "Created"), "400": errRespCode(10001, "Invalid request"), "503": errRespCode(10006, "Templates not wired")},
		},
	}

	// ---- Runs ----
	runLogArr := &schema{Type: "object", Properties: map[string]*schema{
		"items": sArrOf(sRef("RunLog")),
	}}
	paths["/api/v1/runs"] = &pathItem{
		Post: &operation{
			Tags:        []string{"Runs"},
			Summary:     "Trigger a manual run (returns within ~3s with the new run_id; DAG walks in a goroutine)",
			Parameters:  []*parameter{p},
			RequestBody: &requestBody{Required: true, Content: map[string]*mtRo{"application/json": {Schema: sRef("RunTrigger")}}},
			Responses:   map[string]*response{"201": okRespWithStatus(201, "Run created"), "400": errRespCode(10001, "Invalid request"), "503": errRespCode(10006, "Executor not wired")},
		},
		Get: &operation{
			Tags:       []string{"Runs"},
			Summary:    "List runs (filterable)",
			Parameters: []*parameter{p, page, pageSize, pQ("strategy_id", intID), pQ("status", sEnum("", "pending", "running", "succeeded", "failed", "cancelled")), pQ("from", sStr("ISO 8601", "2025-01-01T00:00:00Z")), pQ("to", sStr("ISO 8601", "2025-01-31T23:59:59Z"))},
			Responses:  map[string]*response{"200": {Description: "OK", Content: map[string]*mtRo{"application/json": {Schema: pageMeta}}}},
		},
	}

	paths["/api/v1/runs/{id}"] = &pathItem{
		Parameters: []*parameter{p, pID("id")},
		Get: &operation{
			Tags:      []string{"Runs"},
			Summary:   "Run detail",
			Responses: map[string]*response{"200": okResp(), "404": errRespCode(10002, "Run not found")},
		},
	}

	paths["/api/v1/runs/{id}/logs"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Runs"},
			Summary:    "Per-node log entries for a run",
			Parameters: []*parameter{p, pID("id"), pQ("level", sEnum("", "debug", "info", "warn", "error")), pQ("node_key", sStr("", ""))},
			Responses:  map[string]*response{"200": {Description: "OK", Content: map[string]*mtRo{"application/json": {Schema: runLogArr}}}},
		},
	}

	paths["/api/v1/runs/{id}/retry"] = &pathItem{
		Post: &operation{
			Tags:       []string{"Runs"},
			Summary:    "Clone a failed run into a new run and re-dispatch",
			Parameters: []*parameter{p, pID("id")},
			Responses:  map[string]*response{"201": okRespWithStatus(201, "Created"), "400": errRespCode(10001, "Invalid request"), "404": errRespCode(10002, "Run not found")},
		},
	}

	paths["/api/v1/runs/{id}/events"] = &pathItem{
		Get: &operation{
			Tags:        []string{"Runs"},
			Summary:     "SSE event stream for a run (supports Last-Event-ID resume)",
			Description: "Server-Sent Events stream. Wire format: `event: <name>\\nid: <unix-nano>\\ndata: <json>\\n\\n`. The `id` field is a Unix-nanosecond timestamp usable as the Last-Event-ID header on reconnect. The stream closes when the run reaches a terminal state (succeeded/failed/cancelled) or after 5 minutes of inactivity.",
			Parameters:  []*parameter{p, pID("id"), {Name: "Last-Event-ID", In: "header", Schema: sStr("Resume from this Unix-nanosecond id", "")}},
			Responses: map[string]*response{
				"200": {Description: "SSE stream", Content: map[string]*mtRo{"text/event-stream": {Schema: sStr("event frames", "event: node.started\\nid: 1737033600000000000\\ndata: {\\\"node_key\\\":\\\"ma_filter\\\"}\\n\\n")}}},
				"404": errRespCode(10002, "Run not found"),
			},
		},
	}

	// ---- Recommendations ----
	paths["/api/v1/recommendations"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Recommendations"},
			Summary:    "List persisted recommendations (post-persist-node output)",
			Parameters: []*parameter{p, page, pageSize, pQ("run_id", intID), pQ("strategy_id", intID), pQ("stock_code", sStr("", "")), pQ("session_tag", sStr("", "")), pQ("from", sStr("ISO date", "2025-01-01")), pQ("to", sStr("ISO date", "2025-01-31"))},
			Responses:  map[string]*response{"200": {Description: "OK", Content: map[string]*mtRo{"application/json": {Schema: pageMeta}}}},
		},
	}

	// ---- Perf ----
	perfSnapHistory := &schema{Type: "object", Properties: map[string]*schema{
		"items": sArrOf(sRef("PerfSnapshot")),
		"total": sInt("", "int32"),
	}}
	paths["/api/v1/perf/recommendations/{id}"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Perf"},
			Summary:    "Latest perf snapshot for a single recommendation (T+N returns + drawdown)",
			Parameters: []*parameter{p, pID("id")},
			Responses:  map[string]*response{"200": okResp(), "404": errRespCode(10002, "No snapshot for this recommendation yet")},
		},
	}
	paths["/api/v1/perf/recommendations/{id}/history"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Perf"},
			Summary:    "All perf snapshots for a single recommendation (one per recalculation)",
			Parameters: []*parameter{p, pID("id")},
			Responses:  map[string]*response{"200": {Description: "OK", Content: map[string]*mtRo{"application/json": {Schema: perfSnapHistory}}}},
		},
	}
	paths["/api/v1/perf/calculate"] = &pathItem{
		Post: &operation{
			Tags:        []string{"Perf"},
			Summary:     "Recompute perf snapshots (single recommendation by id, OR a date range)",
			Parameters:  []*parameter{p},
			RequestBody: &requestBody{Required: true, Content: map[string]*mtRo{"application/json": {Schema: sRef("PerfCalculate")}}},
			Responses:   map[string]*response{"201": okRespWithStatus(201, "OK"), "400": errRespCode(10001, "Invalid request")},
		},
	}
	paths["/api/v1/perf/aggregations"] = &pathItem{
		Get: &operation{
			Tags:       []string{"Perf"},
			Summary:    "Group-by aggregations (win-rate, avg T+N return, avg drawdown)",
			Parameters: []*parameter{p, {Name: "group_by", In: "query", Required: true, Schema: sEnum("", "strategy", "session_tag", "stock")}, pQ("from", sStr("ISO date", "2025-01-01")), pQ("to", sStr("ISO date", "2025-01-31"))},
			Responses:  map[string]*response{"200": {Description: "Aggregations", Content: map[string]*mtRo{"application/json": {Schema: sRef("PerfAggregations")}}}, "400": errRespCode(10001, "Invalid request")},
		},
	}

	return paths
}

func healthSchema() *schema {
	return sProps(
		[]string{"status", "version", "ts", "uptime"},
		map[string]*schema{
			"status":  &schema{Type: "string", Enum: []any{"ok", "degraded"}},
			"version": sStr("", "0.1.0"),
			"ts":      sStr("ISO 8601", ""),
			"uptime":  sStr("Go duration string", "1m23s"),
			"db":      &schema{Type: "string", Enum: []any{"ok", "down"}, Description: "Omitted when no DB is configured"},
		},
	)
}

func buildComponents() map[string]any {
	strat := sProps(
		[]string{"id", "code", "name", "status", "current_version"},
		map[string]*schema{
			"id":              sInt("", "int64"),
			"code":            sStr("Unique business code", "ma_breakout_v1"),
			"name":            sStr("", ""),
			"description":     sStr("", ""),
			"status":          sEnum("", "draft", "active", "disabled"),
			"tags":            sStr("Comma-separated list", "momentum,mid-cap,weekly"),
			"current_version": sInt("Pointer to the active StrategyVersion row", "int32"),
			"dag_json":        sStr("JSON-encoded DAG. Long text.", ""),
			"cron_expression": sStr("5-field cron string", "0 9 * * 1-5"),
			"created_at":      sStr("ISO 8601", ""),
			"updated_at":      sStr("ISO 8601", ""),
		},
	)
	stratInput := sProps(
		[]string{"code", "name", "dag_json"},
		map[string]*schema{
			"code":            sStr("", ""),
			"name":            sStr("", ""),
			"description":     sStr("", ""),
			"tags":            sStr("", ""),
			"status":          &schema{Type: "string", Enum: []any{"draft", "active", "disabled"}, Default: "draft"},
			"dag_json":        sStr("", ""),
			"cron_expression": sStr("", ""),
		},
	)
	stratUpdate := sProps(
		[]string{},
		map[string]*schema{
			"name":            sStr("", ""),
			"description":     sStr("", ""),
			"tags":            sStr("", ""),
			"status":          sEnum("", "draft", "active", "disabled"),
			"dag_json":        sStr("", ""),
			"change_note":     sStr("Audit note for the new version", ""),
			"cron_expression": sStr("", ""),
		},
	)
	run := sProps(
		[]string{"id", "strategy_id", "status", "trigger"},
		map[string]*schema{
			"id":            sInt("", "int64"),
			"strategy_id":   sInt("", "int64"),
			"strategy_code": sStr("", ""),
			"status":        sEnum("", "pending", "running", "succeeded", "failed", "cancelled"),
			"trigger":       sEnum("", "manual", "cron", "retry"),
			"started_at":    sStr("ISO 8601", ""),
			"finished_at":   sStr("ISO 8601", ""),
			"duration_ms":   sInt("", "int64"),
			"error":         sStr("", ""),
			"events_count":  sInt("", "int64"),
			"retry_of":      sInt("Source run id when trigger=retry", "int64"),
		},
	)
	runLog := sProps(
		[]string{"id", "run_id", "node_key", "ts", "level", "message"},
		map[string]*schema{
			"id":       sInt("", "int64"),
			"run_id":   sInt("", "int64"),
			"node_key": sStr("", ""),
			"ts":       sStr("ISO 8601", ""),
			"level":    sEnum("", "debug", "info", "warn", "error"),
			"message":  sStr("", ""),
			"data":     sObjMap(),
		},
	)
	rec := sProps(
		[]string{"id", "run_id", "stock_code", "picked_at"},
		map[string]*schema{
			"id":            sInt("", "int64"),
			"run_id":        sInt("", "int64"),
			"strategy_id":   sInt("", "int64"),
			"strategy_code": sStr("", ""),
			"stock_code":    sStr("", ""),
			"stock_name":    sStr("", ""),
			"score":         sNum("", "double"),
			"rank":          sInt("", "int32"),
			"session_tag":   sStr("morning / afternoon / any custom tag", ""),
			"picked_at":     sStr("ISO 8601", ""),
			"tags":          sArrOf(sStr("", "")),
		},
	)
	perf := sProps(
		[]string{"id", "recommendation_id", "as_of_date"},
		map[string]*schema{
			"id":                sInt("", "int64"),
			"recommendation_id": sInt("", "int64"),
			"as_of_date":        sStr("ISO date", "2025-01-15"),
			"t1_return":         sNum("Return over the 1st trading day after pick (0.012 = +1.2%)", "double"),
			"t3_return":         sNum("", "double"),
			"t5_return":         sNum("", "double"),
			"max_drawdown":      sNum("Positive fraction (0.05 = 5%)", "double"),
			"win":               sBool("true if t5_return > 0"),
		},
	)
	runTrigger := sProps(
		[]string{},
		map[string]*schema{
			"strategy_id":   sInt("Either this OR strategy_code is required", "int64"),
			"strategy_code": sStr("", ""),
			"strategy_name": sStr("", ""),
			"inputs":        sObjMap(),
		},
	)
	perfCalculate := sProps(
		[]string{},
		map[string]*schema{
			"recommendation_id": sInt("Either this OR from+to must be set", "int64"),
			"from":              sStr("ISO date", ""),
			"to":                sStr("ISO date", ""),
		},
	)
	perfAgg := sProps(
		[]string{"group_by", "from", "to", "groups"},
		map[string]*schema{
			"group_by": sEnum("", "strategy", "session_tag", "stock"),
			"from":     sStr("ISO date", ""),
			"to":       sStr("ISO date", ""),
			"groups": sArrOf(sProps(
				[]string{"key", "count"},
				map[string]*schema{
					"key":              sStr("", ""),
					"count":            sInt("", "int32"),
					"win_rate":         sNum("Fraction of groups where t5_return > 0", "double"),
					"avg_t1_return":    sNum("", "double"),
					"avg_t3_return":    sNum("", "double"),
					"avg_t5_return":    sNum("", "double"),
					"avg_max_drawdown": sNum("", "double"),
				},
			)),
		},
	)
	tmpl := sProps(
		[]string{"code", "name", "dag_json"},
		map[string]*schema{
			"code":        sStr("", ""),
			"name":        sStr("", ""),
			"description": sStr("", ""),
			"tags":        sArrOf(sStr("", "")),
			"dag_json":    sStr("", ""),
		},
	)
	notifyTest := sProps(
		[]string{"channel", "message"},
		map[string]*schema{
			"channel":  sStr("", "wechat"),
			"message":  sStr("", ""),
			"metadata": sObjMap(),
		},
	)
	notifyHealth := sProps(
		[]string{"channel", "ok"},
		map[string]*schema{
			"channel":    sStr("", ""),
			"ok":         sBool(""),
			"last_error": sStr("", ""),
			"last_ok_at": sStr("ISO 8601", ""),
		},
	)
	dsHealth := sProps(
		[]string{"name", "ok"},
		map[string]*schema{
			"name":       sStr("", "tushare"),
			"ok":         sBool(""),
			"latency_ms": sInt("", "int64"),
			"last_error": sStr("", ""),
			"last_ok_at": sStr("ISO 8601", ""),
		},
	)
	pageMeta := sProps(
		[]string{"items", "total", "page", "page_size"},
		map[string]*schema{
			"items":     sArrOf(sObjMap()),
			"total":     sInt("", "int64"),
			"page":      sInt("", "int32"),
			"page_size": sInt("", "int32"),
		},
	)
	stratVer := sProps(
		[]string{"id", "strategy_id", "version"},
		map[string]*schema{
			"id":                sInt("", "int64"),
			"strategy_id":       sInt("", "int64"),
			"version":           sInt("", "int32"),
			"dag_json":          sStr("", ""),
			"change_note":       sStr("", ""),
			"snapshot_taken_at": sStr("ISO 8601", ""),
		},
	)
	runEvent := sProps(
		[]string{"event", "id"},
		map[string]*schema{
			"event": sStr("", "node.started"),
			"id":    sInt("Unix-nanosecond timestamp used as SSE id", "int64"),
			"data":  sObjMap(),
		},
	)

	schemas := map[string]*schema{
		"OKResponse":               okEnvelope(),
		"ErrResponse":              errRespSchema(),
		"PageMeta":                 pageMeta,
		"HealthResponse":           healthSchema(),
		"Strategy":                 strat,
		"StrategyInput":            stratInput,
		"StrategyUpdate":           stratUpdate,
		"StrategyVersion":          stratVer,
		"Run":                      run,
		"RunLog":                   runLog,
		"RunEvent":                 runEvent,
		"Recommendation":           rec,
		"PerfSnapshot":             perf,
		"RunTrigger":               runTrigger,
		"PerfCalculate":            perfCalculate,
		"PerfAggregations":         perfAgg,
		"StrategyTemplate":         tmpl,
		"NotifyTestRequest":        notifyTest,
		"NotifyHealthResponse":     notifyHealth,
		"DatasourceHealthResponse": dsHealth,
	}

	page := &parameter{Name: "page", In: "query", Schema: &schema{Type: "integer", Minimum: 1, Default: 1}}
	pageSize := &parameter{Name: "page_size", In: "query", Schema: &schema{Type: "integer", Minimum: 1, Maximum: 200, Default: 20}}

	return map[string]any{
		"parameters": map[string]*parameter{
			"RequestIDHeader": {Name: "X-Request-ID", In: "header", Schema: sStr("Optional caller-supplied id. If absent, server generates UUIDv4.", "")},
			"Page":            page,
			"PageSize":        pageSize,
		},
		"schemas": schemas,
		"responses": map[string]*response{
			"BadRequest":    {Description: "Invalid request (code 10001)", Content: map[string]*mtRo{"application/json": {Schema: errRespSchema()}}},
			"NotFound":      {Description: "Resource not found (code 10002)", Content: map[string]*mtRo{"application/json": {Schema: errRespSchema()}}},
			"Conflict":      {Description: "Resource already exists or in conflicting state (code 10003)", Content: map[string]*mtRo{"application/json": {Schema: errRespSchema()}}},
			"InternalError": {Description: "Server error (code 10000). Share the request_id when filing a bug.", Content: map[string]*mtRo{"application/json": {Schema: errRespSchema()}}},
			"Unavailable":   {Description: "Dependency unavailable (code 10006)", Content: map[string]*mtRo{"application/json": {Schema: errRespSchema()}}},
		},
	}
}
