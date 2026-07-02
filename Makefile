# =============================================================================
# AF Selector — Makefile
#
# `make help`      list all targets
# `make dev-up`    start mysql + redis via docker compose
# `make dev`       dev-up + run-backend + run-frontend (in 3 terminals)
# `make test`      run all tests
#
# All targets exit non-zero on the first failure.
# =============================================================================

# ---- Toolchain (override with `GO=... make test` etc.) --------------------
GO            ?= go
NPM           ?= npm
NPM_FLAGS     ?= --no-audit --no-fund
DOCKER        ?= docker
COMPOSE       ?= docker compose
COMPOSE_FILE  ?= docker-compose.yml

# ---- Project layout --------------------------------------------------------
BACKEND_DIR   ?= backend
FRONTEND_DIR  ?= frontend
BIN_DIR       ?= $(BACKEND_DIR)/bin
COVERAGE_DIR  ?= coverage

# ---- Help -----------------------------------------------------------------
.DEFAULT_GOAL := help
.PHONY: help
help:  ## list all targets
	@awk 'BEGIN {FS = ":.*## "; printf "AF Selector — common targets:\n\n"} \
		/^[a-zA-Z0-9_-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' \
		$(MAKEFILE_LIST) | sort
	@echo ""
	@echo "Tip: 'make dev' = dev-up + run-backend + run-frontend"

# =============================================================================
# Setup
# =============================================================================
.PHONY: tidy
tidy:  ## go mod tidy + npm install
	@echo "==> go mod tidy"
	cd $(BACKEND_DIR)  && $(GO) mod tidy
	@echo "==> npm install"
	cd $(FRONTEND_DIR) && $(NPM) install $(NPM_FLAGS)

.PHONY: env
env:  ## copy example env files (idempotent)
	@test -f .env                          || cp .env.example .env 2>/dev/null || echo "(no .env.example; see backend/configs/config.example.yaml)"
	@test -f $(BACKEND_DIR)/configs/config.yaml || cp $(BACKEND_DIR)/configs/config.example.yaml $(BACKEND_DIR)/configs/config.yaml
	@echo "env files ready (config.yaml in $(BACKEND_DIR)/configs/)"

.PHONY: setup
setup: env  ## one-time dev setup: env files + git hooks (idempotent)
	@git config core.hooksPath .githooks
	@chmod +x .githooks/pre-commit
	@echo "git hooks installed (core.hooksPath = .githooks)"
	@echo "pre-commit will run gofmt + go vet + go build + tsc + eslint on staged files"

# =============================================================================
# Build
# =============================================================================
.PHONY: build
build: build-backend build-frontend  ## build backend + frontend

.PHONY: build-backend
build-backend:  ## build Go binary -> backend/bin/af-backend
	@mkdir -p $(BIN_DIR)
	cd $(BACKEND_DIR) && CGO_ENABLED=0 $(GO) build -o ../$(BIN_DIR)/af-backend ./cmd/server

.PHONY: build-frontend
build-frontend:  ## build frontend bundle (frontend/dist)
	cd $(FRONTEND_DIR) && $(NPM) run build

# =============================================================================
# Test
# =============================================================================
.PHONY: test
test: test-backend test-frontend  ## run all tests

.PHONY: test-backend
test-backend:  ## go test ./...
	cd $(BACKEND_DIR) && $(GO) test ./... -count=1 -timeout 120s

.PHONY: test-frontend
test-frontend:  ## frontend typecheck + lint + vitest
	cd $(FRONTEND_DIR) && $(NPM) run typecheck
	cd $(FRONTEND_DIR) && $(NPM) run lint
	cd $(FRONTEND_DIR) && $(NPM) test

.PHONY: test-coverage
test-coverage:  ## backend coverage report
	@mkdir -p $(COVERAGE_DIR)
	cd $(BACKEND_DIR) && $(GO) test ./... -coverprofile=../$(COVERAGE_DIR)/coverage.out -count=1
	$(GO) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "Coverage: $(COVERAGE_DIR)/coverage.html"

# =============================================================================
# Lint
# =============================================================================
.PHONY: lint
lint:  ## go vet + frontend lint
	cd $(BACKEND_DIR)  && $(GO) vet ./...
	cd $(FRONTEND_DIR) && $(NPM) run lint

# =============================================================================
# Run
# =============================================================================
.PHONY: run-backend
run-backend:  ## run backend on :8080 (uses $(BACKEND_DIR)/configs/config.yaml)
	cd $(BACKEND_DIR) && $(GO) run ./cmd/server -config configs/config.yaml

.PHONY: run-frontend
run-frontend:  ## run vite dev server on :5173
	cd $(FRONTEND_DIR) && $(NPM) run dev

.PHONY: run
run:  ## dev-up + run both backend and frontend (foreground, Ctrl-C to stop)
	@$(MAKE) -j2 dev-up run-backend run-frontend

# =============================================================================
# Infrastructure (docker compose)
# =============================================================================
.PHONY: dev-up
dev-up:  ## start mysql + redis via docker compose (waits for health)
	$(COMPOSE) -f $(COMPOSE_FILE) up -d mysql redis
	@echo "Waiting for MySQL..."
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		if $(DOCKER) exec $$( $(COMPOSE) -f $(COMPOSE_FILE) ps -q mysql ) mysqladmin ping -h 127.0.0.1 --silent 2>/dev/null; then \
			echo "MySQL ready"; break; \
		fi; sleep 2; \
	done
	@echo "Waiting for Redis..."
	@for i in 1 2 3 4 5; do \
		if $(DOCKER) exec $$( $(COMPOSE) -f $(COMPOSE_FILE) ps -q redis ) redis-cli ping 2>/dev/null | grep -q PONG; then \
			echo "Redis ready"; break; \
		fi; sleep 2; \
	done

.PHONY: dev-up-pg
dev-up-pg:  ## start postgres + redis via docker compose (pg profile; waits for health)
	$(COMPOSE) -f $(COMPOSE_FILE) --profile pg up -d postgres redis
	@echo "Waiting for Postgres..."
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		if $(DOCKER) exec $$( $(COMPOSE) -f $(COMPOSE_FILE) --profile pg ps -q postgres ) pg_isready -U af -d astock_selector 2>/dev/null | grep -q "accepting connections"; then \
			echo "Postgres ready"; break; \
		fi; sleep 2; \
	done
	@echo "Run the backend against it with:"
	@echo "  DB_DRIVER=postgres DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=af DB_PASSWORD=afpass DB_NAME=astock_selector make run-backend"

.PHONY: dev-down
dev-down:  ## stop all dev containers incl. the pg profile (preserves volumes)
	$(COMPOSE) -f $(COMPOSE_FILE) --profile pg down

.PHONY: dev-logs
dev-logs:  ## tail mysql + redis logs
	$(COMPOSE) -f $(COMPOSE_FILE) logs -f

.PHONY: dev-up-stack
dev-up-stack:  ## start the FULL stack (app + web + mysql + redis) via docker compose
	$(COMPOSE) -f $(COMPOSE_FILE) up -d

# =============================================================================
# Migrations
# =============================================================================
.PHONY: migrate
migrate:  ## GORM AutoMigrate runs at backend startup; restart to re-run
	@echo "No-op. AutoMigrate runs on every backend startup."
	@echo "To re-run: \`make dev-down && make dev-up && make run-backend\`"

# =============================================================================
# Smoke / Pre-commit
# =============================================================================
.PHONY: smoke
smoke: build-backend  ## build + smoke test the API (requires running dev-up)
	@$(MAKE) dev-up
	@echo "Starting backend on :18080..."
	@cd $(BACKEND_DIR) && \
		DB_USER=root DB_PASSWORD=root DB_NAME=astock_selector \
		SERVER_PORT=18080 \
		$(GO) run ./cmd/server -config configs/config.example.yaml &
	@sleep 3
	@echo "Hitting /healthz..."
	@curl -sS -o /dev/null -w "healthz: HTTP %{http_code}\n" http://127.0.0.1:18080/healthz
	@echo "Hitting /api/v1/strategies..."
	@curl -sS -o /dev/null -w "strategies: HTTP %{http_code}\n" http://127.0.0.1:18080/api/v1/strategies
	@pkill -f "go-build.*af-server" 2>/dev/null || true
	@pkill -f "exe/af-server" 2>/dev/null || true

.PHONY: check
check:  ## pre-commit: lint + typecheck (fast; ~10s)
	cd $(BACKEND_DIR)  && $(GO) vet ./...
	cd $(FRONTEND_DIR) && $(NPM) run typecheck
	cd $(FRONTEND_DIR) && $(NPM) run lint

# =============================================================================
# Deploy (remote)
# =============================================================================
.PHONY: deploy
deploy:  ## deploy to remote via scripts/deploy.sh (rsync + ssh + systemctl)
	bash scripts/deploy.sh

.PHONY: deploy-dry-run
deploy-dry-run:  ## print the deploy plan without SSHing
	bash scripts/deploy.sh --dry-run

# =============================================================================
# Cleanup
# =============================================================================
.PHONY: clean
clean:  ## remove build artifacts
	rm -rf $(BIN_DIR) $(COVERAGE_DIR)
	rm -rf $(FRONTEND_DIR)/dist $(FRONTEND_DIR)/node_modules
	cd $(BACKEND_DIR) && $(GO) clean
