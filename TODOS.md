# TODOS

> Maintenance file for outstanding work. Items are grouped by component,
> then priority (P0 critical → P4 nice-to-have). Completed items move to
> the bottom section with the shipping version.
>
> **Status (2026-06-25):** v1.3.4 shipped. The fe1 / fe2 / perf sections
> below are all delivered — kept for history. The v1.1-deferred big
> features (§10/§11/§12/§14.9) all shipped across v1.2.x–v1.3.x; checkboxes
> below corrected. Remaining open work is the small/optional items.

## v1.1 deferred (from /autoplan v1 cut-line) — now mostly shipped

> These were explicitly deferred when cutting v1.0.0. The four big ones
> shipped in v1.2.x–v1.3.x; only the optional SSE test remains.

- [x] **§10 Visualization dashboard** — shipped v1.2.0 (positions / recommendations / win-rate heatmap / review entry).
- [x] **§11 AI assistant** — shipped v1.2.0 (mock) + v1.2.1 (real LLM via runtime settings, DeepSeek verified). OpenAI-compatible, schema-validated intent, LLM off the critical path.
- [x] **§14.9-14.11 Auto review summaries** — shipped v1.2.0 (daily/weekly LLM-summarized review scheduler).
- [x] **§12 Multi-user / auth** — shipped v1.3.0 (JWT login gate, bcrypt, middleware, frontend login page; data model + token claims multi-user-ready). Default off; verified e2e.
- [ ] **fe2 M4 (optional)** — backend SSE integration test (handler_test.go SSE frame parse). The two sibling items (detail-page hotkeys, paused header visual) shipped in M4 polish (12bb179).

## datasource

- [x] **P2 — eastmoney push2his K-line endpoint unreachable (EOF), local + prod.**
  Shipped on main (`fix(datasource): resolve K-line blocker...`, 2026-08-31),
  **verified live on prod**. Two-part fix:
  - **Sidecar dual upstream:** `scripts/akshare-sidecar.py` now normalizes
    both sina and tencent kline DataFrames (`_rows_from_df`), adds
    `fetch_klines_tx` (`ak.stock_zh_a_hist_tx` — fully independent
    upstream, `hasattr`-guarded for old akshare builds), and
    `fetch_klines` falls through sina→tencent instead of raising. The
    akshare adapter was the only enabled source in prod
    (`config.docker.yaml`: eastmoney/sina both `enabled:false`), so this
    is the fix that actually unblocks `ind_vr`.
  - **Eastmoney browser fingerprint:** UA `af-backend/0.1` → real Chrome
    UA + `Accept-Language`/`Connection` headers + lazy cookie warm-up
    (`sync.Once`, probe kline host root after first success). Dormant in
    prod while eastmoney is `enabled:false`; takes effect if re-enabled.
  - **Prod verification:** sidecar rebuilt; `/kline?code=600519` returns
    real candles; `fetch_klines_tx` independently returns 6 rows;
    simulated sina failure → tencent takeover confirmed end-to-end.
  - Payload-shape bug in the same path (klines array-vs-string) was
    fixed earlier by /qa (ISSUE-002, a1bff63).

- [x] **P2 — Real news source for the sidecar.** Shipped in
  `af-v1.3.2`. The akshare sidecar `/news` now serves per-stock news via
  `akshare.stock_news_em` (eastmoney's news endpoint IS reachable from
  the prod IP — only its quote/push endpoints are blocked; confirmed by
  probing). Maps to the Go adapter's `{title, content, url,
  published_at}` shape; converts akshare's CST timestamp to RFC3339
  `+08:00` (Go's `time.Time` requires it). Verified e2e on prod:
  news→filter `matched:4, dropped:6` on real 茅台 news. Sidecar-only —
  no Go change.
  - **Files:** `scripts/akshare-sidecar.py` (`fetch_news` + `_to_rfc3339`).
  - **Limitation kept:** `stock_news_em` is per-stock, not batchable —
    news screens over a large universe are still N calls; keep to small
    explicit `stock_codes`.

## executor / nodes

- [x] **P3 — Filter multi-keyword OR.** Shipped in `af-v1.3.3`. Added a
  `contains_any` op to the `filter` node: true when the field contains
  ANY of several substrings. Value is a JSON array (`["分红","重组"]`) or
  a comma-separated string. Verified e2e on prod: `新闻关键词监控` with
  `contains_any ["分红","回购","增持"]` → `matched:5` across multiple
  keywords (茅台 分红 + 五粮液 增持).
  - **Files:** `backend/internal/executor/nodes/filter.go` (`matchExpr`
    + `toStringList`), `frontend/src/components/canvas/NodeConfigPanel.tsx`.
  - **Still open (separate, larger):** a general union/merge node to OR
    two parallel branches.

## perf (post-merge cleanups from PR #1 review)

> All delivered — see "## Completed" below (M1/M2/M3).

## fe1 — FE1 M4 + M4-EXT (CEO Review 2026-06-17) — DELIVERED in v1.0.0

> M1-M3 已交付,M4+M4-EXT 是 FE1 收尾。基础件(Toast/ConfirmDialog/ErrorBoundary)需从零建,覆盖 7 处 bug + 5 项扩展。
> 详细计划:见 `FE1_PLAN.md`(已由 `/plan-ceo-review` 更新)。


### M4 基础件(CC ~1 天)

- [x] **Build ToastProvider** — 安装 react-hot-toast,挂载 `<App>` 根,`index.css` 已有 `.animate-toast-in` 样式
  - **Files:** `frontend/src/App.tsx`, `frontend/src/index.css`(已有)
- [x] **Build ErrorBoundary** — class 组件包裹 `<RouterProvider>`,fallback UI 含错误原因 + 刷新按钮
  - **Files:** `frontend/src/components/shared/ErrorBoundary.tsx`(新建), `frontend/src/main.tsx`
- [x] **Build ConfirmDialog + useConfirm hook** — `role="dialog"`,Esc 关闭,焦点陷阱,danger 态样式,默认焦点在取消
  - **Files:** `frontend/src/components/shared/ConfirmDialog.tsx`(新建), `frontend/src/hooks/useConfirm.tsx`(新建)
- [x] **Build notifyError 工具** — `getErrorMessage(err, fallback)` → toast,接至 `api/client.ts` axios 拦截器
  - **Files:** `frontend/src/lib/notify.tsx`(新建), `frontend/src/api/client.ts`
- [x] **Build 三态组件** — 抽取 `StrategiesPage` 已有模式为 `<LoadingState/>` `<EmptyState/>` `<ErrorState/>`
  - **Files:** `frontend/src/components/shared/LoadingState.tsx`(新建), `EmptyState.tsx`, `ErrorState.tsx`

### M4 bug 修复(7 处)

- [x] Fix `NodeConfigPanel.tsx:47` → `useConfirm()` 替换 `confirm()`
- [x] Fix `StrategiesPage.tsx:95,105,123` → `useConfirm()` + `toast.error()` 替换 `confirm()`/`alert()`
- [x] Fix `RunDetailPage.tsx:78` → `useConfirm()` 替换 `confirm()`
- [x] Fix `RecommendationsPage.tsx:83` → `toast.warning()` 替换 `alert()`
- [x] Fix `FilterForm.tsx:250` → inline 红色 "JSON 格式错误" 替换空 `catch {}`
- [x] Fix `StrategyEditorPage.tsx` save → `toast.error()` 替换 `console.error`(Toolbar 已显示;此处仅保险,无静默)
- [x] Fix `canvasStore.ts` → 所有 setter 已用 immutable update(零重构,已是 immutable)

### M4-EXT 扩展(5 项,全部采纳,CC ~1 天)

- [x] **E1 Undo toast** — 删除/归档后弹 toast + "撤销"按钮,5 秒内可撤,前端缓存浅拷贝
- [x] **E3 useConfirm Promise hook** — `await confirm({...})` 语义,调用点用 then/await 替代回调
- [x] **E4 键盘快捷键** — Esc 关 dialog,Enter 确认(danger 态不触发),画布 Del 删节点,Ctrl+S 保存
  - **Files:** `ConfirmDialog.tsx`, `Canvas.tsx`, `frontend/src/hooks/useCanvasHotkeys.ts`(新建)
- [x] **E5 空态引导** — 空方案列表显示"创建第一个方案 →"引导卡,链接模板库
  - **Files:** `StrategiesPage.tsx`

### M4 单元测试

- [x] `ConfirmDialog.test.tsx` — 10 tests
- [x] `ErrorBoundary.test.tsx` — 4 tests
- [x] `FilterForm.test.tsx` — 6 tests
- [x] `notify.test.ts` — 13 tests
- [x] `useCanvasHotkeys.test.tsx` — 10 tests
- [x] `client.test.ts` — 防御性测试(拦截器永不 toast)

### M4 后续修复(eng review 推动)

- [x] P1 — `client.ts` 拦截器移除 toast(消除双 toast)
- [x] P1 — `StrategiesPage` undo 流程重构(static imports + try/await)
- [x] P2 — `useCanvasHotkeys` 改 ref 存 onSave(listener 稳定)
- [x] P2 — `ConfirmDialog` 改 `useLayoutEffect` 焦点 + ref 存回调
- [x] P2 — `undo-delete.spec.ts` e2e 覆盖 E1 undo 流程

## fe2 — FE2 运行历史 + SSE 实时日志 (2026-06-17)

> 全栈已就位(M1-M4 范围内已交付,本计划聚焦补测试 + 质量打磨)。
> 详细计划:见 `FE2_PLAN.md`。

### M1 单测:runs/ 组件

- [x] `RunStatusBadge.test.tsx` — 9 tests(6 status × 文案/颜色 + 3 边角)
- [x] `RunTimeline.test.tsx` — 6 tests(空态/排序/错误块/时长格式/时钟偏移)

### M2 单测:LogStreamViewer + pages

- [x] `LogStreamViewer.test.tsx` — 11 tests(EventSource mock/2000 上限/暂停/自动滚/重连/错误/卸载清理/malformed JSON)
- [x] `RunHistoryPage.test.tsx` — 8 tests(列表/筛选/分页/触发模态框/成功跳转/失败错误)
- [x] `RunDetailPage.test.tsx` — 6 tests(加载/错误/重试 confirm/取消/重试失败)

### M3 e2e + TriggerModal 改造

- [x] `e2e/runs-list.spec.ts` — 列表 + 跳转 + 空态
- [x] `e2e/run-detail.spec.ts` — 详情 + 重试 confirm/cancel
- [x] `e2e/run-manual-trigger.spec.ts` — 手工触发模态框 + 跳转
- [x] TriggerModal 错误改 toast 反馈(移除 inline error div,call `notifyError`)

### M4 打磨(可选)

- [ ] 详情页 Esc 回列表 / Ctrl+R 重试 键盘快捷键
- [ ] LogStreamViewer 暂停时 header 视觉提示
- [ ] 后端 SSE 集成测试(handler_test.go SSE frame 解析)

## Completed

## Completed

- [x] **M1** Make startup-backfill timeout configurable
  - **Completed:** post-PR #1 follow-up
  - **What changed:** `PerfConfig.StartupBackfillTimeout` (default `5m`, env `PERF_STARTUP_BACKFILL_TIMEOUT`, yaml `perf.startup_backfill_timeout`); `cmd/server/main.go` now reads `d.Cfg.StartupBackfillTimeout` and falls back to 5m when zero/negative. Goroutine logs the timeout alongside processed/errored.
  - **Files:** `backend/internal/config/config.go`, `backend/cmd/server/main.go`, `backend/configs/config.example.yaml`

- [x] **M2** Document `singleRecWinRate` semantics at the handler boundary
  - **Completed:** post-PR #1 follow-up
  - **What changed:** `aggregationRow` struct in `handler.go` now carries a doc comment that names the nil-when-undefined contract and per-field `// nil when...` tags on each `*float64` JSON field, plus a one-line note in `aggregate()`'s docstring linking the rule to the source-snapshot semantics. Consumers of `/api/v1/perf/aggregations` can now distinguish a JSON `null` (no T+N data) from `0.0` (zero return) without reading `service.go`. No code logic change.
  - **Files:** `backend/internal/perf/handler.go`

- [x] **M3** Narrow `aggregate()` SELECT to only the fields the aggregator reads
  - **Completed:** post-PR #1 follow-up
  - **What changed:** `aggregate()`'s `Select(...)` now projects only 4 columns from `performance_snapshots` (`t1_return` / `t3_return` / `t5_return` / `max_drawdown`) plus 3 rec-level aliases (`r.id` / `r.strategy_code` / `r.stock_code`). The flat `joined` struct (Scan target, all primitive fields) avoids GORM v1.31.1's relation inference on nested structs. The narrow `aggregateCol` type carries only the 4 numeric fields consumed by `groupAccum.add`. Aliasing is documented in a comment near the SELECT — `ps.id` is the snapshot PK (not selected), `r.id AS rec_id` is the rec PK used for dedupe. At 1M rows the wire footprint and Scan struct-copy pass are both ~70% smaller.
  - **Files:** `backend/internal/perf/handler.go`, `backend/internal/perf/aggregate_test.go` (new)

