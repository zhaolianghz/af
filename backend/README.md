# AF Backend

Go service for the A-Stock Selector System.

## Stack

- Gin (HTTP), GORM (MySQL), go-redis/v9, Viper (config), zap (logging), robfig/cron/v3.

## Run

```bash
# from repo root
cp .env.example .env

cd backend
go mod tidy
go run ./cmd/server
```

Endpoints:

- `GET /healthz` — health check (`{"status":"ok","version":"...","ts":...}`)
- `GET /api/v1/ping` — returns `pong`

## Env

See `.env.example` at the repo root. Configuration is loaded from (in order of increasing priority):

1. `configs/config.yaml`
2. `.env`
3. Real environment variables

## Layout

```
cmd/server/         entrypoint (load config, init logger, run router, graceful shutdown)
internal/
  config/           Viper loader + nested config structs
  logger/           zap setup (dev / prod)
  router/           Gin engine + middleware chain + route registration
  handler/          HTTP handlers (one file per resource)
  middleware/       CORS, recovery, request logging
  apperr/           Common error types (BizError, codes)
  model/            GORM entities  (A2)
  datasource/       Market data adapters  (A3)
  strategy/         Strategy engine  (planned)
  notify/           Multi-channel notification  (A4)
pkg/                shared utilities
migrations/         SQL / GORM migration files
configs/            YAML templates
```

## Build

```bash
make build           # outputs backend/bin/af-server
make test
make lint
make smoke           # build + run scripts/smoke.sh
```
