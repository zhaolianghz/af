# §D 可靠性闭环

> 调研日期:2026-07-02 · 主文档:[`../2026-07-02-af-system-closed-loop-design.md`](../2026-07-02-af-system-closed-loop-design.md)

---

## D-1 现状:8 类可靠性机制

### D-1.1 可靠性机制清单

| # | 机制 | 配置点 | 入口 | 默认阈值 |
|---|---|---|---|---|
| 1 | **数据源断路器** | `cfg.Datasource.FailThreshold / FailWindow / Cooldown / ConsistencyThreshold` | `datasource.NewManager`(`cmd/server/main.go:472`) | fail_threshold / fail_window / cooldown 见 `config.example.yaml` |
| 2 | **通知重试 + 断路器** | `cfg.Notify.Channel` | `notify.NewManager`(`main.go:110`)+ `registry.Options` | 指数退避 × 3 次 + 5 次/5 分钟熔断 |
| 3 | **交易日历守护** | `cfg.Calendar` | `calendar.NewService`(`main.go:622,751`)+ `executor.Scheduler.guardTradingSession` | trading_day + session_window |
| 4 | **SSE Last-Event-ID** | `cfg.Executor.SSEHeartbeat` | `executor.DrainEvents`(`executor.go:640`) | 默认 20s 心跳 |
| 5 | **request_id 贯穿** | middleware | `middleware.RequestID` + `httpresp.Err` + zap logger | UUIDv4 |
| 6 | **多数据源路由** | `cfg.Datasource.Sources` 顺序 | `datasource.Manager` 内部 chain | YAML 配置顺序 |
| 7 | **perf startup backfill** | `cfg.Perf.StartupBackfillTimeout` + `KlineLookback` | `perf.NewService` + 后台 goroutine(`main.go:773-791`) | 默认 5m(<=0 时 fallback 5m) |
| 8 | **graceful shutdown** | `cfg.Server.ShutdownTimeout` | `cmd/server/main.go:340-390`(SIGINT/SIGTERM handler) | 见 `config.yaml` |

### D-1.2 错误响应单源真理

所有非 2xx 响应走 `internal/httpresp.Err` + `internal/apperr.HTTPCode`。已知错误码枚举:

```
CodeOK           = 0      // 成功
CodeInvalidArg   = 10000  // 参数错
CodeNotFound     = 10001  // 找不到
CodeConflict     = 10002  // 冲突
CodeInternal     = 10003  // 内部错
CodeUnavailable  = 10004  // 服务暂不可用(503)
```

错误响应格式固定:`{ "code": 10001, "message": "...", "request_id": "<uuid>" }`(`httpresp.Err`)。`request_id` 永远回填(就算客户端没带 `X-Request-ID`,服务端也会生成 UUIDv4)。

### D-1.3 健康检查与心跳

- **`/api/v1/healthz`** + **`/healthz`** alias:Gin handler,返回 `{ status: "ok" | "degraded", version, ts, uptime, db: "up" | "down" | "n/a" }`
- **`datasource_health` 表**:每源每 ~60s 心跳一行,字段 `source / last_ok / last_fail / fail_count_5m / status`
- **`provider_switch_event` 表**:每次源切换一行,字段 `from / to / reason / ts`
- **`/api/v1/datasource/health`**:路由到 `datasource.NewHealthHandler`(读 health 表 + 计算实时 breaker 状态)
- **`/api/v1/notify/health`**:路由到 `notify.NewHealthHandler`(读每通道 breaker 状态)

---

## D-2 闭环是否成立

按 4 个问题拷问:

| 问题 | 答案 | 证据 |
|---|---|---|
| **起点** | 任何一行代码报错都会被 `apperr.Wrap` 包装;任何 IO 失败都会进 breaker | `cmd/server/main.go` 全链路有 middlewares;`datasource.Manager` 每个 Fetch 走 breaker |
| **终点** | 错误信息透传到 `httpresp.Err` 返回;breaker 状态透传到 `/healthz` / `/datasource/health` | `KNOWN-LIMITATIONS §6`:无错误聚合面板,但每个错误都有 `request_id` 可追 |
| **核对** | request_id 关联(zap 日志 + 错误响应 + X-Request-ID) | `operatortions.md §1`(30 秒 triage 流程)的第一件事就是 grep `request_id` |
| **接续** | systemd 自动重启 + Scheduler.LoadFromDB + perf startup backfill | `documents/OPERATIONS.md §11`(全量重启流程)+ `documents/OPERATIONS.md §14`(备份恢复) |

**结论:可靠性基本盘完整,8 类机制都连得上**;但**部分边界有限制**(见 D-3)。

---

## D-3 断点 / 风险

### D-3.1 SSE 跨进程无 fan-out(KNOWN-LIMITATIONS §3)

`internal/orchestrator/eventbus.go` 的 `MemBus` 是进程内的 `chan`,每个事件只在本进程内广播。如果 prod 从单进程切到多副本(nginx upstream 多 backend):

- 后端 A 上跑的 run,事件只到 A 上的 SSE 客户端
- 后端 B 上跑的 run,事件只到 B 上的 SSE 客户端
- nginx 上挂的客户端如果轮流连两个后端,**会丢失部分事件**

`KNOWN-LIMITATIONS.md §3` 已显式标注,但**生产配置目前是单进程**(124.156.213.179:9090),无故障压力。一旦需要 HA,这条限制会立刻变绊脚石。

### D-3.2 EventBus 内存缓冲重启即丢

`MemBus` 用 `chan Event` 做缓冲(channel buffer 大小未查),如果进程**非优雅**退出(SIGKILL / OOM / panic)而 buffer 里有未消费的事件,这些事件永久丢失。

- 进程重启后,客户端 `Last-Event-ID` 头回带的 ID 找不到事件 → 服务端只能发 "ready" + "run_finished"(终态),中间的过程事件全丢
- 受影响的客户端:LogStreamViewer 在刷新时会跳到 run 的最终状态,中间节点事件不见

### D-3.3 prod config 改完没自动审计

`OPERATIONS.md §2` 警告:"直接编辑 config.yaml 会被下次 deploy 覆盖"。但目前**没有**:

- `config-changes.log` / 类似的审计日志
- 配置文件 hash 校验(对比 git HEAD)
- CHANGELOG 强制要求("每次手动改 config 必须写到 CHANGELOG"的流程)

如果 prod 改了 config 后出问题,排查需要"上次谁改了什么"、"那时候跑了什么"——目前只能靠人脑 / 随机会议回忆。

### D-3.4 perf Backfill 粗粒度

`perf startup backfill` 默认 5m timeout(`cfg.Perf.StartupBackfillTimeout`,<=0 fallback 到 5m)。但**数据规模增长后**:

- 数据规模 50k 推荐 → 5m 可能不够,会超时杀掉
- 数据规模 500k 推荐 → 5m 远远不够,但又必须终止否则 OOM

OPERATIONS §9 提到:`Out of memory: Killed process` 的常见原因是 "perf backfill trying to load too many rows in one transaction"。当前没有按"批次"切的逻辑。

### D-3.5 全源告警非实时

任务 §3.10 "全源告警:3 路全部失败时,立即通过 7.5 通知通道发'行情源全挂'告警(**不走 cron,下一次执行触发即可**)"。但 spec 措辞已经埋了雷:

- "下一次执行触发即可" = 如果所有策略都禁用 / 没有 cron 触发,全源挂了**直到用户手动触发才知道**
- 没有独立的"全源心跳"goroutine(例如每小时主动确认下)

### D-3.6 Graceful shutdown 的 in-flight 兜底靠 timeout

`main.go:365-390` 在 SIGINT/SIGTERM 后:

- 调用 `srv.Shutdown(ctx)` 等 HTTP 请求完成(shutdown_timeout 默认 30s?见 config)
- 调 `schedulerStop` 等 cron 在跑的 job 完成
- 调 `perfStop` 等 perf scheduler 在跑的 job 完成
- 调 `reviewStop` 等 review scheduler 在跑的 job 完成

如果 in-flight 的是"卡在外部 HTTP API 那边"——`document/OPERATIONS §7` 给的例子:tushare 没响应,无 client timeout,连接挂死——shutdown 会等满 30s 后**强制** `cancel()`,在跑的 run 会带着 ctx.Canceled 退出。

### D-3.7 nginx proxy_buffering 必须关闭(SSE 看起来"卡住")

OPERATIONS §6 显式警告:nginx 默认 `proxy_buffering on`,SSE 必须 `proxy_buffering off` + `proxy_cache off` + `proxy_read_timeout 300s`。运维文档有写,但**手动部署的机器**容易踩这个坑。

### D-3.8 没有"主动健康检查 + 自愈"的 goroutine

当前 8 类机制都是"被动响应":

- breaker 是"被动触发"——失败 N 次才熔断
- retry 是"被动重试"——失败才退避
- backfill 是"被动补跑"——启动时 / 定时才触发

没有"主动探活 + 发现不一致"的 goroutine(例如每小时对比 `performance_snapshot` 与 `recommendation` 数量,差距 > X 报警)。

### D-3.9 `errors.Is` 链路依赖 zentinel

`apperr.Wrap(code, msg, cause)` + `errors.Is(err, X)` 在 Go 标准库中是常见模式。AF 当前依赖每个调用方 `errors.Is` 检错 — 如果某处漏检,breaker 不会触发熔断(因为没被认为是"已知失败")。

### D-3.10 备份策略已自动,但 restore 没验证过自动路径

`OPERATIONS §14` 描述了 nightly backup + 14 天保留 + 手动 restore 流程。"Installed on prod (2026-06-20)"—— 但 **14 天前的 prod 真实恢复演练**没有记录。也就是说:备份是否能在真出故障时还原**没人验证过**。

---

## D-4 重排候选(外部参考)

| # | 候选 | 触发条件(假设) | 不做的代价 |
|---|---|---|---|
| 1 | 把 SSE EventBus 切到 Redis pub/sub(支持跨进程) | 多副本部署需求 / HA 部署 | 现状单副本够,但切多副本时事件丢失 |
| 2 | 给 EventBus 缓冲加可持久化(recent N events 写 disk / Redis stream) | 出现"重启正好打断运行,前端看不到 events" | 现状 Last-Event-ID 只回放 in-flight |
| 3 | 把 prod config 改动纳入审计日志(写入 `af-config-changes.log`,带 request_id 由 notify 调用触发) | 出现"改了 config 没记" | 现状靠人脑在 CHANGELOG 里写 |
| 4 | 把 perf Backfill 改为"每批 N 个记录 + 多轮",超时只杀当前批 | 数据规模 > 50k | 现状粗粒度,慢的标志是 OOM |
| 5 | 加独立"数据源心跳"goroutine,全源都挂时主动调 notify.Manager.Send(不靠下一次 cron) | 所有策略禁用 + 数据源全挂 | 现状等下一次执行触发告警 |
| 6 | 给所有外部 HTTP 调用 client 加显式 timeout(per-source 配置) | 出现"shutdown 被卡满 30s 因为外部 API 没响应" | OPERATIONS §7 已警示,但没强制 |
| 7 | 加"主动健康检查"goroutine:每小时对比 perf snapshot 数 vs recommendation 数,差距 > X 报警 | 出现"数据不一致但无人发现" | 现状靠人脑 + 偶尔的 `/perf/aggregations` 漂移 |
| 8 | 把 backup 做一次"恢复演练"(每个季度),写演练报告 | 出现"备份文件存在但恢复失败" | 没有人验证,等于没备份 |
| 9 | 给 apperr 增加细粒度 cause label(给 breaker 喂准确的"网络 / 业务 / 数据"分类) | 出现"breaker 误熔断" / 想做"分类告警" | 现状所有失败统一熔断 |
| 10 | 把 nginx `proxy_buffering off` 校验加入 deploy 脚本的"前置检查" | 新部署遇到 SSE 卡住 | 现状靠运维记得 |

---

## D-5 引用

- 主文档:§D 在总览中的位置
- A-§:产品主链路中的"实时反馈"段(对应 SSE / Last-Event-ID)
- B-§:可靠性机制在模块边界上的归属(breaker 在 datasource / retry+circuit 在 notify)
- C-§:可靠性对数据一致性的影响(异步写 switch_event 丢 vs breaker 跳过字段)
- E-§:演进(可靠性机制的"已知 vs 未知"差距)
