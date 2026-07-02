## ADDED Requirements

### Requirement: 基础账号登录

系统 MUST 提供基于账号 + 密码的本地登录，返回 JWT。

#### Scenario: 登录成功
- **WHEN** 用户提交正确账号密码
- **THEN** 系统返回 JWT（24h 有效）并写入 HttpOnly Cookie

#### Scenario: 登录失败
- **WHEN** 用户提交错误密码
- **THEN** 系统返回 401 且不暴露"用户存在与否"的细节

### Requirement: 方案访问隔离

系统 MUST 保证不同用户之间的方案 / 推荐 / 执行历史互不可见。

#### Scenario: 用户 A 无法查看用户 B 的方案
- **WHEN** 用户 A 用 `GET /api/strategies/{B 的方案 ID}`
- **THEN** 系统返回 404

### Requirement: 角色权限

系统 MUST 支持 `admin` / `editor` / `viewer` 三种角色，权限依次递减。

#### Scenario: viewer 尝试编辑方案
- **WHEN** viewer 角色用户调用 `PUT /api/strategies/{id}`
- **THEN** 系统返回 403
