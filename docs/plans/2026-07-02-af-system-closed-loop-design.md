# AF Selector — 系统逻辑闭环梳理

> 调研日期:2026-07-02
> 调研对象:`af` 仓库(v1.3.4)
> 调研路径:静态文档(README + openspec + TODOS + KNOWN-LIMITATIONS + DX_REVIEW + 各模块顶层 .go)
> 证据强度:代码 + 现有文档(不跑测试、不连 prod)
> 重排候选标度:**外部参考 / 未来可讨论**(不打 P 级、不估工期)

---

## 0. 阅读指引

本文是 AF Selector 系统"逻辑闭环"梳理的**主文档**。它是 5 份分册的索引和摘要,本身不重复各分册的细节。

| 分册 | 主题 | 文件 |
|---|---|---|
| A | 产品主链路(用户视角) | [`01-product-loop.md`](./2026-07-02-af-system-closed-loop/01-product-loop.md) |
| B | 模块边界与依赖 | [`02-module-boundaries.md`](./2026-07-02-af-system-closed-loop/02-module-boundaries.md) |
| C | 数据流闭环 | [`03-data-loop.md`](./2026-07-02-af-system-closed-loop/03-data-loop.md) |
| D | 可靠性闭环 | [`04-reliability-loop.md`](./2026-07-02-af-system-closed-loop/04-reliability-loop.md) |
| E | 演进闭环 | [`05-evolution-loop.md`](./2026-07-02-af-system-closed-loop/05-evolution-loop.md) |

每份分册的固定骨架:

1. **现状**:用代码 + 文档证据描述这一层当前怎么连
2. **闭环是否成立**:用证据做判断,不替读者猜
3. **断点 / 风险**:这一层未闭合的位置
4. **重排候选**:外部参考,标"做什么 / 触发条件 / 不做的代价",**不提建议**

---

## 1. 调研边界与口径

### 1.1 范围

- **范围**:AF Selector v1.3.4 整套后端(Go 1.25)+ 前端(React 18)+ 部署(systemd + nginx)
- **不在范围**:实盘交易、跨市场、回测、§6.7 节点模板引用、§8.4 多标签交集、§8.5 CSV 导出——这些在 `openspec/changes/astock-selector-system/tasks.md` 中明确标为"推迟到 v2/v1.1+",本文不视为缺漏

### 1.2 调研方法

- **静态文档**:README §1-13,openspec/{proposal,design,tasks}.md,docs/{KNOWN-LIMITATIONS,OPERATIONS,RELEASE-NOTES-1.0.0}.md,TODOS.md,DX_REVIEW.md
- **代码顶层**:cmd/server/main.go,internal/{orchestrator,executor,perf,datasource,notify,calendar,ai,auth,position,review,settings}/* 的顶层 .go
- **数据模型**:internal/model/* 的结构体清单(20 个 GORM 实体)
- **不调研**:不初始化 CodeGraph(项目未初始化),不跑测试,不动 prod,不开浏览器

### 1.3 "逻辑闭环"的工作定义

每一层都用 4 个问题拷问:

1. **起点在哪** — 这一层的输入从何处来?
2. **终点在哪** — 输出落到何处、被谁消费?
3. **谁来核对** — 出错 / 偏离时,有什么机制发现并收敛?
4. **谁来接续** — 进程重启 / 中间组件挂掉后,链路是否能在下一个周期继续?

如果 4 个问题都有明确答案 → "闭环成立";有任一缺失 → 标注断点。

---

## 2. 现状快照(对照 v1 spec 与代码)

### 2.1 规格期望 vs 实际实现

来源:openspec/changes/astock-selector-system/{proposal,design,tasks}.md + backend/internal/* + README.md + TODOS.md。

| 维度 | v1 spec 期望(原文) | 当前实现(v1.3.4) | 是否闭环 |
|---|---|---|---|
| 数据源 | ≥3 路 + 单源熔断 + 实时心跳 + 全源告警 + 切换可见 + cross-source 一致性 | eastmoney/sina/akshare + `breaker.go` + `datasource_health` 表 + `provider_switch_event` 表 + Redis/memory cache + 0.5% 偏差阈值 | ✅(告警链路见 §D-2) |
| 编排 / 执行 | DAG 节点插件化 + Kahn 拓扑 + 并发执行 + 错误短路 + 8 内置节点 + trial-run | `orchestrator.Node` 接口 + `orchestrator.Executor`(Kahn + 协程池 + errgroup 改写) + `nodes/` 8 节点(`register.go`) + `trial_handler.go` | ✅ |
| 调度 | robfig/cron + A 股交易日历 + 手工触发 + SSE 实时日志 | `executor.Scheduler`(robfig/cron) + `calendar.trading_day` + `executor.Trigger`(3s 同步返回 + 异步 goroutine) + `MemBus` + `DrainEvents` SSE | ✅ |
| 多通道通知 | 飞书 / 钉钉 / 企业微信 + 重试 + 熔断 + 模板 + 多通道主备 | `notify/channel/{feishu,dingtalk,wecom}` + `retry.go` 指数退避 + `circuit.go` 熔断 + `template.go` + `manager.go` 路由 | ✅ |
| 推荐 + 时段标签 | 多对多标签 + source 字段 + 同票多时段叠加 | `model/recommendation.go` + `model/recommendation_tag`(关联表,`tag`+`source`) + `session_tag` 节点 | ✅(事务边界见 §C-3) |
| 绩效 §9 | T+1/T+3/T+5 累计胜率 + 回撤 + 多维聚合 + 启动补跑 + 收盘后 cron | `perf.Service` + `Calculator` + `Backfill`(默认 5m) + `Scheduler`(默认 02:00) + `aggregate`(已窄化 SELECT) | ✅ |
| AI 助手 §11 | OpenAI 协议 + JSON Schema + 两阶段 + 审计表 + 供应商可换 | `ai.Service` + `ai.OpenAIClient` + `ai.MockClient` + `LLMProvider` 热替换 + `settings` 面板 + `ai_audit` 表 | ✅ |
| 复盘 §14.9 | 每日 15:30 / 每周日 20:00 cron + 多通道推送 | `review.Service` + `review.Scheduler`(DailyCron/WeeklyCron) + 共享 `LLMProvider` | ✅ |
| 鉴权 §12 | v1 单用户本地使用即可(推迟到 v2) | `auth.Service`(bcrypt + JWT) + `auth.Middleware` + Bootstrap 第一个 admin + public/protected routes + `user`/`role` 表 | ✅(原推迟项已交付,数据模型预留多用户) |
| 持仓 / 大屏 §10 | 1920×1080 大屏 + 持仓 + 今日推荐 + 胜率排行 + 热力图 | `position.Service` + `position.Handler` + `executor.DashboardStats` + 前端 `/dashboard` | ✅ |
| 回测 / 多标签交集 / CSV 导出 | v1 不做 | 未做 | N/A(按 spec) |

> **结论**:v1 spec 范围里所有"硬约束"项,代码层都已实现。推迟项未做,与 spec 一致。

### 2.2 与 DX_REVIEW(2026-06-17 快照)的关系

`DX_REVIEW.md` 是 v0.1 时代的快照,其 P0/P1/P2 中绝大多数已在 v1.0.0/v1.1 期间修复(Makefile 已实、`.env.example` 已实、docker-compose 已实、OpenAPI+Swagger UI 已实、`/api/v1/healthz` 已实、`request_id` 已加、CHANGELOG 已加、devcontainer 已加、pre-commit hook 已加)。本文不把 DX_REVIEW 列为"现存的 P0",而是把它视作"曾经过的历史快照"。

---

## 3. 全局拓扑(一张图)

下图把"模块依赖 + 主流转"合并呈现,作为后续各分册的地图。

```
                 ┌────────────────────────────────────────────────────────┐
                 │  Frontend (React 18 + TS + Vite + ReactFlow + ECharts) │
                 │   路由:/dashboard /strategies /runs /recommendations  │
                 │   /reviews /positions /settings /health               │
                 └─────────────┬──────────────────────────────────────────┘
                               │  HTTP + SSE  (axios + X-Request-ID)
                               ▼
            ┌──────────────────────────────────────────────────────────────┐
            │  Router (internal/router) + Middleware (CORS/RequestID/...)  │
            └──┬────────┬────────┬────────┬────────┬────────┬──────────┬──┘
               │        │        │        │        │        │          │
   ┌───────────┘   ┌────┘   ┌────┘   ┌────┘   ┌────┘   ┌────┘   ┌──────┴──────┐
   ▼               ▼        ▼        ▼        ▼        ▼        ▼             ▼
┌──────────┐ ┌──────────┐ ┌─────────┐ ┌──────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐
│ strategy │ │   run    │ │   perf  │ │   pos    │ │  ai    │ │review  │ │settings│ │  auth  │
│  CRUD +  │ │  + SSE   │ │  §9     │ │  + dash  │ │  §11   │ │ §14.9  │ │  LLM   │ │  §12   │
│ trial    │ │  + retry │ │  T+N    │ │  + quote │ │ chat   │ │ daily/ │ │ hot-   │ │ JWT    │
│ + tpl    │ │  + tpls  │ │  agg    │ │          │ │ apply  │ │ weekly │ │ swap   │ │ bcrypt │
└────┬─────┘ └────┬─────┘ └────┬────┘ └────┬─────┘ └───┬────┘ └───┬────┘ └───┬────┘ └───┬────┘
     │            │            │            │           │          │          │          │
     ▼            ▼            ▼            ▼           ▼          ▼          ▼          ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│  internal/orchestrator  (DAG runtime + Node interface + 8 builtin nodes + bus)     │
│  internal/executor      (run lifecycle + cron + SSE + templates + dashboard)      │
│  internal/datasource    (3 sources + breaker + cache + health + switch events)    │
│  internal/notify        (3 channels + retry + circuit + templates + manager)      │
│  internal/calendar      (trading-day + session window, Asia/Shanghai)             │
│  internal/ai            (LLM provider + service + chain + audit)                   │
│  internal/auth          (service + middleware + JWT + bcrypt + Bootstrap)         │
│  internal/settings      (LLM provider hot-swap + chain load)                      │
│  internal/review        (service + scheduler + LLM summary)                       │
│  internal/position      (service + quote source + handler)                         │
│  internal/perf          (calculator + service + scheduler + aggregate)            │
│  internal/httpresp      (envelope + request_id)                                    │
│  internal/apperr        (Code + HTTP status + Wrap)                                 │
└─────────────────────────────────────────────────────────────────────────────────────┘
                              │                              │
                              ▼                              ▼
                       ┌────────────┐                ┌────────────┐
                       │  MySQL 8   │                │  Redis 7   │
                       │  (sqlite   │                │  + miniredis│
                       │  fallback) │                │  in-proc   │
                       └────────────┘                └────────────┘
```

---

## 4. 五层摘要(每层一句话)

| 层 | 一句话现状 | 主要风险 | 分册 |
|---|---|---|---|
| **A 产品主链路** | "用户定义策略 → cron/手工触发 → DAG 跑 → 通知 → 复盘 → 自动周报" 5 段都连得上 | 复盘报告与推荐之间的时间窗契约不显式;前端"运行中"状态依赖 SSE,断流后只回放当前进程 | [01](./2026-07-02-af-system-closed-loop/01-product-loop.md) |
| **B 模块边界** | `orchestrator`(纯 DAG)与 `executor`(run 生命周期)分得很清;`ai` / `auth` / `settings` / `review` / `position` 是 v1.2/1.3 后期加的"横向能力" | 两层 `Executor` 同名增加认知成本;`executor` 承担 4 件事;`strategy/` 空目录 | [02](./2026-07-02-af-system-closed-loop/02-module-boundaries.md) |
| **C 数据流** | 数据从 datasource → orchestrator node → executor persist → recommendation/recommendation_tag → perf snapshot → review/report,5 段都写到了 DB | 跨节点无显式事务;`provider_switch_event` 异步写可能丢;`ReviewReport` 与 `Recommendation` 无 FK | [03](./2026-07-02-af-system-closed-loop/03-data-loop.md) |
| **D 可靠性** | breaker + circuit + retry + 交易日历守护 + health 表 + request_id + SSE Last-Event-ID,基本盘完整 | SSE 跨进程无 fan-out;EventBus 内存缓冲重启即丢;prod config 改完没自动审计 | [04](./2026-07-02-af-system-closed-loop/04-reliability-loop.md) |
| **E 演进** | v1.1 推迟项基本都被 v1.3.x 收掉了,只剩 §6.7 / §8.4 / §8.5 + fe2 M4 可选项;DX_REVIEW 的 3 P0 已修 | 没做新一轮 `/plan-devex-review` 验证;多用户 RBAC 已建表但只支持单 admin | [05](./2026-07-02-af-system-closed-loop/05-evolution-loop.md) |

---

## 5. 重排候选汇总(外部参考)

> **重要**:此清单是"未来可讨论"的素材,**不是"应该做"的清单**。
> 每条候选后标 **做什么 / 触发条件 / 不做的代价**。不打 P 级、不估工期。

### 5.1 模块边界类(详见 §02)

| 候选 | 触发条件(假设) | 不做的代价 |
|---|---|---|
| 重命名 `executor.Executor` 为 `executor.Runner` 或 `executor.RunService` | 新成员加入,在 `orchestrator.Executor` / `executor.Executor` 之间混淆 | 长期让"导入名字"和"职责"对不上;code review 时反复解释 |
| 把 `executor` 包的"运行生命周期 / 持久化 / dashboard 聚合 / 推荐查询" 拆为子包 | `executor/executor.go` 单文件继续膨胀 | 当前单文件 ~667 行,继续叠加会让单元测试目标模糊 |
| 清理空的 `internal/strategy/` 目录 | 任何 `git grep strategy/` 出现歧义 | 现状是"幽灵包",留着只会让未来的 `grep` 反复撞 |
| `notify.registry` 与 `notify` 主包存在重叠 | `notify/registry` 与 `notify` 主包同时被引用,边界越发不明显 | 现状还能跑,但 `internal/notify` 的 import 列表已经开始膨胀 |

### 5.2 数据流类(详见 §03)

| 候选 | 触发条件(假设) | 不做的代价 |
|---|---|---|
| 把 `persist` 节点 + `session_tag` 节点放入同一事务(或合并为单一节点) | "主表落了但标签没落"成为生产可观察的 bug | 现状是节点级"自然分割",如果出现标签漏写,排查要跨两张表 |
| 把 `provider_switch_event` 写库下沉到 `manager` 的同一临界区 | 出现"切换发生了但事件没记"的 case | 现状是 manager 异步记,manager panic 时事件丢失 |
| 给 `ReviewReport` 与 `Recommendation` 之间加"软 FK"(逻辑引用) | 出现"推荐被删后复盘报告还在引用"的语义不一致 | 现状 schema 没约束,只能靠应用层不删推荐 |
| `performance_snapshot` 写入与 `recommendation` 删除的级联策略显式化 | 出现"删除推荐后 T+5 快照还在,但前端报告说没数据" | 当前没有"删推荐"功能,所以暂时不爆 |

### 5.3 可靠性类(详见 §04)

| 候选 | 触发条件(假设) | 不做的代价 |
|---|---|---|
| 把 SSE EventBus 切到 Redis pub/sub 模式(支持跨进程) | 单进程成为水平扩展瓶颈,需要部署多副本 | 现状 v1 单副本够用;但当 prod 切到多副本时,事件会丢 |
| 把 `EventBus` 缓冲改为可持久化的小队列(重启不丢) | 出现"重启正好打断一次运行,前端看不到 events"的 case | 现状 Last-Event-ID 只回放在跑的事件,已经结束的事件不会重发 |
| 把 `config.yaml` 的"prod 手动编辑"纳入 `git` 之外的变更审计(写一行到 `/home/ubuntu/af/logs/config-changes.log`) | 出现"改了 config 没记 CHANGELOG,排查时找不到上次改了什么" | 现状靠人脑在 CHANGELOG 里写,DOC §2 明确警告"直接编辑 config.yaml 会被下次 deploy 覆盖" |
| 把 perf `Backfill` 默认超时按"每千条记录"动态化(替代固定 5m) | 数据规模跨过 ~50k 推荐后启动慢 | 现状粗粒度,慢的标志是 OOM 而不是超时 |

### 5.4 演进类(详见 §05)

| 候选 | 触发条件(假设) | 不做的代价 |
|---|---|---|
| 跑一轮新的 `/plan-devex-review` | 任何对外接口变更或新外部依赖接入 | 现状 dx-review 是 v0.1 快照,不能反映 v1.3.4 现状 |
| 把 TODOS.md 的"已完成"段折叠 / 移到 CHANGELOG | 文档变得很长,但条目都是"已发" | 现状可读性已经下降,但不影响功能 |
| §6.7 节点模板保存引用 / §8.4 多标签交集 / §8.5 CSV 导出 | 出现真实用户提需求 | spec 里就是推迟,默认不做 |
| 多用户 RBAC(`user`/`role` 表已建,只跑单 admin 模式) | 真的要让多人共用一台机 | 现状用 `auth.Bootstrap` 只建第一个 admin,数据模型已多用户就绪但代码路径只走单实例 |

### 5.5 横向(不止于单一层)

| 候选 | 触发条件(假设) | 不做的代价 |
|---|---|---|
| 把本文(`2026-07-02-af-system-closed-loop-design.md` + 5 分册)作为下一轮 `/plan-devex-review` 的输入 | 当你想做下一次"系统体检" | 现状知识散落在 README/openspec/OPERATIONS,review 时要从头拼 |
| 把 `Makefile` 与 `docs/OPERATIONS.md` §11 串成一个"Make vs 运维命令"对照表 | 出现"运维读了 OPERATIONS,开发者用 Makefile,两边术语对不上" | 现状两边术语基本一致,但缺少单一对照页 |
| 给"重排候选"建一个独立的 `docs/REFACTOR-CANDIDATES.md`,与本文解耦 | 候选条数超过 ~30 条,本文不再适合承载 | 现状 5.1-5.4 共 17 条,还轻 |

---

## 6. 不做的事(明确边界)

为防止后续误读,本文**明确**不做:

- **不写实现任务** — 用户已选"梳理 + 重排候选(外部参考)",本文不产生 task 清单
- **不动代码** — 所有改动留给未来的 PR
- **不跑测试 / 不连 prod / 不开浏览器** — 调研路径是 A 静态文档
- **不重写 README / 不改 OpenSpec** — 本文以新文件形式并存
- **不引入新依赖 / 不改 go.mod / 不改配置** — 调研动作,纯只读
- **不打 P 级 / 不估工期** — 重排候选只标"做什么 / 触发条件 / 不做的代价"

---

## 7. 元信息

- 调研产物版本:v1
- 调研完成日期:2026-07-02
- 受众:本文主要给"想理解 AF Selector 现状 + 决定未来做什么"的读者;不是为了"明天就开始改"
- 与现有文档的关系:本文**补充** README/OPERATIONS/KNOWN-LIMITATIONS,**不替代**它们

如果需要进一步深入某一层,直接读对应分册;如果分册的信息密度不够,回到对应模块的 .go 顶层 + model/ 实体清单继续钻。
