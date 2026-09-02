# §A 产品主链路(用户视角端到端)

> 调研日期:2026-07-02 · 主文档:[`../2026-07-02-af-system-closed-loop-design.md`](../2026-07-02-af-system-closed-loop-design.md)

---

## A-1 现状:五段主链路 + 三段次链路

### A-1.1 主链路 5 段(每段标 spec 编号 + 代码入口)

| 段 | 用户动作 | spec § | 代码入口 | 关键文件 |
|---|---|---|---|---|
| 1. 策略定义 | 在画布拖节点 / 用内置模板创建 | §6/§5/§14.1-14.6 | `internal/orchestrator/strategy_service.go` + `executor/templates/*.json` | `orchestrator.StrategyService.Create/Update` + `templates.Loader.SyncToDB` |
| 2. 触发 | 等 cron / 点"立即执行" | §7.1/§7.2 | `internal/executor/scheduler.go` + `executor/handler.go` | `executor.Scheduler.Start` + `executor.Trigger`(3s 同步返回 + 异步 goroutine) |
| 3. DAG 执行 | 观察节点事件流 | §7.2 | `internal/orchestrator/executor.go` | `orchestrator.Executor.Execute`(Kahn + 协程池 + errgroup 改写) |
| 4. 通知 | 收到飞书/钉钉/企微消息 | §7.5/§4 | `internal/notify/manager.go` + `notify/registry/` | `notify.Manager.Send` + `channel/{feishu,dingtalk,wecom}.go` |
| 5. 复盘 | 看 `T+1/T+3/T+5` 胜率 | §9 | `internal/perf/handler.go` | `perf.Service.RecommendationPerformance` + `perf.Service.Aggregate` |
| 6. 自动周报 | 每天 15:30 / 周日 20:00 收到复盘报告 | §14.9-14.11 | `internal/review/scheduler.go` + `review/service.go` | `review.Scheduler.Start` + `review.Service.GenerateReport` |

### A-1.2 次链路 3 段

| 段 | 用户动作 | spec § | 代码入口 |
|---|---|---|---|
| a. 持仓大屏 | 看 `/dashboard` 1920×1080 | §10 | `position.Handler` + `executor.DashboardStats` |
| b. AI 助手 | 在画布右侧抽屉与 LLM 对话 | §11 | `ai.Service.Chat` + `ai.Service.ApplyIntent`(两阶段提交) + `ai.Provider`(LLM 热替换) |
| c. LLM 设置 | 在 `/settings` 换供应商 / 改 base_url | §11.2 | `settings.Service.LoadChainOnStart`(启动加载) + `settings.Handler.UpdateProvider`(运行时热替换) |

### A-1.3 触发链路的"3 秒承诺"

`internal/executor/handler.go`:`Trigger` 创建 run row 后,3 秒内把 `run_id` 同步返回给前端,DAG 执行丢到 goroutine 异步跑。这条不变量是产品体验的"刚性"约束,在 `executor.go:Execute` 里用 `ExistingRun` 字段显式传递(run 行已经 pre-create)。

来源:`executor/executor.go:108-111`(`ExistingRun` 注释:让 HTTP Trigger 路径能在 ~3s 内返回 `run_id`)。

---

## A-2 闭环是否成立

按"逻辑闭环"工作定义(主文档 §1.3)的 4 个问题拷问:

| 问题 | 答案 | 证据 |
|---|---|---|
| **起点在哪** | 用户在 `/strategies` 页拖节点 / 用模板创建策略;`StrategyService.Create/Update` 落 DB;`Scheduler.LoadFromDB`(启动时)或 `SetScheduleReloader`(运行时更新)注册 cron | `cmd/server/main.go:164-166`(scheduler 重载器);`executor/scheduler.go:LoadFromDB` |
| **终点在哪** | `recommendation` + `recommendation_tag` + `performance_snapshot` + `review_report` 都落 DB;前端 `/recommendations` `/runs/:id` `/perf/aggregations` `/reviews` 读取 | 4 张表都在 `model/`,对应 handler 都在 `cmd/server/main.go:317-326` 的 router 注入链里 |
| **谁来核对** | 每段都有 run_log 记录(节点级日志) + SSE 事件流(实时反馈) + request_id 贯穿(zap 日志 + 错误响应 + X-Request-ID) | `executor.persistLogs` 把 `summary.NodeResults` 转 `run_log` 行;`executor.DrainEvents` 是 SSE 心跳 + 事件转发;`middleware.RequestID` 在每个请求链路上盖戳 |
| **谁来接续** | cron 下一周期自动重跑;手动触发后重启,`Scheduler.LoadFromDB` 重新注册 | `executor/scheduler.go:LoadFromDB`(cmd/server/main.go:660);`graceful shutdown` 在 SIGINT/SIGTERM 等 in-flight 后退出(运维手册 §11) |

**结论:5 段主链路 + 3 段次链路,主链路闭环成立。**

---

## A-3 断点 / 风险

### A-3.1 复盘报告与推荐之间的时间窗契约不显式

`review.Service.GenerateReport` 按 `from` / `to` 时间窗聚合 `recommendation`,`ReviewReport` 表里**没有**指向某条 `recommendation.id` 的外键。语义上是"这段窗口内所有推荐的总结",而非"这条推荐 N 天后变怎样"。

> 如果未来要把"按推荐维度看复盘",需要重写聚合入口。

### A-3.2 前端"运行中"状态完全依赖 SSE

`frontend/src/components/runs/LogStreamViewer.tsx`(EventsSource)订阅 `/api/v1/runs/:id/events`,断开后只回放当前进程在跑的事件(`Last-Event-ID` 头带过来时由 `executor.DrainEvents` 重新广播当前 `MemBus` 缓冲)。已结束的事件不会重发——前端只能靠 `GET /api/v1/runs/:id` 拉终态。

> 单进程够用,多副本部署时事件丢失(详见 §D-3.1)。

### A-3.3 复盘报告推送依赖 notify.Manager

`review.Service.GenerateReport` 写完 `review_report` 行后会调 `notify.Manager.Send`(经 `feishu/dingtalk/wecom` 通道)。如果 notify 挂了(通道全熔断),**报告已经落 DB**,但用户收不到推送。

> 推送失败不影响数据,但用户体验降级。需要 §D-5 的"notify 监控"补一刀。

### A-3.4 持仓 position 表与 recommendation 表无外键

`internal/model/position.go`(Position 结构体:stock_code / qty / cost_price / etc.)**没有**外键到 `recommendation`。语义上是"用户手动建仓 + 后续自动同步",但代码层不保证"持仓里的票一定来自某次推荐"。

> 现状不影响功能,但审计/复盘角度缺一条"推荐 → 持仓"的链路。

### A-3.5 §10 大屏依赖 dashboard 聚合 + position + perf 三方数据

`executor.DashboardStats`(在 `executor.go` 里)把"今日推荐数 / 总推荐数 / N 天成功率 / 最近 5 条失败 run"做成单接口,但持仓卡片数据从 `position.Handler` 读,胜率排行从 `perf.Handler` 读。前端 `/dashboard` 页同时挂 3-4 个接口。

> 任何一个接口慢都会让大屏体验降级;当前没有单接口"拉一屏"的能力。

### A-3.6 跨节点事务边界未显式

`persist` 节点写 `recommendation` 表,`session_tag` 节点写 `recommendation_tag` 表。两者**不是**同一事务,如果 `session_tag` 失败,推荐还在但标签缺失。详见 §C-3.1。

---

## A-4 重排候选(外部参考)

> 提示:每条都标"做什么 / 触发条件 / 不做的代价"。不打 P 级、不估工期。

| # | 候选 | 触发条件(假设) | 不做的代价 |
|---|---|---|---|
| 1 | 把复盘报告的"时间窗聚合"改为显式 FK(每条复盘报告关联具体推荐集合) | 出现"推荐被删但报告还在"的语义不一致 | 数据冗余但每条报告语义更清楚;改造面较大 |
| 2 | 给 notify.Manager 的复盘推送加 fallback(失败时降级为"已在 DB + 前端可见") | 复盘报告推送失败率 >0 持续 N 天 | 用户体验降级但不丢数据 |
| 3 | 把 position 与 recommendation 的逻辑关联文档化(在 `model/position.go` 加注释) | 出现持仓数据与推荐不一致 | 数据完整性靠人脑维护 |
| 4 | 为"策略 → run → 推荐 → 复盘"链路加一条端到端 smoke test(playwright 跑全栈) | 任何模块接口变更,需要快速验证未破坏主链路 | 现状 `scripts/smoke.sh` 只覆盖 API;e2e 是单元粒度的 |
| 5 | 把 §10 大屏的"3-4 接口"合并为单一聚合接口(`/api/v1/dashboard/complete`) | 大屏首屏延迟 >2s | 前端多发请求,简单但慢 |
| 6 | 把 SSE 跨进程 fan-out 的限制显式写入 README 的"已知限制"段 | 用户咨询"为何多副本部署事件丢失" | 已经有 `KNOWN-LIMITATIONS.md` §3 在用,但 README 没引用 |
| 7 | 把 `ExistingRun` 的"3 秒同步返回"承诺写进 PR 模板的"性能不变量"清单 | 新人加 PR 但未感知这条不变量 | 维护靠口头 |

---

## A-5 引用

- 主文档:§A 在总览中的位置 + 与 §B-§E 的交叉点
- B-§:模块边界如何支撑主链路(尤其是 `executor.Runner` 的双层角色)
- C-§:数据流(主链路触发的 5 类数据写入)
- D-§:可靠性(breaker / circuit / SSE 持久化的所有限制)
- E-§:演进(主链路未覆盖的 §6.7 / §8.4 / §8.5 推迟项)
