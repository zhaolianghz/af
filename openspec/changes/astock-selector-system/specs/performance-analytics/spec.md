## ADDED Requirements

### Requirement: 收盘后批量回算

系统 MUST 在每个交易日 15:30（收盘后）触发 `PerformanceJob`，对所有"未到期"的推荐回算 T+1 / T+3 / T+5 收益与累计收益，写入 `performance_snapshot` 表。

#### Scenario: 计算 T+1 收益
- **WHEN** 推荐 R 在 2025-06-09 给出，加入区间 10.00 - 10.50
- **THEN** 在 2025-06-10 收盘后系统写入 `performance_snapshot`：`t1_close=10.80, t1_return=0.0476`

### Requirement: 胜率与回撤

系统 MUST 计算并暴露以下指标：`累计胜率`（推荐以来达到加入区间上沿或更高的占比）、`T+1 胜率`、`T+3 胜率`、`T+5 胜率`、`最大回撤`、`平均持有收益`。

#### Scenario: 方案胜率排行
- **WHEN** 前端调用 `GET /api/analytics/win-rate?dimension=strategy&range=90d`
- **THEN** 系统按方案聚合返回 `{ strategy_code, total, t1_win_rate, t3_win_rate, t5_win_rate, cumulative_win_rate, max_drawdown }`

### Requirement: 多维聚合

系统 MUST 支持按 `strategy / session_tag / industry / stock` 维度聚合胜率与收益。

#### Scenario: 按时段标签聚合
- **WHEN** 前端调用 `GET /api/analytics/win-rate?dimension=session_tag&range=30d`
- **THEN** 系统按 `MORNING/AFTERNOON/NO_POST/REVIEW` 分别返回胜率，支持单票多标签的"任一匹配"与"全部匹配"两种口径

### Requirement: 缺数补跑

系统 MUST 在启动时检查 `performance_snapshot` 是否有断点（如停服 / 补数据），若有则在后台触发补跑。

#### Scenario: 启动补跑
- **WHEN** 系统启动时发现 2025-06-08 的快照缺失
- **THEN** 自动在后台触发该日所有方案的复算，并在日志中记录 `perf_backfill`
