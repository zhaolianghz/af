## ADDED Requirements

### Requirement: 方案 CRUD

系统 MUST 允许用户对"荐股方案"（strategy）进行创建、查询、修改、删除操作；方案的最小字段为 `id, code, name, description, dag_json, version, status, tags[]`。

#### Scenario: 创建方案
- **WHEN** 用户提交新方案表单（编码 `MACD_GOLD_V1`、名称 "MACD 金叉 V1"）
- **THEN** 系统在 `strategy` 表插入一条新记录，状态为 `draft`，并返回方案 ID

#### Scenario: 软删除方案
- **WHEN** 用户对方案 X 选择"停用"
- **THEN** 系统将 `status` 置为 `disabled`，但保留历史推荐 / 胜率数据可查

### Requirement: 版本管理

系统 MUST 在方案的 `dag_json` 变更时生成新版本号，且能查看历史版本与 diff。

#### Scenario: 修改方案生成新版本
- **WHEN** 用户保存对方案 DAG 的修改
- **THEN** 系统在 `strategy_version` 表追加一条新记录，`version` 自增，并保留旧版本可回滚

### Requirement: 克隆与导入导出

系统 MUST 支持方案的"克隆为新方案"、JSON 导入与导出。

#### Scenario: 克隆方案
- **WHEN** 用户在方案 A 上选择"克隆"
- **THEN** 系统创建新方案 B，`code` 自动追加 `_COPY_n`，DAG 节点 ID 重新生成，避免与 A 冲突

#### Scenario: 导入 JSON
- **WHEN** 用户上传一份方案 JSON 文件
- **THEN** 系统校验 JSON Schema 通过后插入新方案；校验失败时返回错误位置
