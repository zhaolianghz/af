#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
# =============================================================================
# scripts/smoke.sh — End-to-end smoke test for the AF backend.
#
# Boots the backend with DB_DRIVER=sqlite DB_NAME=":memory:" (no MySQL
# needed), waits for it to be ready, then hits every public endpoint and
# validates the response shape.
#
# Endpoints exercised:
#   GET  /healthz                     -> 200 with status=ok
#   GET  /api/v1/ping                 -> 200 "pong"
#   GET  /api/v1/notify/health        -> 200 with channels + healthy
#   GET  /api/v1/datasource/health    -> 200 with sources + breakers
#   POST /api/v1/notify/test          -> 502 (no channels configured) OR 200
#   GET  /api/v1/does-not-exist       -> 404
#
# Usage:
#   ./scripts/smoke.sh
#   PORT=9090 ./scripts/smoke.sh
#
# Exits 0 on success, non-zero on any failure.
# =============================================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="${ROOT_DIR}/backend"
BIN_DIR="${BACKEND_DIR}/bin"
# Use a smoke-specific binary so the production `make build` output
# (CGO_ENABLED=0 static binary in backend/bin/af-server) is never
# overwritten or used for the smoke test. The smoke test needs cgo
# for sqlite, but the production image ships with a static sqlite.
SMOKE_BIN_DIR="${BACKEND_DIR}/bin"
SMOKE_BIN="${SMOKE_BIN_DIR}/af-server-smoke"
PORT="${SERVER_PORT:-8080}"
BASE="http://localhost:${PORT}"
HEALTH_URL="${BASE}/healthz"
PING_URL="${BASE}/api/v1/ping"
NOTIFY_HEALTH_URL="${BASE}/api/v1/notify/health"
NOTIFY_TEST_URL="${BASE}/api/v1/notify/test"
DATASOURCE_HEALTH_URL="${BASE}/api/v1/datasource/health"
LOG_FILE="/tmp/af-smoke.log"

log()  { echo "[smoke] $*"; }
fail() { echo "[smoke] FAIL: $*" >&2; cleanup; exit 1; }

PID=""
cleanup() {
  if [[ -n "${PID}" ]] && kill -0 "${PID}" 2>/dev/null; then
    log "stopping server pid=${PID}"
    kill "${PID}" 2>/dev/null || true
    wait "${PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

# ---- Build if needed ------------------------------------------------------
# Note: the smoke test uses DB_DRIVER=sqlite below, which requires
# cgo (go-sqlite3 is a cgo library). We therefore build WITHOUT
# CGO_ENABLED=0 here, so the binary dynamically links against the
# system sqlite. The production `make build` target uses CGO_ENABLED=0
# because the docker image has a static sqlite baked in.
# We always rebuild to avoid using a stale CGO_ENABLED=0 binary that
# the production `make build` may have produced.
log "building smoke binary -> ${SMOKE_BIN} (with cgo for sqlite)"
mkdir -p "${SMOKE_BIN_DIR}"
( cd "${BACKEND_DIR}" && go build -o "${SMOKE_BIN}" ./cmd/server )

# ---- Free the port (best effort) ------------------------------------------
if command -v lsof >/dev/null 2>&1; then
  EXISTING_PID="$(lsof -ti tcp:"${PORT}" 2>/dev/null || true)"
  if [[ -n "${EXISTING_PID}" ]]; then
    log "killing stale process on :${PORT} (pid=${EXISTING_PID})"
    kill "${EXISTING_PID}" 2>/dev/null || true
    sleep 1
  fi
fi

# ---- Run with sqlite in-memory -------------------------------------------
log "starting server on :${PORT} (DB_DRIVER=sqlite DB_NAME=:memory:)"
cd "${BACKEND_DIR}"
DB_DRIVER=sqlite DB_NAME=":memory:" \
  APP_ENV=development \
  LOG_LEVEL=info LOG_ENCODING=console \
  "${SMOKE_BIN}" > "${LOG_FILE}" 2>&1 &
PID=$!
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

# ---- /healthz -------------------------------------------------------------
log "GET ${HEALTH_URL}"
HEALTH_BODY="$(curl -fsS "${HEALTH_URL}")"
log "  -> ${HEALTH_BODY}"
echo "${HEALTH_BODY}" | grep -q '"status":"ok"' || fail "healthz missing status=ok"
echo "${HEALTH_BODY}" | grep -q '"version"'     || fail "healthz missing version"
echo "${HEALTH_BODY}" | grep -q '"ts"'          || fail "healthz missing ts"
echo "${HEALTH_BODY}" | grep -q '"uptime"'      || fail "healthz missing uptime"

# ---- /api/v1/ping ---------------------------------------------------------
log "GET ${PING_URL}"
PING_BODY="$(curl -fsS "${PING_URL}")"
log "  -> ${PING_BODY}"
[[ "${PING_BODY}" == "pong" ]] || fail "ping did not return 'pong' (got '${PING_BODY}')"

# ---- /api/v1/notify/health ------------------------------------------------
log "GET ${NOTIFY_HEALTH_URL}"
NOTIFY_HEALTH_BODY="$(curl -fsS "${NOTIFY_HEALTH_URL}")"
log "  -> ${NOTIFY_HEALTH_BODY}"
echo "${NOTIFY_HEALTH_BODY}" | grep -q '"channels"' || fail "notify/health missing channels"
echo "${NOTIFY_HEALTH_BODY}" | grep -q '"healthy"'  || fail "notify/health missing healthy"
echo "${NOTIFY_HEALTH_BODY}" | grep -q 'feishu'    || fail "notify/health missing feishu channel key"

# ---- /api/v1/datasource/health -------------------------------------------
# This route is only registered when at least one source is enabled in
# the config. On a fresh install (no config file) the route is
# deliberately absent and we expect 404. Either response is valid;
# we just want the server to keep running.
log "GET ${DATASOURCE_HEALTH_URL} (200 when sources configured, 404 when not)"
HTTP_CODE="$(curl -s -o /tmp/af-smoke-ds.body -w "%{http_code}" "${DATASOURCE_HEALTH_URL}" || true)"
DATASOURCE_HEALTH_BODY="$(cat /tmp/af-smoke-ds.body)"
log "  -> HTTP ${HTTP_CODE} ${DATASOURCE_HEALTH_BODY}"
case "${HTTP_CODE}" in
  200)
    echo "${DATASOURCE_HEALTH_BODY}" | grep -q '"sources"'  || fail "datasource/health missing sources"
    echo "${DATASOURCE_HEALTH_BODY}" | grep -q '"breakers"' || fail "datasource/health missing breakers"
    log "  datasource/health: live (sources configured)"
    ;;
  404)
    log "  datasource/health: not registered (no sources configured — expected on fresh install)"
    ;;
  *)
    fail "datasource/health unexpected HTTP code ${HTTP_CODE}"
    ;;
esac

# ---- /api/v1/notify/test (no channels configured -> 502 is the spec) ----
log "POST ${NOTIFY_TEST_URL} (expect 200 OR 502 — no channels configured in fresh db)"
HTTP_CODE="$(curl -s -o /tmp/af-smoke-notify.body -w "%{http_code}" -X POST "${NOTIFY_TEST_URL}" || true)"
NOTIFY_TEST_BODY="$(cat /tmp/af-smoke-notify.body)"
log "  -> HTTP ${HTTP_CODE} ${NOTIFY_TEST_BODY}"
case "${HTTP_CODE}" in
  200)
    echo "${NOTIFY_TEST_BODY}" | grep -q '"ok":true' || fail "notify/test 200 missing ok:true"
    log "  notify/test: at least one channel configured and accepted"
    ;;
  502)
    echo "${NOTIFY_TEST_BODY}" | grep -q '"ok":false' || fail "notify/test 502 missing ok:false"
    log "  notify/test: no channels configured (expected on fresh install)"
    ;;
  *)
    fail "notify/test unexpected HTTP code ${HTTP_CODE}"
    ;;
esac

# ---- Negative: unknown route -> 404 --------------------------------------
log "GET /api/v1/does-not-exist (expect 404)"
HTTP_CODE="$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/api/v1/does-not-exist")"
log "  -> ${HTTP_CODE}"
[[ "${HTTP_CODE}" == "404" ]] || fail "unknown route did not return 404 (got ${HTTP_CODE})"

# ---- Method not allowed -> 405 -------------------------------------------
log "GET ${NOTIFY_TEST_URL} (expect 405 — POST-only endpoint)"
HTTP_CODE="$(curl -s -o /dev/null -w "%{http_code}" "${NOTIFY_TEST_URL}")"
log "  -> ${HTTP_CODE}"
[[ "${HTTP_CODE}" == "404" || "${HTTP_CODE}" == "405" ]] \
  || fail "GET on POST-only endpoint returned ${HTTP_CODE} (want 404 or 405)"

log "OK — all smoke checks passed"
