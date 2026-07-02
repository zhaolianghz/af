## 0. v1 交付优先级（2025-06 研判后调整）

> **A7-INT 收尾更新（2026-06）**：本轮已勾选 §5 全、§6.1-6.6、§7.1-7.5、§8.1-8.3/8.6、§14.1-14.8；推迟到 Phase 3 的有 §6.7、§8.4-8.5、§14.9-14.11。Phase 1（§1-§4、§7.5）当时已交付，本轮未重新勾选。

**v1 第一批（2-3 个月）核心交付顺序**（解决 hermes 痛点）：

1. **B：确定性执行引擎**（任务 7）—— cron + DAG + 日志 + 失败可重试，**核心执行路径 100% 确定性，禁止 LLM 状态依赖**（hermes 记忆错乱问题的对策）。
2. **A：多通道通知**（任务 7.5）—— 飞书 / 钉钉 / 企业微信 三选一必发；**微信通道不作为 v1 首选**（用户已确认不稳）。
3. **E：多渠道行情数据**（任务 3.7-3.10）—— 至少 3 路行情源 + 实时健康监控 + 单源熔断 + 自动切换 + 异常告警（防止单渠道获取失败或异常——用户原话）。
4. **C：3-5 个内置策略模板**（任务 14）—— 开箱即用 + AI 解释（用户知识储备不足的对策）。
5. **D：每日/每周自动复盘报告**（任务 14.9-14.11）—— 通过多通道推到用户（用户没有复盘习惯的对策）。
6. **F：可视化大屏**（任务 10）—— 持仓 / 推荐 / 胜率 / 热力图 / 复盘入口（用户原话"必须做"）。
7. **G：AI 对话式改方案**（任务 11）—— 自然语言 → DAG 操作 → 两阶段提交（用户原话"必须做"）。

**v1 不做（推迟到 v2）**：仅多用户权限 / 登录注册（任务 12，**用户已确认"先自己用"，不设计**）。

## 1. 项目骨架与基础设施

- [ ] 1.1 初始化 Go 后端工程（`go mod init`，Gin + GORM + zap + viper + robfig/cron/v3）
- [ ] 1.2 初始化前端工程（Vite + React 18 + TypeScript + ReactFlow + ECharts + Tailwind）
- [ ] 1.3 编写根目录 `docker-compose.yml`（app / web / mysql 8 / redis 7）
- [ ] 1.4 编写 `.env.example`、配置文件加载与启动脚本
- [ ] 1.5 编写基础 CI（lint + test + build）

## 2. 数据模型与迁移

- [ ] 2.1 设计 `strategy` / `strategy_version` / `node_definition` 表并落 migration
- [ ] 2.2 设计 `recommendation` / `recommendation_tag` 表（多对多标签 + source 字段）
- [ ] 2.3 设计 `run` / `run_log` 表（执行历史 + 节点级日志）
- [ ] 2.4 设计 `trading_calendar` 表与缓存表
- [ ] 2.5 设计 `performance_snapshot` 表
- [ ] 2.6 设计 `user` / `role` / `ai_audit` 表
- [ ] 2.7 编写 GORM model + 单元测试

## 3. 行情数据源适配层

> **硬约束**：必须支持**至少 3 路**行情源（eastmoney + sina + akshare），单源失败/异常必须**自动切换**到备用源；任何"全源不可用"必须**告警**（走 7.5 通知通道），不可静默失败。

- [ ] 3.1 定义 `internal/datasource` 接口（Quote / Kline / Fundamental / News）
- [ ] 3.2 实现 `ds/eastmoney`（主源）：实时五档 + 日线 + 财务摘要
- [ ] 3.3 实现 `ds/sina`（备源 1）：实时五档 + 日线
- [ ] 3.4 实现 `ds/akshare`（备源 2）：日线 + 财务 + 龙虎榜
- [ ] 3.5 接入 Redis K 线缓存（key = stock_code:period:trade_date，TTL 24h）
- [ ] 3.6 实现限流器（令牌桶）+ 失败降级 + 单源熔断 + 自动切源
- [ ] 3.7 编写 `datasource.Manager` 路由与配置驱动切换（main / fallback1 / fallback2 顺序可配）
- [ ] 3.8 **数据源健康监控**：每个源每 60s 心跳一次，写入 `datasource_health(source, last_ok, last_fail, fail_count_5m, status)` 表
- [ ] 3.9 **每源独立熔断**：单源 5 分钟内失败 > 5 次自动熔断 10 分钟；熔断期间不向该源发请求
- [ ] 3.10 **全源告警**：3 路全部失败时，立即通过 7.5 通知通道发"行情源全挂"告警（不走 cron，下一次执行触发即可）
- [ ] 3.11 **行情源切换日志**：每次切换记录 `provider_switch` 事件（from / to / reason / ts），可在前端"系统状态"页查看
- [ ] 3.12 单元测试覆盖：单源失败切换 / 双源失败告警 / 熔断恢复 / 数据一致性（不同源对同一只票的报价偏差 < 0.5%）

## 4. 交易日历

- [ ] 4.1 引入交易日历同步任务（从 tushare/akshare 拉到 `trading_calendar`）
- [ ] 4.2 提供 `IsTradingDay(date) / NextTradingDay(date) / PreviousTradingDay(date)` API
- [ ] 4.3 单元测试覆盖节假日 / 周末 / 调休

## 5. 策略管理 (strategy-management)

- [x] 5.1 实现 `POST/GET/PUT/DELETE /api/strategies`（CRUD + 软删除）  ← A7-BE1
- [x] 5.2 实现版本管理（保存时自增 version，写入 `strategy_version`）  ← A7-BE1
- [x] 5.3 实现克隆（生成新 code、节点 ID 重映射）  ← A7-BE1
- [x] 5.4 实现 JSON 导入导出 + Schema 校验  ← A7-BE1
- [x] 5.5 前端"方案列表 / 详情 / 编辑"页  ← A7-FE1

## 6. 编排画布与 DAG 引擎 (selection-orchestration)

- [x] 6.1 设计节点接口 `type Node interface { Run(ctx, in) (out, err) }` + JSON Schema  ← A7-BE1
- [x] 6.2 实现内置节点：`data_source` / `indicator` / `filter` / `rank` / `dedupe` / `session_tag` / `persist` / `notify`  ← A7-BE1
- [x] 6.3 实现技术指标（MA / EMA / MACD / KDJ / BOLL / 量比 / 换手率等）  ← A7-BE1
- [x] 6.4 实现 DAG 执行器（拓扑排序 + 并发调度 + 错误短路）  ← A7-BE1
- [x] 6.5 前端 ReactFlow 画布（拖拽 + 连线 + 撤销重做）  ← A7-FE1
- [x] 6.6 实现"试运行到节点" + 节点级日志抽屉  ← A7-BE1 + A7-FE2
- [ ] 6.7 节点模板保存与引用  ← 推迟到 Phase 3

## 7. 执行引擎 (execution-engine)

> **硬约束**：核心执行路径 100% 确定性（cron + DAG + 数据库 + 日志），**禁止 LLM 状态依赖**。LLM 仅用于节点内的"指标解释"和"AI 助手"路径，不参与执行状态机。

- [x] 7.1 接入 `robfig/cron/v3` 调度器  ← A7-BE2
- [x] 7.2 实现 `POST /api/runs`（手工触发）+ SSE 实时日志推送  ← A7-BE2
- [x] 7.3 执行前交易日历判定 + 交易时段判定  ← A7-BE2（`internal/calendar/trading_day.go` + `Scheduler.guardTradingSession`）
- [x] 7.4 写 `run` / `run_log`，失败可重试（同一 run_id 重试不丢历史）  ← A7-BE2（`POST /api/v1/runs/:id/retry` → 新 run + `retry_of` 链）
- [x] 7.5 前端"执行历史 / 日志查看"页  ← A7-FE2

## 7.5 多通道通知（v1 核心）

> 解决用户痛点：微信通道不稳；hermes 通知经常不发。v1 必须保证"任务跑 → 必发 → 用户收得到"。

- [ ] 7.5.1 设计 `notify` 通道接口（`type Channel interface { Send(ctx, msg) error }`）
- [ ] 7.5.2 实现**飞书** webhook 通道（首选，主推）
- [ ] 7.5.3 实现**钉钉** webhook 通道（备选）
- [ ] 7.5.4 实现**企业微信（WeCom）** webhook 通道（备选）
- [ ] 7.5.5 通道失败重试：指数退避 × 3 次，全部失败写告警表
- [ ] 7.5.6 通道熔断：单通道 5 分钟内失败 > 5 次自动熔断，自动切到备用通道
- [ ] 7.5.7 通知内容模板：早盘推荐 / 尾盘推荐 / 复盘报告 / 执行失败告警
- [ ] 7.5.8 用户可在策略配置中指定"主通道 + 备用通道"
- [ ] 7.5.9 单元测试覆盖：发送成功 / 失败重试 / 熔断切换 / 全部失败

> 微信通道（个人微信/服务号）v1 不实现：用户已确认不稳定；如需支持，v2 用 itchat / Wecom 协议单独评估。

## 8. 推荐落库与时段标签 (recommendation + session-tagging)

- [x] 8.1 `persist` 节点写入 `recommendation` 表  ← A7-BE1
- [x] 8.2 `session_tag` 节点按当前时间 / 用户配置写入 `recommendation_tag`（支持多标签）  ← A7-BE1
- [x] 8.3 实现 `GET /api/recommendations` 多维筛选  ← A7-BE2
- [ ] 8.4 实现 `GET /api/recommendations/tags/intersect` 多标签交集  ← 推迟到 Phase 3（前端多标签过滤当前用 `?tag=` 单值 + 前端组合）
- [ ] 8.5 CSV / Excel 导出  ← 推迟到 Phase 3
- [x] 8.6 前端"今日推荐 / 历史推荐 / 复盘"页  ← A7-FE2（`/recommendations` 列表页）

## 9. 绩效引擎 (performance-analytics)

- [ ] 9.1 收盘后定时任务：拉今日 K 线 / 财务收盘价
- [ ] 9.2 计算 T+1 / T+3 / T+5 / 累计收益与胜率
- [ ] 9.3 写入 `performance_snapshot`，幂等
- [ ] 9.4 实现多维聚合 API（strategy / session_tag / industry / stock）
- [ ] 9.5 启动时缺数补跑
- [ ] 9.6 单元测试覆盖收益 / 回撤 / 胜率边界

## 10. 可视化大屏 (visualization-dashboard)

- [ ] 10.1 大屏路由 `/dashboard`，1920×1080 自适应
- [ ] 10.2 "当前持仓"卡片 + 实时行情 WebSocket 刷新
- [ ] 10.3 "今日新增推荐" + "方案胜率排行 TOP 10"
- [ ] 10.4 T+1 / T+3 / T+5 胜率热力图（方案 × 时段标签）
- [ ] 10.5 "复盘"入口跳转 `/review`
- [ ] 10.6 首屏 Redis 缓存，延迟 < 2s

## 11. AI 对话式方案管理 (ai-assistant)

- [ ] 11.1 设计 LLM 操作意图 JSON Schema（add_node / update_param / delete_node / create_strategy / clone / disable / import）
- [ ] 11.2 实现 `POST /api/ai/chat`（OpenAI 兼容协议 + 配置驱动供应商）
- [ ] 11.3 实现 schema 校验 + 失败重试一次
- [ ] 11.4 前端 AI 助手抽屉（消息流 + 操作预览 diff）
- [ ] 11.5 画布高亮"待应用变更" + 二次确认弹窗
- [ ] 11.6 写 `ai_audit` 审计表
- [ ] 11.7 单元测试覆盖：合法 / 非法 / 拒绝 diff / 多轮澄清

## 12. 用户与权限 (v2 推迟)

- [ ] 12.1 实现账号注册 / 登录 / JWT（HttpOnly Cookie）
- [ ] 12.2 资源级访问隔离（strategy / recommendation / run 都按 user_id 过滤）
- [ ] 12.3 角色 admin / editor / viewer 中间件
- [ ] 12.4 前端登录页 + 路由守卫

## 14. v1 业务内容（内置策略 + 强制复盘）

> 解决用户痛点：知识储备不足、没复盘习惯。v1 必交付。

- [x] 14.1 设计"内置策略模板"模型（`strategy_template` 表）：模板编码、名称、行业、参数默认值、AI 解释话术  ← A7-BE2
- [x] 14.2 模板 1：**早盘放量突破** —— 9:30-10:00 期间涨幅 2-5% + 量比 > 2 + 突破 20 日均线  ← A7-BE2（`morning_volume_breakout.json`）
- [x] 14.3 模板 2：**尾盘主力净流入** —— 14:00-14:50 主力净流入 > 1000 万 + 量比 > 1.5  ← A7-BE2（`afternoon_main_inflow.json`，v1 降级为"涨幅+量比"近似）
- [x] 14.4 模板 3：**MACD 金叉 + 站上 5 日均线** —— 经典技术形态  ← A7-BE2（`macd_golden_cross.json`）
- [x] 14.5 模板 4：**龙虎榜机构买入** —— 昨日龙虎榜机构净买入 + 当日开盘不破前低  ← A7-BE2（`dragon_tiger_institutional.json`，v1 降级为新闻标题近似）
- [x] 14.6 模板 5：**低估值高分红** —— PE < 15 + 股息率 > 3% + ROE > 10%  ← A7-BE2（`low_valuation_high_dividend.json`）
- [x] 14.7 实现"启用模板 → 一键生成方案"功能（无需画 DAG）  ← A7-BE2（`POST /api/v1/strategies/from-template/:code`）
- [x] 14.8 每个模板附 AI 解释：用户启用后，AI 自动生成"该模板适用场景 / 注意事项 / 风险点" 100-200 字  ← A7-BE2（`ai_explanation` 字段硬编码在每个 JSON 内；**非 LLM 动态生成**，是产品侧预写文案）
- [ ] 14.9 实现"每日 15:30 自动复盘" cron：今日推荐数、命中数、当日涨幅 TOP 3 / BOTTOM 3  ← 推迟到 Phase 3
- [ ] 14.10 实现"每周日 20:00 自动周复盘" cron：本周命中数 / 周胜率 / 错过的票 / 下周建议  ← 推迟到 Phase 3
- [ ] 14.11 复盘报告通过 7.5 多通道推送  ← 推迟到 Phase 3

## 15. 部署与交付

- [ ] 15.1 编写 README（一键启动 / 默认账号 / 行情源说明 / 通知通道配置）
- [ ] 15.2 编写 CHANGELOG / 升级指南
- [ ] 15.3 编写 e2e：登录 → 启用模板 → 手工执行 → 多通道通知收到 → 自动复盘
- [ ] 15.4 编写运维手册：交易日历维护、行情源切换、备份恢复
- [ ] 15.5 LICENSE 选择（建议 AGPL-3 或 Apache-2.0）

## 16. 合规与免责 (重要!)

- [ ] 16.1 登录页 / 推荐页 / 复盘页统一显示"投资有风险，本系统仅供研究，不构成投资建议"
- [ ] 16.2 推荐数据默认仅自己可见，不开放分享接口
- [ ] 16.3 AI 解释中提示用户"AI 助手可能产生错误，请人工复核后再应用"
