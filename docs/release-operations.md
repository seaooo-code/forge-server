# 发布运维与模块能力约束

本文说明四项平台能力：

- Common 与 Feature 模块之间的能力约束；
- 停用与回滚发布；
- 发布操作记录；
- 拉包统计。

这四项都属于 Forge 主线，和具体接入方无关。小米 YRN 的能力名约定见[小米 RN 接入指南](xiaomi-rn-integration.md)第 4 节。

## 1. 模块能力约束

### 1.1 背景

`runtimeVersion` 保证"包和客户端原生版本兼容"，但不能保证"业务包和 common 包兼容"。同一个 `runtimeVersion` 下，业务包可能用到了更新的 common API。如果客户端拿到的 common 包比业务包要求的旧，就会运行出错。

### 1.2 声明方式

构建产物的 `forge.json` 可以带两个可选字段：

```json
{"runtimeVersion": "725-n1-m24", "provides": {"commonApi": 3}}
{"runtimeVersion": "725-n1-m24", "requires": {"commonApi": 2}}
```

- `provides`：Common 模块声明自己提供的能力等级。
- `requires`：Feature 模块声明它要求所属 Common 模块达到的最低等级。
- 键是能力名，以字母开头，最多 64 个字符，只允许字母、数字和 `. _ -`。值是 0 到 2147483647 之间的整数。每个字段最多 16 项。
- 平台不解释能力名，只比较大小。比较规则：`requires` 里的每一项，都要在对方的 `provides` 里存在，并且等级不低于要求。
- 没有声明的构建不参与校验，旧构建不受影响。

worker 校验 `forge.json` 时会解析这两个字段（`internal/worker/artifact_contract.go`），结果写入构建记录的 `provided_capabilities` 和 `required_capabilities` 列。构建日志和构建详情里都能看到。

### 1.3 发布时校验

发布时的校验在 `internal/infra/postgres/release_capability.go`。它保证：发布之后，在同一环境、同一运行时版本下，客户端可能同时拿到的每一对 Common 构建和 Feature 构建都是兼容的。

- **发布 Feature 构建**：它的 `requires` 要被所属 Common 模块**每一条**生效中的发布满足，包括全量和比例灰度。没分到 Common 灰度的用户拿到的是 Common 全量，所以 Feature 也必须兼容 Common 全量。如果 Feature 声明了 `requires`，而 Common 在这个环境和运行时版本下没有任何生效中的发布，也会拒绝，原因是 `common_missing`。
- **发布 Common 构建**：它的 `provides` 要满足所有绑定它的 Feature 模块的每一条生效中的发布。
- **不需要重新校验的操作**：比例灰度转全量、停用发布都不会引入新的构建，所以不再校验。回滚本身就是一次新的全量发布，照常校验。
- **并发**：同一个 Common 模块、环境、运行时版本下的发布，用 advisory lock 串行执行，避免 Common 和 Feature 同时发布时各自都通过了校验。

不兼容时接口返回 `409`，`error = "release capability conflict"`，`conflicts` 列出冲突的发布和每一项不满足的能力（`shortfalls`）。控制台会把它转成可读的提示。

### 1.4 拉包时兜底

发布时的校验管不到提测单。比如提测单里只换了 Common 构建，或者只换了业务包构建。所以拉包时（`internal/service/artifact.go`）还会按**本次请求实际拿到的**构建再检查一遍：Feature 构建的要求高于本次返回的 Common 构建时，这个 Feature 模块会被移到 `missingModules`，原因是 `common_incompatible`，客户端回退到内置包。

Common 模块本身没有可用构建时不做处理，因为客户端会整组回退。

## 2. 停用与回滚

两个接口都需要 `release:rollback` 权限。拥有这项权限的角色和能创建发布的角色相同：全局 SuperAdmin、ReleaseAdmin，以及应用 Owner 和 Release 角色。

| 接口 | 作用 |
| --- | --- |
| `POST /api/v1/releases/{id}/deactivate` | 把一条生效中的发布标为失效，可以填写原因。不会自动恢复其他发布。停用全量后，该运行时版本下没有命中灰度或提测单的客户端拿不到这个模块的包，会回退到内置包。结束比例灰度后，原本命中灰度的用户回到全量。 |
| `POST /api/v1/releases/{id}/rollback` | 只对生效中的全量发布有效。它找到同一模块、环境、运行时版本下更早的、构建不同的最近一次全量发布，把那个构建重新发布为全量。这是一次新的发布：会结束进行中的比例灰度，也会经过能力约束校验。 |

控制台的入口在发布详情的顶部按钮："回滚到上一版本"、"停用全量"、"结束灰度"。线上现状页原有的"回退到此"仍然是对指定的旧构建做一次全量发布，行为不变。

## 3. 发布操作记录

每次发布状态变化都在同一个事务里写入 `release_events` 表：

| kind | 时机 | detail |
| --- | --- | --- |
| `created` | 创建发布，包括回滚 | `releaseType`、`buildRecordId`、`runtimeVersion`；灰度另有 `grayPercentage`；回滚另有 `rollbackFromReleaseId` |
| `gray_percentage_updated` | 调整灰度比例 | `from`、`to` |
| `promoted_to_full` | 比例调到 100% 转为全量 | `fromPercentage` |
| `replaced` | 被同一运行时版本下的新发布替换 | `byReleaseId` |
| `deactivated` | 停用 | `reason` |

`GET /api/v1/releases/{id}/events` 按时间倒序返回最近 100 条，控制台在发布详情里展示。上线之前的发布没有操作记录。

## 4. 拉包统计

- **统计对象**：每次 manifest 请求（包括返回 304 的）里，每个模块的解析结果。维度是：日期、应用、环境、模块、运行时版本、平台、结果（`testPlan`、`percentage`、`full`，或 `missing:{原因}`）、构建。不记录 `userId`。
- **写入方式**：API 进程先在内存里累加，每 30 秒批量 upsert 一次（`internal/service/artifact_pull_stats.go`），进程正常关闭前会再写一次。进程异常退出时，最多丢失 30 秒的计数。多个实例各自累加，靠主键合并，结果仍然正确。内存里最多保留 5 万个维度组合。
- **保留期**：90 天，API 每小时清理一次过期数据。
- **查询**：`GET /api/v1/artifact-pull-stats?applicationId=&environment=&moduleId=&buildRecordId=&days=`。应用、模块、构建三个参数至少填一个，`days` 取 1 到 90，默认 7。
- **控制台**：发布详情展示这条发布的构建在该环境下近 7 天的拉取次数，按天汇总，并列出各平台的数量。

## 5. 数据库迁移

- `000006_build_capabilities.sql`：`build_records` 新增两列能力声明。
- `000007_release_operations.sql`：新增 `release_events` 和 `artifact_pull_daily_stats` 两张表。

两个迁移都只加列、加表，部署期间旧版本进程可以正常运行：旧进程写入的构建没有能力声明，也不会写操作记录。
