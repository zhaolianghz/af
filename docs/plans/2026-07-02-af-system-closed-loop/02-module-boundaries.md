# §B 模块边界与依赖

> 调研日期:2026-07-02 · 主文档:[`../2026-07-02-af-system-closed-loop-design.md`](../2026-07-02-af-system-closed-loop-design.md)

---

## B-1 现状:三层模块划分

按 `cmd/server/main.go` 装配顺序,后端模块分三类:

### B-1.1 核心引擎 5 个(主链路依赖)

| 模块 | 职责单一吗? | 入口函数 | 关键文件 |
|---|---|---|---|
| `internal/orchestrator` | ✅ 纯 DAG 引擎 | `NewExecutor`(`executor.go:33`) | `executor.go`(536 行) + `dag.go` + `node.go` + `registry.go` + `eventbus.go` |
| `internal/executor` | ⚠️ 4 件事(run 生命周期 / 持久化 / dashboard 聚合 / 推荐查询) | `NewExecutor`(`executor.go:73`) | `executor.go`(667 行,见 B-3.2) |
| `internal/datasource` | ✅ 多源 + 路由 + 缓存 + 健康 | `NewManager`(`main.go:472`) | `manager.go` + `breaker.go` + `cache.go` + `redis_cache.go` + `source/{eastmoney,sina,akshare}/` |
| `internal/notify` | ⚠️ registry 与主包重叠 | `registry.New`(`main.go:110`) | `manager.go` + `retry.go` + `circuit.go` + `template.go` + `channel/*.go` + `registry/` |
| `internal/calendar` | ✅ 交易日历 + 交易时段 | `calendar.NewService`(`main.go:622,751`) | `trading_day.go` |

注意:**`orchestrator` 和 `executor` 各自都有一个 `Executor` 结构体**——前者是纯 DAG 引擎,后者是 run 生命周期。这是已知的"认知陷阱"(见 B-3.1)。

### B-1.2 横向能力 5 个(v1.2 / v1.3 后期交付)

| 模块 | 触发场景 | 入口函数 | 依赖 |
|---|---|---|---|
| `internal/ai` | 用户在 AI 助手抽屉里跟 LLM 对话改策略 | `ai.NewService(db, llm, strat)`(`main.go:231`) | 依赖 `orchestrator.StrategyService`(改策略)+ `ai.Provider`(共享 LLM 客户端)+ DB |
| `internal/auth` | `cfg.Auth.Enabled = true` 时拦截 `/api/v1/*` | `auth.NewService(db, secret, ttl)` + `auth.NewHandler`(`main.go:283-302`) | 依赖 DB(必须);secret 走 `cfg.Auth.JWTSecret`,缺则随机生成(ephemeral) |
| `internal/settings` | 用户在 `/settings` 改 LLM 供应商 / 加 fallback chain | `settings.NewService(db, llmProvider, timeout)`(`main.go:222`) | 依赖 `ai.Provider` + `ai.Client`,**热替换共享同一 Provider** |
| `internal/review` | `cfg.Review.Enabled = true` 时每日/每周复盘 | `review.NewService(db, llmProvider)` + `review.NewScheduler`(`main.go:243-257`) | 共享 LLM provider(nil-safe → 数据摘要) |
| `internal/position` | 大屏持仓卡片 | `position.NewService(db, quote)`(`main.go:199`) | 依赖 `datasource.Manager`(作 `QuoteSource`)+ DB |

注意:**`settings`、`review` 都持有一份 `ai.Provider`(指针传递),热替换一处全局生效**。

### B-1.3 基础设施 3 个(横向被依赖)

| 模块 | 职责 | 入口 |
|---|---|---|
| `internal/httpresp` | JSON envelope + request_id 单源真理 | `httpresp.OK / Err / Created` |
| `internal/apperr` | `Code` + `HTTPStatus` + `Wrap / NotFound / InvalidArg / Unavailable / Conflict` | `apperr.CodeXxx` 常量 |
| `internal/database` | GORM open / ping / migrate | `database.Open / Ping / Migrate` |

---

## B-2 闭环是否成立

### B-2.1 职责边界评估

| 评估项 | 是否成立 | 证据 |
|---|---|---|
| 主链路装配的"唯一入口"是 `cmd/server/main.go` | ✅ | 全部 `init*` 函数都在 main.go 顺序调用,router 把 10 个 handler 注入链(`main.go:308-329`) |
| 横向能力(ai/auth/settings/review/position)之间无依赖 | ✅ | grep 验证:`internal/auth` 不 import `internal/ai`,`internal/position` 不 import `internal/review`,等等 |
| 核心引擎 5 个各自只暴露一个 `New*` 装配函数 | ✅ | `NewExecutor` / `NewManager` / `registry.New` / `calendar.NewService` 都返回单一类型 |
| `notify` 与 `notify/registry` 边界清晰 | ⚠️ | 见 B-3.3 |

### B-2.2 横向能力的"启停开关"是否清晰

| 能力 | 配置开关 | 行为 |
|---|---|---|
| ai | `cfg.AI.Enabled` + `cfg.AI.Provider` | 默认 openai(需 base_url + api_key + model),缺则降级 mock(`main.go:207-217`) |
| auth | `cfg.Auth.Enabled` | 关闭时整个 `/api/v1/*` 不拦截(开发默认,见 `main.go:305`) |
| settings | 无显式开关 | 始终启用,因为是 ai 的运行时依赖 |
| review | `cfg.Review.Enabled` | 关闭时 `reviewStop` 为 nil,scheduler 不启(`main.go:242-261`) |
| position | 无显式开关 | 始终启用(dashboard 依赖) |

### B-2.3 依赖方向评估

```
router.New(...)
   ├─ strategyHandler   → orchestrator.StrategyService → DB
   ├─ trialHandler      → orchestrator.StrategyService + orchestrator.Executor
   ├─ executorRegistrar → executor.Executor → orchestrator.Executor + datasource.Manager + notify.Manager
   ├─ perfRoutes        → perf.Service → datasource.Manager + calendar.Service + DB
   ├─ positionRoutes    → position.Service → datasource.Manager(QuoteSource) + DB
   ├─ aiRoutes          → ai.Service → orchestrator.StrategyService + ai.Provider + DB
   ├─ reviewRoutes      → review.Service → ai.Provider + DB
   ├─ settingsRoutes    → settings.Service → ai.Provider + DB
   ├─ authPublicRoutes  → auth.Service → DB
   ├─ authProtectedRoutes → auth.Service → DB
   └─ middleware (auth, if enabled)
```

依赖图无环(`ai.Service` 与 `settings.Service` 共享 `ai.Provider`,但不互引)。

---

## B-3 断点 / 风险

### B-3.1 `orchestrator.Executor` vs `executor.Executor` 同名

两个不同的结构体,职责完全不同:

- `orchestrator.Executor` = 纯 DAG 引擎,Kahn + 协程池,无外部依赖(`orchestrator/executor.go:16`)
- `executor.Executor` = run 生命周期 + 持久化 + 推荐查询,内含前者的指针(`executor/executor.go:42`)

代码中已用别名规避:`import orchestratorpkg` / `import executorpkg`(`main.go:42-43`)。但同名仍然在新成员加入时是高频踩坑点。

### B-3.2 `executor.Executor` 4 件事集中

`internal/executor/executor.go`(~667 行)同时承担:

1. **Run 生命周期**:`Execute`(L113-215)+ `Retry`(L295-319)+ `CreateRunRow`(L424-437)
2. **持久化**:`persistLogs`(L325-346)+ `publishRunCompleted`(L348-369)
3. **Dashboard 聚合**:`DashboardStats`(L576-636)
4. **推荐查询**:`ListRecommendations`(L475-524)+ `enrichRecNames`(L536-548)

四个职责各自有不同的测试 mock 入口,但**测试目标不独立**——任何新增字段都需要在这 4 个方法之间协调。

### B-3.3 `notify.registry` 与 `notify` 主包重叠

```
internal/notify/
├── channel.go      // Manager 接口 + Channel 接口
├── manager.go      // Manager 实现(routing + fallback)
├── registry/       // 装配入口 + breaker map + Notify Test handler
├── retry.go        // 指数退避
├── circuit.go      // 熔断
├── template.go     // 消息模板
└── channel/        // feishu/dingtalk/wecom 三个实现
```

`registry` 子包其实**就是** Notify 的"装配 + 健康查询"两个职责,把它从 `notify` 主包抽出来的理由是"`channel/*` 路径下不要耦合 registry"——但当前 `channel/*.go` 反过来 import registry(`notify/channel/feishu/feishu.go` import `notify/registry`?需要核对),边界其实有反复。

### B-3.4 `internal/strategy/` 空目录

`backend/internal/strategy/` 是空目录(早先可能用于策略 CRUD,已合并到 `orchestrator/`)。`git ls-files backend/internal/strategy` 应为空。属于"幽灵包"。

### B-3.5 `perf` 与 `executor` 各持有一份 `calendar.Service`

`cmd/server/main.go:622`(给 executor)+ `:751`(给 perf)两次调 `calendar.NewService(d.CalCfg, d.DB)`。两边配置相同,**但**也意味着:如果未来 perf 需要不同 kalender 配置(比如"复盘只看交易日,perf 看完整日历"),代码上需要拆分。

### B-3.6 `settings` 与 `ai` 共享 `ai.Provider`,启动顺序耦合

```
main.go:219  llmProvider := aipkg.NewProvider(initLLM)      // 1. 创建 provider
main.go:222  settingsSvc := settings.NewService(..., llmProvider, ...) // 2. settings 持 provider
main.go:226  settingsSvc.LoadChainOnStart(...)                // 3. settings 加载链 → 可能 swap provider
main.go:231  aiSvc := aipkg.NewService(db, llmProvider, ...)  // 4. ai 也持 provider
```

如果 step 3 swap 了 provider,step 4 创建的 ai.Service 仍然持有旧 provider 吗?—— 取决于 NewService 是值传递还是指针。当前 `ai.Service` 字段是 `*ai.Provider`(`ai/service.go` 推测),**应该共享同一指针**。需要 grep 验证。

### B-3.7 横切关注点(observability)的归属未明确

`apperr` / `httpresp` / `middleware` / `logger` 当前是在"基础设施"层,**没有**统一的 "tracing / metrics" 模块。`OPERATIONS.md §12` 列了"未来 Prometheus/Grafana",但还没人认领。

---

## B-4 重排候选(外部参考)

| # | 候选 | 触发条件(假设) | 不做的代价 |
|---|---|---|---|
| 1 | 重命名 `executor.Executor` 为 `executor.Runner` 或 `executor.RunService` | 新成员频繁混淆两个 Executor | 长期认知负担 |
| 2 | 把 `executor` 拆为 `executor/{runner, persist, dashboard, query}` 子包 | `executor.go` 单文件继续膨胀(>1000 行) | 当前测试目标需要重构 |
| 3 | 清理空的 `internal/strategy/` 目录 | 任何 `git grep strategy/` 出现歧义 | "幽灵包"留作未来复用位 |
| 4 | `notify.registry` 扁平化到 `notify` 主包 | `notify/registry/` 与 `notify` 互相 import | 现状能跑;但 import 列表会简化 |
| 5 | 统一 `calendar.Service` 为单例(perf 和 executor 共享) | 任何"perf 与 executor 日历不一致"的报告 | 现状两边配置相同,合并省一行 |
| 6 | 给 `settings.Service` 与 `ai.Service` 的 Provider 共享显式文档化(注释:同一指针) | 出现"启动顺序导致 Provider 不一致"的 bug | 现状靠"指针 / 值"传播 |
| 7 | 把 `position` 与 `auth` 提到"主链路必备"(必须 enabled) | 任何"生产部署忘了开启"导致基础功能缺失 | 现状是开发默认开,但 prod 关闭影响 |
| 8 | 引入 `internal/observability/` 子包(tracing + metrics + structured error) | 任何新外部依赖接入 / ops 团队要求 | 现状在 `OPERATIONS §12` 描述但没人 commit |

---

## B-5 引用

- 主文档:§B 在总览中的位置 + 全局拓扑图
- A-§:产品主链路如何经由这些模块跑通
- C-§:数据流(模块间共享的 8 张表)
- D-§:可靠性(breaker/circuit/retry 在模块边界上的归属)
- E-§:演进(模块边界随版本演进的变化)
