# OPERATIONS — AF Selector Runbook

Last updated: 2026-06-17
Owner: backend (Go) + frontend (React/Vite) + deploy (systemd + nginx)

This runbook covers the most common failure modes on a **deployed**
instance (`124.156.213.179` is the current prod). For local dev
loops, `make help` lists every target and `make dev-up` + `make run-backend`
+ `make run-frontend` are the only commands you need.

> **Always include the `request_id` from a failing API response** when
> filing a bug. Every error body has a `request_id` field that matches
> the `request_id` field in the server's zap log lines — one grep,
> one stack trace.

---

## 0. Where things live on the server

| Path | What |
|------|------|
| `/home/ubuntu/af/bin/af-backend` | Compiled Go binary |
| `/home/ubuntu/af/config/config.yaml` | Live config (do not edit — see §2) |
| `/home/ubuntu/af/config/env` | DB password + secrets (chmod 600, owned by `ubuntu`) |
| `/home/ubuntu/af/logs/af.log` | All zap logs (JSON lines, rotated weekly by `logrotate`) |
| `/home/ubuntu/af/frontend/dist/` | Built SPA served by nginx |
| `/etc/systemd/system/af-backend.service` | systemd unit for the backend |
| `/etc/nginx/conf.d/af.conf` | nginx vhost (port 9091 → 9090 + SPA) |
| `/var/log/nginx/af.access.log`, `af.error.log` | nginx access + error logs |

Service is reachable on:
- `http://124.156.213.179:9091/` (frontend SPA)
- `http://124.156.213.179:9091/api/...` (proxied to backend on :9090)
- `http://124.156.213.179:9091/docs` (Swagger UI)
- `http://124.156.213.179:9091/api/v1/openapi.json` (raw OpenAPI 3 spec)

The MySQL container is named `af-mysql` (port 33306 on the host;
the backend connects via `127.0.0.1:33306` inside the container network).
Redis is on the host's existing `6379`.

---

## 1. Health checks — 30-second triage

```bash
# Is the backend process up and listening?
systemctl status af-backend
curl -s http://127.0.0.1:9090/api/v1/healthz | jq

# Expected: {"status":"ok","version":"...","ts":"...","uptime":"..."}
# If status="degraded", db field will say "down" — see §3.

# Is nginx proxying correctly?
curl -sI http://127.0.0.1:9091/api/v1/healthz

# Can a browser reach it from the public internet?
curl -sI https://your-public-host/api/v1/healthz
```

If `/healthz` returns `degraded` → jump to §3 (DB).
If `/healthz` returns 5xx → §4 (service crash).
If `/healthz` is reachable but `/docs` 404s → §5 (nginx misconfig).

---

## 2. Config changes (never edit `config.yaml` directly)

The deployed `config.yaml` is generated from `config.example.yaml` +
overrides. To change a setting:

```bash
# On the server:
sudo -u ubuntu nano /home/ubuntu/af/config/config.yaml
# OR for secrets (passwords, API tokens):
sudo -u ubuntu nano /home/ubuntu/af/config/env
sudo systemctl restart af-backend
```

Common edits:
- `server.port` — only change if you also update `af.conf` + `:9091` listener
- `db.*` in `config/env` — DB password
- `cron.timezone` — `Asia/Shanghai` by default; the cron scheduler uses this for wall-clock interpretation
- `perf.startup_backfill_timeout` — default 30s; raise if backfill is timing out on slow days

If you edit `config.yaml` directly and the next deploy overwrites it,
**the change is lost**. Configs should be the source of truth in
git. If you need a one-off prod change, document it in
`docs/CHANGELOG.md` (TODO — does not exist yet; track in
`DX_REVIEW.md` P2 for now).

---

## 3. Database down / connection lost

**Symptoms:**
- `/healthz` returns `{"status":"degraded", "db":"down", ...}`
- API endpoints fail with 500 + `request_id` + message "dial tcp ...: connection refused" or "Error 1045: Access denied"
- Frontend toasts "Internal Server Error" on every action

**Diagnosis:**
```bash
# 1. Is the MySQL container running?
sudo docker ps | grep af-mysql
# Expected: af-mysql   ...   Up 3 days   ...   0.0.0.0:33306->3306/tcp

# 2. Can the backend reach it?
sudo docker exec af-mysql mysqladmin ping -h 127.0.0.1 --silent \
  && echo "MySQL OK" || echo "MySQL DOWN"

# 3. Are the credentials in /home/ubuntu/af/config/env correct?
sudo -u ubuntu cat /home/ubuntu/af/config/env

# 4. Recent errors in the backend log?
sudo tail -n 50 /home/ubuntu/af/logs/af.log | \
  jq -r 'select(.level=="error") | "\(.ts) \(.request_id) \(.caller) \(.message)"'
```

**Recovery:**
```bash
# If container is stopped:
sudo docker start af-mysql

# If container is up but auth is broken (e.g. password rotated):
sudo -u ubuntu nano /home/ubuntu/af/config/env  # fix DB_PASSWORD
sudo systemctl restart af-backend

# If the schema drifted (GORM AutoMigrate failed at boot):
sudo tail -n 200 /home/ubuntu/af/logs/af.log | grep -i migrate
# The error will name the table + column. Manual fix:
# 1. sudo docker exec -it af-mysql mysql -uroot -pafbackendpass astock_selector
# 2. SHOW CREATE TABLE <offending_table>;
# 3. ALTER TABLE ... to match the model
# 4. sudo systemctl restart af-backend
```

**Why this happens:**
- The deploy script restarts `af-backend` on every deploy; if
  `af-mysql` isn't up first, the migration fails. The systemd
  `After=docker.service` + `Requires=docker.service` dependencies
  don't guarantee MySQL is up; only that Docker is.
- A previous botched deploy may have left the schema in a
  half-migrated state. Always check `tail -n 200 af.log | grep -i migrate`
  after a deploy.

---

## 4. Backend service crashed (5xx on everything)

**Symptoms:**
- `systemctl status af-backend` shows `inactive (dead)` or `failed`
- `journalctl -u af-backend -n 100` shows a panic stack
- `/healthz` returns 502 (nginx can't reach upstream)

**Diagnosis:**
```bash
# Recent service state:
sudo systemctl status af-backend

# Last 100 lines of journal (panics, OOM, etc.):
sudo journalctl -u af-backend -n 100 --no-pager

# Last panic in the log file:
sudo grep -B2 -A20 '"panic recovered"' /home/ubuntu/af/logs/af.log | tail -50
```

**Recovery:**
```bash
# 1. Restart:
sudo systemctl restart af-backend
sleep 2
sudo systemctl status af-backend

# 2. If it crashes immediately, check the log:
sudo journalctl -u af-backend -n 50 --no-pager

# 3. If the panic is in app code (not a dep), file a bug with:
#    - The stack trace (from journalctl)
#    - The request_id of the failing request
#    - The curl command that triggered it
#    The `request_id` correlates to a single zap log line:
sudo grep '"request_id":"<id>"' /home/ubuntu/af/logs/af.log
```

**OOM kill:**
- If the journal shows `Out of memory: Killed process` (SIGKILL 9),
  the host is under memory pressure. The fix is **not** to keep
  restarting — investigate what's holding memory:
  ```bash
  free -h
  sudo ps aux --sort=-%mem | head -20
  ```
  Common culprits: a misbehaving node leaking goroutines, or
  the perf backfill trying to load too many rows in one transaction.

---

## 5. Frontend 404s / wrong asset / stale bundle

**Symptoms:**
- User reports a 404 on a page that should exist (e.g. `/runs/42`)
- SPA loads but JS console says "Failed to load resource: 404"
- The user is on an old build that references an API path that no longer exists

**Diagnosis:**
```bash
# 1. Is the new dist/ actually deployed?
ls -la /home/ubuntu/af/frontend/dist/ | head
# Compare mtime to the last commit on main:
cd /home/ubuntu/af && git log -1 --format="%ai"  # server-side

# 2. Is nginx serving it?
curl -sI http://127.0.0.1:9091/ | head
# Expected: 200, content-type text/html, body has <div id="root">

# 3. Is nginx trying to serve from a stale path?
sudo nginx -T 2>/dev/null | grep -A3 "af.conf" | head -20
```

**Recovery:**
```bash
# Rebuild + redeploy the frontend:
cd /home/ubuntu/af/frontend
npm ci
npm run build
# (the deploy script copies dist/ to /home/ubuntu/af/frontend/dist/)

# If only the nginx config is wrong:
sudo nginx -t       # validate config
sudo nginx -s reload  # apply
```

**SPA routing 404:**
- If users hit a deep link (e.g. `/runs/42`) directly and get 404,
  the nginx `try_files` directive is missing the SPA fallback.
  The current `/etc/nginx/conf.d/af.conf` has:
  ```
  location / {
      try_files $uri $uri/ /index.html;
  }
  ```
  If that line is missing, all deep links 404. The deploy script
  regenerates `af.conf` from a template — if the template is broken,
  the fallback is missing.

---

## 6. SSE stream broken (frontend shows no live updates)

**Symptoms:**
- Run progress bar in the UI freezes even though the backend is processing
- `curl -N http://127.0.0.1:9090/api/v1/runs/42/events` returns no data
- Browser DevTools Network tab shows the `/events` request in "pending" forever

**Diagnosis:**
```bash
# 1. From the server, can you reach the SSE endpoint?
curl -N --max-time 5 http://127.0.0.1:9090/api/v1/runs/42/events
# Expected: stream of "event: ..." lines. If 0 lines in 5s → see below.

# 2. Is nginx buffering the response?
#    nginx by default buffers proxied responses. SSE must NOT be buffered.
#    Check /etc/nginx/conf.d/af.conf has:
#      proxy_buffering off;
#      proxy_cache off;
#      proxy_read_timeout 300s;   # 5 min — matches backend idle timeout
#    If any of these are missing, the stream looks "stuck" to the client
#    even though the backend is producing events.

# 3. Is the backend EventBus healthy?
sudo grep -c '"event":"node\.' /home/ubuntu/af/logs/af.log
# If count is going up → events are flowing, problem is network/buffering.
# If count is flat → backend's executor is stuck (see §7).
```

**Recovery:**
```bash
# 1. If proxy_buffering is on, edit /etc/nginx/conf.d/af.conf and reload:
sudo nginx -t && sudo nginx -s reload

# 2. If the backend is stuck, see §7 (jobs stuck).
```

---

## 7. Jobs stuck (runs hang in "running" forever)

**Symptoms:**
- A run shows `status=running` in the UI but no new events for >5 min
- `GET /api/v1/runs/{id}/events` produces no frames
- `ps aux | grep af-backend` shows a goroutine holding a DB tx

**Diagnosis:**
```bash
# 1. List running runs:
curl -s 'http://127.0.0.1:9090/api/v1/runs?status=running&page_size=20' | jq

# 2. For a specific stuck run, look at its log entries:
RUN_ID=42
sudo grep "\"run_id\":${RUN_ID}" /home/ubuntu/af/logs/af.log | tail -30

# 3. Are there any in-flight DB transactions?
sudo docker exec af-mysql mysql -uroot -pafbackendpass astock_selector \
  -e "SHOW FULL PROCESSLIST" | grep -v Sleep
```

**Recovery:**
```bash
# 1. If the run is hung on a datasource call (e.g. Tushare is down),
#    killing the goroutine doesn't help — it'll just hang again.
#    Better: mark the run as failed in the DB:
sudo docker exec af-mysql mysql -uroot -pafbackendpass astock_selector \
  -e "UPDATE runs SET status='failed', error='manually cancelled', finished_at=NOW() WHERE id=${RUN_ID}"

# 2. If the goroutine is leaked (Run row updated but goroutine still alive),
#    a backend restart is the cleanest reset:
sudo systemctl restart af-backend
# (downside: in-flight SSE clients must reconnect; Last-Event-ID resumes them)

# 3. If the issue is a deadlock in a node implementation, it's a code
#    bug — file an issue with the request_id from a /trial-run that
#    reproduces the hang.
```

**Why this happens:**
- The most common cause is an external datasource (Tushare) hanging
  on a request that has no client-side timeout. Check the executor's
  datasource client config for `HTTPClient.Timeout`.
- A second common cause: a node's goroutine holds a `*sql.Tx` and
  panics without rolling back, leaving the tx open until the
  connection is recycled (default 8h on MySQL).

---

## 8. Cron not firing

**Symptoms:**
- A strategy has `cron_expression` set and `status=active` but no runs appear
- The UI shows "Last run: never" even though the cron string is valid

**Diagnosis:**
```bash
# 1. Is the cron expression valid?
#    AF uses 5-field cron (minute hour dom month dow).
#    Example: "0 9 * * 1-5" = 09:00 Mon-Fri.
#    Note: NO seconds field. robfig/cron/v3 is strict about this.

# 2. Is the timezone what you think it is?
#    Cron schedules are interpreted in config.Cron.Timezone (default Asia/Shanghai).
#    A 09:00 cron in Asia/Shanghai = 01:00 UTC. Log lines use UTC.

# 3. Is the scheduler actually loaded?
sudo grep "cron" /home/ubuntu/af/logs/af.log | tail -20
# Look for "scheduler started" + entries per registered strategy.

# 4. Was the strategy saved with cron_expression after the scheduler
#    started? Schedulers in AF cache the strategy list at startup.
#    To pick up a new cron_expression, restart:
sudo systemctl restart af-backend
```

**Recovery:**
- The scheduler is a thin wrapper around `robfig/cron/v3`. To debug,
  check the log lines immediately after restart:
  ```bash
  sudo journalctl -u af-backend -f
  ```
  You should see one `cron: registered strategy <code> "<expression>"` line
  per active strategy with a cron expression.

---

## 9. Performance regressions (post-§9 perf engine)

**Symptoms:**
- `GET /api/v1/perf/aggregations?group_by=strategy&from=...&to=...` is slow (>5s)
- `GET /api/v1/runs/{id}/events` first event takes >2s after reconnect
- The §9 perf compute is using too much memory at startup

**Diagnosis:**
```bash
# 1. How long does a slow endpoint actually take?
time curl -s -o /dev/null \
  'http://127.0.0.1:9090/api/v1/perf/aggregations?group_by=strategy&from=2025-01-01&to=2025-06-30'

# 2. Is the DB doing the heavy lifting?
sudo docker exec af-mysql mysql -uroot -pafbackendpass astock_selector \
  -e "SHOW FULL PROCESSLIST" | head
# Look for "Copying to tmp table" or "Sending data" on big tables.

# 3. The perf engine logs query plans for slow aggregates:
sudo grep '"perf"' /home/ubuntu/af/logs/af.log | tail -30
```

**Recovery:**
- See `TODOS.md` §9 — the M1/M2/M3 perf post-merge cleanups
  (configurable startup-backfill timeout, narrowed aggregate SELECT,
  win-rate semantics docs) cover most perf-engine fixes.
- For ad-hoc slowness: add an index. The most common missing index
  is `(strategy_id, picked_at)` on `recommendations`. The GORM
  model already declares it, but it doesn't hurt to verify:
  ```bash
  sudo docker exec af-mysql mysql -uroot -pafbackendpass astock_selector \
    -e "SHOW INDEX FROM recommendations"
  ```

---

## 10. Frontend dev loop broken

For **local development** (not deployed), the common failures are:

```bash
# Backend won't start: "bind: address already in use"
lsof -i :8080 | grep LISTEN
kill <pid>    # or: make dev-down && make dev-up

# Vite proxy not working: CORS errors in browser console
# Cause: middleware.CORS() in backend only allows :5173 and :3000.
# If you changed Vite's port, add it to internal/middleware/cors.go.

# Frontend typecheck failing after a backend change
cd frontend && npm run typecheck
# If types/.../orchestrator.ts is stale, regenerate from backend
# response shapes (currently a manual process — see TODOS.md).
```

---

## 11. Emergency: full restart

If something is badly broken and you need to reset the world:

```bash
# 1. Stop the backend (lets in-flight runs terminate gracefully):
sudo systemctl stop af-backend

# 2. Verify nothing's still listening on 9090:
sudo lsof -i :9090

# 3. Tail the last log lines so you have a record:
sudo tail -n 200 /home/ubuntu/af/logs/af.log > /tmp/af-crash.log

# 4. Restart:
sudo systemctl start af-backend
sudo systemctl status af-backend

# 5. Verify health:
curl -s http://127.0.0.1:9090/api/v1/healthz | jq
```

If the backend **refuses to start** (panics on every boot):
1. Check the systemd journal: `sudo journalctl -u af-backend -n 100`
2. Check the migration log: `sudo grep -i migrate /home/ubuntu/af/logs/af.log | tail -20`
3. If the schema is broken beyond repair, you can wipe and re-migrate:
   ```bash
   # DANGER: this drops all data. Only do this on a dev instance.
   sudo docker exec af-mysql mysql -uroot -pafbackendpass -e "DROP DATABASE astock_selector; CREATE DATABASE astock_selector;"
   sudo systemctl restart af-backend  # GORM AutoMigrate will recreate tables
   ```
4. If the schema is fine but a config is wrong, fix `config/env` and restart.

---

## 12. Monitoring checklist (for future Prometheus/Grafana work)

Not yet wired, but these are the metrics worth tracking:

- `http_request_duration_seconds{path,status}` — p50/p95/p99 per endpoint
- `http_requests_in_flight` — gauge
- `db_pool_in_use_connections` / `db_pool_idle_connections` — GORM stats
- `runs_in_state{status="running"}` — gauge, alert if > 5 for > 10 min
- `cron_last_fired_seconds_ago{strategy_code}` — gauge per strategy
- `sse_clients_connected` — gauge
- `eventbus_subscribers_per_run` — gauge
- `perf_calculate_duration_seconds` — histogram
- zap log error rate per `request_id` prefix

This section is a placeholder — see `DX_REVIEW.md` P2 for the
"add metrics" follow-up.

---

## 13. Glossary

- **Run** — one execution of a strategy DAG. Identified by `run_id`. Has a status (pending/running/succeeded/failed/cancelled).
- **DAG** — the strategy's node graph, stored as `dag_json` on the `strategies` row and on every `strategy_versions` row (immutable snapshot).
- **Node** — one step in the DAG. Types: `data_source`, `indicator`, `filter`, `rank`, `dedupe`, `session_tag`, `persist`, `notify`.
- **Trial run** — `POST /strategies/:id/trial-run`. Same as a run but with `dry_run=true`: no DB writes, no notifications, no persisted run row.
- **Snapshot** — one row in `performance_snapshots`. Represents a T+N return computation for a single `recommendation` at a point in time.
- **Recommendation** — one row in `recommendations`. A persisted stock pick from a run's `persist` node.
- **request_id** — UUID v4 stamped onto every HTTP request by `middleware.RequestID`. Echoed in `X-Request-ID` response header + `request_id` field of every error body + zap logs.

---

## 14. Backup & restore

The only stateful component is MySQL (the `af-mysql` container on port
33306). Redis is a cache — losing it costs nothing but a cold start.
The Go binary, config, and frontend are all redeployable from git.

### 14.1 What to back up

| What | Where | Matters because |
|------|-------|-----------------|
| MySQL data | `af-mysql` container | strategies, runs, recommendations, perf snapshots — the only irreplaceable state |
| `config/env` | `/home/ubuntu/af/config/env` | DB password + datasource tokens + notify webhooks (NOT in git) |
| `config/config.yaml` | `/home/ubuntu/af/config/config.yaml` | live config (capture any prod-only edits) |

### 14.2 Backup

```bash
# Dump the DB (consistent snapshot, no locking). Creds per §0.
TS=$(date +%Y%m%d-%H%M%S)
mkdir -p /home/ubuntu/af-backups
sudo docker exec af-mysql mysqldump -uroot -pafbackendpass \
  --single-transaction --routines --triggers astock_selector \
  > /home/ubuntu/af-backups/af-${TS}.sql

# Capture the secrets file (keep it OUT of git).
sudo cp /home/ubuntu/af/config/env /home/ubuntu/af-backups/env-${TS}
```

Nightly host cron (separate from the app's cron), keep 14 days:

```cron
# /etc/cron.d/af-backup — 02:30 daily
30 2 * * * root docker exec af-mysql mysqldump -uroot -pafbackendpass --single-transaction --routines --triggers astock_selector > /home/ubuntu/af-backups/af-$(date +\%Y\%m\%d).sql 2>>/home/ubuntu/af-backups/backup.log && find /home/ubuntu/af-backups -name 'af-*.sql' -mtime +14 -delete
```

> **Installed on prod (2026-06-20):** `/etc/cron.d/af-backup` is live and
> a manual backup was verified into `/home/ubuntu/af-backups/`. Log
> rotation for `/home/ubuntu/af/logs/*.log` is also installed at
> `/etc/logrotate.d/af-backend` (daily, keep 14, `copytruncate`).

### 14.3 Restore

```bash
sudo systemctl stop af-backend
sudo docker exec -i af-mysql mysql -uroot -pafbackendpass \
  -e "DROP DATABASE IF EXISTS astock_selector; CREATE DATABASE astock_selector;"
sudo docker exec -i af-mysql mysql -uroot -pafbackendpass astock_selector \
  < /home/ubuntu/af-backups/af-20260618-023000.sql
sudo systemctl start af-backend
sleep 2 && curl -s http://127.0.0.1:9090/api/v1/healthz | jq
```

> **GORM AutoMigrate runs on every boot.** Restoring an OLDER dump under
> a NEWER binary auto-migrates the schema forward. A NEWER dump under an
> OLDER binary is unsupported — match the binary to (or newer than) the dump.

### 14.4 Not backed up

- **Redis** — pure cache, rebuilds on demand.
- **In-flight runs** — only persisted `runs` / `run_logs` rows are captured; re-trigger anything mid-execution after a restore.
- **SSE client state** — ephemeral; clients reconnect.
