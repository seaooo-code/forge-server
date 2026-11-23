# 提测单方案

本文描述「提测单」这项能力，以及它如何替换原来的「按 identifier 定向灰度」和「预览通道」。第一期已实现。第 9 节第二步中，代码已不再读写待删除的列，删除列和收紧约束的迁移还没做；第 11 节第二期尚未实现。

## 1. 背景

现在测试同学要在 App 上拿到某个需求的包，靠的是定向灰度：

- 一条定向发布只能对应**一个** `identifier`（`release_routes` 的唯一索引 `idx_release_routes_identifier_active`）。三个测试同学测同一个包，研发要建三条定向发布；中途有人加入，还要再建一条。研发和测试需要反复交换标识。
- 一个需求经常涉及多个 bundle，每个模块都要单独发一次定向。
- 模块的「预览通道」在自动构建成功后，**用分支名作为 identifier** 建定向发布（原 `internal/service/build_trigger.go` 的 `previewReleaseInput`，已删除）。分支名很容易猜到，知道分支名就能拿到未发布的包。
- 曾经设想让测试在 App 调试面板里直接「选择定向」。这需要一个用访问令牌鉴权的列表接口，而访问令牌打包在 App 里，等于公开，任何人都能列出并加载所有定向构建。

## 2. 目标与边界

目标：

- 一个需求一张提测单，包含「哪些模块用哪个构建」和「哪些用户来测」。
- 测试同学可以自己加入提测单，研发换了构建，测试刷新即可拿到，不再需要传话。
- 用提测单替换按 identifier 定向和预览通道。

不在范围内：

- Forge 不区分正式和测试环境，也不感知晴空塔。客户端在什么环境、请求哪个平台，由客户端决定；Forge 只按通用规则解析请求并返回。
- 不做强鉴权。`userId` 由客户端上报，提测单控制的是「默认给谁下发哪个包」，不是保密（见第 10 节）。
- common 与业务包同时改动时的依赖重建放在第二期（见第 11 节）。

## 3. 概念

### 3.1 提测单

| 字段 | 说明 |
| --- | --- |
| 名称、关联链接 | 例如需求单号、飞书项目链接 |
| 应用、环境 | 提测单属于一个应用的一个环境；环境必须开启「允许提测单」（见第 6 节） |
| 条目 | 多个「模块 + 分支 → 构建」，一个模块最多一条 |
| 成员 | 一组 `userId`，每个可带备注，例如「张三-测试号 2」 |
| 有效期 | 默认 7 天，最长 30 天，可延长；到期自动失效 |
| 状态 | 进行中、已关闭；过期按 `expires_at` 判断，不单独改状态 |

### 3.2 条目

- 每个条目**必须绑定该模块的一个分支**，条目的构建只能是这个分支、同环境、状态为成功的构建。
- **同一模块的同一分支只能出现在一张进行中的提测单里**。分支已被其他进行中的提测单占用时，新增条目直接拒绝并提示是哪张单，不提供自动处理：分支代表需求本身，两张单抢同一个分支说明重复提测，应当由人决定保留哪张。
- 条目有两种模式：
  - **跟随**（默认）：该分支在这个环境下有新的成功构建（手动或自动触发都算）时，条目自动换成新构建（替代预览通道，见第 7 节）。
  - **固定**：构建不再变化，用于回归或复测某一次构建；可以随时切回跟随。
- 不同模块绑定的分支可以不同，所以各仓库不需要使用同一个分支名。

### 3.3 成员冲突规则

客户端请求 manifest 只带 `userId`，不带分支，所以必须保证一个 `userId` 在一个模块上只命中一张提测单：

**同一个 `userId` 不能同时属于两张都包含同一模块的进行中提测单。**

会造成冲突的操作有三种：往提测单里加成员、测试同学自己加入、往提测单里加条目（加入的模块恰好也在某个成员所在的另一张单里）。处理方式：

1. 操作先返回冲突清单，列出每个冲突的 `userId`、对方提测单和重叠的模块，不做任何修改。
2. 控制台弹窗展示清单，操作人确认「从对方提测单中移出这些 userId」后，带上确认标记重新提交。
3. 服务端在**同一个事务**里把这些 `userId` 从对方提测单移出，再完成本次操作；移出动作记入对方提测单的操作记录（谁、因为哪张单、什么时间）。
4. 操作人不确认，就什么都不改。

只移出会冲突的那张单里的这个 `userId`，不动对方提测单的条目和其他成员。对方提测单的创建人可以在操作记录里看到这次移出。

### 3.4 我的测试账号

控制台用户可以保存自己常用的 `userId`（按环境区分，因为不同环境的账号体系可能不同，数值可能重叠），加入提测单时一键带上，不用每次手输。

## 4. manifest 解析规则

客户端接口 `GET /api/v1/artifacts/applications/{slug}/environments/{env}/manifest` 的参数改为 `platform`、`runtimeVersion`、`userId`，删除 `identifier`。

每个模块按以下顺序取第一个命中的构建：

1. **提测单**：`userId` 非空，且存在满足以下全部条件的提测单：
   - 同应用、同环境，状态为进行中，`expires_at > now()`，所在环境开启了「允许提测单」；
   - `userId` 是成员；
   - 有该模块的条目，且条目构建的 `runtime_version` 等于请求的 `runtimeVersion`。
   
   按第 3.3 节的冲突规则，正常情况下一个 `userId` 在一个模块上最多命中一张提测单。为防止并发写入留下例外，解析时若仍命中多张，取该模块条目最近一次更新的那张，并记一条警告日志。
2. **比例灰度**：沿用现有规则，按 `userId` 和发布 ID 分桶。
3. **全量**：沿用现有规则。

都没有命中时，模块照常放进 `missingModules`。

**runtimeVersion 不一致**：条目的构建和客户端运行时版本不同时，这个条目对该客户端无效，按第 2、3 步继续解析，不报错。控制台在条目上显示构建的 runtimeVersion，方便研发判断。

### 4.1 响应变化

模块项新增 `source` 和 `testPlan`，删除 `identifier`：

| 字段 | 说明 |
| --- | --- |
| `source` | `testPlan`、`percentage` 或 `full`，说明这个模块是按哪条规则命中的 |
| `testPlan` | 只在 `source = testPlan` 时有值：`{id, name}` |
| `releaseId`、`releaseType`、`grayStrategy`、`grayPercentage` | 只在命中发布时有值；命中提测单时为空 |

顶层的 `identifier` 改为回显 `userId`。

## 5. 数据模型

按 `AGENTS.md` 的迁移规范新增增量迁移文件，语句可重复执行。

**`000004_test_plans.sql`**

```sql
create table if not exists test_plans (
    id uuid primary key,
    application_id text not null references applications(id) on delete cascade,
    environment_code text not null references environments(code) on delete restrict,
    name text not null,
    link text not null default '',
    status text not null check (status in ('open', 'closed')),
    expires_at timestamptz not null,
    created_by_user_id text not null references users(id) on delete restrict,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    closed_at timestamptz
);

create table if not exists test_plan_items (
    test_plan_id uuid not null references test_plans(id) on delete cascade,
    module_id text not null references modules(id) on delete cascade,
    branch text not null check (branch <> ''),
    mode text not null default 'follow' check (mode in ('follow', 'pinned')),
    build_record_id uuid not null references build_records(id) on delete restrict,
    updated_by_user_id text not null references users(id) on delete restrict,
    updated_at timestamptz not null default now(),
    primary key (test_plan_id, module_id)
);

create table if not exists test_plan_members (
    test_plan_id uuid not null references test_plans(id) on delete cascade,
    user_id text not null,              -- 客户端上报的 userId，不是 Forge 用户
    note text not null default '',
    added_by_user_id text not null references users(id) on delete restrict,
    created_at timestamptz not null default now(),
    primary key (test_plan_id, user_id)
);

create table if not exists user_test_accounts (
    owner_user_id text not null references users(id) on delete cascade,  -- Forge 用户
    environment_code text not null references environments(code) on delete cascade,
    user_id text not null,
    note text not null default '',
    created_at timestamptz not null default now(),
    primary key (owner_user_id, environment_code, user_id)
);

-- 提测单操作记录：条目和成员的增删、因冲突被移出、关闭、延长。
create table if not exists test_plan_events (
    id bigserial primary key,
    test_plan_id uuid not null references test_plans(id) on delete cascade,
    kind text not null,                 -- 例如 member_added、member_removed_by_conflict、item_updated
    detail jsonb not null default '{}', -- 例如 {"userId": "1001", "causedByTestPlanId": "…", "moduleIds": ["claw"]}
    actor_user_id text references users(id) on delete set null,  -- 构建回调触发的更新为空
    created_at timestamptz not null default now()
);

alter table environments
    add column if not exists allow_test_plans boolean not null default false;

create index if not exists idx_test_plan_members_user on test_plan_members(user_id);
create index if not exists idx_test_plan_items_module_branch on test_plan_items(module_id, branch);
create index if not exists idx_test_plans_open
    on test_plans(application_id, environment_code, expires_at)
    where status = 'open';
```

两条唯一性规则（第 3.2 节的「模块 + 分支」、第 3.3 节的「userId + 模块」）都依赖提测单的状态和有效期，这些条件分布在多张表里，无法用部分唯一索引表达，改为在服务层校验：所有修改条目和成员的操作，在事务里先对「应用 + 环境」加 advisory lock（`pg_advisory_xact_lock`），再检查冲突并写入，保证并发操作不会绕过规则。

限制（服务层校验）：每张提测单成员最多 200 个，`userId` 只允许 1 到 64 位的字母、数字和 `_ - .`；名称 1 到 100 字；分支名沿用现有的分支名校验。

## 6. 权限

| 操作 | 谁可以 |
| --- | --- |
| 新建、编辑、关闭、延长提测单；增删条目；增删任意成员 | 应用的 Owner、Developer、QA，以及全局 SuperAdmin、PlatformAdmin。新增动作 `test_plan:manage` |
| 加入、退出提测单（只能操作自己「我的测试账号」里同环境的 userId） | 任何已登录的控制台用户 |
| 开关环境的「允许提测单」 | 有 `environment:manage` 的用户 |
| 管理「我的测试账号」 | 用户本人 |

说明：

- 现在 Developer、QA 只有 `build:create`，不能发布。提测单只影响成员自己的下发，所以单独用 `test_plan:manage`，不放开 `release:create`。
- 测试同学不一定是应用成员，所以「加入」不要求应用角色。他能加入的只是自己声明的 userId，且只在开启了「允许提测单」的环境生效。
- 因冲突从其他提测单移出 `userId`，不需要对方提测单的额外权限：冲突只发生在同一应用内，管理成员的人本来就有该应用的 `test_plan:manage`；自己加入的人移出的是自己的测试账号，等同于自己退出。
- 「允许提测单」是环境的通用开关，默认关闭。Forge 不判断哪个环境是正式环境，由管理员按需打开。

## 7. 跟随分支（替代预览通道）

- 构建成功的回调（`BuildTriggerService`，手动和 webhook 构建都会触发）里，原来创建预览定向发布的逻辑改为：查找同应用、同环境、进行中且未过期的提测单里，模块相同、`branch` 等于构建分支、`mode = 'follow'` 的条目，把条目的构建换成这次构建，并记一条 `item_updated` 操作记录（操作人为空）。
- 按第 3.2 节，同一模块的同一分支最多属于一张进行中的提测单，所以一次构建最多更新一个条目。
- 只替换比当前条目更新的构建（按构建号判断），避免旧构建延迟回调时覆盖新构建。
- 模块配置里的「预览通道」开关（`auto_preview_release`、`auto_preview_ttl_hours`）已从接口和代码中删除，数据库列在第 9 节第二步删除。

## 8. 接口

控制台接口，飞书登录会话鉴权：

| 方法和路径 | 说明 |
| --- | --- |
| `GET /api/v1/test-plans?applicationId=&environment=&status=&memberUserId=` | 列表 |
| `POST /api/v1/test-plans` | 新建：名称、链接、应用、环境、有效期、初始条目和成员 |
| `GET /api/v1/test-plans/{id}` | 详情：条目（含分支、模式、构建号、提交、runtimeVersion）、成员、操作记录、依赖提示 |
| `PATCH /api/v1/test-plans/{id}` | 改名称、链接 |
| `POST /api/v1/test-plans/{id}/close`、`/extend` | 关闭；延长有效期 |
| `PUT /api/v1/test-plans/{id}/items/{moduleId}` | 设置条目：`branch`、`mode`，固定模式时带 `buildRecordId`（跟随模式取该分支最新的成功构建）；可带 `resolveConflicts` |
| `DELETE /api/v1/test-plans/{id}/items/{moduleId}` | 删除条目 |
| `POST /api/v1/test-plans/{id}/members`、`DELETE …/members/{userId}` | 增删成员（`test_plan:manage`）；新增可带 `resolveConflicts` |
| `POST /api/v1/test-plans/{id}/join`、`POST …/leave` | 用自己的测试账号加入、退出，`userIds` 为空表示全部；加入可带 `resolveConflicts` |
| `GET/POST/DELETE /api/v1/me/test-accounts` | 我的测试账号 |

**冲突响应**：新建提测单、设置条目、新增成员、加入这四类写操作，按第 3.2、3.3 节检查冲突：

- **分支被占用**：返回 `409`，`error = "test plan branch in use"`，带上占用的提测单 `{id, name}`。不能自动处理。
- **成员冲突**：请求没带 `resolveConflicts: true` 时返回 `409`，`error = "test plan member conflict"`，`conflicts` 为 `[{userId, testPlan: {id, name}, moduleIds: [...]}]`，不做任何修改。带上 `resolveConflicts: true` 后，服务端在同一事务里重新计算冲突，把这些 `userId` 从对方提测单移出，再完成本次操作，响应里的 `removed` 列出实际移出的内容。
- 重新计算的冲突可能和控制台展示的不同（两次请求之间有人改了提测单）。只要冲突仍然只是成员冲突，就按新的结果移出，并在 `removed` 里如实返回，控制台据此提示用户。

客户端接口只有第 4 节的 manifest 变化。

## 9. 删除按 identifier 定向

客户端还没有接入，现在删除没有兼容负担。按 `AGENTS.md`「删除列要拆成两次发布」的规则分两步。

**第一步（与提测单同一期）**

- 发布只保留全量和比例灰度：删除 `ReleaseGrayStrategyTargeted`、`ReleaseRouteTypeIdentifier`，以及 `ReleaseCreateInput.Identifier`、`ReleaseResolveInput.Identifier`。
- 迁移 `000005_drop_targeted_release.sql`：把生效中的定向发布和 identifier 路由标为失效（`status = 'inactive'`）。暂不改约束和列。
- manifest 删除 `identifier` 参数；`GET /api/v1/releases/resolve` 删除 `identifier`，只解析比例灰度和全量（它返回的是发布路由，不包含提测单）。
- OpenAPI 快照同步更新。

**第二步（下一次发布）**

代码已经不再读写 `release_routes.identifier`、`modules.auto_preview_release`、`modules.auto_preview_ttl_hours`（写入依赖列的默认值）。这一版部署后，再新增迁移完成下面两项：

- 收紧 `release_records`、`release_routes` 的检查约束，不再允许 `targeted` 和 `identifier`；删除 `idx_release_routes_identifier_active`、`idx_release_records_targeted_expiry`、`release_routes.identifier`。
- 删除 `modules.auto_preview_release`、`modules.auto_preview_ttl_hours`。

## 10. 安全说明

- `userId` 由客户端上报，知道某个成员 userId 的人可以冒用。提测单比按分支名定向更难猜，但不是访问控制。
- 产物文件在对象存储上可能是公共读（依赖注入需要），知道地址就能下载。提测单不改变这一点。
- 因此提测单的保护范围是「默认给谁下发」。约束手段是：环境开关默认关闭；成员数量上限；所有增删操作记录操作人和时间。
- 不提供任何用访问令牌鉴权的列表接口，客户端看不到提测单、成员或其他用户的构建。

## 11. common 与业务包同时改动（第二期）

Feature 模块构建时注入的是同环境下**已发布、生效中**的 common 产物（`ResolveBuildDependencies`），不是提测单里的 common 构建。一个提测单里同时放新的 common 和业务包时，业务包是基于旧 common 构建的，可能因为模块 ID 不匹配而崩溃。

- 第一期：条目里同时包含 Common 模块和它的 Feature 模块时，详情页提示「业务包未基于本提测单的 common 构建」，并显示两者的 runtimeVersion。
- 第二期：支持「基于指定 common 构建重新构建」，把提测单里的 common 构建作为依赖注入。

## 12. 控制台

- **提测单列表和详情**（导航放在「发布记录」旁）：详情页分条目、成员、操作记录三块。条目显示模块、分支、模式（跟随或固定）、构建号、提交、runtimeVersion；成员显示 userId 和备注。顶部显示有效期、延长和关闭按钮、加入链接，以及依赖提示。
- **冲突确认弹窗**：加成员、加入、加条目时收到成员冲突，弹窗按 userId 列出「将从提测单 B 移出（重叠模块 claw）」，确认后带 `resolveConflicts` 重新提交；完成后按响应的 `removed` 提示实际移出了什么。分支被占用时只提示占用的提测单并给出跳转链接。
- **我的测试账号**：在用户菜单里，按环境管理。
- **环境表单**：新增「允许提测单」开关和说明。
- **线上现状**：格子详情里列出覆盖该模块和环境的进行中提测单；去掉定向格子和「手动失效」操作。
- **删除定向相关界面**：发布弹窗（`BuildReleaseDialog`、`CreateReleaseDialog`）去掉「定向」选项；发布记录的筛选、标签和详情里的定向字段；模块表单的「预览通道」；登录页示意图里的「定向 qa-zhangwei」。

## 13. 客户端契约

1. 请求 manifest 时传 `platform`、`runtimeVersion`、`userId`。`userId` 用登录账号 ID，未登录时不传，此时只按全量下发。
2. 按响应的 `source` 和 `testPlan` 判断每个模块的来源。
3. 调试面板（只在非正式包开启）：
   - 显示当前 `userId`，带复制按钮，方便测试登记到「我的测试账号」；
   - 显示每个模块的构建号、分支、提交、来源和提测单名称；
   - 「刷新」按钮重新请求 manifest，研发换了构建后测试能马上拿到。
4. 客户端如果缓存 manifest，App 回到前台时和点「刷新」时都要重新请求，否则提测单的变更要等缓存过期才生效。

## 14. 实施步骤

1. 迁移 `000004`；领域模型、仓储、服务；manifest 解析（第 4 节）和对应测试。
2. 提测单、成员、我的测试账号的接口和权限；OpenAPI 快照；前端 `bun run gen:api`。
3. 构建成功回调改为更新跟随分支的条目。
4. 删除 identifier 定向第一步（迁移 `000005`、接口、前端界面）。
5. 控制台页面。
6. 第二期：删除旧列和约束；基于指定 common 重新构建。

## 15. 验证

- 仓储集成测试：runtimeVersion 不一致时回落到比例灰度和全量；过期、关闭、环境开关关闭时不命中；`userId` 为空时不命中。
- 冲突规则测试：
  - 同一模块的同一分支不能进入第二张进行中的提测单；第一张关闭或过期后可以。
  - 加成员、自己加入、加条目三种操作在不带确认时都返回冲突清单且不修改数据；带确认后对方提测单只移出冲突的 userId，条目和其他成员不变，并写入操作记录。
  - 两个事务并发给同一个 userId 加入两张包含同一模块的提测单，最终只有一张成功。
- 服务测试：权限（Owner、Developer、QA 能管理，非成员只能用自己的测试账号加入）；条目构建必须属于该模块、该分支、同环境、成功；成员上限和格式校验。
- 回调测试：跟随模式的条目只用更新的构建替换，旧构建的延迟回调不覆盖；固定模式的条目不变。
- 迁移测试：已有库升级后，原有的定向发布和 identifier 路由变为失效，全量和比例灰度不受影响。
- 控制台：提测单的增删改、加入退出、冲突确认弹窗和分支占用提示用预览验证。
- 端到端：两个 userId、两张提测单、三个模块，分别请求 manifest，核对每个模块的 `source` 和构建；再把其中一个 userId 确认加入另一张单，核对它从原提测单移出、manifest 结果随之变化。

## 16. 待确认

- 有效期上限定为 30 天是否合适。
- 成员上限 200 是否够用。
- 「允许提测单」默认关闭，现有的测试环境需要管理员在上线后手动打开一次。
