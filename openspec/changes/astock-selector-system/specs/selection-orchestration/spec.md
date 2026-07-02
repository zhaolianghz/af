## ADDED Requirements

### Requirement: 可视化节点画布

系统 MUST 提供一个浏览器端的可视化画布，允许用户通过拖拽创建节点、用有向边连接节点，并实时保存为 DAG（有向无环图）JSON。

#### Scenario: 创建节点
- **WHEN** 用户从节点面板拖拽一个"技术指标"节点到画布
- **THEN** 系统在画布上创建该节点实例并写入当前方案的 `dag_json`，且支持撤销 / 重做

#### Scenario: 连接两个节点
- **WHEN** 用户将节点 A 的输出端口拖到节点 B 的输入端口
- **THEN** 系统创建一条带数据契约的有向边，且若端口类型不匹配则阻止连接并提示

### Requirement: 节点类型与数据契约

系统 MUST 内置至少以下节点类型，且每类节点需声明 `input` / `output` 的 JSON Schema：`data_source`（行情/K线/财务/新闻）、`indicator`（MA/EMA/MACD/KDJ/BOLL/成交量等）、`filter`（量价/形态/行业）、`rank`（打分/排序）、`dedupe`、`session_tag`（自动判定时段）、`persist`（落库到 recommendation）、`notify`（推送）。

#### Scenario: 行情节点产出五档数据
- **WHEN** 用户配置 `data_source` 节点为"实时五档"
- **THEN** 节点的 output schema 声明为 `{ stock_code, price, bid5[], ask5[], ts }`，下游节点按此契约消费

### Requirement: 实时调试与节点级日志

系统 MUST 支持对当前画布进行"试运行"，逐节点展示入参 / 出参 / 耗时 / 错误，且不影响生产数据。

#### Scenario: 试运行某节点
- **WHEN** 用户在画布上右键某节点选择"试运行到此处"
- **THEN** 系统在沙箱执行从入口到该节点的链路，右侧抽屉显示每个节点的耗时与样本输出

### Requirement: 节点复用与模板

系统 MUST 支持将任意节点保存为"自定义节点模板"，并在新建方案时引用。

#### Scenario: 引用自定义节点模板
- **WHEN** 用户从模板库拖入一个自定义"换手率>5%"筛选节点
- **THEN** 系统在画布上实例化该模板，参数沿用模板默认值且可在实例上覆写
