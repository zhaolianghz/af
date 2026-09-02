# AF 后端四维审计报告

- 日期：2026-07-02
- 范围：`backend/internal/*` 全模块（auth / middleware / router / config / datasource / calendar / executor / orchestrator / strategy / notify / ai / openapi / perf / position / review / settings）
- 维度：①正确性 ②并发/崩溃 ③安全 ④健壮性
- 方法：源码逐文件精读 + 跨模块 grep（goroutine/recover、SQL 注入、除零、type assertion、map 竞态）定点复核

> **透明声明**：本次审计原本由 6 个分模块子代理并行执行，但其结果仅存于一个中途被中断（API 400）的会话上下文中，**未落盘**。重启后无法读取。本报告由我**在当前会话重新逐模块审计源码**得出，每条发现都标注了可核验的 `file:line` 与根因，未沿用任何无法溯源的旧结论。recap 点名的两条高危（EventBus 崩溃、KDJ 全 NaN）已用源码重新确认并补全影响链。

> **第二轮已落地修复（2026-07-02，#4~#13）**：
> - **#4 production 拒绝裸奔** — `config.Validate()` 在 `app.env=production && auth.enabled=false` 时拒启动；dev 默认不变；main.go 的 "auth disabled" 提示升级为 Warn。新增 `TestProductionRequiresAuth`。
> - **#5 角色 fail-closed** — `roleCode` 改为 `(string, error)`：RoleID 行缺失/查询失败返回错误并拒绝登录（不再默认 Admin）；RoleID=0 仅在"唯一活跃用户"（bootstrap 首管理员）时映射 Admin，其余归 viewer。
> - **#9 RequireRole** — 新增 `auth.RequireRole(roles...)` + `auth.Role(c)`；路由层在 auth 启用时对 `/settings/*` 与 `POST /notify/test` 施加 admin-only。/strategies 读写拆分需动 handler 注册结构，留待后续。
> - **#7 VolumeRatio** — 改为标准量比语义：当前量 / 前置 n 根均量（原实现求和 n-1 根却除以 n，系统性低估）。warm-up 相应变为前 n 根 NaN；测试同步重写。
> - **#8 turnover_rate** — 不再静默输出假 0：节点 Run 时返回明确 ParamError（流通股本数据源未接入），保留 Schema 枚举以免破坏 DAG 校验/前端，但试运行会显式失败而非污染筛选结果。
> - **#10 cron 稳定性** — `SchedulerConfig.BaseCtx` 挂 server root ctx（关停即取消在跑的 cron run）；fire goroutine 加 recover。main.go 新增 rootCtx 并在收到信号时立即 cancel。
> - **#11 CORS 配置化** — `middleware.CORS(origins ...)` 接受配置来源（`server.cors_origins` YAML 或 `SERVER_CORS_ORIGINS` 逗号分隔 env）；空则回落 localhost dev 默认。router.Options 增加 `CORSAllowedOrigins`。
> - **#12 secret 脱敏** — 临时 JWT secret 不再打印明文，只打印 sha256 前 8 字节指纹。
> - **#13 calendar 负缓存** — DB 错误（非 NotFound）也写入 30s TTL 的 transient 负缓存条目，防 DB 抖动惊群；到期自动重查。`NextTradingDay` 返回非交易日兜底的签名改造涉及 perf 调用方，暂缓。

> **第一轮已落地修复（2026-07-02）**：
> - **#2 EventBus panic** — `Publish` 现在在 RLock 临界区内发送（与 unsubscribe 的 close 互斥），`dropped` 改 `atomic.Uint64`。新增 `TestEventBus_ConcurrentPublishAndUnsubscribe` 回归（200 轮 × 8 churn goroutine，`-race` 通过）。
> - **#6 worker 无 recover** — `dispatch` 的 worker goroutine 加 `defer recover()`，panic 降级为失败 `NodeResult`；并用 LIFO defer 保证 panic 时仍释放并发槽位（防死锁）。新增 `TestExecutor_NodePanicRecovered` + `TestExecutor_NodePanicDoesNotDeadlockConcurrency`。
> - **#1 KDJ 全 NaN** — `kdj.go` 的 K/D 改用标准递推 SMA（`(m-1)/m·prev + 1/m·cur`，种子 50），删除会 NaN 中毒的滚动 `MA` 路径。修后 K/D/J 在首个有效 RSV 之后全为有限值，节点层不再输出假信号 0。重写 `TestKDJ_KnownWindow`/`TestKDJ_FlatWindow`（加有限值/单调/恒等断言）。
> - **#3 钉死 bug 的测试** — 删除 `TestKDJ_AllNaNForCurrentImplementation`，替换为 `TestKDJ_FiniteAfterWarmup`（反向回归：预热期后必须有限值，重现 all-NaN 即报错）。
> - 全量 `go test ./... -race` 通过（exit 0）。

---

## 严重度汇总

| # | 级别 | 维度 | 模块 | 位置 | 一句话 |
|---|------|------|------|------|--------|
| 1 | ✅ 已修复 | 正确性 | executor/nodes | `indicators/kdj.go` + `indicators/ma.go` + `nodes/indicator.go` | KDJ 对所有股票恒为 0，KDJ 类选股规则全部失效 |
| 2 | ✅ 已修复 | 并发/崩溃 | orchestrator | `orchestrator/eventbus.go` | SSE 客户端断连触发 send-on-closed-channel panic，拖垮整个进程 |
| 3 | ✅ 已修复 | 测试文化 | executor/nodes | `indicators/indicators_test.go` | 用测试**钉死**了 KDJ 的全 NaN bug，`go test` 永远绿 |
| 4 | 🟠 P1 | 安全 | auth/config | `config/config.go` (default) + `cmd/server/main.go:270` + `router/router.go` | `auth.enabled` 默认 false，默认部署下全部写接口裸奔 |
| 5 | 🟠 P1 | 安全 | auth | `auth/service.go:109-117` | 角色解析失败→fail-open 升级为 Admin |
| 6 | ✅ 已修复 | 健壮性 | orchestrator | `orchestrator/executor.go` dispatch | worker goroutine 无 recover，任一节点 panic 即进程崩溃（放大 #2） |
| 7 | 🟡 P2 | 正确性 | executor/nodes | `indicators/volume_ratio.go:24-26` | 量比求和 n-1 根、除以 n，系统性低估 |
| 8 | 🟡 P2 | 正确性 | executor/nodes | `nodes/indicator.go` turnover_rate 分支 | turnover_rate 恒为 0（float shares 未实现），筛选静默失效 |
| 9 | 🟡 P2 | 安全 | auth/router | `auth/middleware.go` | role 写入上下文但**从未校验**，"multi-user ready" 名不副实 |
| 10 | 🟡 P2 | 健壮性 | executor | `executor/scheduler.go:210` | cron 触发用 `context.Background()` + 无 recover，关停时泄漏且 panic 致命 |
| 11 | 🟢 P3 | 安全 | middleware | `middleware/cors.go` | CORS 白名单硬编码 localhost，无法配置化上生产 |
| 12 | 🟢 P3 | 安全 | cmd/server | `cmd/server/main.go:277-282` | 空 JWT secret 时生成临时密钥并 **WARN 打印到日志** |
| 13 | 🟢 P3 | 健壮性 | calendar | `calendar/trading_day.go` `lookup`/`NextTradingDay` | DB 错误不缓存→每次都打 DB；366 天兜底返回非交易日 |

---

## 详细发现

### 🔴 #1 KDJ 全 NaN（端到端影响链已确认）

**文件**：`backend/internal/executor/nodes/indicators/kdj.go`、`indicators/ma.go`、`nodes/indicator.go`

**根因链**（以默认 n=9, m1=3, m2=3, L≥9 为例）：
1. `kdj.go` 中 `rsv[0..7] = NaN`（预热区，i 从 n-1=8 起才有定义）。
2. `k = SMA(rsv, m1)` → `MA(rsv, 3)`。`ma.go` 的 `MA`：
   ```
   sum := 0.0
   for i := 0; i < period; i++ { sum += values[i]; out[i] = NaN }
   out[period-1] = sum / float64(period)
   ```
   `values[0..2]` 即 `rsv[0..2]` 全是 NaN → `sum = NaN`。
3. 之后 `sum += values[i] - values[i-period]`，`NaN + 有限 = NaN`，**`sum` 永久为 NaN**。
4. 故 `k` 全 NaN → `d = SMA(k, m2)` 全 NaN → `j` 全 NaN。

**节点层放大**：`nodes/indicator.go` 用 `lastFinite(series)` 取最后一个非 NaN 值，找不到则返回 `0`。KDJ 全 NaN ⇒ `lastFinite` 返回 0 ⇒ 对**每一只股票**输出 `kdj_k=0, kdj_d=0, kdj_j=0`。

**业务后果**：任何 `kdj_j < 20`（超卖买入）类筛选规则会把**所有股票**判为命中（0<20），或 `kdj_j > 0` 判为全不命中——KDJ 维度的选股信号退化为常量。

**附带算法错误**：标准 KDJ 的 K 本应是递推 `K[i]=(m1-1)/m1·K[i-1]+1/m1·RSV[i]`（K 在首个有效 RSV 处用 50 种子），当前代码用滚动 SMA，语义本身就不对。

**修法**：**已实施**（见本报告顶部“本轮已落地修复”）：`kdj.go` 的 K/D 改用递推 SMA、种子 50，删除 `SMA(rsv,m1)` 滚动路径；RSV 在 `[n-1, L)` 连续有限，故可直接在 `n-1` 种子后前向递推。

---

### 🔴 #2 EventBus 发送已关闭 channel → 进程 panic

**文件**：`backend/internal/orchestrator/eventbus.go:92-104`

**竞态路径**：
1. SSE handler 经 `executor.DrainEvents` → `bus.Subscribe(runID)`，`defer unsub()`。
2. 客户端断连 / ctx 取消 → `unsub()`（`eventbus.go` 的 unsubscribe 闭包）取**写锁**，把 sub 从 slice 移除并 `close(s.ch)`。
3. 与此**并发**：worker 经 `rc.Bus.Publish(...)`（`orchestrator/executor.go:199/292/377/403/413`，均在 worker goroutine 内）取 `RLock` 拷贝 subs 快照（含正被移除的 sub）→ **`RUnlock()`** → `select { case s.ch <- evt: default: drop }`。
4. 若 `s.ch` 在"拷贝后、发送前"被 close → **`panic: send on closed channel`**。

**为何致命**：`Publish` 在 `RUnlock()`（第 96 行）**之后**才发送（第 99 行），发送与 close 不互斥。而 #6 中 worker goroutine **没有 recover**，gin 的 `Recovery` 中间件只兜住 handler goroutine，兜不住 worker。→ **任一客户端在 run 进行中断开连接，即可崩掉整个后端进程。**

**修法**（二选一）：
- 把发送循环移进 `RLock` 临界区（非阻塞 `select{default}` 不会因此阻塞/死锁，发送与 close 即互斥）：
  ```go
  func (b *memBus) Publish(evt Event) {
      b.mu.RLock()
      defer b.mu.RUnlock()
      subs := make([]*memSub, len(b.subs[evt.RunID]))
      copy(subs, b.subs[evt.RunID])
      for _, s := range subs {
          select { case s.ch <- evt: default: atomic.AddUint64(&b.dropped,1) }
      }
  }
  ```
  （`dropped` 改用原子计数，避免在 RLock 内再取写锁。）
- 或为每个 sub 加 `closed` 标志 + `recover` 兜底。

**修法**：~~把发送循环移进 `RLock` 临界区……~~ **已实施**（见本报告顶部“本轮已落地修复”）：`Publish` 在 `RLock` 内遍历发送，`dropped` 改原子计数；unsubscribe 的 `close` 与 publish 的发送互斥，send-on-closed-channel 不再可能。

**验证**：`go test ./internal/orchestrator/ -race -run TestEventBus_ConcurrentPublishAndUnsubscribe` 通过（已覆盖此竞态）。

---

### 🔴 #3 测试钉死了 KDJ 的数据腐败行为

**文件**：`backend/internal/executor/nodes/indicators/indicators_test.go:227` `TestKDJ_AllNaNForCurrentImplementation`

该测试**主动断言** `k/d/j` 全为 NaN，注释将其表述为"known limitation"，并称"pin the current behavior so the fix is visible as a test diff"。后果：
- `go test` 永远绿，CI 不会拦截。
- 任何人修复 #1 都会先被这个测试挡下，必须同步删除它——形成"破窗"。

这是比 #1 本身更严重的过程问题：**已知的数据正确性 bug 被制度化保护**。**已实施**（见本报告顶部“本轮已落地修复”）：删除该测试，替换为 `TestKDJ_FiniteAfterWarmup`。

---

### 🟠 #4 默认部署 API 全开放

**文件**：`config/config.go`（`auth.enabled` 默认 `false`）、`cmd/server/main.go:270`、`router/router.go`

- `auth.enabled` 默认 false；`main.go` 在 false 时打印 `"/api/v1 is open"` 并把 `AuthMiddleware` 留空（`nil`）。
- `router.go` 仅在 `opts.AuthMiddleware != nil` 时挂中间件，且通过 `api.Use` 只保护其后注册的路由（ping/healthz/openapi/login 正确保持公开）。
- 后果：未显式设置 `AUTH_ENABLED=true` 的部署，`POST /strategies`、`POST /strategies/:id/trial-run`、`/strategies/:id/ai/*`、`POST /reviews/generate`、`POST /notify/test`、`/settings/*` 全部**无需认证**即可调用。

**修法**：把默认值反转为 `true`（或在 `Validate()` 里当 `app.env=="production"` 且未启用 auth 时拒启动），避免"开箱即裸奔"。

---

### 🟠 #5 角色解析 fail-open 升级为 Admin

**文件**：`backend/internal/auth/service.go:109-117` `roleCode`

```go
func (s *Service) roleCode(ctx context.Context, roleID uint64) string {
    if roleID == 0 { return model.RoleCodeAdmin }      // 行 110-111
    var r model.Role
    if s.db...First(&r, roleID).Error == nil && r.Code != "" { return r.Code }
    return model.RoleCodeAdmin                          // 行 117
}
```

`roleID == 0` 或**任何** DB 查询失败/role 行缺失，都回落为 `Admin`。当前单 admin 场景下首个 admin 的 RoleID 多为 0，看似无害；但一旦引入多用户，一次 DB 抖动即可把普通用户**静默提权为 Admin**。配合 #9（role 从不校验），构成潜在越权链。

**修法**：`roleID == 0` 视为未分配角色→拒绝登录或归为最小权限角色；DB 查询失败应返回错误而非默认 Admin（fail-closed）。

---

### 🟠 #6 worker goroutine 无 recover

**文件**：`backend/internal/orchestrator/executor.go:240-246`

```go
dispatch := func(nodeID string) {
    inFlight++
    go func() {
        sem <- struct{}{}
        res := e.runOneNode(ctx, rc, req, nodeID, payloads, &resultsMu, completed)
        <-sem
        done <- res            // runOneNode 内含 rc.Bus.Publish（见 #2）
    }()
}
```

`runOneNode` 内调用 `nodeImpl.Run` 与 `Bus.Publish`，二者任一 panic（如 #2 的 send-on-closed-channel，或某个指标节点的 nil deref）都**没有 recover**，goroutine 内未恢复的 panic 直接崩溃进程。调度循环本身（`for inFlight > 0 { res := <-done ... }`）经审查是正确的（注释说明已修复旧 errgroup 竞态），无死锁，但缺这层兜底。

**修法**：~~在 `go func(){ defer recover() ... }()` 把 panic 降级为单节点失败……~~ **已实施**（见本报告顶部“本轮已落地修复”）：worker 加 `defer recover()`，并用 LIFO 第二层 defer 确保 panic 时仍 `<-sem` 释放并发槽位（防死锁）。

---

### 🟡 #7 VolumeRatio 求和/除数不一致

**文件**：`backend/internal/executor/nodes/indicators/volume_ratio.go:24-26`

```go
for j := i - n + 1; j < i; j++ { sum += volumes[j] }   // j∈[i-n+1, i-1] → n-1 根
avg := sum / float64(n)                                  // 却除以 n
```

求和 `n-1` 根、除以 `n`，系统性低估 `1/n`（n=5 时低估 20%）。注释自相矛盾（说 "avg_volume_n"）。

**修法**：`avg := sum / float64(n-1)`，或循环改为 `j <= i` 并除以 `n`（取决于"是否含当日"的语义约定，需与前端/策略对齐）。

---

### 🟡 #8 turnover_rate 恒为 0

**文件**：`backend/internal/executor/nodes/indicator.go`（`case "turnover_rate"`）

```go
shares := make([]float64, len(vols))   // 全 0
row["turnover_rate"] = lastFinite(indicators.TurnoverRate(vols, shares))
```

`shares` 是零值切片，`TurnoverRate` 必为 0/NaN → `lastFinite` 返回 0。注释承认 "No float-shares series available yet; emit 0"。任何 `turnover_rate` 维度的筛选规则当前**完全失效**（恒为 0）。

**修法**：要么接入真实流通股本数据源，要么在节点 Schema 上把 turnover_rate 标记为 `unavailable` 并在 filter 节点拒绝依赖它，避免静默误用。

---

### 🟡 #9 role 写入上下文但从不校验

**文件**：`backend/internal/auth/middleware.go`

中间件把 `ctxRole` 存入 gin context，代码注释多处声称 "future per-role enforcement"，但全仓没有任何 `RequireRole`/`c.GetBool`/角色判断。当前单 admin 下危害有限，但与 #5 叠加构成多用户场景的越权隐患。

**修法**：新增 `RequireRole(roles...)` 中间件，至少保护 `/settings/*`、`/strategies` 写、`/notify/test` 等高危端点。

---

### 🟡 #10 cron 触发用 Background + 无 recover

**文件**：`backend/internal/executor/scheduler.go:210`

```go
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), ...)
    defer cancel()
    if _, err := s.cfg.Executor.Execute(ctx, ...); err != nil { ... }
}()
```

两个问题：
- `context.Background()` 不挂在 server 关停 ctx 下，优雅关停时正在跑的 cron run 不会随进程退出而被取消（仅受自身超时约束）。
- 无 recover → 同 #6，cron run 内任何 panic（含 #2）崩溃进程。

**修法**：把 server ctx 作为 parent；包一层 recover。

---

### 🟢 #11 CORS 白名单硬编码

**文件**：`backend/internal/middleware/cors.go`

允许源写死 `localhost:5173/3000`。开发期合理，但无法通过配置上生产（注释也承认）。应改为从 `config` 读取。

### 🟢 #12 临时 JWT secret 打印到日志

**文件**：`cmd/server/main.go:277-282`

空 `AUTH_JWT_SECRET` 时生成临时密钥并以 `zap.Warn` 打印明文。方便调试，但生产环境等于把签名密钥写进日志→token 可伪造。建议改为只打印密钥指纹/长度，强制生产显式配置。

### 🟢 #13 calendar 边角问题

**文件**：`backend/internal/calendar/trading_day.go`

- `lookup`：DB 非 `RecordNotFound` 错误不缓存 → DB 抖动时每个 `IsTradingDay` 调用都打 DB（无 singleflight，潜在惊群）。建议对错误也加短 TTL 负缓存。
- `NextTradingDay`/`PreviousTradingDay`：366 天兜底，找不到时**返回 `from`（可能非交易日）**，调用方若假定结果是交易日会产生调度偏差。建议返回 `(time.Time, bool)`。

---

## 已审查且状态良好的部分（避免重复返工）

- **SQL 注入**：全仓 `Where/Raw/Exec` grep，未发现字符串拼接；`strategy_service.go:222/225` 的 LIKE 用了 `escapeLike`，安全。
- **除零**：`review/service.go`、`perf/calculator.go`、`executor.go:615`、`indicators/{ma,boll,ema,volume_ratio}.go` 均有 `>0`/`==0` 守卫。
- **熔断器**：`datasource/breaker.go` 与 `notify/circuit.go` 两份独立实现，状态机（closed/open/half-open + 单探针）正确，锁使用规范，`gcLocked` 原地压缩在锁内安全。
- **notify manager failover**：`notify/manager.go` 主备遍历 + 去重 + 每次重试独立超时，实现干净；`/notify/test` 仅走已配置 channel，无 SSRF。
- **调度核心并发**：`orchestrator/executor.go` 的 ready/pending/completed 调度经审查无死锁、无 spawn-vs-wait 窗口（已用主 goroutine 串行化调度修复了旧 errgroup 竞态）。唯一缺口是 #6 的 recover。
- **缓存**：`datasource/cache.go` `GetOrCompute` 缓存错误/解码错误都正确回退重算，best-effort 写不污染调用方。
- **AI 客户端**：`ai/openai.go` 响应体 `LimitReader(1MB)` 防内存放大，API key 仅在 Authorization 头、不入日志；`baseURL` 来自运维配置无 SSRF。（注：AI 输出为"建议 DAG"，执行前仍经 `Validate` + 注册表校验，节点类型受控；prompt 注入风险受限于已注册节点，定级 P3。）

---

## 建议修复顺序

1. **先修 #2 + #6**（进程稳定性，10 分钟级改动，阻断崩溃）。
2. **再修 #1 + #3**（数据正确性 + 删除钉死测试，影响所有 KDJ 选股）。
3. **#4 + #5 + #9** 一起做一轮安全加固（默认开 auth、角色 fail-closed、加 RequireRole）。
4. **#7 + #8**（指标正确性，需与策略语义对齐后定方案）。
5. 其余 P3 收尾。

每条都可独立成一个最小 PR；#2/#6 建议附 `-race` 并发测试防回归。
