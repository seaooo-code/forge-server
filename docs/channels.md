# 发布渠道（设计稿）

状态：后端已实现（迁移 `000008_release_channels.sql`），控制台见第 7 节。

## 1. 背景

单独打包的内测 App 需要拿到和正式版不同的包，但两者共用同一个应用、同一个环境、同一批构建。现有的维度都不合适：

- `runtimeVersion` 表示原生兼容级别，内测版和正式版通常相同。
- 提测单按 `userId` 圈人，有有效期，圈不住"装了内测 App 的所有人"。
- 独立环境可以做到，但构建绑定环境，同一个构建不能同时发到两个环境，每次都要重复构建。

渠道（channel）是在同一环境里给发布加的一个范围：内测 App 打包时写死一个渠道标识，拉包时上报；发布可以限定只对某个渠道生效。这是通用机制（Expo EAS Update 的 channel、CodePush 的 deployment 都是这个思路），不含任何公司专属逻辑，放进主线。

## 2. 概念

- **渠道**属于应用，用 `code` 标识，例如 `beta`。在所有绑定的环境里通用。
- **默认渠道**是 `code = ''`，不需要创建。现有的所有发布都属于默认渠道，不传渠道的客户端也只看默认渠道，行为和现在完全一致。
- **发布**多一个字段 `channel`。全量、比例灰度、转全量、停用、回滚都只在自己的渠道内生效。
- 构建与渠道无关。同一个构建可以同时发到默认渠道和 `beta`。

## 3. manifest 解析规则

接口 `GET /api/v1/artifacts/applications/{slug}/environments/{env}/manifest` 新增可选参数 `channel`。

客户端带了已注册的渠道 `C` 时，每个模块按以下顺序取第一个命中的构建：

1. **提测单**：规则不变，不区分渠道。
2. **渠道 C 的比例灰度**：按 `userId` 和发布 ID 分桶，规则同现在。
3. **渠道 C 的全量**。
4. **默认渠道的比例灰度**。
5. **默认渠道的全量**。

不带 `channel` 时只走第 1、4、5 步，和现在一致。

要点：

- **按模块回落到默认渠道**。`beta` 只发了 `claw` 模块，内测 App 的其他模块照常拿正式包。这是渠道和独立环境的主要区别。
- 渠道里只有比例灰度、没有全量时，没分到灰度的用户回落到默认渠道。也就是说，渠道灰度是叠加在默认全量之上的。
- 停用渠道 C 的全量后，C 的客户端回落到默认渠道，不会退回内置包。默认渠道停用全量后，仍然退回内置包，这一点不变。
- **未注册的渠道**按默认渠道处理，不报错，免得渠道被删或客户端写错导致内测 App 拿不到任何包。`channel` 格式不合法（见第 4 节）时返回 400。
- 拉包时已有的 Common 与 Feature 能力兜底（`common_incompatible`）照常执行，按实际拿到的构建判断。
- 渠道客户端按顺序取每个模块，同一渠道内取比例灰度优先，所以 `selected` 子查询的排序是"渠道优先，再灰度优先，再按创建时间"。实现见 `internal/infra/postgres/artifact_repository.go`。

### 3.1 响应变化

| 字段 | 说明 |
| --- | --- |
| 顶层 `channel` | 本次实际使用的渠道。未注册的渠道回显为 `''`，客户端调试菜单可以据此提示"渠道未注册" |
| 模块项 `channel` | 这个模块命中的发布属于哪个渠道；命中提测单、或命中默认渠道时为 `''` |

`source` 的取值不变（`testPlan`、`percentage`、`full`）。

ETag 取的是响应体哈希，响应体里带了 `channel`，所以不同渠道的 ETag 自然不同，不用改。

## 4. 数据模型

新增迁移 `000008_release_channels.sql`：

```sql
create table if not exists application_channels (
    application_id text not null references applications(id) on delete cascade,
    code text not null check (code ~ '^[a-z][a-z0-9-]{0,31}$'),
    name text not null,
    description text not null default '',
    created_by_user_id text references users(id) on delete set null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (application_id, code)
);

alter table release_records add column if not exists channel text not null default '';
alter table release_routes  add column if not exists channel text not null default '';

-- 生效路由的唯一约束加上渠道：同一模块、环境、运行时版本下，每个渠道各有一条全量、一条比例灰度。
create unique index if not exists idx_release_routes_default_active_channel
    on release_routes(module_id, environment_code, runtime_version, channel)
    where route_type = 'default' and status = 'active';
create unique index if not exists idx_release_routes_percentage_active_channel
    on release_routes(module_id, environment_code, runtime_version, channel)
    where route_type = 'percentage' and status = 'active';
drop index if exists idx_release_routes_default_active;
drop index if exists idx_release_routes_percentage_active;

-- 拉包统计加渠道维度。
alter table artifact_pull_daily_stats add column if not exists channel text not null default '';
alter table artifact_pull_daily_stats drop constraint if exists artifact_pull_daily_stats_pkey;
alter table artifact_pull_daily_stats add primary key
    (day, application_id, environment_code, module_id, runtime_version, platform, channel, outcome, build_record_id);
```

渠道表里不存 `''`。`release_records.channel` 不加外键：删除渠道前要求它在所有环境都没有生效中的发布（见第 6 节），历史发布记录保留原来的 `channel` 值。

## 5. 发布规则

| 操作 | 渠道下的行为 |
| --- | --- |
| 创建发布 | 请求体新增 `channel`，默认 `''`。非空时必须是已注册的渠道。新发布只替换**同一渠道**、同一运行时版本下的路由 |
| 比例灰度转全量 | 只顶掉同一渠道的全量 |
| 停用 | 渠道发布停用后，该渠道客户端回落到默认渠道 |
| 回滚 | 只在同一渠道内找上一次全量 |
| 操作记录 | `created` 的 detail 增加 `channel` |
| 发布列表 | 筛选条件新增 `channel` |
| `ResolveReleaseRoute`（`internal/server/http/release.go:321`） | 输入新增 `channel`，按第 3 节的顺序解析 |

### 5.1 能力约束

现在的校验范围是"同一环境、运行时版本"。加了回落后，一个渠道 C 的客户端对模块 M 可能拿到的构建集合是：

- C 在 M 上有生效全量：C 的全量和 C 的比例灰度；
- 否则：C 的比例灰度，加上默认渠道的全部生效发布。

默认渠道客户端的集合不变。校验改为：

- **发布到渠道 C**：只检查 C 的客户端可能拿到的 Common 与 Feature 组合。
- **发布到默认渠道**：检查默认渠道，以及所有会在 Common 或 Feature 上回落到默认渠道的渠道。

advisory lock 的范围保持"Common 模块 + 环境 + 运行时版本"，不加渠道。默认渠道的发布会影响所有渠道，所以同一范围内所有渠道的发布都需要串行。

**停用不做校验**。停用渠道全量后，该渠道客户端在这个模块上回落到默认渠道，可能出现发布时没校验过的 Common 与 Feature 组合。停用常用于紧急止损，所以不拦；这类组合由拉包时的 `common_incompatible` 兜底，不兼容的 Feature 回退到内置包。

### 5.2 构建依赖注入

不变。Feature 构建注入的仍然是默认渠道的 Common 全量。构建与渠道无关，注入哪个 Common 只影响构建产物；运行时拿到的 Common 是否兼容，由第 5.1 节的发布校验和拉包兜底保证。

## 6. 渠道管理接口

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/applications/{slug}/channels` | 列表，`activeReleases` 是所有环境下生效中的渠道发布数 |
| `POST /api/v1/applications/{slug}/channels` | 创建，`code` 创建后不能改；重复时返回 409 `release channel already exists` |
| `PATCH /api/v1/applications/{slug}/channels/{code}` | 改名称、说明 |
| `DELETE /api/v1/applications/{slug}/channels/{code}` | 只在所有环境都没有生效中的渠道发布时允许，否则返回 409 `release channel in use` |

列表对登录用户开放；创建、修改、删除需要 `release:create`，与在该应用发布的权限一致。

删除渠道时锁住渠道行（`for update`），创建渠道发布时以 `for share` 读取渠道行，两者互斥，删除时的"没有生效发布"检查才可靠。

## 7. 控制台

- 应用详情新增「渠道」卡片：列表、创建、编辑、删除。
- 创建发布弹窗新增渠道选择，默认「默认渠道」。应用没有渠道时不显示这个选项。
- 发布记录列表新增渠道列和渠道筛选；发布详情显示渠道。
- 拉包统计新增渠道维度。

## 8. 客户端接入

- 内测 App 打包时写死渠道标识，例如 `beta`；正式版不传。
- 拉包请求带上 `channel`。调试菜单显示请求的渠道和响应顶层的 `channel`。两者不一致时，说明渠道未注册。
- ETag 的使用规则不变：只能和相同的 `platform`、`runtimeVersion`、`userId`、`channel` 一起使用。

## 9. 安全

渠道是客户端自己声明的，任何人改一下请求参数，就能拿到 `beta` 的包，这一点和 `userId` 一样。渠道只用来划分发布范围，不是访问控制。如果内测包本身不能外泄，需要另做客户端鉴权，这不在本设计的范围内。

## 10. 上线顺序与兼容

1. 部署后端，迁移自动执行。部署期间新旧实例同时在线，此时：
   - 旧实例不认识 `channel`，解析时会把渠道路由当成默认路由。所以**所有实例升级完成之前不能创建渠道**。旧版控制台没有渠道入口，正常操作不会触发这个问题。
   - 拉包统计换了主键，旧实例 `on conflict` 的目标列对不上，它的统计写入会失败，并记一条 `flush artifact pull stats failed` 错误日志。部署窗口内旧实例的统计可能丢失。
2. `docs/openapi.json` 更新快照，前端运行 `bun run gen:api` 后再上线控制台。
3. 客户端内测包接入 `channel`。

## 11. 测试

- 仓储集成测试：
  - 渠道全量优先于默认全量；
  - 渠道只有灰度时，未命中的用户回落到默认渠道；
  - 停用渠道全量后回落到默认渠道；
  - 未注册的渠道等同于不传；
  - 提测单优先于渠道；
  - 新发布只替换同一渠道的路由；
  - 回滚和转全量不跨渠道。
- 能力约束：
  - 渠道 Common 与默认 Feature 回落组合不兼容时拒绝；
  - 发布默认 Common 时检查所有会回落的渠道。
- 接口测试：
  - `channel` 格式校验；
  - 删除有生效发布的渠道时返回 409；
  - 响应体里的 `channel` 字段。

## 12. 不做

- 按 App 版本范围匹配：版本比较规则因客户端而异，属于适配器的范畴；渠道已经能覆盖"单独打包的内测 App"这个场景。
- 渠道之间的继承链（例如 `alpha` 回落到 `beta`，再回落到默认渠道）：只回落一层，等有实际需求再加。
- 按渠道限制提测单。
