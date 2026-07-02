# Known Limitations — AF Selector v1.0.0

What v1 does NOT do, and the edges where it behaves in non-obvious ways.
Read this before filing a bug — some of these are intentional v1 scope
cuts, not defects.

---

## 1. Deferred features (v1.1+)

These are scoped out of v1.0.0 by design. See `TODOS.md` → "v1.1 deferred".

| Feature | Why deferred | Workaround in v1 |
|---------|--------------|------------------|
| **Visualization dashboard** (§10) | Not on the critical path; the read-only Dashboard page + CLI cover daily use | Use `/dashboard` + `curl /api/v1/recommendations` |
| **AI assistant** (§11) | LLM stays off the critical path by contract; conversational editing is not a v1 need | Edit strategies in the ReactFlow canvas |
| **Auto daily/weekly review** (§14.9-11) | Needs the AI assistant first | The nightly perf cron still recomputes T+N; read it via `/api/v1/perf/aggregations` |
| **Multi-user / auth** (§12) | v1 is single-user self-hosted by contract | Run it behind your own reverse-proxy auth if exposed |
| **Prometheus metrics** | Observability polish, not release-blocking | Logs (`logs/af.log`) + `/api/v1/healthz` + `docs/OPERATIONS.md` §11 |

---

## 2. Performance engine (§9)

- **T+N window caps at 5 trading days.** The engine computes T+1, T+3,
  and T+5 returns only. There is no T+10 / T+20. A recommendation
  younger than 5 trading days has `null` (not `0.0`) for the
  not-yet-elapsed horizons — consumers MUST distinguish `null` from
  `0.0` (see the `aggregationRow` doc comment in
  `internal/perf/handler.go`).
- **Win-rate denominator is "recs with known T+5".** A group where no
  recommendation has reached T+5 yet returns `win_rate_t5: null`, not
  `0%`. Reading `null` as `0%` would mislabel a young strategy as a
  loser.
- **Suspended stocks / data gaps → null, not error.** If a source can't
  return a close price for a T+N date (stock suspended, source error),
  that horizon is `null` for that recommendation. The run does not fail.
- **Drawdown needs ≥2 finite closes.** `max_drawdown` is `null` when
  fewer than two price points are available in the window.

---

## 3. SSE (run event stream)

- **Last-Event-ID resume is best-effort.** On reconnect with a
  `Last-Event-ID` header, the server replays events newer than that id
  from the in-process EventBus. If the run already reached a terminal
  state and the bus buffer was reclaimed, the replay may be empty — the
  client should fall back to `GET /api/v1/runs/:id` for the final state.
- **No cross-process fan-out.** The EventBus is in-process. If you run
  more than one backend replica behind a load balancer, an SSE client
  pinned to replica A will not see events from a run executing on
  replica B. v1 is single-replica; multi-replica SSE needs a shared
  bus (Redis pub/sub) — not built.
- **nginx must not buffer.** SSE looks "stuck" to the client if
  `proxy_buffering` is on. The prod nginx config disables it; see
  `docs/OPERATIONS.md` §6.

---

## 4. Deployment

- **`scripts/deploy.sh` is single-host only.** It builds the backend
  binary + frontend bundle locally and ships them to one server via
  `scp`/`rsync` + `systemctl restart` (matching the prod systemd+nginx
  topology in README §9). There is no multi-host / rolling deploy, no
  load balancer orchestration. Single self-hosted box is the v1 model.
- **No automated rollback.** A bad deploy is recovered manually via the
  `.bak.<timestamp>` binary copy the deploy step makes (README §9.3) +
  `systemctl restart`. There is no one-command rollback.
- **`docker-compose.yml` is local-only.** It runs the full stack
  (app + web + mysql + redis) for local dev via `make dev-up-stack`. It
  is NOT the production deploy path — prod uses the systemd binary.

---

## 5. Data / scale

- **In-memory aggregation.** `/api/v1/perf/aggregations` pulls the
  latest snapshot per recommendation in the date range and aggregates
  in Go memory (not SQL window functions — the `ROW_NUMBER() OVER`
  pattern diverges between MySQL 8 and SQLite). Bounded by "one row per
  rec", so fine at current scale; revisit if recommendations exceed
  ~1M rows in a single query range.
- **No connection-pool tuning for SQLite.** SQLite is the test/dev
  driver only; pool settings are MySQL-only. Don't run SQLite in prod.

---

## 6. Security posture

- **No auth in v1.** Every endpoint is unauthenticated. The contract is
  single-user self-hosted on a trusted network. If you expose the API
  publicly, put your own auth in front of it.
- **CORS allow-list is dev-only.** `internal/middleware/cors.go` allows
  `localhost:5173` / `localhost:3000`. Production serving is same-origin
  via nginx, so CORS doesn't apply there — but if you add a separate
  frontend origin, update the allow-list.
- **Secrets via env only.** DB password / datasource tokens / notify
  webhooks come from `config/env` (chmod 600) or environment variables,
  never from tracked files. Test files contain fixture values like
  `test-secret-123` — those are not real credentials.
