# 租户隔离与用户身份

状态：已实现（2026-11-26，迁移 `000012_tenants.sql`）。

## 1. 背景与原则

租户之间完全隔离：存储、环境、应用、模块、构建、发布、提测单、代码源、构建适配器、GitHub App、制品访问令牌都归属一个租户，其他租户看不到、引用不了。

用户主键与登录方式解耦：用户 ID 由 Forge 生成，飞书只是一种登录身份，为以后接入其他登录方式做准备。

项目还在前期开发测试阶段，这次改造不做向后兼容：不保留旧接口地址、旧字段和旧角色名。迁移只负责把现有数据转换到新结构。

已确定的决策（2026-11-26）：

| 问题 | 决定 |
| --- | --- |
| SuperAdmin 范围 | 平台级角色，可以在控制台切换进入任意租户查看和操作 |
| 环境编码 | 按租户隔离，每个租户都可以有 `prod`、`staging` |
| 构建适配器、GitHub App | 都归租户 |
| 用户身份表 | 和租户隔离一起做 |

## 2. 数据模型

### 2.1 租户

- `tenants(id, name, status, sort_order)`：`status` 为 `active` 或 `disabled`。停用租户后，该租户用户的会话立即失效，客户端拉包不受影响。
- `tenant_feishu_apps(tenant_id, app_id, app_secret)`：租户的飞书登录配置，一个租户一个飞书自建应用，`app_id` 全平台唯一且创建后不能修改。`app_secret` 用 secretbox 加密。
- 原来的 `feishu_tenants` 表删除，数据拆到这两张表里。

### 2.2 用户与身份

- `users(id, tenant_id, name, email, avatar_url)`：用户只属于一个租户，ID 格式 `user-<16 位十六进制>`。
- `user_identities(id, user_id, provider, issuer, subject, profile)`：`(provider, issuer, subject)` 唯一。飞书身份的 `provider` 是 `feishu`，`issuer` 是飞书 App ID，`subject` 是 open_id，`profile` 存 union_id、飞书 user_id、英文名、工号等飞书资料。
- 登录时按身份查用户，查不到就在租户下新建用户和身份。
- 删除 `users` 上的 `feishu_*` 列、`department_ids` 列和 `feishu_users` 表。
- 迁移时为现有用户重新生成 ID，并改写所有引用用户的外键列。

### 2.3 会话

- `auth_sessions(session_token_hash, user_id, identity_id, active_tenant_id, ...)` 取代 `feishu_auth_sessions`。登录方式的 token（飞书 user access token 和 refresh token）存在会话上，用于通讯录搜索。
- `active_tenant_id` 是会话当前所在的租户，登录时等于用户所属租户。
- 迁移时删除所有现有会话，所有用户重新登录一次。

### 2.4 角色

- `user_roles(user_id, role)` 取代 `global_role_assignments`。角色为：
  - `SuperAdmin`：平台级。管理租户，可以进入任意租户，在任意租户内拥有全部权限。
  - `TenantAdmin`（原 `PlatformAdmin`）、`ReleaseAdmin`、`Auditor`：只在用户所属租户内生效。
- 管理租户只有 SuperAdmin 能做。
- 平台初始化时第一个登录的用户成为 `SuperAdmin`。之后每个租户里还没有 `TenantAdmin` 时，第一个登录的用户成为该租户的 `TenantAdmin`。

### 2.5 归租户的根表

以下表新增 `tenant_id not null references tenants(id)`，并加 `unique (tenant_id, id)`：

- `storage_configurations`
- `environments`
- `applications`
- `code_source_connections`
- `code_source_app_configs`：主键改为 `(tenant_id, provider)`
- `build_adapters`
- `artifact_access_tokens`

`modules` 冗余一列 `tenant_id`。根表之间的引用改成带 `tenant_id` 的组合外键，数据库层就保证引用不跨租户：

- `environments (tenant_id, storage_configuration_id) → storage_configurations`
- `modules (tenant_id, application_id) → applications`
- `modules (tenant_id, code_source_connection_id) → code_source_connections`
- `modules (tenant_id, build_adapter_id) → build_adapters`
- `modules (tenant_id, auto_build_environment_id) → environments`

其余表（构建、发布、提测单、渠道、签名密钥、健康数据、统计等）通过所属应用、模块或环境间接归属租户。

### 2.6 环境

- `environments.id` 是新主键，格式 `env-<16 位十六进制>`；`code` 改为 `unique (tenant_id, code)`。
- 子表的 `environment_code` 列全部改为 `environment_id`，迁移时按编码换成新 ID。
- 应用和环境的绑定 `application_environment_bindings` 冗余 `tenant_id`，用组合外键保证两者同租户。应用、模块、存储的负责人也用组合外键限定为同租户用户。
- 对外接口仍然用环境编码：控制台接口在当前租户内解析，客户端接口在应用所属租户内解析，返回值里也是编码。

### 2.7 应用 slug

`applications.slug` 保持全平台唯一。客户端拉包地址按 slug 找到应用，就能确定租户，不用带租户参数。对象存储路径按构建记录 ID 区分，多个租户共用一个 bucket 也不会冲突。

## 3. 请求链路

### 3.1 当前租户

- 鉴权中间件从会话取出用户和 `active_tenant_id`，用 `domain.WithTenant` 放进请求 context。repository 用 `domain.TenantScopeFromContext` 读取当前范围：
  - 租户范围：按租户过滤。
  - 系统范围（`domain.WithSystemScope`）：不过滤，只给 worker、发布健康判定、拉包统计等后台任务，以及客户端接口和 webhook 在确定租户之前查找应用、连接时使用。
  - 两者都没有：需要租户的查询直接返回 `domain.ErrTenantScopeMissing`，漏设范围会在测试里暴露，不会静默返回全部数据。
- SuperAdmin 调用 `PUT /api/v1/auth/session/tenant` 切换当前租户；其他用户调用返回 403。

### 3.2 查询隔离

- 所有列表按当前租户过滤。
- 按 ID 读取、修改、删除其他租户的数据返回 404，不返回 403。
- 构建、发布、提测单等子资源通过所属应用或模块判断租户。

### 3.3 选人

- 应用成员、负责人只能是当前租户的用户。
- `GET /api/v1/users` 搜索当前租户里已有的用户。
- `GET /api/v1/feishu/users/search` 用当前用户的飞书 token 搜通讯录，只在当前租户就是用户所属租户时可用（SuperAdmin 进入其他租户时不可用）。
- `POST /api/v1/feishu/users` 把通讯录里选中的人写成当前租户的用户（按飞书身份查找或新建），返回 Forge 用户 ID。表单里的负责人、成员字段只传 Forge 用户 ID，不再带飞书字段。

### 3.4 客户端与 webhook

- 拉包、启动上报、下载产物：按 slug 找应用，再在应用所属租户内按编码找环境。
- 制品访问令牌只能访问本租户的应用。
- GitLab webhook 按连接 ID 路由，不变。
- GitHub webhook 改为 `/api/v1/webhooks/github/:tenantId`，旧地址删除。GitHub App 的 manifest 和安装流程使用当前租户。

### 3.5 后台任务

worker 领取构建、发布健康判定、拉包统计、构建日志清理都按记录处理，不依赖当前租户。

## 4. 迁移现有数据

迁移 `000012_tenants.sql` 在一个事务里完成：

1. 每条 `feishu_tenants` 生成一个同 ID、同名称、同状态的租户和飞书应用配置。
2. 用户归属：只有一个租户时全部归它；有多个租户时，按用户最近一次会话的租户归属，没有会话的用户归 `sort_order` 最小的租户。
3. 所有业务数据归 `sort_order` 最小的租户。
4. 从 `users` 和 `feishu_users` 生成飞书身份，`issuer` 取用户所属租户的 App ID。
5. 重新生成用户 ID 和环境 ID，改写所有引用列。
6. 角色：`PlatformAdmin` 改名为 `TenantAdmin`，写入 `user_roles`。
7. 删除旧表和旧列，清空会话。

这次表结构变化很大，旧版本进程不能和新库共存：部署时先停掉所有 API 和 worker 实例，再启动新版本（启动时自动执行迁移）。

## 5. 控制台

- 顶栏显示当前租户；SuperAdmin 可以切换，切换后清空查询缓存并回到首页。
- 「飞书租户」页改为「租户」页，只有 SuperAdmin 能看到：新建租户时一起填飞书 App ID 和 App Secret。
- 选人组件：选中通讯录里的人时先调用 `POST /feishu/users` 换成 Forge 用户 ID。SuperAdmin 进入其他租户时只能搜本地用户。
- 角色名 `PlatformAdmin` 改为 `TenantAdmin`。

## 6. 测试

- 迁移：单租户和多租户两种数据各跑一次，核对用户、环境 ID 改写后外键完整，飞书身份齐全。
- 隔离：两个租户各建一套存储、环境（同名 `prod`）、应用、模块。逐个接口验证列表看不到对方的数据、按 ID 访问返回 404、跨租户引用写入失败。
- 客户端：两个租户的同名环境拉包互不影响；租户 A 的令牌访问租户 B 的应用被拒绝。
- 角色：新租户第一个登录的用户成为 `TenantAdmin`；非 SuperAdmin 不能切换租户；SuperAdmin 切换后只看到目标租户的数据。
