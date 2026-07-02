## ADDED Requirements

### Requirement: 多对多时段标签

系统 MUST 在 `recommendation_tag` 关联表中记录每条推荐的一个或多个时段标签，枚举值固定为 `MORNING`（早盘）/`AFTERNOON`（尾盘）/`NO_POST`（无后盘）/`REVIEW`（复盘）。

#### Scenario: 一条推荐挂多个标签
- **WHEN** 推荐 R 由编排流程的 `session_tag` 节点判定同时属于 早盘 与 复盘
- **THEN** `recommendation_tag` 表插入两行：`(R.id, MORNING, source=AUTO_NODE)` 与 `(R.id, REVIEW, source=AUTO_NODE)`

#### Scenario: 同票同日叠加标签
- **WHEN** 股票 600519 在 2025-06-09 早盘被方案 A 推荐、尾盘被方案 B 推荐、复盘被方案 C 推荐
- **THEN** 数据库中 `recommendation` 表有 3 条该票当日记录，每条带各自的时段标签；按 (stock_code, date) 聚合查询时 3 个标签均可见到

### Requirement: 标签来源可追溯

系统 MUST 在 `recommendation_tag` 中记录 `source` 字段，取值 `AUTO_NODE` / `MANUAL` / `AI_AGENT`，用于审计与统计。

#### Scenario: 手动加标签
- **WHEN** 用户在推荐详情页对某条推荐补打"复盘"标签
- **THEN** 系统写入 `(rec_id, REVIEW, source=MANUAL, tagged_at=now)`

### Requirement: 按标签聚合查询

系统 MUST 提供按标签统计的 API，例如"过去 30 天被标 早盘 且 复盘 的股票数量"。

#### Scenario: 多标签交集查询
- **WHEN** 前端调用 `GET /api/recommendations/tags/intersect?tags=MORNING,REVIEW&range=30d`
- **THEN** 系统返回同时具备这两个标签的股票去重列表
