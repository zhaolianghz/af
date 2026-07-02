#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
# =============================================================================
# scripts/a7-smoke.sh — End-to-end smoke test for the A7 surface.
#
# Boots the backend with DB_DRIVER=sqlite DB_NAME=":memory:" (no MySQL
# needed) and a minimal datasource config (so /api/v1/datasource/health
# is registered; the actual sidecars are not contacted), then exercises
# the full v1 优先级 B 端到端链路:
#
#   1. Template gallery         GET  /api/v1/strategies/templates
#   2. Strategy CRUD            GET/POST/PUT/DELETE /api/v1/strategies[/...]
#   3. Clone + node-ID remap    POST /api/v1/strategies/:id/clone
#   4. Export + import          GET/POST /api/v1/strategies/:id/export
#                                            /api/v1/strategies/import
#   5. Trial-run (no DB write)  POST /api/v1/strategies/:id/trial-run
#                               POST /api/v1/strategies/:id/trial-run/node/:nodeId
#   6. Manual run trigger       POST /api/v1/runs                (3s sync return)
#   7. Run detail + logs        GET  /api/v1/runs/:id[/logs]
#   8. SSE event stream         GET  /api/v1/runs/:id/events     (ready + heartbeat)
#   9. Run retry                POST /api/v1/runs/:id/retry
#  10. Recommendations          GET  /api/v1/recommendations
#  11. From-template            POST /api/v1/strategies/from-template/:code
#
# Usage:
#   ./scripts/a7-smoke.sh
#   PORT=9090 ./scripts/a7-smoke.sh
#
# Exits 0 on success, non-zero on any failure.
# =============================================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="${ROOT_DIR}/backend"
BIN_DIR="${BACKEND_DIR}/bin"
SMOKE_BIN="${BIN_DIR}/af-server-a7-smoke"
PORT="${A7_SMOKE_PORT:-${SERVER_PORT:-8080}}"
BASE="http://localhost:${PORT}"
HEALTH_URL="${BASE}/healthz"

# Use a copy of config.example.yaml so datasource.sources is populated
# (the manager refuses to start otherwise). The actual sidecars are
# unreachable; the e2e flow does not require live market data.
SMOKE_CFG_DIR="${BACKEND_DIR}/configs"
SMOKE_CFG="${SMOKE_CFG_DIR}/config.a7-smoke.yaml"

LOG_FILE="/tmp/af-a7-smoke.log"
SSE_LOG="/tmp/af-a7-smoke.sse.log"

log()  { echo "[a7-smoke] $*"; }
fail() { echo "[a7-smoke] FAIL: $*" >&2; cleanup; exit 1; }

PID=""
cleanup() {
  if [[ -n "${PID}" ]] && kill -0 "${PID}" 2>/dev/null; then
    log "stopping server pid=${PID}"
    kill "${PID}" 2>/dev/null || true
    wait "${PID}" 2>/dev/null || true
  fi
  rm -f "${SMOKE_CFG}"
}
trap cleanup EXIT INT TERM

# ---- Build ----------------------------------------------------------------
# Same rationale as scripts/smoke.sh: cgo is required for go-sqlite3.
log "building a7 smoke binary -> ${SMOKE_BIN} (with cgo for sqlite)"
mkdir -p "${BIN_DIR}"
( cd "${BACKEND_DIR}" && go build -o "${SMOKE_BIN}" ./cmd/server )

# ---- Materialize a config -------------------------------------------------
# Copy config.example.yaml so datasource.sources is non-empty (so the
# manager initializes and /api/v1/datasource/health is registered).
cp "${SMOKE_CFG_DIR}/config.example.yaml" "${SMOKE_CFG}"

# ---- Free the port (best effort) ------------------------------------------
if command -v lsof >/dev/null 2>&1; then
  EXISTING_PID="$(lsof -ti tcp:"${PORT}" 2>/dev/null || true)"
  if [[ -n "${EXISTING_PID}" ]]; then
    log "killing stale process on :${PORT} (pid=${EXISTING_PID})"
    kill "${EXISTING_PID}" 2>/dev/null || true
    sleep 1
  fi
fi

# ---- Run ------------------------------------------------------------------
log "starting server on :${PORT} (DB_DRIVER=sqlite DB_NAME=:memory:)"
( cd "${BACKEND_DIR}" && \
    DB_DRIVER=sqlite DB_NAME=":memory:" \
    SERVER_PORT="${PORT}" \
    APP_ENV=development \
    LOG_LEVEL=info LOG_ENCODING=console \
    "${SMOKE_BIN}" --config "${SMOKE_CFG}" > "${LOG_FILE}" 2>&1 & echo $! > /tmp/af-a7-smoke.pid )
PID="$(cat /tmp/af-a7-smoke.pid)"
rm -f /tmp/af-a7-smoke.pid
log "pid=${PID}, log=${LOG_FILE}"

# ---- Wait for /healthz ----------------------------------------------------
READY=false
for i in $(seq 1 60); do
  if curl -fsS -o /dev/null "${HEALTH_URL}" 2>/dev/null; then
    READY=true
    break
  fi
  sleep 0.5
done
if [[ "${READY}" != "true" ]]; then
  log "server did not become ready. log tail:"
  tail -n 80 "${LOG_FILE}" >&2 || true
  fail "server not reachable at ${HEALTH_URL}"
fi

# ---- HTTP helpers ---------------------------------------------------------
# req METHOD PATH BODY    prints "<HTTP_CODE> <body>"
# Empty BODY means "no body". Body is sent as JSON.
req() {
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "${body}" ]]; then
    curl -sS --max-time 10 -o /tmp/a7-smoke.body -w "%{http_code}" \
      -X "${method}" "${BASE}${path}" \
      -H 'Content-Type: application/json' -d "${body}"
  else
    curl -sS --max-time 10 -o /tmp/a7-smoke.body -w "%{http_code}" \
      -X "${method}" "${BASE}${path}"
  fi
  echo " $(cat /tmp/a7-smoke.body)"
}

# expect_code LABEL EXPECTED HTTP_OUTPUT
expect_code() {
  local label="$1" expected="$2" got="$3"
  local code="${got%% *}"
  if [[ "${code}" != "${expected}" ]]; then
    log "FAIL: ${label} — expected HTTP ${expected}, got ${code}"
    log "      body: $(cat /tmp/a7-smoke.body)"
    fail "${label}"
  fi
  log "OK   ${label} (HTTP ${code})"
}

# jq_get '.data.field'   extracts a JSON path via python3 (jq not required)
jq_get() {
  local path="$1"
  python3 -c "
import json
d = json.load(open('/tmp/a7-smoke.body'))
parts = '${path}'.lstrip('.').split('.')
v = d
for p in parts:
    if p.isdigit() and isinstance(v, list):
        v = v[int(p)]
    elif isinstance(v, dict) and p in v:
        v = v[p]
    else:
        v = ''
        break
print(v if v is not None else '')
"
}

# ---- 1. healthz / ping -----------------------------------------------------
log "section 1: system"
HEALTH_OUT="$(req GET /healthz)"
expect_code "GET /healthz" "200" "${HEALTH_OUT}"
grep -q '"status":"ok"' /tmp/a7-smoke.body || fail "healthz missing status=ok"

PING_OUT="$(req GET /api/v1/ping)"
expect_code "GET /api/v1/ping" "200" "${PING_OUT}"
[[ "$(cat /tmp/a7-smoke.body)" == "pong" ]] || fail "ping did not return 'pong'"

# ---- 2. Template gallery --------------------------------------------------
log "section 2: template gallery"
TPL_OUT="$(req GET /api/v1/strategies/templates)"
expect_code "GET /api/v1/strategies/templates" "200" "${TPL_OUT}"
TPL_TOTAL="$(jq_get .data.total)"
[[ "${TPL_TOTAL}" == "5" ]] || fail "expected 5 builtin templates, got ${TPL_TOTAL}"
log "  templates total = ${TPL_TOTAL} (expect 5)"

# ---- 3. From-template instantiation ---------------------------------------
log "section 3: from-template instantiation"
TPL_CODE="morning_volume_breakout"
INST_OUT="$(req POST "/api/v1/strategies/from-template/${TPL_CODE}" "")"
expect_code "POST /api/v1/strategies/from-template/morning_volume_breakout" "201" "${INST_OUT}"
STRAT_ID="$(jq_get .data.strategy.id)"
[[ -n "${STRAT_ID}" && "${STRAT_ID}" != "0" ]] || fail "instantiate did not return a strategy id"
log "  instantiated strategy_id=${STRAT_ID}"

# ---- 4. Strategy CRUD -----------------------------------------------------
log "section 4: strategy CRUD"
LIST_OUT="$(req GET /api/v1/strategies)"
expect_code "GET /api/v1/strategies" "200" "${LIST_OUT}"
LIST_TOTAL="$(jq_get .data.total)"
[[ "${LIST_TOTAL}" -ge 1 ]] || fail "strategies list is empty after instantiate"

DETAIL_OUT="$(req GET "/api/v1/strategies/${STRAT_ID}")"
expect_code "GET /api/v1/strategies/:id" "200" "${DETAIL_OUT}"
grep -q '"current_version_dag"' /tmp/a7-smoke.body \
  || fail "strategy detail missing current_version_dag"

UPDATE_OUT="$(req PUT "/api/v1/strategies/${STRAT_ID}" '{"name":"smoke-updated"}')"
expect_code "PUT /api/v1/strategies/:id" "200" "${UPDATE_OUT}"
UPDATED_NAME="$(jq_get .data.strategy.name)"
[[ "${UPDATED_NAME}" == "smoke-updated" ]] || fail "update did not change name (got '${UPDATED_NAME}')"

# ---- 5. Clone --------------------------------------------------------------
log "section 5: clone + node-id remap"
CLONE_OUT="$(req POST "/api/v1/strategies/${STRAT_ID}/clone" "")"
expect_code "POST /api/v1/strategies/:id/clone" "201" "${CLONE_OUT}"
CLONE_ID="$(jq_get .data.strategy.id)"
[[ "${CLONE_ID}" != "${STRAT_ID}" && -n "${CLONE_ID}" ]] || fail "clone did not produce a new id"
log "  clone strategy_id=${CLONE_ID}"

# ---- 6. Export / import ---------------------------------------------------
log "section 6: export / import"
EXPORT_OUT="$(req GET "/api/v1/strategies/${STRAT_ID}/export")"
expect_code "GET /api/v1/strategies/:id/export" "200" "${EXPORT_OUT}"
EXPORT_BODY="$(cat /tmp/a7-smoke.body)"
echo "${EXPORT_BODY}" | grep -q '"dag_json"' || fail "export missing dag_json"
echo "${EXPORT_BODY}" | grep -q '"name"'    || fail "export missing name"

# Build a minimal but valid import body using a separate python file
# (avoids bash/python interpolation issues with embedded JSON braces).
IMPORT_BODY="$(python3 /dev/stdin <<'PYEOF' /tmp/a7-smoke.body
import json, sys
with open(sys.argv[1]) as f:
    src = json.load(f)
src['code'] = 'smoke_imported_' + src['code']
src['name'] = 'smoke imported'
src.pop('status', None)
print(json.dumps(src))
PYEOF
)"
IMPORT_OUT="$(req POST /api/v1/strategies/import "${IMPORT_BODY}")"
expect_code "POST /api/v1/strategies/import" "201" "${IMPORT_OUT}"
IMPORT_ID="$(jq_get .data.strategy.id)"
[[ -n "${IMPORT_ID}" && "${IMPORT_ID}" != "0" ]] || fail "import did not return a strategy id"
log "  imported strategy_id=${IMPORT_ID}"

# ---- 7. Trial-run ---------------------------------------------------------
log "section 7: trial-run (no DB writes, no notify)"
TRIAL_OUT="$(req POST "/api/v1/strategies/${STRAT_ID}/trial-run" "")"
expect_code "POST /api/v1/strategies/:id/trial-run" "200" "${TRIAL_OUT}"
grep -q '"dry_run":true' /tmp/a7-smoke.body || fail "trial-run missing dry_run=true"
TRIAL_STATUS="$(jq_get .data.status)"
[[ -n "${TRIAL_STATUS}" ]] || fail "trial-run missing status"
log "  trial-run status=${TRIAL_STATUS}"

# trial-run to a specific node (should also be 200; node_results may be empty
# because the sidecar is offline)
TRIAL_NODE_OUT="$(req POST "/api/v1/strategies/${STRAT_ID}/trial-run/node/ds_kline" "")"
expect_code "POST /api/v1/strategies/:id/trial-run/node/ds_kline" "200" "${TRIAL_NODE_OUT}"
grep -q '"dry_run":true' /tmp/a7-smoke.body || fail "trial-run-to-node missing dry_run=true"

# ---- 8. Manual run trigger ------------------------------------------------
log "section 8: manual run trigger"
TRIG_OUT="$(req POST /api/v1/runs "{\"strategy_id\":${STRAT_ID}}")"
expect_code "POST /api/v1/runs" "201" "${TRIG_OUT}"
RUN_ID="$(jq_get .data.run_id)"
[[ -n "${RUN_ID}" && "${RUN_ID}" != "0" ]] || fail "trigger did not return a run_id"
log "  triggered run_id=${RUN_ID}"

# ---- 9. Run detail / logs -------------------------------------------------
log "section 9: run detail + logs"
DETAIL_OUT="$(req GET "/api/v1/runs/${RUN_ID}")"
expect_code "GET /api/v1/runs/:id" "200" "${DETAIL_OUT}"
RUN_STATUS="$(jq_get .data.status)"
[[ "${RUN_STATUS}" =~ ^(success|failed|skipped|running)$ ]] \
  || fail "unexpected run status '${RUN_STATUS}'"
log "  run status=${RUN_STATUS}"

LOGS_OUT="$(req GET "/api/v1/runs/${RUN_ID}/logs")"
expect_code "GET /api/v1/runs/:id/logs" "200" "${LOGS_OUT}"

# ---- 10. SSE event stream -------------------------------------------------
log "section 10: SSE event stream (3s window)"
# Subscribe to the run's SSE stream. The handler is expected to send
# at least one "ready" event on subscribe; we just verify the stream
# opens and emits the "ready" frame.
curl -sS --max-time 3 -N "${BASE}/api/v1/runs/${RUN_ID}/events" > "${SSE_LOG}" 2>&1 || true
if ! grep -q '^event: ready' "${SSE_LOG}"; then
  log "SSE log:"; cat "${SSE_LOG}" >&2
  fail "SSE stream did not emit 'ready' event for run ${RUN_ID}"
fi
log "  SSE emitted 'ready' event"

# ---- 11. Run list + retry -------------------------------------------------
log "section 11: run list + retry"
LIST_OUT="$(req GET /api/v1/runs)"
expect_code "GET /api/v1/runs" "200" "${LIST_OUT}"
RUNS_TOTAL="$(jq_get .data.total)"
[[ "${RUNS_TOTAL}" -ge 1 ]] || fail "runs list is empty after trigger"

RETRY_OUT="$(req POST "/api/v1/runs/${RUN_ID}/retry" "")"
# 201 Created (new run row) OR 200 OK (in-place retry) are both valid.
RETRY_CODE="${RETRY_OUT%% *}"
if [[ "${RETRY_CODE}" != "201" && "${RETRY_CODE}" != "200" ]]; then
  fail "POST /api/v1/runs/:id/retry returned HTTP ${RETRY_CODE} (expected 200/201)"
fi
log "  retry returned HTTP ${RETRY_CODE}"

# ---- 12. Recommendations list --------------------------------------------
log "section 12: recommendations list"
REC_OUT="$(req GET /api/v1/recommendations)"
expect_code "GET /api/v1/recommendations" "200" "${REC_OUT}"

# ---- 13. Soft delete -----------------------------------------------------
log "section 13: soft delete"
# Delete the clone and the import (the original instantiation must survive
# the smoke test so a re-run is idempotent at the file level).
DEL1_OUT="$(req DELETE "/api/v1/strategies/${CLONE_ID}" "")"
expect_code "DELETE /api/v1/strategies/:id (clone)" "200" "${DEL1_OUT}"
DEL2_OUT="$(req DELETE "/api/v1/strategies/${IMPORT_ID}" "")"
expect_code "DELETE /api/v1/strategies/:id (import)" "200" "${DEL2_OUT}"

# Verify soft delete: GET on the deleted id should 404.
GONE_OUT="$(req GET "/api/v1/strategies/${CLONE_ID}")"
GONE_CODE="${GONE_OUT%% *}"
[[ "${GONE_CODE}" == "404" || "${GONE_CODE}" == "410" ]] \
  || fail "soft-deleted strategy still GET-able (HTTP ${GONE_CODE})"
log "  soft-deleted strategy is 404/410 (got ${GONE_CODE})"

log "OK — A7 e2e smoke checks passed (run id was ${RUN_ID})"
