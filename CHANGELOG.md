# Changelog

All notable changes to AF Selector are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> **Conventions**
> - `Added` — new features
> - `Changed` — changes in existing functionality
> - `Fixed` — bug fixes
> - `Removed` — features removed in this release
> - `Security` — security fixes
> - `Perf` — performance improvements (not in the standard set, but useful here)
> - Every entry links to the commit that introduced the change. When the
>   project is published with GitHub Releases, this becomes the source
>   of the release-notes body.

---

## [Unreleased]

### Planned (v1.4+)
- Bulk kline fetch (sina has no multi-stock history API; kline-based
  screens over large universes are still per-stock)
- Multi-user roles enforcement (the data model + token claims are
  multi-user ready; per-role route gating is not wired yet)

---

## [1.4.4] — 2026-06-30

Fixes a hard frontend load failure when the app is served over plain
HTTP to an IP (the production setup, `http://124.156.213.179:9091`).
Tagged `af-v1.4.4`.

### Fixed
- **`crypto.randomUUID is not a function` broke the first request.** The
  axios request interceptor used `crypto.randomUUID()` for the
  `X-Request-ID` correlation header, but that API only exists in a SECURE
  context (HTTPS or localhost). Over plain HTTP to an IP it is undefined,
  so the very first API call threw and the page showed "加载失败".
  (Never surfaced earlier because dev/eval always used localhost or the
  same-origin proxy.) Added a `requestId()` helper that uses
  `crypto.randomUUID()` when available and falls back to a Math.random
  v4-shaped id otherwise — it's only a log-correlation id, so no crypto
  strength is needed.

### Tested
- New client test asserts the interceptor still sets a valid
  UUID-shaped `X-Request-ID` when `crypto.randomUUID` is unavailable
  (the HTTP+IP case). Frontend 185 → 186, all green.

---

---

## [1.4.3] — 2026-06-28

Production migrated to Docker Compose. The live host (a multi-project
shared server) now serves AF from containers instead of a systemd bare
binary, with zero impact on the other apps sharing the box. Tagged
`af-v1.4.3`.

### Added
- **`docker-compose.prod.yml`** — a prod-only compose that containerizes
  ONLY AF's three pieces (app + web + sidecar) and REUSES the existing
  infra: the manually-started `af-mysql` container (host :33306, data in
  the `af-mysql-data` volume) and host Redis (:6379). It does NOT start
  its own mysql/redis — that would collide with the other projects'
  :3306/:6379 and ignore AF's existing data. The app reaches host
  services via `host.docker.internal` (extra_hosts host-gateway);
  validation ports 9095 (app) / 9096 (web) bind to localhost only.

### Changed (production deploy topology — no app code change)
- The live deploy moved from `systemd` (bare `af-backend` + a Python
  `af-akshare-sidecar`) to the compose stack: host nginx :9091 now
  `proxy_pass`es to the `afd-web` container (:9096), which serves the SPA
  and proxies `/api` to `afd-app`. The legacy systemd units were stopped
  and disabled but kept on disk for rollback.
- The database was NOT migrated — the same `af-mysql` container + volume
  back both the old and new stacks, so the cutover carried all data
  (strategies, users, llm_providers) with no copy step.

### Migration notes
- Parallel-validated before cutover: the new stack ran alongside the live
  systemd one (same DB) and was verified (healthz `db:ok`, login, 3
  existing strategies visible) before any public traffic moved.
- Gotcha handled: the host nginx serving :9091 is a raw process, not a
  systemd unit, so `systemctl reload nginx` fails — the reload is a
  `kill -HUP <master pid>`. Documented in the on-host `ROLLBACK.md`.
- Rollback (kept ready on the host `ROLLBACK.md`): revert nginx
  `proxy_pass` to :9090 + HUP reload, `systemctl enable --now` the two
  units, `docker compose down`.
- Only AF's own services + AF's nginx site were touched; the co-hosted
  apps (healthcare-bridge / hedgedoc / openmaic) were untouched.

---

---

## [1.4.2] — 2026-06-27

The Docker Compose stack now runs the COMPLETE app locally, including
the akshare data sidecar that the selection engine depends on. Local
dev/eval only — production is still the systemd deploy. Tagged
`af-v1.4.2`.

### Added
- **akshare sidecar container** (`scripts/Dockerfile.sidecar`):
  python:3.12-slim + akshare, serves the kline/quote/fundamental/
  universe/news contract on :18800, with a `/health` HEALTHCHECK. Added
  as the `sidecar` service in docker-compose.yml; the app depends on it
  (`condition: service_healthy`).
- **`config.docker.yaml`** (backend/configs): same as the example but the
  akshare source points at `http://sidecar:18800` — inside a container
  `127.0.0.1` is the container itself, so the compose `app` overrides its
  entrypoint to use this config. DB/redis/auth still come from env.

### Fixed
- **Frontend container had no API proxy.** `frontend/nginx.conf` now
  proxies `/api/*` → `http://app:8080` (with SSE-friendly buffering off +
  long read timeout). Without it every API call in the Docker stack hit
  the SPA fallback and returned index.html.
- **`.env.example` documented a `DATASOURCE_SOURCES` env var that is
  never parsed.** Removed the misleading line (the source list lives in
  the YAML config, not env) and added the real, parsed `AUTH_*` and
  `AI_*` knobs.

### Notes
- Not verified by an actual `docker compose build/up` (no Docker on the
  authoring machine) — validated statically (compose + config YAML parse,
  build-context paths, env keys confirmed parsed by the loader, Go
  builds). Run `cp .env.example .env && docker compose up --build` on a
  Docker host to bring up app + web + sidecar + mysql + redis.
- The sidecar needs outbound internet (sina/akshare upstreams).

---
---

## [1.4.1] — 2026-06-26

A change-password UI on the settings page. Frontend-only — the backend
`POST /auth/change-password` already shipped in v1.3.0. Tagged
`af-v1.4.1`.

### Added
- **Change-password card (settings page).** Shown only when auth is
  active (a user is logged in); hidden when auth is disabled. Old
  password + new password + confirm, with client-side checks (new ≥ 8
  chars, confirm must match) before calling the existing
  `/auth/change-password` endpoint. On success the fields clear and a
  toast reminds the user to use the new password next login.

### Changed
- **`Field` now wraps its control in the `<label>`** (settings page), so
  label and input are associated for accessibility (and testability)
  without manual ids.

### Tested
- 5 new SettingsPage tests (card hidden when auth off / shown with
  username when logged in / rejects too-short new pw / rejects mismatched
  confirm / submits a valid change). Frontend 180 → 185, all green.

---
---

## [1.4.0] — 2026-06-26

Multi-provider LLM fallback chain. Configure several model providers in
priority order; when one is unavailable (error / timeout / rate-limit)
the assistant automatically falls through to the next. Tagged
`af-v1.4.0`.

### Added
- **LLM fallback chain (`internal/ai/chain.go`).** A `chainClient`
  implements the existing `Client` interface over an ordered list of
  backends: it tries them top-to-bottom and returns the first success;
  any that fail (network error, HTTP 4xx/5xx, timeout, rate-limit, or an
  empty response) are skipped so the next gets a turn. It IS a `Client`,
  so it drops into the hot-swappable `Provider` with zero changes to
  `ai.Service` / `review.Service`. **Sequential, not parallel** — a
  healthy primary means exactly one upstream call (one provider's
  tokens); later backends are only hit on failure. A single configured
  backend is returned unwrapped (no chain overhead). The active name
  reads `chain[deepseek:deepseek-chat>glm:glm-4.5>minimax:...]` for the
  audit log.
- **New providers — MiniMax, 阿里云百炼 (通义千问), 智谱 GLM.** All three are
  OpenAI-compatible (verified 2026-06), so no new client code — just
  presets. `ai.Presets` maps each to its base_url + default model;
  `ResolveEndpoint` fills them in so an entry needs only provider + key.
  Endpoints: MiniMax `https://api.minimax.io/v1`, 百炼
  `https://dashscope.aliyuncs.com/compatible-mode/v1`, GLM
  `https://open.bigmodel.cn/api/paas/v4`. (This supersedes the old
  "MiniMax needs its own adapter" note — it now has an OpenAI-compatible
  endpoint.)
- **Ordered provider config (`model.LLMProvider`, table `llm_providers`).**
  Priority-ordered rows replace the single-row `llm_settings`. On first
  boot the legacy single row is migrated in as priority 0 — existing
  deployments keep working with no manual step.
- **Settings API + UI.** New `GET/PUT /api/v1/settings/llm/providers`
  (ordered list, keys masked) + `POST .../providers/test` (test one).
  The settings page is now an add/remove/reorder provider list with
  up/down priority controls and per-row connection test; presets include
  MiniMax / 百炼 / GLM. Legacy single-provider routes kept for
  compatibility.

### Tested
- 8 chain tests (first-wins / fall-through-on-error / fall-through-on-
  empty / aggregate-all-failed / summarize fallback / cancelled-context
  short-circuit / single-not-wrapped / name ordering). settings: 5 → 15
  (save builds ordered chain, single-not-wrapped, disabled skipped,
  preset fill, keep-key across reorder, legacy migration, load-on-start,
  mask, empty disables). Frontend 174 → 180 (SettingsPage list: render /
  blank-default / add / save-order / test-one / remove). Full backend
  suite + 180 frontend tests green. **E2E verified**: a chain of
  [deepseek with a bad key, mock] → AI preview fails on deepseek and
  falls through to mock, returning a valid proposal
  (`llm=chain[openai:deepseek-chat>mock]`).

### Notes
- Sequential fallback keeps cost at one provider's tokens in the common
  case. If you want a true parallel race (N× cost, lowest latency),
  that's a separate mode — not built.

---
---

## [1.3.4] — 2026-06-25

Graceful frontend cold-start. A fresh `make run` no longer floods the
console with misleading 500s while the backend is still coming up.
Frontend/dev-tooling only — no backend change. Tagged `af-v1.3.4`.

### Fixed
- **Cold-start 500s were really "backend not listening yet".** During a
  fresh `make run` the Go backend takes a few seconds to compile +
  migrate + listen. Requests arriving in that window hit a closed socket,
  and vite's http-proxy default surfaced that as a `500` — which the
  browser console reported as a hard error even though the backend was
  *starting*, not broken. (The backend itself never served on
  un-migrated tables: `initDB` runs `database.Migrate` synchronously
  before the `ListenAndServe` goroutine, so migration always completes
  before the socket opens. The 500 was purely the proxy's translation.)

### Changed
- **vite proxy maps transient connection errors to 503.** `vite.config.ts`
  gains a proxy `configure` hook: connection refused/reset/timeout/
  host-unreachable → `503` + `Retry-After: 1` with a clear
  `后端正在启动，请稍候…` body; other proxy errors → `502`.
- **Axios cold-start retry.** `client.ts` retries idempotent GETs up to
  3× with linear backoff (400/800/1200ms) on `503`/`502` or a network
  error (no response). Non-GET requests and genuine 4xx/5xx are
  untouched, so a real error still surfaces immediately.

### Tested
- `tsc` clean; 164 frontend tests green (incl. the interceptor
  behavior test). Verified locally: backend down → proxy returns `503`
  (was `500`); on restart the proxy emits `503×4 → 200` and the GET
  retry rides through the window so first paint recovers on its own.

### Notes
- The retry budget (~2.4s) covers a fast restart / prebuilt-binary cold
  start. A full `go run` compile (~19s) can outlast it; the page then
  rests on the clean `503` "starting" message and a single refresh
  recovers — no more scary 500.

---

## [1.3.3] — 2026-06-25

Multi-keyword OR in the filter node. One step can now match "title
contains 分红 OR 回购 OR 增持" — useful for news keyword monitoring.
Tagged `af-v1.3.3`.

### Added
- **`contains_any` filter op.** The `filter` node's `contains` matches a
  single substring, and chaining two filters ANDs them, so there was no
  single-step OR. The new `contains_any` op returns true when the field
  contains ANY of several substrings. Value accepts a JSON array
  (`["分红","重组"]`) or a comma-separated string (`"分红,重组"`, each term
  trimmed). Non-string fields and empty/blank-only values are handled
  (the latter is a param error). The filter config panel gained the
  `contains_any 含任一` option + a `["分红","重组"]` placeholder.
- **Files:** `backend/internal/executor/nodes/filter.go` (`matchExpr`
  case + `toStringList` helper + Schema op enum),
  `frontend/src/components/canvas/NodeConfigPanel.tsx` (FilterForm).

### Tested
- 4 backend cases (array form, comma-string with whitespace, no-match,
  empty-value param error) + full suite green; 164 frontend tests green.
  End-to-end on prod: the `新闻关键词监控` strategy with
  `contains_any ["分红","回购","增持"]` over 50 real news rows →
  `matched:5` spanning multiple keywords (茅台 分红 ×4 + 五粮液 增持 ×1),
  which a single `contains` could not express.

### Notes
- No union/merge node exists to OR two parallel branches; a single-node
  multi-keyword op is the cleanest path. A general branch-union node
  remains a separate, larger item.

---

## [1.3.2] — 2026-06-25

Real per-stock news, end to end. The `news` data_source subtype now
returns actual articles instead of an empty stub. Sidecar-only change —
no Go binary rebuild. Tagged `af-v1.3.2`.

### Added
- **Real news source in the akshare sidecar.** `GET /news?code=&limit=`
  now serves per-stock news via `akshare.stock_news_em` (eastmoney-backed
  — its *quote/push* endpoints are blocked from the prod Tencent Cloud
  IP, but the *news* endpoint is reachable, confirmed by probing on the
  box). Maps akshare's `新闻标题/新闻内容/新闻链接/发布时间` to the Go
  adapter's `{title, content, url, published_at}` shape. The akshare
  `发布时间` (`2026-06-21 18:05:00`, CST, no zone) is converted to
  RFC3339 with `+08:00` — required because the Go adapter unmarshals
  `published_at` into `time.Time`, which only accepts RFC3339; an
  unconverted value would fail the whole GetNews parse. Unparseable
  timestamps degrade to a zero time (not an error).
- **Files:** `scripts/akshare-sidecar.py` (`fetch_news` + `_to_rfc3339`
  + `/news` route). The Go side is unchanged — `akshare.GetNews`,
  `News.Row()`, and the `news` data_source subtype already consume this
  shape (plumbed in v1.3.1).

### Tested
- End-to-end on prod with the deployed binary: a `news → filter(title
  contains 分红)` strategy returned `ds_news count:10` (real 茅台 news,
  `published_at` parsed by Go's `time.Time`) → `filter matched:4,
  dropped:6`. Confirms the full path: real source → sidecar time
  conversion → manager fallback → akshare.GetNews → News.Row → filter.

### Notes
- `stock_news_em` is per-stock and not batchable, so news screens over a
  large universe are still N calls — keep `news` strategies to explicit
  small `stock_codes`, same guidance as kline.

---

## [1.3.1] — 2026-06-25

Internal refactor: a single projection layer for market-data rows.
Fixes a latent news bug along the way. No API or behavior change for
existing strategies. Tagged `af-v1.3.1`.

### Fixed
- **News was not filterable.** The `news` data_source subtype emitted a
  typed `[]datasource.News` under a `news` key, but every downstream
  node (filter/rank/dedupe/persist/notify) finds its input via
  `findFirstSlice`, which only matches `[]any`. A typed slice was
  invisible — a filter placed after a news data_source silently matched
  zero. Same class of bug fundamentals hit earlier. News now emits the
  `{items:[]any, count}` row envelope like quote/fundamental, so it is
  filterable. Regression test wires news → filter and asserts a
  non-empty match.

### Changed
- **Projection layer (`internal/datasource/rows.go`).** The
  canonical→row mapping moved out of `data_source.go` (where it was
  hand-written per subtype and prone to drift — the source of the
  fundamentals and entry_price bugs) into one place: `Quote.Row()`,
  `Fundamental.Row()`, `News.Row()`, plus a `Rower` interface and a
  generic `ToRows[T]` helper. The single semantic convention —
  `Quote.Price` mirrored to `close` so indicator-less screens can filter
  on `close` — now lives in `Quote.Row()` only.
- **KLine stays a typed series.** It deliberately has no `Row()`: the
  indicator node groups candles per stock under the `klines` key.
  Flattening would break grouping; documented as the intentional
  exception.
- **`data_source` Schema Outputs now match reality.** Previously
  declared `quotes`/`fundamentals` keys that were never emitted; now
  declares `items` (quote/fundamental/news) + `klines` (kline) + `count`.

### Tested
- New `rows_test.go` (Row() field coverage + `price→close` mirror +
  `ToRows` typed-slice→`[]any` projection). New news→filter regression
  test. Full backend suite green.

### Notes
- Borrowed the *idea* from OpenBB's provider standardization (each data
  model has one declarative projection), implemented the lightweight Go
  way (a `Row()` method) — no per-source alias tables, no Fetcher
  pipeline, no plugin registry. Right-sized for 3 A-share sources.

---

## [1.3.0] — 2026-06-25


A login gate protects the whole API. JWT auth with a bootstrapped admin,
multi-user-ready data model + token claims. Off by default (dev stays
open); flip `auth.enabled` to lock it down. Tagged `af-v1.3.0`.

### Added
- **§12 authentication — JWT login gate.** New `internal/auth` package:
  - **Service** — `Login` (bcrypt verify + constant-time dummy compare on
    miss, no user-enumeration leak), `issue`/`Verify` (HS256 JWT carrying
    `uid`/`usr`/`role` claims), `Bootstrap` (creates the first admin on an
    empty DB; generates + returns a random password when none is
    configured, logged once), `ChangePassword` (verifies old, min-8 new).
  - **Middleware** — verifies the `Authorization: Bearer <jwt>` header,
    stashes the principal (`auth_user_id`/`auth_username`/`auth_role`) in
    the gin context for downstream handlers + future per-role enforcement.
  - **Handler** — `POST /auth/login` (public), `GET /auth/me` +
    `POST /auth/change-password` (protected).
- **Config.** `AuthConfig` (`auth.enabled`, `auth.jwt_secret`,
  `auth.admin_user`, `auth.admin_password`, `auth.token_ttl`) with env
  bindings `AUTH_ENABLED` / `AUTH_JWT_SECRET` / `AUTH_ADMIN_USER` /
  `AUTH_ADMIN_PASSWORD` / `AUTH_TOKEN_TTL`. Defaults to **disabled** so
  dev/local stays open; an empty `jwt_secret` generates an ephemeral one
  (logged, tokens invalidate on restart — set a stable secret in prod).
- **Router wiring.** Login mounts BEFORE `api.Use(authMiddleware)`, so the
  public basics (`/ping`, `/healthz`, `/openapi.json`, `/auth/login`) stay
  open while everything registered after requires a valid Bearer token.
- **Frontend.** `authStore` (zustand + localStorage, SSR/test-safe
  guards), `api/auth.ts` (login + change-password), a `/login` page
  outside the admin layout, a Header username + 退出 button (shown only
  when a session exists), and axios interceptors that attach the Bearer
  token and bounce to `/login?next=…` on a 401 (skipping the login call
  itself). The frontend is adaptive: it only redirects on a real backend
  401, so it works whether auth is on or off without a hard route guard.

### Tested
- 9 backend service tests (bootstrap idempotency + generated-vs-explicit
  password, login success / wrong-pw / unknown-user, verify success /
  foreign-secret reject / expiry reject, change-password full cycle).
- 164 frontend tests green. End-to-end verified with auth enabled +
  sqlite: protected route w/o token → 401, public `/ping` → 200, login →
  JWT, `/me` w/ token → principal, protected w/ token → 200, wrong pw →
  401.

### Notes
- v1 ships as a single-admin login gate. The `User`/`Role` model and the
  `uid`/`role` token claims are multi-user ready — per-role route
  enforcement is deferred (see Planned).

---

## [1.2.3] — 2026-06-25

Quote-based screens scale to large universes — one whole-market call
instead of N per-stock requests. Tagged `af-v1.2.3`.

### Perf
- **Batch quote.** The data_source quote path now fetches the whole
  A-share spot snapshot in ONE cached call (sidecar `/spot`, backed by
  `stock_zh_a_spot`, ~5500 rows, 60s cache) and filters to the
  requested codes, instead of N per-stock GetQuote calls that hit sina
  rate limits on a universe. `datasource.BatchQuoter` (optional
  interface on the manager) + `data_source.fetchQuotes` prefer it,
  falling back to per-stock on failure. Verified: universe=hs300
  limit=50 quote run → 1 `/spot` call (vs 50) → 48 quotes → 5 picks.

### Notes
- Only the QUOTE path is batched. Historical kline is still fetched per
  stock — sina has no bulk-history API — so MACD/MA-style screens over
  large pools remain slow; keep `universe_limit` modest for those. Spot
  quote screens (放量突破 / 午后量能) now run over big pools cheaply.

---

## [1.2.2] — 2026-06-24

Strategies can now screen a real stock pool instead of a hardcoded
3-4 name list. Tagged `af-v1.2.2`.

### Added
- **Stock universe.** The data_source node takes a `universe` param
  (hs300 / sse50 / zz500 / zz1000 / all) + `universe_limit`, resolved
  to its constituents and merged (deduped, order-stable) with any
  explicit stock_codes. `stock_codes` is now optional when a universe
  is set. Backed by a sidecar `/universe` endpoint (akshare
  `index_stock_cons` for index pools, `stock_zh_a_spot` for 全A),
  returning canonical `NNNNNN.SH` codes; the manager exposes it via an
  optional `UniverseLister` interface (UniverseURL from the akshare
  source). The data_source node config panel gained a 股票池 dropdown +
  上限 field, so no DAG hand-editing.
- The akshare sidecar's kline fetch now retries with backoff to ride
  out sina rate-limiting during multi-stock universe runs.

### Fixed
- The data_source `kline` branch still passed the raw (often empty)
  `stock_codes` instead of the universe-merged code list, so a
  resolved universe fetched nothing. quote/fundamental/news were
  already correct; kline now matches. (found via end-to-end testing)

### Notes
- Each universe stock is a separate sina request, so large pools are
  slow and can hit rate limits — keep `universe_limit` modest (≤ 50).
  A bulk fetch is planned.

Verified end to end (local, real data): universe=hs300 limit=8 →
312 klines → MACD filter → 5 persisted recommendations.

---

## [1.2.1] — 2026-06-24

Runtime LLM settings + first real provider. §11/§14.9 no longer need
the rule-based mock — pick a backend in the UI, no restart. Tagged
`af-v1.2.1`.

### Added
- **Settings page + runtime LLM provider.** `ai.Provider` is a
  hot-swappable Client; `model.LLMSetting` persists the active backend;
  `settings.Service` validates → saves → swaps the live client and is
  loaded at boot (falls back to `ai.*` config when unset). Endpoints:
  `GET/PUT /api/v1/settings/llm`, `POST /api/v1/settings/llm/test`. The
  frontend 设置 page has a provider dropdown (Mock / DeepSeek / OpenAI /
  custom OpenAI-compatible), a test-connection button, and masks the
  stored API key (saves with keep_key so you needn't re-type it).
- **DeepSeek verified end to end** (local + prod): `provider=openai`,
  `base_url=https://api.deepseek.com/v1`, `model=deepseek-chat`. Real
  natural-language edits the mock could never do — e.g. "把 MACD 快线改
  8、慢线改 21" → fast 12→8, slow 26→21; "排序取前10改前5" → top 20→5 —
  and real narrative review summaries.

### Security notes
- The LLM API key is stored in plaintext in the DB (`llm_settings`),
  per the single-user self-hosted contract. The settings endpoints are
  unauthenticated (v1) — do not expose the API port to untrusted
  networks. Masked on read; never returned in plaintext.

---

## [1.2.0] — 2026-06-24

The three deferred v1 features ship: §10 visualization + positions,
§11 AI conversational strategy editing, and §14.9 automated reviews.
All three are live on prod and dogfooded against real market data.
Tagged `af-v1.2.0`.

### Added — §10 visualization dashboard + positions
- **Dashboard landing page is real** (was a "v1 待实现" placeholder):
  `GET /api/v1/dashboard` returns today's recommendation count, total
  recs, run success rate over a window, and recent errors. Frontend
  renders stat cards, an ECharts win-rate-by-strategy bar chart, a
  方案 × {T+1,T+3,T+5} average-return heatmap, and a recent-errors list.
- **Positions ledger** (`positions` table + `/api/v1/positions` CRUD):
  mark a recommendation as bought (or add manually), hand-maintain cost
  price + quantity; current price / market value / P&L / P&L% are
  computed on read from the live datasource (never stored), red-up /
  green-down per A-share convention. Inline edit + close (soft-delete).
  A "标记建仓" button on the recommendations page and a 当前持仓 strip
  on the dashboard.

### Added — §11 AI conversational strategy editor
- Pluggable LLM `Client`: **mock** (rule-based, zero-cred — understands
  e.g. "把 ma 周期改成 10", "top 取 5") and **openai** (OpenAI-compatible
  /chat/completions: OpenAI, DeepSeek, 通义, Kimi). Config `ai.*`,
  defaults to mock.
- Two-stage, audited flow: `POST /strategies/:id/ai/preview` (LLM
  proposes → `ParseDAG` validates → node-level diff → AIAudit, never
  mutates) → `POST .../ai/apply` (re-validates server-side, commits a
  new immutable strategy version → AIAudit). The LLM stays OFF the
  critical path — it only proposes; validation + commit are
  deterministic Go.
- Frontend: a "✨ AI 助手" panel in the strategy editor — instruction
  in, diff preview, apply reloads the canvas.

### Added — §14.9 automated daily/weekly reviews
- `review_reports` table + `review.Service`: gather a period's recs +
  their latest T+N snapshots, compute T+5 win-rate / avg return, and
  summarize via the shared AI client (data-only fallback when no LLM).
- Two-cron scheduler (daily 15:30 Mon-Fri, weekly Sun 20:00,
  configurable). `GET /api/v1/reviews`, `GET /reviews/:id`,
  `POST /reviews/generate`. Frontend 复盘 page (master/detail + manual
  generate).

### Fixed
- **Canvas rendered blank** — DAG nodes existed in the DOM but stayed
  `visibility:hidden`. ReactFlow is controlled here; it writes measured
  `width`/`height` back via `onNodesChange`, but the `rfNodes` memo
  cherry-picked only `{id,type,position,data}` and dropped them, so
  ReactFlow never saw the nodes as measured. Spread the whole node; also
  re-fit the viewport when a DAG loads (0→N nodes). (9d60773)
- **Perf routes were mounted at the wrong path** — `/api/v1/aggregations`
  etc. instead of the documented `/api/v1/perf/*` (collided with the
  executor's `/recommendations`). The handler test mounted under
  `/perf`, so it passed while prod served the wrong path. Now mounted
  under `api.Group("/perf")`. (0b51f95)
- **Recommendation entry prices persisted as 0/0** — no node emits
  `entry_price_*`; persist now falls back to the row's `close`. (2437904)

### Notes
- §11/§14.9 ship with the rule-based mock LLM by default. Set
  `ai.provider=openai` + `ai.base_url`/`ai.api_key`/`ai.model` for real
  natural-language understanding (env `AI_*`).
- Positions are a single-user holdings ledger (no trade execution); the
  system never places orders.
- Production data source remains the sina-backed akshare sidecar (the
  prod IP is blocked by eastmoney); see `scripts/akshare-sidecar.py`.

---

## [1.1.2] — 2026-06-23

Fundamental data is now wired, closing the last v1.1.0 known gap: the
`low_valuation_high_dividend` template screens on real PE / PB / ROE /
dividend-yield instead of returning empty. Tagged `af-v1.1.2`.

### Added
- **`akshare-sidecar.py` `/fundamental` returns real metrics.** PE/PB
  from baidu (`stock_zh_valuation_baidu`), ROE from sina
  (`stock_financial_analysis_indicator` → 净资产收益率), and
  `dividend_yield` computed from the trailing-12-month cash dividend
  (`stock_history_dividend_detail` 派息 ÷ 10 ÷ latest close). Each
  metric is guarded so one failing upstream doesn't blank the row.
  Verified on prod: 600519 → PE 18.76, PB 5.73, ROE 10.06, div 6.42%.
  (70750c9)

### Fixed
- **Fundamental / quote filters always matched 0.**
  `data_source.fetchFundamentals` / `fetchQuotes` returned a typed
  `[]datasource.Fundamental` / `[]datasource.Quote`, but the
  filter/rank nodes find their input via `findFirstSlice`, which
  matches `[]any` ONLY — a typed slice was invisible, so every
  fundamental/quote filter dropped everything. Both now emit per-stock
  ROWS as `[]any` of `map[string]any` (`stock_code`, `pe`, `pb`,
  `roe`, `dividend_yield`, …), the same per-stock-row contract the
  indicator already emits. Verified end to end on prod: the
  low-valuation funnel now screens 4 real stocks (PE filter drops 2,
  ROE filter drops the rest) — a legitimate value screen, not an empty
  passthrough. (70750c9)

### Notes
- **ROE is the latest *reporting-period* figure, not annualized.**
  sina's 净资产收益率 returns the most recent quarterly report value
  (e.g. a Q1 ROE of 2.46 is a single quarter, not a TTM/annual number),
  so a `roe > 10` rule rarely matches mid-year. Use an annual report
  period or annualize if you need TTM ROE — the wiring is correct, this
  is a data-cadence caveat.
- `news` via the sina sidecar is still a stub (returns empty); no
  bundled template depends on it.

---

## [1.1.1] — 2026-06-23

Follow-up to 1.1.0: recommendations now carry a real entry price.
Tagged `af-v1.1.1`.

### Fixed
- **Recommendations persisted `entry_price_low/high` = 0/0.** No node
  in the bundled templates emits `entry_price_*`; only `close` flows
  through from the indicator rows, but persist read `entry_price_*`
  directly. Now when both entry prices are unset, persist falls back to
  the row's latest `close` — the honest reference price at
  recommendation time (no fabricated spread). An explicit
  `entry_price_*` from an upstream node is still preserved. Verified on
  prod with real market data: 600519/000858/601318 persisted with
  entry = close (1241.41 / 76.55 / 51.98). (2437904)

### Known gaps (still deferred)
- `fundamental` data via the sina sidecar is not wired (returns empty);
  templates that need fundamentals screen on what klines provide.

---

## [1.1.0] — 2026-06-23

**The selection engine actually works now.** From v1.0.0 through
v1.0.4 a strategy run reported `success` while producing zero picks:
the core "run a DAG → screen stocks → persist recommendations" path
was broken end to end and had never run successfully even once. This
release fixes the six defects in that path and verifies it on the
production box against real A-share market data. Tagged `af-v1.1.0`.

### Fixed
- **Orchestrator scheduler race — runs did nothing yet reported
  success.** `Execute()` used errgroup + a coordinator goroutine that
  only scheduled a node AFTER a predecessor finished; the main
  goroutine reached `g.Wait()` while the errgroup counter was still 0,
  so Wait returned immediately, the derived context was cancelled, and
  every node was skipped. Empty results then aggregated to "success".
  This was a KNOWN, documented race with 7 tests `t.Skip`'d — so CI
  stayed green while the engine ran nothing. Rewrote the scheduler
  (main goroutine schedules, workers report on a channel, semaphore
  bounds concurrency, all shared state mutated serially); ctx-cancel
  mid-run now maps to skipped, not failed. Un-skipped all 7 tests;
  `go test -race` green. (35e8f17)
- **Stock-code format mismatch — every template failed at fetch.**
  Templates ship codes as `600519.SH`, but the eastmoney/sina adapters
  hard-required a bare 6-digit code (`expected 6-digit code`). Added a
  shared `datasource.SplitCode` (accepts both forms) and routed both
  adapters through it. (35e8f17)
- **eastmoney `klt` mapping was off by a whole scheme.** `1m→101`
  (101 is actually *daily*) and `1d→107` (not a valid klt) meant every
  daily request came back empty/EOF. Fixed to eastmoney's real codes
  (1/5/15/30/60 intraday, 101/102/103 daily/weekly/monthly). (35e8f17)
- **Node data model was incoherent — pipeline ran but yielded 0
  picks.** The indicator flattened ALL stocks' bars into one series
  (no per-stock grouping) and emitted time-series ARRAYS, but
  filter/rank/dedupe/persist consume PER-STOCK ROWS with scalar fields.
  Nothing produced that shape, so `filt_macd` matched 0 every run.
  Indicator now groups klines by stock, computes each on its own
  series, and emits one row per stock with the latest scalar values
  (`macd_hist`, `close`, …). (ac9e84f)
- **`recommendation_tags.tag` was varchar(16); template tags overflow
  it.** `macd_golden_cross` (17), `dragon_tiger_institutional` (26),
  `low_valuation_high_dividend` (27) all exceed 16, so persist failed
  with `Error 1406: Data too long for column 'tag'` the instant a pick
  was produced. Widened to varchar(64) (AutoMigrate expands the live
  column on restart). (9f0023e)

### Perf
- **K-line fetch is one ranged call, not one per day.**
  `manager.GetKLine` split the range into per-day windows and made one
  source call PER DAY (~60 calls for a 60-day window per stock) — which
  is exactly what trips free providers' rate limiters. Now coalesces
  cache-missing days into a single ranged call and buckets the result
  back per day (per-day cache preserved). Guard test asserts a 10-day
  window makes exactly one call. (283f652)

### Added (ops)
- **`scripts/akshare-sidecar.py`** — the production akshare data
  sidecar. The Go akshare adapter expects a `/kline`,`/quote`,
  `/fundamental` contract that aktools' generic API doesn't provide, so
  this thin wrapper calls akshare directly and reshapes the result. It
  uses the SINA backend (`stock_zh_a_daily`): the prod box is a Tencent
  Cloud IP that eastmoney's push endpoints block (RemoteDisconnected)
  even via akshare/curl_cffi, while sina works. Runs under systemd as
  `af-akshare-sidecar` on `127.0.0.1:18800`; prod datasource chain set
  to akshare-first. (9f0023e)
- **`scripts/mock-aktools.py`** — deterministic fake-data sidecar for
  local end-to-end testing (the free sources throttle a dev IP). Speaks
  the same adapter contract; test-only. (ac9e84f)

### Verified
- End to end on the production box against real market data: a
  `macd_golden_cross` run fetched 114 real sina klines for the 3
  configured stocks, computed per-stock indicators (茅台 close 1241.41),
  filtered/ranked/deduped, and persisted 3 recommendations
  (600519/000858/601318, tagged `macd_golden_cross`). Full backend test
  suite: 0 failures.

### Known gaps (deferred)
- Recommendation `entry_price_low/high` persist as 0 — the persist node
  doesn't yet copy the ranked row's `close` into the entry-price fields.
- `fundamental` data via the sina sidecar is not wired (returns empty);
  templates that need fundamentals (e.g. low-valuation) screen on what
  klines provide.

---

## [1.0.4] — 2026-06-22

PostgreSQL is now a real, verified driver — not just a config switch.
No behavior change for the live MySQL-backed prod. Tagged `af-v1.0.4`.

### Fixed
- **`TimeZone=Local` in the Postgres DSN crashed every connection.**
  `PostgresDSN()` reused the shared `db.loc` value, which defaults to
  `Local` — but Postgres only accepts IANA zone names, so AutoMigrate
  failed at boot with `invalid value for parameter "TimeZone": "Local"`.
  Now coerces `Local` / empty → `UTC`. MySQL/SQLite are unaffected (they
  read `loc` separately). (d4e27f8)
- **`type "longtext" does not exist` on Postgres AutoMigrate.** Ten large
  JSON/text fields (`dag_json` ×3, `default_params_json`, `intent_json`,
  `dag_diff_json`, `payload_in/out`, `config_json`, `node_snapshot`)
  carried a hardcoded `gorm:"type:longtext"` tag. `longtext` is
  MySQL-only — Postgres has no such type. Dropped the tags and now rely
  on GORM's native per-dialect mapping: an unsized `string` becomes
  `longtext` on MySQL, `text` on Postgres/SQLite. **Verified against
  `gorm.io/driver/{mysql,postgres,sqlite}@v1.6.0` source: zero schema
  change on MySQL** (the existing prod columns are already `longtext`).
  A convention note in `models.go` documents why the tags must not come
  back. (d4e27f8)

### Verified
- End-to-end Postgres path on the prod box via a throwaway `postgres:16`
  container + a test backend on `:19091`: AutoMigrate creates all tables,
  the 5 bundled templates seed, and the strategy write path round-trips.
  The live `af-backend` (MySQL) and `af-mysql` were never touched; all
  test resources were torn down afterward.

---

## [1.0.3] — 2026-06-20

Production-grade ops hardening. No app code change — fixes a runbook
correctness bug and installs the operational safety nets that were
documented but never deployed. Tagged `af-v1.0.3`.

### Fixed
- **`docs/OPERATIONS.md` had the wrong MySQL credentials in all 12
  backup/restore/diagnostic commands** — `-uaf -pafbackendpass af`. The
  live `af-mysql` container is `root` / `afbackendpass` / DB
  `astock_selector` (verified via `docker inspect`). Every command in
  §0/§3/§7/§9/§11/§14 would have failed when actually needed. All
  corrected to `-uroot -pafbackendpass astock_selector`. (8926fe0)

### Added (server-side, recorded in OPERATIONS.md §14)
- **Nightly DB backup is now actually running** — the cron was
  documented but never installed (zero disaster recovery). Installed
  `/etc/cron.d/af-backup` (root, 02:30 daily, 14-day retention) and
  verified a manual backup (31K `.sql` in `/home/ubuntu/af-backups/`).
- **Log rotation installed** — `/home/ubuntu/af/logs/*.log` grew
  unbounded. `/etc/logrotate.d/af-backend` (daily, keep 14,
  `copytruncate` so the zap process + systemd fds don't need a
  reopen). Dry-run validated.

### Verified (no change needed)
- systemd unit is production-grade: `Restart=on-failure` + `enabled`
  (auto-recovers from crash, survives reboot).
- Frontend v1.0.2 bundle live on prod; all endpoints 200 through
  nginx `:9091`.

---

## [1.0.2] — 2026-06-20

Frontend hotfix. No backend change. Tagged `af-v1.0.2`.

### Fixed
- **API base URL is now relative (`/api/v1`), fixing a CORS failure on
  every data page.** It previously defaulted to the absolute
  `http://localhost:8080/api/v1`, so the browser made a cross-origin
  request that the backend's CORS allow-list (only `:5173` / `:3000`)
  rejected — every templates/strategies/runs page showed "Network
  Error". The absolute default was also wrong in prod (the browser
  would resolve `localhost:8080` to the user's own machine, not the
  `:9090`-behind-nginx server). Relative base → same-origin requests →
  Vite proxies in dev, nginx proxies in prod, CORS never applies.
  `VITE_API_BASE_URL` still overrides for a genuinely remote API. (341c074)
- Dropped stale "skeleton" / `v0.1.0` labels — the app is a real v1
  release, not a skeleton. Header + sidebar now read `v1.0.1`/`ready`;
  the Dashboard's "§10 待实现" placeholder text stays (the dashboard
  IS still a deferred §10 feature). (341c074)

Verified live against the sqlite-backed stack: templates page renders
all 5 seeded templates. 164 frontend tests + typecheck + lint green.

---

## [1.0.1] — 2026-06-18

Deploy hotfix. No application code changed — `scripts/deploy.sh` now
actually works end-to-end against the production systemd+nginx
topology. Tagged `af-v1.0.1`.

### Fixed
- `scripts/deploy.sh` reconciled with prod reality: builds a
  linux/amd64 binary + frontend bundle locally, then ships via
  `scp`/`rsync` + `systemctl restart` to `/home/ubuntu/af` +
  `/var/www/af`. The previous version was a docker-compose deploy
  targeting `/opt/astock:8080` that never matched the live box. (8adad6e)
- Frontend dist + `config.example.yaml` now stage through `/tmp` and
  are `sudo`-moved into the `www-data`-owned web dir — a direct rsync
  hit permission-denied on the first real deploy. (02a6ef8)
- Deploy step order fixed so the `stop → install → start` sequence is
  the last atomic step with an inline rollback; a frontend-sync
  failure can no longer leave the backend stopped. (02a6ef8)
- SSH key path typo corrected (`~/ssheks` → `~/sshkeys`). (8adad6e)

Verified live: backend + frontend SPA + `/docs` all return 200 through
nginx `:9091`.

---

## [1.0.0] — 2026-06-18

First feature-complete release. Tagged `af-v1.0.0`. Full A1-A7 stack + §9 post-hoc performance engine + frontend (FE1 + FE2) + DX polish (P0/P1/P2) + license/compliance. **Deployed on `124.156.213.179`.**

### Added (post-0.1.0 release-prep)
- Dual licensing: `LICENSE` (Apache-2.0) + `LICENSE-AGPL-3.0` + `NOTICE` (Apache-2.0 §4(d) attribution). Every source file carries a `SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later` header, applied by the idempotent `scripts/add-spdx-headers.py`. (a08198f, d34aaee, 457d191)
- `CHANGELOG.md` (this file), `.devcontainer/` (VSCode one-click onboarding), `.githooks/pre-commit` (gofmt + vet + build + tsc + eslint on staged files, installed via `make setup`). (c2e9ba4)
- `make deploy` / `make deploy-dry-run` now wire to the fully-implemented `scripts/deploy.sh` (was a stub).

### Changed
- Backend test coverage raised 80.5% → **85.0%** to meet the spec §7.5 gate. New tests across `perf` (61→81%), `database` (66→79%), `executor` (74→84%). (ba045e8)
- README rewritten to match v1.0.0 reality (25-target Makefile, OpenAPI/Swagger, systemd+nginx deploy topology); coverage badge now reports the real measured number instead of a stale test count. (ad1b541, ba045e8)



### DX — DX Review / P0 + P1 fixes

#### Added (DX P1 — 0b261d1)
- `internal/httpresp` package is the single source of truth for the JSON envelope. All handlers now return `{"code","message","data"}` on success and `{"code","message","request_id"}` on error. The `request_id` ties back to the `X-Request-ID` response header and the `request_id` field in the server's zap log lines, so a client can grep the server logs by id.
- `GET /api/v1/healthz` is the canonical liveness path under the API group. `GET /healthz` is kept at the root for backward compatibility with load balancers and k8s probes.
- `GET /api/v1/openapi.json` serves the OpenAPI 3.0 spec (25 endpoints, 20 reusable schemas). The spec is built at runtime from Go data structures, so a new endpoint = a struct literal in `internal/openapi/spec.go` — no hand-edited JSON to drift.
- `GET /docs` serves Swagger UI. Loads the spec from the same host.
- `docs/OPERATIONS.md` — 13-section runbook covering health checks, DB down, service crash, frontend 404s, SSE buffering, stuck jobs, cron, perf, dev loop, full restart, monitoring checklist, glossary.

#### Fixed (DX P0 — bceecf3)
- `.github/workflows/ci.yml` was running Go 1.22/1.23 against a `go.mod` that requires 1.25. CI was failing on every push. Pinned to Go 1.25; added a version-check job that fails fast on go.mod drift.
- `Makefile` was referenced in `make help` output but did not exist. Rewrote as 25 self-contained targets (`tidy/build/test/lint/run-*/dev-*/migrate/smoke/check/deploy/clean`).
- `backend/Dockerfile` was on Go 1.22. Bumped to Go 1.25; renamed the artifact from `af-server` to `af-backend`; added `-trimpath -ldflags="-s -w"` for smaller images.
- CI was running a `make smoke` job that called scripts that did not exist. Removed.
- README §11 and §5 referenced a `scripts/deploy.sh` and `docker-compose.yml` flow that did not exist. Those tools now exist; the docs no longer lie.

### Performance — §9 post-hoc performance engine (4e814ab + b1797d0 + 78fe7ff)

#### Added
- `GET /api/v1/perf/recommendations/:id` — latest T+N return + max-drawdown snapshot for one recommendation.
- `GET /api/v1/perf/recommendations/:id/history` — all snapshots for a recommendation (one per recalculation).
- `POST /api/v1/perf/calculate` — recompute snapshots for a single recommendation OR a date range.
- `GET /api/v1/perf/aggregations?group_by=strategy|session_tag|stock&from=...&to=...` — group-by win-rate, avg T+N return, avg drawdown.
- §9 cron — nightly recompute at 02:00 in `config.Cron.Timezone` (default `Asia/Shanghai`).
- Startup backfill (one-shot at boot) for any recommendations missing a snapshot.

#### Changed
- Aggregate SELECT narrowed to the columns actually rendered. Drops the temp-table sort on `performance_snapshots` from ~6s to <100ms on the production dataset. (78fe7ff)
- Win-rate semantics documented in `internal/perf/service.go` — "win" = `t5_return > 0` (was previously ambiguous between "ever went positive intraday" and "positive at close"). (78fe7ff)

#### Perf
- `perf.startup_backfill_timeout` is now configurable; was hardcoded to 30s. Default is still 30s. (b1797d0)

### Frontend — FE1 (DAG editor) + FE2 (run history + SSE) + M4 polish

#### Added (FE1 — d24542a)
- ReactFlow-based DAG editor with drag-and-drop, validation, node CRUD.
- Test infrastructure: 164 unit tests (Vitest) + 13 e2e tests (Playwright) on the FE.
- Canvas hotkeys: `Cmd+Z` undo, `Cmd+Shift+Z` redo, `Delete` to remove, `Cmd+D` duplicate, `Cmd+S` save.

#### Added (FE2 — 7250eae)
- Run history page: filterable list of past runs (strategy/status/date).
- Run detail page with per-node log stream and event timeline.
- SSE client with Last-Event-ID resume — reconnect picks up where the stream dropped, doesn't replay from the start.
- TriggerModal (manual run trigger) uses the standard toast pattern (no double-toast).

#### Fixed (post-M4 — 5e1218c, 3a3f901)
- Killed the double-toast regression on save errors: 4-layer callback hell collapsed into a single `notifyError` call. (5e1218c)
- SSE listener stability: re-subscribed on every reconnect (was a one-shot subscription that leaked after a tab restore). (3a3f901)
- RunDetail hotkeys: `j`/`k` to step through log lines, `g`/`G` to jump to top/bottom. (12bb179)
- LogStreamViewer "paused" state visible in the chrome (was previously only inferable from "no new lines"). (12bb179)

### Backend — AF v1 full stack (1184f78)

#### Added
- A1 (config) + A2 (calendar) + A3 (datasource Tushare) + A4 (notify channels).
- A5 (cron scheduler) + A6 (orchestrator DAG engine) + A7 (executor + node registry).
- Strategy CRUD with immutable DAG versioning (`strategy_versions`).
- Trial-run endpoint: dry-run a DAG end-to-end with no DB writes and no notifications.
- Built-in strategy templates (`/api/v1/strategies/templates`) with one-click instantiation.
- 738 backend unit tests at the v1 cut-line.

### Polish — pre-v1 incremental (42b77d2 + b08484f + 71eee8e + 9b36166)

#### Added
- Dashboard stats: today's runs, success rate, last triggered strategy, recent errors.
- Pagination bounds: `page_size` is capped at 200 server-side (was unbounded — DoS risk).
- CORS allow-list for `localhost:5173` (Vite dev) and `localhost:3000`.
- Pre-delete checks for strategy / run: refuse to delete a strategy that has running children.

#### Changed
- Frontend redesigned with an Apple-style aesthetic (typography scale, spacing, motion).
- `tag` filter on the strategies list: server now filters by tag membership (was a substring match on the CSV).

---

## [0.0.1] — 2026-05-15 (initial scaffold)

The first commit. Repo + Makefile + Go module + GORM schema for a single `strategies` table. No HTTP layer, no frontend, no tests. Everything after this is built on top of that foundation.
