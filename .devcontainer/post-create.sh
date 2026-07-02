#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
# post-create.sh — runs once when the dev container is first built.
#
# Idempotent. Re-runnable on rebuild. Failures abort so VSCode
# shows the error in the bottom-right notification.

set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> [1/4] git safe.directory"
git config --global --add safe.directory /workspaces/af

echo "==> [2/4] env files"
make env

echo "==> [3/4] Go modules + npm packages"
make tidy

echo "==> [4/4] verifying toolchain"
go version
node --version
npm --version

cat <<'EOF'

========================================================
 AF Selector dev container is ready.

 Next steps:
   1. Start MySQL + Redis (uses the host docker socket):
        make dev-up
   2. In one terminal:  make run-backend
   3. In another:        make run-frontend
   4. Open http://localhost:5173 in your browser

 Tests:
   make test         # all unit tests
   make check        # pre-commit: go vet + tsc + eslint

 Docs:
   - /docs/OPERATIONS.md  server-side runbook
   - /docs/openapi/...    OpenAPI 3 spec (also served at /docs once backend is up)
========================================================
EOF
