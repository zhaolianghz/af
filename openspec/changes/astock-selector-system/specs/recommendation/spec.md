## ADDED Requirements

### Requirement: 推荐结果落库

系统 MUST 在每次方案执行成功后，将推荐结果写入 `recommendation` 表，必含字段：`id, run_id, date, stock_code, stock_name, entry_price_low, entry_price_high, strategy_code, strategy_name, node_snapshot_json, created_at`。

#### Scenario: 落库一条推荐
- **WHEN** 编排 DAG 的 `persist` 节点输出一只股票 600519，加入区间 1680.00 - 1700.00
- **THEN** 系统在 `recommendation` 表插入对应记录，`node_snapshot_json` 存本次执行的节点链路与参数

### Requirement: 推荐查询与筛选

系统 MUST 提供按 `date / stock_code / strategy_code / session_tag` 单维与组合查询接口。

#### Scenario: 按日期 + 方案查询
- **WHEN** 前端调用 `GET /api/recommendations?date=2025-06-09&strategy_code=MACD_GOLD_V1`
- **THEN** 系统返回当日该方案产出的所有推荐，按 `created_at` 倒序

### Requirement: 导出 CSV / Excel

系统 MUST 支持将查询结果导出为 CSV 与 Excel 文件。

#### Scenario: 导出当日推荐
- **WHEN** 用户点击"导出 CSV"
- **THEN** 系统返回 CSV 文件，列顺序与 `recommendation` 字段一致
