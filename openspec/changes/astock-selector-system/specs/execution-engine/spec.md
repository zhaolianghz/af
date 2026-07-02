## ADDED Requirements

### Requirement: 手工触发执行

系统 MUST 为每个方案提供"立即执行"按钮，并在执行过程中实时返回节点级日志与进度。

#### Scenario: 手工执行方案
- **WHEN** 用户在方案详情页点击"立即执行"
- **THEN** 系统在 3 秒内启动一次 `run`，返回 `run_id`，并通过 SSE 或 WebSocket 推送节点级日志

#### Scenario: 执行结果落库
- **WHEN** 一次手工执行完成
- **THEN** 系统将本次 run 写入 `run` 表，状态 `success/failed`，产出推荐写入 `recommendation` 表

### Requirement: Cron 定时调度

系统 MUST 支持为方案配置 Cron 表达式，并在表达式命中时自动执行。

#### Scenario: 设置每日 9:35 调度
- **WHEN** 用户为方案 X 设置 cron `35 9 * * *`
- **THEN** 系统在每个交易日的 9:35 启动一次自动执行；非交易日跳过

### Requirement: A 股交易日历感知

系统 MUST 在执行前判定"今天是否为 A 股交易日 + 当前是否在交易时段内"，非交易日不执行；交易日历从 `tushare` / `akshare` 缓存到 DB 离线维护。

#### Scenario: 周末不执行
- **WHEN** 调度命中时间为周六
- **THEN** 系统判定为非交易日，跳过本次调度并记录 `skip_reason=non_trading_day`

#### Scenario: 节假日不执行
- **WHEN** 调度命中时间为国庆假期
- **THEN** 系统通过交易日历表判定为非交易日并跳过

### Requirement: 执行历史与可重试

系统 MUST 保留所有执行的 `run` 记录（id, strategy_id, trigger_type, status, started_at, finished_at, log_url, error），并支持失败重试。

#### Scenario: 失败重试
- **WHEN** 某次 run 状态为 `failed`
- **THEN** 用户可点击"重试"，系统用相同参数重新执行并写入新 run 记录（保留旧 run 用于回溯）
