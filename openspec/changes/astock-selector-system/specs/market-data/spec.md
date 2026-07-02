## ADDED Requirements

### Requirement: 统一数据源接口

系统 MUST 暴露统一的行情接口 `internal/datasource`，至少包含 `GetQuote / GetKLine / GetFundamental / GetNews` 四个方法，参数与返回类型稳定。

#### Scenario: 通过统一接口取行情
- **WHEN** 节点调用 `datasource.Manager.GetQuote(ctx, "600519")`
- **THEN** 返回结构化的 `Quote{Price, Bid5, Ask5, Volume, TS}`，且不关心底层是东方财富还是新浪

### Requirement: 多实现可插拔

系统 MUST 支持至少 `eastmoney`、`sina` 两种实现，并通过 `datasource.Manager` 路由；新实现通过注册方式接入。

#### Scenario: 切换默认数据源
- **WHEN** 运维在 `config.yaml` 把 `default_provider` 从 `eastmoney` 改为 `sina`
- **THEN** 系统无需重启（或平滑重启）后所有数据请求走 sina 实现

### Requirement: 限流与降级

系统 MUST 对每个数据源实现令牌桶限流 + 失败降级到次级源；同一接口在 5 秒内连续 3 次失败则熔断 60 秒。

#### Scenario: 主源失败降级
- **WHEN** eastmoney 接口超时
- **THEN** 系统自动 fallback 到 sina 拉取同一数据，并在日志中记录 `provider_fallback`

### Requirement: K 线缓存

系统 MUST 在 Redis 中按 `(stock_code, period, trade_date)` 缓存 K 线结果 24 小时，避免重复拉取。

#### Scenario: 命中缓存
- **WHEN** 同一只股票同一周期 K 线在 1 小时内被请求两次
- **THEN** 第二次直接走 Redis，毫秒级返回
