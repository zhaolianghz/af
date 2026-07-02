<!--
  AF Selector — Root README
  Last updated for v0.1.0 (DX polish landed: 2026-06-18).
  Keep this in sync with CHANGELOG.md. Each release should update both.
-->

<div align="center">

# AF Selector

**Deterministic, multi-source, multi-channel A-share (沪深) stock screening platform.**

[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Node Version](https://img.shields.io/badge/Node-20-339933?logo=node.js&logoColor=white)](https://nodejs.org)
[![License](https://img.shields.io/badge/License-AGPL--3%20%7C%20Apache--2.0-blue)](./LICENSE)
[![Build Status](https://img.shields.io/badge/CI-passing-brightgreen)](./.github/workflows/ci.yml)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-3.0-6BA539?logo=openapiinitiative&logoColor=white)](./docs/openapi)
[![Coverage](https://img.shields.io/badge/backend%20coverage-85%25-brightgreen)](./CHANGELOG.md)

> Built to solve the **hermes** problem: messages get lost, single-source data
> dies silently, LLM state contaminates execution. AF is **deterministic** on
> the critical path (cron + DAG + DB + log) and **LLM-free** there. AI only
> touches explanations and the assistant UI.

[Quick Start](#4-quick-start--local-development) ·
[API Reference](#6-api-surface) ·
[OpenAPI](./docs/openapi) ·
[Operations](./docs/OPERATIONS.md) ·
[Changelog](./CHANGELOG.md)

</div>

---

## 1. What is this?

AF Selector is a self-hosted **A-share stock screener + recommender** that
ingests market data from multiple sources with automatic failover, runs
deterministic scheduled selection jobs, persists recommendations, and pushes
results to the user over **multi-channel webhooks** (Feishu / DingTalk /
WeCom) with retries + circuit-breakers — so the user always sees the
recommendation no matter which IM is up.

It is specified end-to-end in
[`openspec/changes/astock-selector-system/`](./openspec/changes/astock-selector-system/)
(proposal + design + tasks + 10 capability specs). The current
implementation covers **A1 through A7 + the §9 post-hoc performance
engine** (see [§8 Roadmap](#8-roadmap) for the full status).

---

## 2. Architecture

```
                     ┌──────────────────────────────────┐
                     │  Frontend (React 18 + TS + Vite) │
                     │   ┌──────────────────────────┐   │
                     │   │  AdminLayout              │   │
                     │   ├──────────────────────────┤   │
                     │   │ /dashboard  /strategies  │   │
                     │   │ /runs       /health      │   │
                     │   │ /recommendations         │   │
                     │   └──────────┬───────────────┘   │
                     └─────────────┼───────────────────┘
                                   │  HTTP / SSE  (axios + X-Request-ID)
                                   ▼
                     ┌──────────────────────────────────────────────┐
                     │  Backend (Go 1.25 + Gin)                     │
                     │                                              │
                     │  /healthz              (legacy root alias) │
                     │  /api/v1/healthz       (canonical)         │
                     │  /api/v1/openapi.json  (OpenAPI 3 spec)    │
                     │  /docs                 (Swagger UI)        │
                     │  /api/v1/ping                               │
                     │  /api/v1/notify/test | /health              │
                     │  /api/v1/datasource/health                  │
                     │  /api/v1/strategies  (CRUD+import+export+   │
                     │     /:id/trial-run + /:id/trial-run/node/   │
                     │     templates + from-template)               │
                     │  /api/v1/runs       (manual+retry+SSE)      │
                     │  /api/v1/recommendations                    │
                     │  /api/v1/perf/recommendations/:id[/history]│
                     │  /api/v1/perf/calculate + /aggregations      │
                     │                                              │
                     │  ┌────────────┐  ┌────────────┐              │
                     │  │ datasource │  │   notify   │              │
                     │  │ (3 sources │  │ (3 chans   │              │
                     │  │  + manager │  │  + retry / │              │
                     │  │ + breaker) │  │  breaker)  │              │
                     │  └─────┬──────┘  └──────┬─────┘              │
                     │  ┌─────┴────────────────┴──────┐              │
                     │  │  orchestrator (DAG + 8     │              │
                     │  │  builtin nodes + parser)    │              │
                     │  │  executor (cron + scheduler │              │
                     │  │  + run persistence + SSE)   │              │
                     │  │  perf (§9 T+N returns,      │              │
                     │  │      drawdown, win-rate,    │              │
                     │  │      aggregations, cron)    │              │
                     │  │  calendar (trading day +    │              │
                     │  │  session window)            │              │
                     │  │  httpresp (single envelope  │              │
                     │  │  + request_id everywhere)  │              │
                     │  │  openapi (spec + Swagger    │              │
                     │  │  UI, generated from Go)     │              │
                     │  └─────────────────────────────┘              │
                     └─────────┼───────────────────┼────────────────┘
                               │                   │
                ┌──────────────┘                   └───────────────┐
                ▼                                                  ▼
        ┌──────────────┐                                  ┌──────────────┐
        │  MySQL 8     │                                  │  Redis 7     │
        │  (sqlite     │                                  │  (cache,     │
        │   fallback   │                                  │   pub-sub)   │
        │   for tests) │                                  │  + miniredis │
        └──────────────┘                                  │    in-proc   │
                                                          └──────────────┘
```

### Backend modules (current)

| Module                          | Purpose                                                                                      |
| ------------------------------- | -------------------------------------------------------------------------------------------- |
| `internal/datasource`           | Provider-agnostic `Source` interface + 3 adapters + manager + breaker                       |
| `internal/notify`               | `Channel` interface + 3 webhook adapters (feishu/dingtalk/wecom) + retry + breaker + templates |
| `internal/orchestrator`         | DAG runtime: parser (ReactFlow JSON), executor (Kahn + concurrent goroutines), 8 builtin nodes, registry, eventbus, strategy CRUD + trial-run |
| `internal/executor`             | Run lifecycle: create run row, walk DAG, persist logs, cron scheduler, SSE event stream, recommendation persistence, 5 builtin templates + template loader |
| `internal/perf`                 | §9 post-hoc performance: T+N returns, max drawdown, win-rate, group-by aggregations, startup backfill, nightly cron |
| `internal/calendar`             | Trading-day / session-window detection (Asia/Shanghai, weekend-aware)                         |
| `internal/model`                | GORM entities (`strategy`, `strategy_version`, `node_definition`, `recommendation`, `recommendation_tag`, `run`, `run_log`, `performance_snapshot`, `datasource_health`, `trading_calendar`, …) |
| `internal/httpresp`             | Single source of truth for the JSON envelope (`OKResponse` / `ErrResponse` w/ `request_id`) |
| `internal/openapi`              | OpenAPI 3 spec + Swagger UI handler (spec generated from Go data structures)                  |
| `internal/router`               | Gin engine + middleware chain + handler registration                                          |
| `internal/middleware`           | CORS, recovery, request logging, `request_id` injection                                      |
| `internal/handler`              | HTTP handlers (`/healthz`, `/api/v1/healthz`, `/api/v1/ping`)                                |
| `internal/apperr`               | Shared error types + `Code` → HTTP status mapping                                             |
| `internal/database`             | GORM open / ping / migrate                                                                  |
| `internal/config`               | Viper + .env loader (typed `executor.*` / `calendar.*` / `perf.*` / `cron.*` sections)     |
| `internal/logger`               | zap dev/console vs prod/json                                                                |

### Deferred (future phases)

* `internal/ai` — assistant endpoints (LLM never on critical path)
* `internal/dashboard` — 1920×1080 visualization
* `internal/replay` — auto daily / weekly review cron

---

## 3. Tech Stack

| Layer             | Library / Tool                                                              |
| ----------------- | --------------------------------------------------------------------------- |
| Language (BE)     | **Go 1.25** (pinned via `backend/go.mod`; CI enforces via version-check job) |
| HTTP framework    | [`gin-gonic/gin`](https://github.com/gin-gonic/gin)                         |
| ORM               | [`gorm.io/gorm`](https://gorm.io) + MySQL / PostgreSQL / SQLite drivers      |
| Cache / PubSub    | [`redis/go-redis/v9`](https://github.com/redis/go-redis)                   |
| In-proc cache     | [`alicebob/miniredis/v2`](https://github.com/alicebob/miniredis) — for tests / DB-less dev |
| Config            | [`spf13/viper`](https://github.com/spf13/viper) + godotenv                  |
| Logging           | [`uber-go/zap`](https://github.com/uber-go/zap)                             |
| Scheduler         | [`robfig/cron/v3`](https://github.com/robfig/cron)                          |
| Request ID        | [`google/uuid`](https://github.com/google/uuid)                             |
| Language (FE)     | TypeScript 5                                                                |
| FE framework      | React 18                                                                    |
| FE build          | Vite 5                                                                      |
| FE styling        | TailwindCSS 3 + PostCSS                                                    |
| FE charts / graph | `echarts`, `echarts-for-react`, `reactflow`                                 |
| FE HTTP           | `axios`                                                                     |
| FE lint           | ESLint 9 + `@typescript-eslint`                                             |
| FE tests          | Vitest + `@testing-library/react` + Playwright                              |
| Container         | Docker + Docker Compose                                                     |
| CI                | GitHub Actions (Go 1.25 / Node 20 / Vitest / Playwright / coverage)        |
| API spec          | OpenAPI 3.0 (built at runtime from Go data) + Swagger UI                    |

---

## 4. Quick Start — Local Development

The happy path: Docker installed, MySQL + Redis come up via `make dev-up`,
the backend talks to them, frontend in Vite dev mode talks to the backend.
For a no-Docker path, see [§4.1 No-Docker fallback](#41-no-docker-fallback).

```bash
# 1. Clone
git clone <repo>
cd af

# 2. One-time dev setup — env files + git hooks (pre-commit, request_id, etc.)
make setup

# 3. Install deps
make tidy

# 4. Start MySQL + Redis (foreground logs: `make dev-logs`)
make dev-up

# 5. In terminal 1 — backend (Gin on :8080)
make run-backend

# 6. In terminal 2 — frontend (Vite on :5173)
make run-frontend

# 7. Open
open http://localhost:5173                   # frontend SPA
curl http://localhost:8080/api/v1/healthz    # backend health (canonical)
curl http://localhost:8080/docs              # Swagger UI
curl http://localhost:8080/api/v1/openapi.json  # raw OpenAPI 3 spec
```

`make setup` is idempotent — run it again any time you want to reset
`.env` files or re-install the pre-commit hook.

### 4.1 No-Docker fallback

The backend falls back gracefully when MySQL/Redis aren't available:

* **DB-less mode** (`DB_DRIVER=none`) — `/healthz` returns `status=ok` but
  omits the `db` section.
* **SQLite** (`DB_DRIVER=sqlite` with `DB_NAME=:memory:`) — fully functional
  single-binary dev. **This is what the test suite uses.**
* **miniredis** — Redis dependency is probed at startup; if the configured
  Redis is unreachable, the datasource layer transparently switches to an
  in-process cache. Notifications are unaffected.

```bash
# Pure no-Docker local dev (sqlite + in-process cache):
DB_DRIVER=sqlite DB_NAME=":memory:" make run-backend
```

### 4.2 Tests

```bash
# All tests (Go + frontend, ~30s)
make test

# Just backend
make test-backend             # go test ./... -count=1

# Just frontend
cd frontend
npm test                      # vitest run (~3s, 164 tests)
npm run test:watch            # vitest watch
npm run coverage              # vitest --coverage
npm run e2e                   # playwright e2e (13 tests, mocks /api/v1/*)
npm run e2e:headed            # same, in a real browser
npm run e2e:report            # open the last HTML report
```

The e2e suite is **hermetic** — no backend, MySQL, or Redis required. All
`/api/v1/*` traffic is intercepted by `frontend/e2e/fixtures.ts` and served
from JSON fixtures, so the tests are deterministic and run anywhere.

### 4.3 VSCode devcontainer (one-click onboarding)

If you use VSCode, open the repo in a container instead:

1. Install the [Dev Containers](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers) extension.
2. `Cmd+Shift+P` → "Dev Containers: Reopen in Container".
3. The container builds (~3 min on first open), then `post-create.sh` runs
   `make env` + `make tidy` and prints next steps.

The dev container mounts the host's Docker socket so you can still
`make dev-up` MySQL/Redis as sibling containers from inside the dev
environment. See [`.devcontainer/`](./.devcontainer).

---

## 5. Quick Start — Docker Compose (full local stack)

```bash
# Boot the full stack: app (Go) + web (Nginx) + mysql + redis
docker compose up -d

# Wait ~30s for mysql/redis healthchecks
docker compose ps      # STATUS should be "healthy" for mysql + redis

# Open
open http://localhost:8080/api/v1/healthz    # backend
open http://localhost:5173/                 # frontend (Nginx-served SPA)
open http://localhost:8080/docs             # Swagger UI
```

Tear down:

```bash
make dev-down          # docker compose down (preserves volumes)
# Wipe volumes too:
make dev-down && docker volume rm af_mysql_data af_redis_data
```

> **Production deployments use `systemd` + `nginx` + a statically-linked
> Go binary, not docker-compose.** The compose file is for local dev only.
> See [§9 Deployment](#9-deployment) for the prod procedure.

---

## 6. API Surface

Every endpoint below is live in v0.1.0. **The authoritative source of
truth is the spec at [`/api/v1/openapi.json`](./docs/openapi) (also
served as a Swagger UI at `/docs`).** This table is a summary — the
spec has the request/response schemas.

### 6.1 System

| Method | Path                          | Description                                                   |
| ------ | ----------------------------- | ------------------------------------------------------------- |
| GET    | `/healthz`                    | System + DB status (legacy root alias; same handler)          |
| GET    | `/api/v1/healthz`             | **Canonical** liveness path (preferred for k8s probes etc.)    |
| GET    | `/api/v1/ping`                | Readiness ping — returns text `pong`                           |
| GET    | `/api/v1/openapi.json`        | OpenAPI 3.0 spec (built at runtime)                           |
| GET    | `/docs`                       | Swagger UI (loads the spec from `/api/v1/openapi.json`)       |
| POST   | `/api/v1/notify/test`         | Send a test notification through the configured channel chain |
| GET    | `/api/v1/notify/health`       | Per-channel circuit-breaker state                             |
| GET    | `/api/v1/datasource/health`   | Per-source breaker state + last `datasource_health` rows      |

### 6.2 Strategy management (A7)

| Method | Path                                            | Description                                       |
| ------ | ----------------------------------------------- | ------------------------------------------------- |
| POST   | `/api/v1/strategies`                            | Create strategy (+ v1 `StrategyVersion`)          |
| GET    | `/api/v1/strategies`                            | List (`?status=`, `?tags_contains=`, `?code_like=`, `?page=`, `?page_size=`) |
| GET    | `/api/v1/strategies/:id`                        | Detail (with `current_version_dag`)               |
| PUT    | `/api/v1/strategies/:id`                        | Update (writes new `StrategyVersion` + supersedes) |
| DELETE | `/api/v1/strategies/:id`                        | Soft delete (status=disabled)                     |
| POST   | `/api/v1/strategies/:id/clone`                  | Clone (new code, node IDs remapped)               |
| GET    | `/api/v1/strategies/:id/export`                 | Export JSON                                       |
| POST   | `/api/v1/strategies/import`                     | Import (schema-validated)                         |
| GET    | `/api/v1/strategies/templates`                  | List builtin templates                            |
| POST   | `/api/v1/strategies/from-template/:code`        | Instantiate strategy from builtin template        |
| POST   | `/api/v1/strategies/:id/trial-run`              | Dry-run (no DB writes, no notify)                 |
| POST   | `/api/v1/strategies/:id/trial-run/node/:nodeId` | Dry-run to a specific node                        |

### 6.3 Runs + recommendations + SSE

| Method | Path                                | Description                                                       |
| ------ | ----------------------------------- | ----------------------------------------------------------------- |
| POST   | `/api/v1/runs`                      | Manually trigger a run (returns `run_id` in ≤3s, DAG runs async)  |
| GET    | `/api/v1/runs`                      | List (`?strategy_id=`, `?status=`, `?from=`, `?to=`)              |
| GET    | `/api/v1/runs/:id`                  | Detail                                                            |
| GET    | `/api/v1/runs/:id/logs`             | Per-node logs                                                     |
| POST   | `/api/v1/runs/:id/retry`            | Retry (clones the run + DAG with `retry_of` link)                 |
| GET    | `/api/v1/runs/:id/events`           | **SSE** — real-time node events, supports `Last-Event-ID` resume  |
| GET    | `/api/v1/recommendations`           | List persisted recommendations (`?strategy_code=`, `?tag=`, …)    |

### 6.4 Post-hoc performance (§9)

| Method | Path                                          | Description                                                         |
| ------ | --------------------------------------------- | ------------------------------------------------------------------- |
| GET    | `/api/v1/perf/recommendations/:id`            | Latest T+N return + max-drawdown snapshot for one recommendation    |
| GET    | `/api/v1/perf/recommendations/:id/history`    | All snapshots for a recommendation (one per recalculation)          |
| POST   | `/api/v1/perf/calculate`                      | Recompute snapshots (single id OR `from`+`to` date range)          |
| GET    | `/api/v1/perf/aggregations`                   | Group-by win-rate, avg T+N return, avg drawdown                     |

### 6.5 Envelope + error semantics

Every response uses a uniform JSON envelope:

```jsonc
// Success
{ "code": 0, "message": "ok", "data": { ... } }

// Error (always includes request_id for log correlation)
{
  "code": 10002,
  "message": "strategy 99999 not found",
  "request_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

* `code` is `0` on success. Error codes are stable integers (10000–10007
  are the canonical `apperr` codes).
* `request_id` on error responses is always set, even if no `X-Request-ID`
  was sent on the inbound request. The server generates a UUIDv4 if
  none is supplied. **Share the `request_id` when filing a bug** —
  the same id is in the `X-Request-ID` response header and in the
  server's `request_id` field in the zap log lines.

* SSE stream events: `ready` (on subscribe), `node_started`, `node_finished`,
  `node_error`, `run_finished`, `heartbeat` (every `executor.sse_heartbeat`,
  default 20s).
* Trial-runs do **not** touch DB or notify — they are pure in-memory DAG walks
  for fast editor feedback.

---

## 7. Project Structure

```
af/
├── .devcontainer/                       # VSCode Remote Containers (P2 DX)
│   ├── Dockerfile                       # golang:1.25-bookworm + mysql/redis CLI
│   ├── devcontainer.json                # mounts host docker socket, post-create
│   └── post-create.sh                   # runs `make env && make tidy`
├── .github/workflows/ci.yml             # CI: version-check + Go 1.25 + Node 20 + e2e
├── .githooks/pre-commit                 # pre-commit hook (gofmt + vet + tsc + eslint)
├── backend/                             # Go service (Gin + GORM + zap + viper + cron)
│   ├── cmd/server/                      # main entrypoint + graceful shutdown
│   ├── internal/
│   │   ├── apperr/                      # shared error types + Code → HTTP status
│   │   ├── calendar/                    # trading-day + session window (Asia/Shanghai)
│   │   ├── config/                      # Viper + .env loader + typed config
│   │   ├── database/                    # GORM open / ping / migrate
│   │   ├── datasource/                  # market data adapters (A3) — 3 sources + breaker
│   │   │   └── source/{eastmoney,sina,akshare}/
│   │   ├── executor/                    # A7-BE2: run lifecycle + cron + SSE + templates
│   │   │   ├── executor.go              # Execute entry: 3s sync return + async DAG walk
│   │   │   ├── scheduler.go             # robfig/cron + trading-day guard
│   │   │   ├── handler.go               # /runs + /recommendations + SSE
│   │   │   ├── template_handler.go      # /strategies/templates + /from-template
│   │   │   ├── nodes/                   # 8 builtin nodes
│   │   │   └── templates/               # 5 builtin strategy templates + Loader
│   │   ├── handler/                     # /healthz, /api/v1/healthz, /api/v1/ping
│   │   ├── httpresp/                    # JSON envelope (single source of truth)
│   │   ├── logger/                      # zap dev/console vs prod/json
│   │   ├── middleware/                  # CORS, recovery, request-id, logger
│   │   ├── model/                       # GORM entities (§2) incl. StrategyTemplate
│   │   ├── notify/                      # multi-channel notify (A4) — 3 chans + breaker
│   │   │   ├── channel/{feishu,dingtalk,wecom}/
│   │   │   └── registry/
│   │   ├── openapi/                     # OpenAPI 3 spec + Swagger UI handler
│   │   ├── orchestrator/                # A7-BE1: DAG runtime + 8 nodes + strategy CRUD
│   │   │   ├── dag.go                   # ReactFlow JSON parser
│   │   │   ├── executor.go              # Kahn + concurrent goroutines
│   │   │   ├── eventbus.go              # in-proc pub/sub for SSE
│   │   │   ├── handler.go               # /strategies CRUD routes
│   │   │   ├── trial_handler.go         # /trial-run routes
│   │   │   └── strategy_service.go
│   │   ├── perf/                        # §9: T+N returns, drawdown, win-rate, aggregations
│   │   └── router/                      # Gin engine + route registration
│   ├── configs/config.example.yaml      # template config (copy to config.yaml)
│   ├── Dockerfile                       # golang:1.25-alpine, statically linked
│   ├── go.mod
│   └── go.sum
├── docs/
│   ├── OPERATIONS.md                    # server-side runbook (P1 DX)
│   └── openapi/                         # human-readable copy of the OpenAPI spec
├── frontend/                            # Vite + React 18 + TypeScript
│   ├── e2e/                             # Playwright e2e tests (13 specs, hermetic)
│   ├── src/
│   │   ├── api/                         # axios client + per-resource modules
│   │   ├── components/                  # canvas/, runs/, shared/
│   │   ├── hooks/                       # useConfirm, useCanvasHotkeys, …
│   │   ├── layouts/                     # AdminLayout
│   │   ├── lib/                         # notify (toast helpers), format
│   │   ├── pages/                       # Dashboard, Strategies, StrategyEditor,
│   │   │                                # RunHistory, RunDetail, Recommendations,
│   │   │                                # TemplateGallery, Health
│   │   ├── stores/                      # zustand: canvas nodes/edges/selection
│   │   └── types/                       # api.ts, orchestrator.ts
│   ├── Dockerfile                       # node:20-alpine + nginx
│   ├── nginx.conf
│   ├── package.json
│   ├── playwright.config.ts
│   └── vite.config.ts
├── openspec/                            # source-of-truth specs (do not edit)
│   └── changes/astock-selector-system/
│       ├── proposal.md
│       ├── design.md
│       ├── tasks.md
│       └── specs/{market-data,strategy-management,execution-engine,
│                  selection-orchestration,recommendation,session-tagging,
│                  performance-analytics,visualization-dashboard,user-auth,
│                  ai-assistant}/
├── scripts/                             # dev / deploy / smoke / check helpers
│   ├── dev-up.sh                        # docker compose up mysql+redis (with fallbacks)
│   ├── deploy.sh                        # rsync + remote systemctl (idempotent)
│   ├── smoke.sh                         # end-to-end smoke test (sqlite, no DB needed)
│   ├── check.sh                         # pre-commit style: vet + build + test + lint
│   └── a7-smoke.sh                      # A7-phase specific smoke (deprecated by smoke.sh)
├── .env.example                         # copy to .env (DB_PASSWORD, REDIS_HOST, etc.)
├── .gitignore
├── CHANGELOG.md                         # release history (Keep a Changelog 1.1)
├── docker-compose.yml                   # full local stack (app + web + mysql + redis; postgres via --profile pg)
├── DX_REVIEW.md                         # last /plan-devex-review output (3 P0 + 5 P1 + 4 P2, all DONE)
├── LICENSE                              # dual-license: AGPL-3.0 OR Apache-2.0
├── Makefile                             # 25 self-contained targets — `make help`
└── README.md                            # this file
```

---

## 8. Roadmap

> **v1.0.0 shipped (2026-06-18, tag `af-v1.0.0`).** Phases 1, 2, 2.5,
> and 3 are in v1. Phases 4-7 are deferred to v1.1+ (see `TODOS.md`
> → "v1.1 deferred").
>
> The authoritative checklist is `openspec/changes/astock-selector-system/tasks.md`.
> The release history is `CHANGELOG.md`. This section is the bird's-eye view.

### ✅ Phase 1 — A1 through A6 (foundation + v1 first-batch skeleton)

* Backend skeleton (Gin + GORM + zap + viper + cron)
* Data models (15+ entities) + GORM migrations
* Multi-source market data layer (eastmoney + sina + akshare) with failover
  + per-source circuit-breaker + Redis cache + cross-source consistency check
* Multi-channel notification (feishu + dingtalk + wecom) with retry +
  per-channel circuit-breaker + templates + manager
* Frontend skeleton (Layout + Dashboard + Health + Strategies placeholders)

### ✅ Phase 2 — A7 (编排 + 执行 + 模板)

* **A7-BE1** — orchestrator: DAG parser (ReactFlow JSON), executor
  (Kahn + goroutine-per-node + short-circuit), 8 builtin nodes, registry
  + eventbus, strategy CRUD + clone + import/export + trial-run
* **A7-BE2** — executor: run lifecycle (3s sync return + async DAG walk),
  `robfig/cron/v3` scheduler with trading-day + session guard, **SSE**
  event stream with `Last-Event-ID` resume, run retry, recommendation
  persistence, **5 builtin templates** (早盘放量 / 午后量能 / MACD 金叉 /
  龙虎榜 / 低估值高股息) + template loader
* **A7-FE1** — ReactFlow canvas: Canvas / NodePalette / NodeView /
  NodeConfigPanel / Toolbar, zustand store, Strategies list / editor /
  new pages
* **A7-FE2** — runs UI: status badge, timeline, **LogStreamViewer**
  (EventSource-backed), Run history / detail pages, Recommendations
  page, Template Gallery page
* **A7-INT** — e2e smoke (sqlite + in-memory), docs, CI

### ✅ Phase 3 — §9 Post-hoc performance engine (PR #1, merged 2026-06-15)

* T+N return + max-drawdown snapshots per recommendation
* Group-by aggregations (win-rate, avg T+N return, avg drawdown)
* Startup backfill for missing snapshots (configurable timeout)
* Nightly cron at 02:00 (configurable timezone)
* Query plan narrowed: aggregate SELECT drops from ~6s to <100ms on the
  production dataset

### ✅ Phase 2.5 — DX polish (P0 + P1 + P2 from DX_REVIEW, all DONE)

* **P0** — `ci.yml` Go version pin, `Makefile` made real, `Dockerfile`
  on Go 1.25 + renamed to `af-backend`
* **P1** — single `internal/httpresp` envelope, `request_id` in every
  error response, `/api/v1/healthz` (with `/healthz` alias), OpenAPI 3
  spec + Swagger UI, `docs/OPERATIONS.md` runbook
* **P2** — `CHANGELOG.md`, VSCode devcontainer, pre-commit hook
  (`make setup`)

**Test status:** 747 backend tests + 164 frontend unit tests + 13 e2e
tests, all green in CI.

### ⏳ Phase 4 — Visualization dashboard (tasks.md §10)

* Big screen (1920×1080), positions / recommendations / win-rate
  heatmap / review entry
* No work started yet

### ⏳ Phase 5 — AI assistant (tasks.md §11)

* OpenAI-compatible protocol, schema-validated intent, two-stage
  commit, audit log
* LLM **never** on the critical path — only on explanations and the
  assistant UI

### ⏳ Phase 6 — Auto daily / weekly review (tasks.md §14.8-14.11)

* Daily 15:30 / weekly Sun 20:00 review cron
* Template AI explanations (uses the Phase 5 LLM integration)

### ⏳ Phase 7 — E2E + ops handbook + compliance (tasks.md §15-16)

* License, full e2e, disclaimer pages, backup guide, trading-day
  maintenance

---

## 9. Deployment

The current production deployment is `124.156.213.179` (Ubuntu 24.04).
It runs the backend as a `systemd` service behind an `nginx` reverse
proxy, with MySQL as a sibling Docker container and Redis on the host.

### 9.1 Topology

```
       :9091  ───  nginx  ──►  :9090  ──►  af-backend (Go binary, systemd)
                │            │
                │            └──►  /api/* → :9090  (proxied)
                │            └──►  /*     → /var/www/af/  (frontend SPA)
                │
                └──►  /healthz, /api/v1/healthz, /docs, /api/v1/openapi.json
                     all return 200 from either side

       :33306  ───  af-mysql (Docker container, port 33306 → 3306)
       :6379   ───  redis-server (host)
```

### 9.2 One-time server setup

See [`docs/OPERATIONS.md` §0](./docs/OPERATIONS.md) for the full file
layout (`/home/ubuntu/af/bin/af-backend`, `config/`, `logs/af.log`, etc.)
and the systemd unit file.

### 9.3 Deploying a new build

```bash
# Local: build a static Linux binary (cross-compile from macOS/Linux)
cd backend
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath -ldflags="-s -w" -o /tmp/af-backend-new ./cmd/server

# Upload + restart via SSH
scp -i ~/sshkeys/tx.pem /tmp/af-backend-new ubuntu@124.156.213.179:/tmp/
ssh -i ~/sshkeys/tx.pem ubuntu@124.156.213.179 <<'EOF'
  sudo systemctl stop af-backend
  sudo cp /home/ubuntu/af/bin/af-backend /home/ubuntu/af/bin/af-backend.bak.$(date +%Y%m%d)
  sudo install -m 0755 /tmp/af-backend-new /home/ubuntu/af/bin/af-backend
  sudo systemctl start af-backend
  sleep 2 && curl -s http://127.0.0.1:9090/api/v1/healthz
EOF
```

Or use the wrapper script: `./scripts/deploy.sh` (prints the exact
rsync + ssh commands it would run; requires `AF_SSH_KEY` env var or
`~/sshkeys/tx.pem` to exist).

### 9.4 Frontend deploy

```bash
cd frontend
npm ci
npm run build
# copy dist/ to /var/www/af/ on the server (the deploy script does this)
```

`nginx` re-serves the new files immediately — no restart needed.

### 9.5 What lives where (server paths)

| Path                                            | What                          |
| ----------------------------------------------- | ----------------------------- |
| `/home/ubuntu/af/bin/af-backend`                | Compiled Go binary            |
| `/home/ubuntu/af/config/config.yaml`            | Live config                   |
| `/home/ubuntu/af/config/env`                    | DB password + secrets         |
| `/home/ubuntu/af/logs/af.log`                   | All zap logs (JSON lines)     |
| `/var/www/af/`                                  | Frontend SPA                  |
| `/etc/systemd/system/af-backend.service`       | systemd unit                  |
| `/etc/nginx/conf.d/af.conf`                     | nginx vhost (port 9091)       |
| `/var/log/nginx/af.{access,error}.log`          | nginx logs                    |

> **The full troubleshooting runbook (DB down, service crash, frontend
> 404s, SSE buffering, stuck jobs, cron, full restart) is in
>  [`docs/OPERATIONS.md`](./docs/OPERATIONS.md).** Read that before
>  filing a bug — it covers the 8 most common failure modes.

---

## 10. Development

```bash
# Common
make setup              # one-time: env files + git hooks
make tidy               # go mod tidy + npm install
make build              # both backend (af-backend) and frontend bundle
make test               # all tests (747 BE + 164 FE + 13 e2e)
make check              # pre-commit: go vet + tsc + eslint (~10s)
make lint               # go vet + frontend lint
make clean              # remove build artifacts

# Run
make dev-up             # start MySQL + Redis (docker compose)
make dev-down           # stop them
make dev-logs           # tail MySQL + Redis logs
make run-backend        # go run ./cmd/server on :8080
make run-frontend       # vite dev on :5173
make run                # dev-up + run-backend + run-frontend in parallel

# Coverage
make test-coverage      # writes coverage/coverage.html

# Deploy
make deploy             # print deploy steps (real deploy is the SSH script in §9.3)
make deploy-dry-run     # same, without actually SSHing
```

### 10.1 Coverage targets

* Backend coverage is **85%** (measured via `make test-coverage` →
  `coverage/coverage.html`). The v1 spec (`tasks.md` §7.5) sets ≥85%
  as the gate; CI does not yet fail below it, but PRs are reviewed
  against the number.
* New code should add tests. The lowest-covered packages are `perf`,
  `database`, and `executor` — start there if you're adding coverage.

### 10.2 A7 end-to-end manual walk-through

```bash
# 1. Boot the backend (uses configs/config.yaml; copy from config.example.yaml)
cd backend
DB_DRIVER=sqlite DB_NAME=":memory:" APP_ENV=development \
  go run ./cmd/server &

# 2. List the 5 builtin templates
curl -sS http://localhost:8080/api/v1/strategies/templates | jq '.data.total'
# -> 5

# 3. Instantiate the 早盘放量突破 template
curl -sS -X POST http://localhost:8080/api/v1/strategies/from-template/morning_volume_breakout \
  | jq '.data.strategy.id'
# -> 1

# 4. Trial-run (no DB writes, no notify) — fast DAG walk
curl -sS -X POST http://localhost:8080/api/v1/strategies/1/trial-run \
  | jq '.data.status'
# -> "success"

# 5. Manually trigger a real run (returns run_id in ≤3s, DAG runs async)
RUN_ID=$(curl -sS -X POST http://localhost:8080/api/v1/runs \
  -H 'Content-Type: application/json' -d '{"strategy_id":1}' | jq -r '.data.run_id')

# 6. Watch the SSE event stream
curl -N http://localhost:8080/api/v1/runs/$RUN_ID/events

# 7. Inspect the run
curl -sS http://localhost:8080/api/v1/runs/$RUN_ID | jq '.data.status'
# -> "success" | "failed" | "skipped" (trading-day guard)

# 8. List persisted recommendations
curl -sS http://localhost:8080/api/v1/recommendations | jq '.data.items | length'
```

### 10.3 Adding a new module

1. Add the package under `backend/internal/<module>/`.
2. Export a `New(opts)` or `RegisterRoutes(r *gin.RouterGroup)` from
   that package.
3. Wire it into `backend/cmd/server/main.go` and
   `backend/internal/router/router.go`.
4. Mirror the route(s) in `frontend/src/api/<module>.ts`.
5. Add tests in `<package>_test.go`.
6. Add a `c.X` entry in `internal/openapi/spec.go` (the file is
   checked-in Go, not generated JSON — additions are struct literals).
7. Run `go test ./internal/openapi/...` — the test asserts every wired
   HTTP route appears in the spec (catches future drift).

### 10.4 Adding a new builtin DAG node

1. Implement `orchestrator.Node` in
   `backend/internal/executor/nodes/<name>.go` (with its `NodeSchema`
   for editor rendering).
2. Add a `*_test.go` covering param validation, happy path, error
   path, and short-circuit semantics (for `persist` / `notify`).
3. Register it in `nodes/register.go` via `DefaultRegistry.Register(...)`.
4. Add a `nodeForms.ts` form schema so the canvas can render a config
   panel; the editor wires it via the `NodeConfigPanel` dispatch table.

---

## 11. Makefile

`make help` prints all 25 targets. The short version:

| Target              | Description                                                                                  |
| ------------------- | -------------------------------------------------------------------------------------------- |
| `make help`         | List all targets                                                                             |
| `make setup`        | One-time dev setup: env files + git hooks (idempotent)                                      |
| `make env`          | Copy example env files only (idempotent)                                                    |
| `make tidy`         | `go mod tidy` + `npm install`                                                               |
| `make build`        | Build both backend binary and frontend bundle                                               |
| `make build-backend`| Backend only → `backend/bin/af-backend`                                                     |
| `make build-frontend`| Frontend only → `frontend/dist`                                                            |
| `make test`         | `go test ./...` + `npm run typecheck` + `npm run lint` + vitest                             |
| `make test-backend` | `go test ./... -count=1 -timeout 120s`                                                      |
| `make test-frontend`| `npm run typecheck && npm run lint && npm test`                                             |
| `make test-coverage`| `go test ./... -coverprofile=...` + `go tool cover -html`                                  |
| `make lint`         | `go vet` + frontend lint                                                                    |
| `make check`        | Pre-commit: `go vet` + `npm run typecheck` + `npm run lint` (fast, ~10s)                    |
| `make run`          | `dev-up` + `run-backend` + `run-frontend` in parallel                                       |
| `make run-backend`  | `go run ./cmd/server -config configs/config.yaml` on :8080                                  |
| `make run-frontend` | `cd frontend && npm run dev` on :5173                                                       |
| `make dev-up`       | `docker compose up -d mysql redis` (waits for healthchecks)                                 |
| `make dev-up-pg`    | `docker compose --profile pg up -d postgres redis` — Postgres instead of MySQL             |
| `make dev-down`     | `docker compose --profile pg down` (stops all incl. pg; preserves volumes)                  |
| `make dev-logs`     | Tail mysql + redis logs                                                                     |
| `make dev-up-stack` | `docker compose up -d` — the full stack (app + web + mysql + redis)                         |
| `make migrate`      | GORM AutoMigrate runs at backend startup; restart to re-run                                |
| `make smoke`        | Build + smoke test the API (requires `dev-up` running)                                      |
| `make deploy`       | Print the deploy steps (real deploy: see [§9.3](#93-deploying-a-new-build))                 |
| `make deploy-dry-run` | Same, without SSHing                                                                       |
| `make clean`        | Remove build artifacts                                                                     |

---

## 12. Contributing

The project is in active build-out toward v1. The intended flow:

1. **Read the spec** —
   `openspec/changes/astock-selector-system/proposal.md` and
   `tasks.md`.
2. **Read the design** —
   `openspec/changes/astock-selector-system/design.md`.
3. **Pick a task** — find an unchecked item in `tasks.md`, or one
   of the unchecked roadmap items in [§8](#8-roadmap).
4. **Run `make setup`** if it's your first time — installs env
   files + git hooks.
5. **Code** — follow the layered `handler → service → repo`
   pattern; `internal/router/router.go` is the single place routes
   are registered. Use `httpresp.OK/Err/Created` for every JSON
   response (so `request_id` propagates automatically).
6. **Test** — every PR must keep `make test` green. The pre-commit
   hook (`make setup` installed this) runs gofmt + go vet + go
   build + tsc + eslint on staged files; you can also run it
   manually with `make check`.
7. **PR** — squash, conventional commit message
   (`<type>(<scope>): <subject>`). Types used in this repo:
   `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`.
   Scopes used: `fe`, `be`, `perf`, `dx`, `ci`, `deploy`.

   Bypass the hook only for good reason: `git commit --no-verify`.
   CI will catch what the hook missed.

8. **Update the docs** — if you change a public API, update
   `internal/openapi/spec.go` (the openapi test will fail until
   you do). If you ship a user-facing change, add a CHANGELOG.md
   entry under `[Unreleased]`.

---

## 13. License

AF Selector is **dual-licensed** under the user's choice of:

* **Apache License, Version 2.0** — full text in [`LICENSE`](./LICENSE)
* **GNU Affero General Public License, version 3 or later** — full
  text in [`LICENSE-AGPL-3.0`](./LICENSE-AGPL-3.0)

SPDX-License-Identifier: `Apache-2.0 OR AGPL-3.0-or-later`

This matches the intent recorded in
`openspec/changes/astock-selector-system/tasks.md` §15.5. The
LICENSE file is the canonical default; pick AGPL-3.0-or-later at
distribution time if you prefer copyleft.

**Before v1 ships** — all done:
* ✅ SPDX headers on every source file (one-line per file) —
  generated by `python3 scripts/add-spdx-headers.py` (idempotent)
* ✅ [`NOTICE`](./NOTICE) — Apache-2.0 §4(d) attribution file. Currently
  only carries the project attribution; if you add a dependency that
  requires NOTICE propagation, append a "Third-party NOTICE"
  subsection per the instructions in `NOTICE` itself.
