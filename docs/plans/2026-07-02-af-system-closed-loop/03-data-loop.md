# §C 数据流闭环

> 调研日期:2026-07-02 · 主文档:[`../2026-07-02-af-system-closed-loop-design.md`](../2026-07-02-af-system-closed-loop-design.md)

---

## C-1 现状:数据从哪来、到哪去

AF Selector 的数据有 20 个 GORM 实体(在 `internal/model/`),但**核心主流转**只过其中 13 张表。本节按"数据进入 → 加工 → 沉淀"的顺序铺开。

### C-1.1 数据进入(从外部世界拉入)

| 来源 | 类型 | 入口 | 目的地 |
|---|---|---|---|
| 公开行情 | eastmoney / sina / akshare 三路 | `datasource.Manager.GetQuote / FetchKLine` | Redis/memory cache + 上层 consumer(节点) |
| 交易日历 | akshare 推送 + 本地 `trading_calendar` 表 | `calendar.Service.IsTradingDay` | scheduler session 守护 |
| LLM 调用 | OpenAI 兼容协议(可换 Ollama/DeepSeek) | `ai.Provider.Chat` | 节点解释 / AI 助手 / 复盘 summary |
| 用户输入 | HTTP POST / PUT / DELETE | 10 个 handler 模块 | `strategies / runs / recommendations` 等表 |

### C-1.2 数据加工(DAG 内节点)

每个策略对应一个 DAG(`strategies.current_version_dag`),DAG 节点类型在 `internal/executor/nodes/register.go` 注册,8 个内置节点:

| 节点类型 | subtype | 数据消费 | 数据产出 |
|---|---|---|---|
| `data_source` | quote / kline | datasource.Manager | `[]stock_quote` / `[]kline_row` |
| `indicator` | ma / macd / kdj / boll / 量比 / 换手率 | kline_row + 上游节点的 quote | `[]indicator_value` |
| `filter` | 含 `==` / `<` / `contains_any` 等 op | 上游产物 | 过滤后子集 |
| `rank` | sort + limit | 上游 list | topN 列表 |
| `dedupe` | by field | 上游 list | 去重后列表 |
| `session_tag` | MORNING/AFTERNOON/NO_POST/REVIEW + source | 当前时间 + run 时段 | `[]recommendation_tag`(待 §C-1.3 中 persist 落库) |
| `persist` | — | 上游 list(已 rank + dedupe) | `recommendation` 行 |
| `notify` | 飞书/钉钉/企微 | 上游 list | notify.Manager.Send |

**当前 8 个节点,但 proposal 原承诺"≥8 种",spec 落地一致**。

### C-1.3 数据沉淀(写 DB)

写库路径 9 条:

| 路径 | 写入模块 | 表 | 触发 |
|---|---|---|---|
| 1 | `StrategyService.Create/Update` | `strategies` + `strategy_versions` | 用户创建/编辑策略 |
| 2 | `Executor.Execute` | `runs` + `run_logs` | 触发一次 run |
| 3 | `persist` 节点 | `recommendation` | DAG 跑通 |
| 4 | `session_tag` 节点 | `recommendation_tag` | DAG 跑通(有标签时) |
| 5 | `perf.Calculator` + `Backfill` | `performance_snapshot` | perf 定时(02:00)+ startup backfill + 手动 `/perf/calculate` |
| 6 | `datasource.Manager.Heartbeat` | `datasource_health` | 每 60s |
| 7 | `datasource.Manager.Switch` | `provider_switch_event` | 切换发生时 |
| 8 | `review.Service.GenerateReport` | `review_report` | 每日 15:30 / 每周日 20:00 + 手动触发 |
| 9 | `position.Service.Update` | `positions` | 用户手动建仓 + 自动同步 |
| 10 | `auth.Service.Bootstrap` + `Login` | `users` + `roles` | 首次启动 + 登录 |
| 11 | `ai.Service.ApplyIntent` | `ai_audits` | 用户确认 AI 改动 |
| 12 | `settings.Service.Update` | `llm_settings` + `llm_providers` | 用户改 LLM 配置 |

### C-1.4 数据消费(读 DB)

读库路径 6 类:

| 路径 | 读取模块 | 终点 |
|---|---|---|
| `/api/v1/strategies/*` | `orchestrator.StrategyHandler` | 前端画布 |
| `/api/v1/runs/*` | `executor.Handler` | 前端运行历史 + 实时日志 |
| `/api/v1/recommendations` | `executor` `ListRecommendations` | 前端推荐列表 |
| `/api/v1/perf/*` | `perf.Handler` | 大屏胜率排行 + 复盘页 |
| `/api/v1/positions/*` + `/dashboard` | `position.Handler` + `executor.DashboardStats` | 大屏持仓卡片 |
| `/api/v1/reviews/*` | `review.Handler` | 复盘报告页 |

---

## C-2 闭环是否成立

按 4 个问题拷问(主文档 §1.3):

| 问题 | 答案 | 证据 |
|---|---|---|
| **起点** | 公开行情(经 datasource) + 用户输入(HTTP POST) + cron 触发 + 内部 cron scheduler(`perf` + `review`) | `cmd/server/main.go` 中 cron start 与 cron stop 完整闭环 |
| **终点** | 13 张核心表 + Redis cache + LLM 回答 + HTTP 响应 | `cmd/server/main.go:308-329` 的 router 注入 10 个 handler |
| **核对** | run_log(节点级)+ recommend_id(全局)+ recommendation_id(在 snapshot 中)+ review_report.time_window(时间窗) | 每条写入都有对应"读"路径验证(不代表真正一致) |
| **接续** | GORM AutoMigrate 启动重建 + perf startup backfill + scheduler LoadFromDB(orchestrator 启动时重载) | `cmd/server/main.go:415`(migrate)+ `:660`(scheduler.LoadFromDB)+ `:780`(perf startup backfill) |

**结论:主数据流闭环成立**;但**部分写入路径缺乏约束**(见 C-3)。

---

## C-3 断点 / 风险

### C-3.1 跨节点无显式事务(persist + session_tag 不同事务)

```
executor.Executor.Execute
  └─ orchestrator.Executor.Execute
       └─ DAG walk:
            ├─ persist 节点     →  DB INSERT recommendation       ← 单事务
            ├─ session_tag 节点 →  DB INSERT recommendation_tag  ← 单事务
            └─ notify 节点      →  notify.Manager.Send           ← 非 DB
```

`persist` 节点与 `session_tag` 节点**不在同一个 DB 事务**。如果:

- `persist` 成功,`session_tag` 失败(网络抖动 / DB timeout) → 主表有记录,标签缺失
- `session_tag` 成功,`persist` 失败 → 标签孤儿

`feature.executor_test.go` 当前没有"半成功"的状态断言(因为 design.md §7 明确"per-node 单事务"是正确的设计取舍)。问题在于产品语义:`recommendation` 是为了让用户"看到被推过的票"+ 标签是为了"分组统计"。如果标签缺失,前端 `/recommendations?tag=morning` 看不到这条,但 `/recommendations` 还能看到。

### C-3.2 `provider_switch_event` 异步写可能丢

`datasource.Manager` 的切换逻辑:

```
Switch(from, to) {
    HealthRepo.UpdateLastSwitch(from, to)  // 同步?异步?
    ProviderSwitchEventRepo.Record(from, to, reason)  // 异步?
}
```

`OPERATIONS.md` 没明确这两步是否在同一临界区。如果 manager panic 在 health 写之后、event 写之前,**`datasource_health.last_switch` 会更新,但 `provider_switch_event` 表少了这条记录**——前端"系统状态"页查不到这次切换。

### C-3.3 `ReviewReport` 与 `Recommendation` 无 FK

`review_report` 表里**没有** `recommendation_ids JSON` 或类似的链接字段。复盘报告按 `from / to / strategy_code` 时间窗聚合(`review.Service.GenerateReport`),完全靠时间窗对齐。

> 如果未来"按推荐维度看复盘",需要重写聚合入口(详见 §A-4 候选 1)。

### C-3.4 `performance_snapshot` 与 `recommendation` 只通过 ID 关联

```
type PerformanceSnapshot struct {
    ID                uint64
    RecommendationID  uint64  // 无 FK(只有 ID)
    T1Return          *float64
    T3Return          *float64
    T5Return          *float64
    MaxDrawdown       *float64
    RecalculatedAt    time.Time
    ...
}
```

只通过 `RecommendationID = X` 引用,无 FK 约束。如果删除某条推荐(目前没这个 API,但未来可能加),快照会变孤儿。

### C-3.5 `enrichRecNames` 是列表查询时的"运行时回填"

`executor.ListRecommendations` 调用 `enrichRecNames`(`executor.go:536`)在**读时**去 datasource 拉 `stock_name`。如果 datasource 挂了,该字段仍可读但保持空。如果 datasource 整个挂 + 推荐很多,N+1 拉会拖慢列表。

> 正常情况感知不到,但极端场景性能会塌。

### C-3.6 `fuzz` 没考虑:跨 cron 周期的 stale 数据

`performance_snapshot` 是"特定 recalculated_at 时刻对当时已知数据的一次计算"。如果回填策略是"过去 N 天都缺,补一次",那么**早一次补的快照**可能因为晚一次的实际数据更新而产生"快照与现状不一致"。

> 这是 §9 系统的固有特性(spec 接受),不算 bug,但容易让运营困惑。

### C-3.7 `ai_audit` / `llm_settings` / `llm_providers` 的写路径

`ai.Service.ApplyIntent` 写 `ai_audits`(用户确认那次 AI 改动),`settings.Service` 写 `llm_settings` + `llm_providers`(用户改 LLM 配置)。两条线在写时**没有**清理"旧 LLM chain 的状态"——例如把第一项 provider 删了,但 `ai.Provider` 仍持有它的引用,需要重启才生效(`settings.LoadChainOnStart` 才会全量重读)。

---

## C-4 重排候选(外部参考)

| # | 候选 | 触发条件(假设) | 不做的代价 |
|---|---|---|---|
| 1 | 把 `persist` 节点 + `session_tag` 节点合并为单一节点(同事务落 recommendation + tag) | 出现"主表有但标签没落"的生产 bug | 修复要改 orchestrator 内节点系统,代价较高 |
| 2 | 把 `provider_switch_event` 写库下沉到 `datasource.Manager` 的同一临界区(健康表 + 事件表同写) | 出现"切换发生但事件没记"的 case | 局部修复,代价小 |
| 3 | 给 `ReviewReport` 加逻辑 FK(`linked_recommendation_ids JSON`) | 需要"按推荐维度看复盘" | 现状时间窗聚合能用,改造面大 |
| 4 | 给 `performance_snapshot` 加 ON DELETE SET NULL + 删除推荐时的清理 job | 真要做"删除推荐"功能 | 现状无该 API,推迟 |
| 5 | 把 `enrichRecNames` 改为 `persist` 节点写入时即填充 + 缓存 | 出现大量 stock_name 为空的推荐(或列表性能塌) | 性能问题可用缓存缓解 |
| 6 | 给 `performance_snapshot.recalculated_at` 加全局版本号,与 recommendation 的 `updated_at` 一致 | 出现"快照与实际 K 线不同步"的运营困惑 | 加版本号字段,但不解决根本 |
| 7 | 把 `ai.Provider` 与 `settings.Service` 的引用关系文档化(注释 + 单元测试) | 出现"启动顺序导致 Provider 不一致"的 bug | 现状靠"指针共享" |
| 8 | 把 `recommendation_tag.source` 显式索引(便于按 source 过滤) | 出现"想看 AI 助手打的标签 vs 节点自动打的"分析需求 | 全表扫描 30ms 内,不影响 |

---

## C-5 引用

- 主文档:§C 在总览中的位置 + 全局拓扑图(数据流路线)
- A-§:产品主链路如何消费这些数据
- B-§:写库路径对应的模块所有权
- D-§:数据一致性在可靠性边界上的约束(breaker / retry / fallback)
- E-§:演进(哪些表是推迟项,哪些表是 v1.3 加的)
