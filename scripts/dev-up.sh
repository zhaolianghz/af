#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
# =============================================================================
# scripts/dev-up.sh — Bring up local dev dependencies (mysql + redis).
#
# Usage:
#   ./scripts/dev-up.sh           # docker compose up mysql redis
#   ./scripts/dev-up.sh --dry-run # print what would happen, do nothing
#
# Docker is OPTIONAL. If it's missing or the daemon isn't reachable,
# the script falls back to:
#   - `brew services` start mysql / redis  (macOS with Homebrew)
#   - Otherwise prints a tip that the backend will auto-fallback to
#     sqlite + miniredis (in-process).
#
# This script is idempotent: re-running it is a no-op once services are up.
# =============================================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/docker-compose.yml"

DRY_RUN=false
[ "${1:-}" = "--dry-run" ] && DRY_RUN=true

log() { echo "[dev-up] $*"; }

# ---- Docker available? ----------------------------------------------------
has_docker() {
  command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1
}

# ---- Dry-run short-circuit ------------------------------------------------
if [ "$DRY_RUN" = "true" ]; then
  log "dry-run: would attempt docker compose up -d mysql redis"
  if has_docker; then
    log "dry-run: docker detected — would run:"
    log "  docker compose -f ${COMPOSE_FILE} up -d mysql redis"
  else
    log "dry-run: docker NOT detected — would fall back to brew services"
    log "  brew services start mysql || true"
    log "  brew services start redis || true"
  fi
  log "dry-run: done"
  exit 0
fi

# ---- Real path ------------------------------------------------------------
if has_docker; then
  log "docker detected — running: docker compose up -d mysql redis"
  docker compose -f "${COMPOSE_FILE}" up -d mysql redis

  log "waiting for mysql to be healthy..."
  for i in $(seq 1 30); do
    if docker compose -f "${COMPOSE_FILE}" exec -T mysql \
         mysqladmin ping -h 127.0.0.1 -uroot -p"${MYSQL_ROOT_PASSWORD:-root}" \
         >/dev/null 2>&1; then
      log "mysql up"
      break
    fi
    sleep 2
  done

  log "waiting for redis..."
  for i in $(seq 1 15); do
    if docker compose -f "${COMPOSE_FILE}" exec -T redis \
         redis-cli ping >/dev/null 2>&1; then
      log "redis up"
      break
    fi
    sleep 1
  done
else
  log "docker NOT available — falling back to brew services (macOS) / in-process"
  if command -v brew >/dev/null 2>&1; then
    brew services start mysql || log "  (mysql start failed or already running; continuing)"
    brew services start redis || log "  (redis start failed or already running; continuing)"
  else
    log "  no brew either — the backend will fall back to:"
    log "    DB_DRIVER=sqlite DB_NAME=:memory:"
    log "    datasource cache: in-process (miniredis embedded)"
  fi
fi

log "dev environment ready."
log "  - run 'make run-backend'  in one terminal"
log "  - run 'make run-frontend' in another"
log "  - then open http://localhost:5173"
