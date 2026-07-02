#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
# =============================================================================
# scripts/check.sh — Pre-commit style: vet + build + test + typecheck + lint.
#
# Used by CI and by devs before pushing. Exits non-zero on first failure.
#
# Usage:
#   ./scripts/check.sh
#   ./scripts/check.sh --skip-frontend   # only run backend checks
#   ./scripts/check.sh --skip-backend    # only run frontend checks
# =============================================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

SKIP_BACKEND=false
SKIP_FRONTEND=false
for arg in "$@"; do
  case "$arg" in
    --skip-backend)  SKIP_BACKEND=true  ;;
    --skip-frontend) SKIP_FRONTEND=true ;;
    -h|--help)
      sed -n '3,12p' "$0"
      exit 0
      ;;
    *)
      echo "Unknown arg: $arg" >&2
      exit 2
      ;;
  esac
done

log() { echo "[check] $*"; }
fail() { echo "[check] FAIL: $*" >&2; exit 1; }

command -v go  >/dev/null 2>&1 || fail "go not installed"
if [ "$SKIP_FRONTEND" != "true" ]; then
  command -v npm >/dev/null 2>&1 || fail "npm not installed"
fi

# ---- Backend --------------------------------------------------------------
if [ "$SKIP_BACKEND" != "true" ]; then
  log "==> backend: go vet"
  ( cd backend && go vet ./... )

  log "==> backend: go build"
  ( cd backend && go build ./... )

  log "==> backend: go test (count=1, timeout 60s)"
  ( cd backend && go test ./... -count=1 -timeout 60s )
fi

# ---- Frontend -------------------------------------------------------------
if [ "$SKIP_FRONTEND" != "true" ]; then
  if [ -d frontend ]; then
    if [ ! -d frontend/node_modules ]; then
      log "==> frontend: npm install (node_modules missing)"
      ( cd frontend && npm install )
    fi

    log "==> frontend: typecheck"
    ( cd frontend && npm run typecheck )

    log "==> frontend: lint"
    ( cd frontend && npm run lint )

    log "==> frontend: build"
    ( cd frontend && npm run build )
  else
    log "==> frontend: skipped (frontend/ not present)"
  fi
fi

log "All checks passed."
