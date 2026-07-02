# AF Selector v1.0.0 — Release Notes

**Released:** 2026-06-18 · **Tag:** `af-v1.0.0`

The first feature-complete release of AF Selector — a self-hosted
A-share (沪深) stock screener that runs deterministic, scheduled
selection strategies and pushes results over multi-channel webhooks.

---

## What it does

You build a stock-screening strategy as a DAG (data source → indicators
→ filters → rank → dedupe → tag → persist → notify), schedule it with
cron, and AF runs it deterministically — no LLM on the critical path.
Results are persisted as recommendations and pushed to Feishu / DingTalk
/ WeCom with retries and per-channel circuit breakers. A post-hoc
performance engine tracks T+N returns, drawdown, and win-rate so you can
tell which strategies actually work.

## Try it first

```bash
git clone <repo> && cd af
make setup          # env files + git hooks
make dev-up         # MySQL + Redis
make run-backend    # :8080
make run-frontend   # :5173

# Instantiate a builtin template and dry-run it:
curl -sS http://localhost:8080/api/v1/strategies/templates | jq '.data.total'   # → 5
curl -sS -X POST http://localhost:8080/api/v1/strategies/from-template/morning_volume_breakout | jq '.data.strategy.id'
curl -sS -X POST http://localhost:8080/api/v1/strategies/1/trial-run | jq '.data.status'

# Browse the API:
open http://localhost:8080/docs        # Swagger UI
```

## What's in 1.0.0

- **Deterministic execution** — cron + DAG + run lifecycle + per-node
  logs + SSE event stream with Last-Event-ID resume + retry.
- **Multi-source market data** — 3 sources (eastmoney / sina / akshare)
  with automatic failover, per-source circuit breaker, Redis cache.
- **Multi-channel notify** — Feishu / DingTalk / WeCom with retry +
  per-channel breaker + templates.
- **5 builtin strategy templates** — 早盘放量突破 / 午后量能 / MACD 金叉 /
  龙虎榜 / 低估值高股息. One-click instantiate, then edit in the canvas.
- **Post-hoc performance (§9)** — T+1/T+3/T+5 returns, max drawdown,
  group-by win-rate aggregations, nightly recompute cron.
- **Frontend** — ReactFlow DAG editor + run history + live SSE log
  stream + recommendations + template gallery.

## Quality

- 798 backend tests, **85.0%** coverage (meets spec §7.5 gate)
- 164 frontend unit tests + 13 Playwright e2e
- OpenAPI 3 spec + Swagger UI at `/docs`
- `request_id` on every error response, correlated to zap logs

## Operations

- systemd + nginx + statically-linked Go binary
- `docs/OPERATIONS.md` — 13-section runbook (DB down, service crash,
  SSE buffering, stuck jobs, cron, emergency restart, …)
- Dual-licensed: Apache-2.0 OR AGPL-3.0-or-later (`LICENSE`, `NOTICE`,
  SPDX headers on every file)

## Deferred to v1.1+

Visualization dashboard (§10), AI assistant (§11), auto review
summaries (§14.9-11), multi-user auth (§12), Prometheus metrics.
See [`KNOWN-LIMITATIONS.md`](./KNOWN-LIMITATIONS.md) for the full list
plus the behavioral edges (T+N window, SSE resume, deploy topology).

## Upgrade notes

First release — no upgrade path. Fresh install only.

## Known limitations

See [`docs/KNOWN-LIMITATIONS.md`](./KNOWN-LIMITATIONS.md).
