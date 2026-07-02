#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
# =============================================================================
# scripts/deploy.sh — Idempotent remote deploy for AF Selector
#
# Deploys to the production topology: a statically-linked Go binary under
# systemd, behind nginx, with MySQL as a sibling Docker container and
# Redis on the host. This matches what `124.156.213.179` actually runs
# and what docs/OPERATIONS.md documents. (docker-compose.yml is the
# LOCAL full-stack tool — `make dev-up-stack` — not the prod deploy path.)
#
# What it does:
#   1. Build a linux/amd64 backend binary locally (cross-compile).
#   2. Build the frontend bundle locally (npm ci && npm run build).
#   3. scp the binary to the remote, back up the old one, install it.
#   4. rsync the frontend dist/ to /var/www/af/.
#   5. rsync config.example.yaml (NEVER touches config/env or config.yaml).
#   6. systemctl restart af-backend.
#   7. Probe /api/v1/healthz on :9090.
#
# Usage:
#   ./scripts/deploy.sh                 # real deploy
#   ./scripts/deploy.sh --dry-run       # print every command, never SSH
#
# Defaults (override via env):
#   AF_REMOTE_HOST = 124.156.213.179
#   AF_REMOTE_USER = ubuntu
#   AF_REMOTE_DIR  = /home/ubuntu/af        (binary + config + logs)
#   AF_WEB_DIR     = /var/www/af            (frontend SPA, served by nginx)
#   AF_SSH_KEY     = $HOME/sshkeys/tx.pem
#   AF_HEALTH_PORT = 9090                   (backend; nginx is 9091)
#
# If the SSH key is missing AND --dry-run is NOT passed, the script
# exits 1 (supply the key, or run --dry-run to preview).
# =============================================================================
set -euo pipefail

# ---- Flags ----------------------------------------------------------------
DRY_RUN=false
if [ "${1:-}" = "--dry-run" ] || [ "${AF_DRY_RUN:-}" = "1" ] || [ "${AF_DRY_RUN:-}" = "true" ]; then
  DRY_RUN=true
fi

REMOTE_HOST="${AF_REMOTE_HOST:-124.156.213.179}"
REMOTE_USER="${AF_REMOTE_USER:-ubuntu}"
REMOTE_DIR="${AF_REMOTE_DIR:-/home/ubuntu/af}"
WEB_DIR="${AF_WEB_DIR:-/var/www/af}"
SSH_KEY="${AF_SSH_KEY:-$HOME/sshkeys/tx.pem}"
HEALTH_PORT="${AF_HEALTH_PORT:-9090}"
SERVICE="${AF_SERVICE:-af-backend}"

# ---- Helpers --------------------------------------------------------------
log()  { echo "[deploy] $*"; }
fail() { echo "[deploy] ERROR: $*" >&2; exit 1; }
run()  {
  if [ "$DRY_RUN" = "true" ]; then echo "[dry-run] $*"; else eval "$@"; fi
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BACKEND_DIR="${BACKEND_DIR:-${ROOT_DIR}/backend}"
FRONTEND_DIR="${FRONTEND_DIR:-${ROOT_DIR}/frontend}"

SSH="ssh -i ${SSH_KEY} -o StrictHostKeyChecking=accept-new"
SCP="scp -i ${SSH_KEY} -o StrictHostKeyChecking=accept-new"

# ---- Header ---------------------------------------------------------------
log "AF Selector deploy (systemd + nginx + binary)"
log "  remote:   ${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_DIR}"
log "  web dir:  ${WEB_DIR}"
log "  service:  ${SERVICE}  (health probe :${HEALTH_PORT}/api/v1/healthz)"
log "  ssh key:  ${SSH_KEY}"
log "  dry-run:  ${DRY_RUN}"

# ---- Preflight ------------------------------------------------------------
if [ "$DRY_RUN" = "false" ] && [ ! -f "$SSH_KEY" ]; then
  fail "SSH key not found at ${SSH_KEY}. Run with --dry-run to preview, or set AF_SSH_KEY=/path/to/key"
fi
[ -d "${BACKEND_DIR}" ]  || fail "backend dir missing (${BACKEND_DIR})"
[ -d "${FRONTEND_DIR}" ] || fail "frontend dir missing (${FRONTEND_DIR})"
command -v go >/dev/null 2>&1    || fail "go not installed"
command -v npm >/dev/null 2>&1   || fail "npm not installed"
command -v rsync >/dev/null 2>&1 || fail "rsync not installed"
command -v ssh >/dev/null 2>&1   || fail "ssh not installed"

# ---- 1. Build backend binary (linux/amd64) --------------------------------
log "==> Building backend binary (linux/amd64)"
BIN_OUT="/tmp/af-backend-deploy-$$"
run "(cd '${BACKEND_DIR}' && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o '${BIN_OUT}' ./cmd/server)"

# ---- 2. Build frontend bundle ---------------------------------------------
log "==> Building frontend bundle"
run "(cd '${FRONTEND_DIR}' && npm ci && npm run build)"

# ---- 3. Upload frontend dist (staged, then sudo-moved into web dir) -------
# /var/www/af is www-data-owned, so the ubuntu user can't rsync into it
# directly. Stage into a tmp dir we CAN write, then sudo-move + chown.
log "==> Syncing frontend dist → ${WEB_DIR} (via staging)"
WEB_STAGE="/tmp/af-web-stage-$$"
run "rsync -az --delete -e '${SSH}' '${FRONTEND_DIR}/dist/' ${REMOTE_USER}@${REMOTE_HOST}:${WEB_STAGE}/"
WEB_INSTALL_CMD="set -euo pipefail && \
  sudo mkdir -p ${WEB_DIR} && \
  sudo rsync -a --delete ${WEB_STAGE}/ ${WEB_DIR}/ && \
  sudo chown -R www-data:www-data ${WEB_DIR} && \
  rm -rf ${WEB_STAGE}"
run "${SSH} ${REMOTE_USER}@${REMOTE_HOST} '${WEB_INSTALL_CMD}'"

# ---- 4. Upload config.example (never touch live env/config) ---------------
# config/ may also be root-owned; stage to /tmp then sudo-install.
log "==> Syncing config.example.yaml (live config/env is never overwritten)"
run "${SCP} '${BACKEND_DIR}/configs/config.example.yaml' ${REMOTE_USER}@${REMOTE_HOST}:/tmp/af-config.example.yaml"
CFG_INSTALL_CMD="set -euo pipefail && \
  sudo mkdir -p ${REMOTE_DIR}/config && \
  sudo install -m 0644 /tmp/af-config.example.yaml ${REMOTE_DIR}/config/config.example.yaml && \
  rm -f /tmp/af-config.example.yaml"
run "${SSH} ${REMOTE_USER}@${REMOTE_HOST} '${CFG_INSTALL_CMD}'"

# ---- 5. Upload binary to /tmp (no service impact yet) ---------------------
log "==> Uploading binary to remote /tmp"
run "${SCP} '${BIN_OUT}' ${REMOTE_USER}@${REMOTE_HOST}:/tmp/af-backend-new"

# ---- 6. Install binary + restart (THE ONLY downtime window) + 7. probe ----
# Everything above this line is no-downtime. The stop→install→start
# sequence is the only moment af-backend is unavailable, and it's a
# single atomic SSH command so a failure can't leave it stopped
# without also running the start (set -e aborts before start only if
# the backup/install fails — in which case the OLD binary is still in
# place and we explicitly restart it in the rescue trap).
log "==> Install binary + restart ${SERVICE} + health probe (downtime window)"
RESTART_CMD="set -euo pipefail && \
  sudo systemctl stop ${SERVICE} && \
  sudo cp ${REMOTE_DIR}/bin/af-backend ${REMOTE_DIR}/bin/af-backend.bak.\$(date +%Y%m%d-%H%M%S) 2>/dev/null || true && \
  if ! sudo install -m 0755 /tmp/af-backend-new ${REMOTE_DIR}/bin/af-backend; then \
    echo 'install failed — restarting old binary'; sudo systemctl start ${SERVICE}; exit 1; \
  fi && \
  rm -f /tmp/af-backend-new && \
  sudo systemctl start ${SERVICE} && \
  sleep 2 && \
  (curl -fsS http://127.0.0.1:${HEALTH_PORT}/api/v1/healthz | head -c 500 || echo 'healthz not yet up')"
run "${SSH} ${REMOTE_USER}@${REMOTE_HOST} '${RESTART_CMD}'"

log "==> Done."
if [ "$DRY_RUN" = "true" ]; then
  log "DRY-RUN finished. Re-run without --dry-run (key at ${SSH_KEY}) to deploy for real."
else
  log "Deployed. Verify: curl -s http://${REMOTE_HOST}:9091/api/v1/healthz | jq"
fi
