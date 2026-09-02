# §E 演进闭环

> 调研日期:2026-07-02 · 主文档:[`../2026-07-02-af-system-closed-loop-design.md`](../2026-07-02-af-system-closed-loop-design.md)

---

## E-1 现状:v1 spec 交付对照

来源:`openspec/changes/astock-selector-system/tasks.md` + `TODOS.md` + README §8 + README §9 + README §10。

### E-1.1 v1 spec 各项交付状态

| Spec § | 内容 | 状态 | v1.0.0 之前/期间的变化 |
|---|---|---|---|
| §1 | 项目骨架 | ✅ v0.1 期间完成 | |
| §2 | 数据模型(16+ 张表) | ✅ v0.1 | |
| §3 | 数据源(3.1-3.12) | ✅ v0.1 | |
| §4 | 交易日历 | ✅ v0.1 | |
| §5 | 策略管理(5.1-5.5) | ✅ A7-BE1(2026-06) | |
| §6.1-6.6 | 编排画布 + DAG + 8 节点 + trial-run | ✅ A7-BE1 | |
| **§6.7** | **节点模板保存引用** | ⏳ 推迟到 v2(spec 原话) | |
| §7 | 执行引擎(cron + SSE + retry + 日历守护) | ✅ A7-BE2 | |
| §7.5 | 多通道通知(7.5.1-7.5.9) | ✅ A4 | |
| §8.1-8.3 + 8.6 | 推荐落库 + 时段标签 + 前端 | ✅ A7 | |
| **§8.4** | **多标签交集** | ⏳ 推迟到 v2 | |
| **§8.5** | **CSV / Excel 导出** | ⏳ 推迟到 v2 | |
| §9 | 绩效引擎 | ✅ §9(PR #1 合并于 2026-06-15) | |
| §10 | 可视化大屏 | ✅ v1.2.0(README 仍标 ⏳,**文档滞后**) | |
| §11 | AI 对话式方案管理 | ✅ v1.2.0(mock)+ v1.2.1(LLM 热替换) | |
| §12 | 多用户 / 鉴权 | ✅ v1.3.0(原推迟项,后改为默认关) | |
| §14.1-14.8 | 5 内置模板 + 一键生成 + AI 解释(硬编码) | ✅ A7-BE2 | |
| §14.9-14.11 | 每日/周复盘报告 | ✅ v1.2.0 | |
| §15 | 部署 + LICENSE | ✅ v1.0.0 | |
| §16 | 合规与免责 | ✅ v1.0.0 | |

### E-1.2 推迟项一览(明确"不做"清单)

按 `openspec/tasks.md` 原话的推迟项:

- [ ] **§6.7** 节点模板保存与引用(任务状态未勾)
- [ ] **§8.4** `GET /api/recommendations/tags/intersect` 多标签交集
- [ ] **§8.5** CSV / Excel 导出

### E-1.3 v1.0 → v1.3.x 的"原推迟项"现已交付清单

| 原推迟项 | 现状 | 对应版本 |
|---|---|---|
| §10 可视化大屏 | ✅ 交付 | v1.2.0 |
| §11 AI 助手 | ✅ 交付(mock → real LLM) | v1.2.0 / v1.2.1 |
| §14.9-14.11 每日/周复盘 | ✅ 交付 | v1.2.0 |
| §12 多用户 / 鉴权 | ✅ 交付(默认关) | v1.3.0 |
| §10.2 datasrc news 真实源 | ✅ 交付 | v1.3.2 |
| filter 节点 `contains_any` | ✅ 交付 | v1.3.3 |

### E-1.4 v1.1 → v1.3.4 期间补的"小项"

- perf M1/M2/M3(post-PR #1 review 推动)(TODOS.md "Completed")
- DX_REVIEW P0(2 个)+ P1(3 个)+ P2(2 个)全部 DONE(TODOS.md 历史段)
- fe1 M4 + M4-EXT 全部交付(FE1_PLAN.md 已审)
- fe2 M1-M3 全部交付;M4 抛光 3 项中 2 项 shipped,1 项仍可选(SSE 集成测试)

### E-1.5 DX_REVIEW 状态

`DX_REVIEW.md` 是 2026-06-17 v0.1 时代的快照。其 P0/P1/P2 在 v1.0.0 / v1.1 期间**绝大部分**已修:

- P0 2/2 done(Makefile + README 修正)
- P1 3/3 done(OpenAPI / 健康检查错信息 / 错误信息友好)
- P2 2/2 done(makefile / 跨平台)

也就是说:**当前生产不再有 DX_REVIEW 列举的 P0**。但文档本身没"已处理"标记,是这次调研读它时第一件困惑的事。

---

## E-2 闭环是否成立

按 4 个问题拷问:

| 问题 | 答案 | 证据 |
|---|---|---|
| **起点** | `openspec/changes/astock-selector-system/proposal.md` 定义了"为什么做";`tasks.md` 定义了"做什么";每次 PR 增量更新 | proposal 是设计早期文档,任务状态以 tasks.md checkbox 为唯一真源 |
| **终点** | 代码实现 + 测试覆盖 + CHANGELOG 记录 + DX_REVIEW 已修复 | 11 个维度在主文档 §2.1 表中体现 |
| **核对** | `tasks.md` 是 task 状态唯一真源;`TODOS.md` + `CHANGELOG.md` 持续累计 | 实际验证:v1.0.0 → v1.3.4 的所有合并记录可见 |
| **接续** | `proposal.md` §3 列出"v1 不做"项,§5/§10/§11/§12/§14.9 推迟到 v1.1,大部分在 v1.2/v1.3 收回 | 推迟项在 v1.2/v1.3 命中 |

**结论:演进闭环基本成立**;但**几处"真源同步"在抖**(见 E-3)。

---

## E-3 断点 / 风险

### E-3.1 README 路线图滞后于实际状态

README §8 路线图:

> ⏳ Phase 4 — Visualization dashboard (tasks.md §10)
> ⏳ Phase 5 — AI assistant (tasks.md §11)
> ⏳ Phase 6 — Auto daily / weekly review (tasks.md §14.8-14.11)
> ⏳ Phase 7 — E2E + ops handbook + compliance (tasks.md §15-16)

但实际代码中:

- §10 dashboard:`position.Service` + `executor.DashboardStats` 已存在
- §11 AI assistant:`ai.Service` + OpenAI/Mock provider 已存在(v1.2.1)
- §14.8-14.11:`review.Service` + `review.Scheduler` 已存在
- §15-16:README + LICENSE + CHANGELOG + OPERATIONS 已存在

**README 路线图与实现不同步,新读者照着 README 会以为还没做。**

### E-3.2 没做新一轮 `/plan-devex-review`

`DX_REVIEW.md` 是 v0.1 快照,现状 v1.3.4 的 DX 状态没有"验证过"。如果新接入外部依赖或者改了对外 API,**没人用 devx review 的标尺验证一遍**。

### E-3.3 TODOS.md 的"已完成"段累积长,影响可读性

`TODOS.md` 现在 158 行,顶部是 v1.1-deferred(全 ✅),中部是 datasrc/executor/nodes/perf/fe1/fe2(全 ✅),底部是 Completed 段(包含 perf M1/M2/M3 + 三段 historical)。**全是已完成**,但条目占用了文档大部分空间。

### E-3.4 前端 `types/orchestrator.ts` 手工同步

后端改了 `model/recommendation.go` 加字段,前端类型不会自动更新——frontend/src/types/orchestrator.ts 是手工维护的。**没有**从 OpenAPI spec 自动生成 TypeScript 的脚本(`openapi-typescript` 之类未引入)。

### E-3.5 多用户 RBAC 数据模型已建,代码路径只走单 admin

`user` / `role` / `ai_audit` 表已建(`model/user.go`),`auth.Service` 只跑"Bootstrap 第一个 admin + 登录"流程。多用户场景("admin / editor / viewer 三种角色 + 策略按 user_id 隔离")**没有 RBAC 中间件、没有策略级 access control**。

> 数据模型已多用户就绪,代码路径只走单实例。要做真实多用户要重写 `auth.Middleware` + 跨表加 `user_id`。

### E-3.6 fe2 M4"可选"项仍保留——后端 SSE 集成测试未做

TODOS.md "fe2 M4 打磨(可选)" 段:

- [ ] 详情页 Esc 回列表 / Ctrl+R 重试 键盘快捷键
- [ ] LogStreamViewer 暂停时 header 视觉提示
- [ ] 后端 SSE 集成测试(handler_test.go SSE frame 解析)

前两项未实现,第三项未写。属于"剩余可选项",不影响功能。

### E-3.7 proposal §10 Open Questions(210-218)未关闭

proposal.md 末尾 10 个 Open Questions(行情源选型 / K 线深度 / 回测范围 / 多用户 / 部署形态 / 通知渠道 / 数据合规 / AI 供应商 / AI 改动粒度 / 多标签 UI),在 v1.3.4 的实现里**部分**有答案(例如 #4 多用户→ v1.3 部分交付,#1 行情源→ eastmoney / sina / akshare,#6 通知→ feishu / dingtalk / wecom),但**没有**回填到 proposal.md 关闭这些问题。

### E-3.8 CHANGELOG.md 风格一致性

`CHANGELOG.md` 48KB,Keep-a-Changelog 1.1 风格,版本 v1.0.0 → v1.3.4 累计记录完整。但**头部更新频率依赖每次 PR 作者自觉**——没有"release checklist" 强制要求。

---

## E-4 重排候选(外部参考)

| # | 候选 | 触发条件(假设) | 不做的代价 |
|---|---|---|---|
| 1 | 跑一轮新的 `/plan-devex-review` | 任何对外接口变更或新外部依赖接入 | 现状 dx-review 是 v0.1 快照,不能反映 v1.3.4 现状 |
| 2 | 更新 README §8 路线图,把已交付的 §10/§11/§12/§14.9 改为 ✅ | 任何对外展示 / onboarding | 新成员按 README 会以为未交付 |
| 3 | 把 TODOS.md 的"Completed"段折叠或移到 CHANGELOG | 文档超过 200 行,可读性下降 | 不影响功能但降低效率 |
| 4 | 实现 §6.7 节点模板保存引用 | 用户提出"我想复用一个节点子树" | spec 里就是推迟,默认不做 |
| 5 | 实现 §8.4 多标签交集(`/recommendations/tags/intersect?tags=MORNING,AFTERNOON`) | 出现"需要按多标签筛选" | 现状前端用单 tag + 组合 |
| 6 | 实现 §8.5 CSV / Excel 导出 | 出现"需要导出推荐数据"的需求 | 推迟项 |
| 7 | 把前端 `types/orchestrator.ts` 改为从 OpenAPI spec 自动生成(`openapi-typescript`) | 任何后端 API 字段变更 | 手工同步容易漏 |
| 8 | 给多用户 RBAC 做一次实际 e2e 验证(2 个 user + 不同策略隔离) | 真的要让多人共用 | 数据模型已就绪,代码改动只在中间件 |
| 9 | 把 fe2 M4 三项全部交付(详情页 hotkey / LogStreamViewer 暂停头视觉 / SSE handler 集成测试) | 任何前端抛光需求 | 可选小项 |
| 10 | 在 proposal.md 末尾加"决策回填"段,把 v1.3.4 实现对 10 个 Open Questions 的答案记录下来 | 任何对外 spec 审查 | 现状"question 形而上",没有"decision 形而下" |
| 11 | 加 PR 模板中的"是否更新 CHANGELOG / 是否更新 README 路线图" checklist | PR 流程 review 需求 | 现状靠 reviewer 记得 |
| 12 | 把 §16 合规与免责(投资风险提示)做成"前端强制显示"(首次访问必须确认) | 法律 / 用户体验需求 | 现状只在 README + LICENSE 提及,前端没有强制声明 |

---

## E-5 引用

- 主文档:§E 在总览中的位置
- A-§:产品主链路的演进状态
- B-§:模块边界随版本演进(ai/auth/review/position 是后期加的)
- C-§:数据流的演进(§9 perf 是后期加的,§10 大屏是后期加的)
- D-§:可靠性机制的演进(断路器 + retry + 心跳等)
