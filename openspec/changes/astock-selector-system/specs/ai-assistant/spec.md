## ADDED Requirements

### Requirement: 对话式方案管理

系统 MUST 在方案编辑器提供常驻 AI 助手抽屉，支持用自然语言完成"创建 / 编辑 / 复制 / 删除 / 导入"选股方案，并把对话翻译为画布上的 DAG 操作。

#### Scenario: 自然语言创建方案
- **WHEN** 用户说"帮我写一个早盘 MACD 金叉 + 量比 > 2 的方案"
- **THEN** AI 返回结构化操作 `{action:"create_strategy", name, code, dag:[...]}`，前端在画布上**预览**新增节点

#### Scenario: 自然语言编辑参数
- **WHEN** 用户对方案 X 说"把 MACD 的快线从 12 改到 10"
- **THEN** AI 返回 `{action:"update_param", node_id, param:"fast_period", value:10}`，前端在画布上对应节点的参数框高亮显示"12 → 10"

### Requirement: 两阶段提交

系统 MUST 强制"LLM 输出 diff → 用户在画布预览 → 用户点应用"三步走才落库；任何破坏性操作需二次确认。

#### Scenario: 破坏性操作二次确认
- **WHEN** AI 输出 `{action:"delete_node", node_id:"n5"}` 涉及核心节点
- **THEN** 前端弹出确认框"将删除节点 n5 及其下游链路，确定吗？"，用户确认后才落库

#### Scenario: 用户拒绝 diff
- **WHEN** 用户对 AI 提出的 diff 点击"拒绝"
- **THEN** 系统不修改 `strategy.dag_json`，并把对话标记为"已拒绝"

### Requirement: 结构化输出与校验

系统 MUST 要求 LLM 输出符合预定义 JSON Schema 的"操作意图"，并由后端做 schema 校验，校验失败则要求 LLM 重试一次；重试仍失败则回到对话继续澄清。

#### Scenario: 输出不合规
- **WHEN** LLM 返回一段非 JSON 文本
- **THEN** 后端重试一次 LLM（带"请按 schema 输出"的提示）；仍失败则返回前端"AI 输出未通过校验，请重述需求"

### Requirement: LLM 供应商可切换

系统 MUST 通过 OpenAI 兼容协议接入 LLM，`.env` 中配置 `LLM_BASE_URL` / `LLM_API_KEY` / `LLM_MODEL` 即可切换供应商（OpenAI / DeepSeek / 通义千问 / Ollama）。

#### Scenario: 切换到本地 Ollama
- **WHEN** 运维把 `LLM_BASE_URL` 改为 `http://localhost:11434/v1`
- **THEN** 系统下一次对话请求走 Ollama，敏感数据不出本机

### Requirement: 操作审计

系统 MUST 把每次 AI 的"操作意图 JSON + 用户决策（应用/拒绝）+ 落库后的 dag_json diff" 写入 `ai_audit` 表，可追溯。

#### Scenario: 审计记录
- **WHEN** 用户应用了 AI 给出的 diff
- **THEN** `ai_audit` 表新增一行：`{user_id, strategy_id, intent_json, decision:"applied", dag_diff_json, ts}`
