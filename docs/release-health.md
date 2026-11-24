# 发布健康与自动停用（设计稿）

状态：第一期已实现（迁移 `000010_release_health.sql`）。

## 1. 要解决的问题

比例灰度现在只能靠人盯：拉包统计只记录 manifest 请求次数（`artifact_pull_daily_stats`），不知道新包在客户端上能不能正常启动。灰度包有问题时，要等用户反馈或有人看崩溃平台，才会去手动停用。

这份设计让客户端上报每个模块的启动结果，Forge 按发布统计启动失败率。失败率超过阈值时，比例灰度自动停用，全量发布给出告警。

## 2. 范围

**第一期（本设计）**

- 客户端上报启动结果的接口。
- 按发布计算健康状态，在控制台展示。
- 比例灰度不健康时自动停用；全量不健康时只告警，不自动改动（见第 6 节的待定项）。
- 按应用、环境配置健康策略。

**客户端接入是可选的**，见第 2.1 节。

**以后再做**

- 接入 Sentry 等崩溃平台作为第二个指标来源。按项目约定做成适配器，见第 9 节。
- 告警发到飞书或邮件。现在只记操作记录、在控制台展示，等通知适配器做好后接上。
- 分阶段自动放量（例如 1% → 10% → 50%，每一步健康才继续）。

### 2.1 客户端可选接入

不做上报的客户端照常拉包和使用，Forge 这边没有任何变化，只是这些客户端的发布得不到健康判定和自动停用。

| 客户端情况 | 结果 |
| --- | --- |
| 完全不上报 | 发布的健康状态是 `no_reports`，不判定、不自动停用 |
| 只有部分 App 版本上报 | 失败率和基线都只按上报的用户算，判定仍然成立；上报的用户少时，容易一直停在 `insufficient_data` |
| 只上报 `launch_failed`，不上报 `launched` | 算不出失败率（`launched` 是分母），等同于不可用。**`launched` 和 `launch_failed` 是最小接入单元，必须一起做** |
| 不上报 `verify_failed`、`download_failed` | 不影响判定，这两项只用于展示 |

策略打开了、客户端却没有上报时，灰度会一直得不到判定，看起来在被监控，实际上没有保护。所以 `no_reports` 和 `insufficient_data` 要分开显示（第 7 节），让人一眼看出这个应用还没接入上报。

## 3. 客户端上报

### 3.1 启动结果

每个模块的一次启动，客户端判定为下面一种结果：

| outcome | 含义 | 计入失败率 |
| --- | --- | --- |
| `launched` | 新包加载成功，并且到达"就绪点"：这个模块的首屏渲染完成 | 是（作为成功） |
| `launch_failed` | 新包在到达就绪点前崩溃或报错，客户端在下次启动时回退到上一个包或内置包 | 是（作为失败） |
| `verify_failed` | sha256 或签名校验失败，包没有被使用 | 否，单独展示 |
| `download_failed` | 下载失败，包没有被使用 | 否，单独展示 |

`launch_failed` 依赖客户端的本地回退：新包第一次加载前先记下"待确认"，到达就绪点后清掉；如果下次启动时这个标记还在，就说明上次没走到就绪点，客户端回退并上报 `launch_failed`。这和 CodePush 的 `notifyAppReady` 是同一个思路，客户端需求写在第 8 节。

### 3.2 接口

```
POST /api/v1/artifacts/applications/{slug}/environments/{env}/launch-reports
Authorization: Bearer <访问令牌>，与 manifest 接口相同
```

```json
{
  "platform": "android",
  "runtimeVersion": "725-n1-m24",
  "channel": "beta",
  "userId": "10001",
  "events": [
    {
      "moduleId": "claw",
      "buildRecordId": "c4938f50-7fc8-4187-a322-412211b4f905",
      "releaseId": "28e76366-7b15-43e6-b00d-f9882fea10d0",
      "outcome": "launch_failed",
      "reason": "js_exception",
      "occurredAt": "2026-11-24T08:01:02Z"
    }
  ]
}
```

- `releaseId` 取 manifest 模块项里的值，命中提测单时为空。`buildRecordId` 必填。
- `reason` 是客户端定义的简短原因码，最长 64 个字符，例如 `js_exception`、`timeout`；不要放堆栈，堆栈留给崩溃平台。
- 每次最多 50 条事件。`occurredAt` 早于 24 小时或晚于当前时间 5 分钟以上的事件丢弃。
- 返回 202，不返回统计结果。格式不对的单条事件跳过，不让整批失败。

### 3.3 防刷

访问令牌是平台级的，会打包进 App，任何人都能拿来伪造上报。设计上限制伪造上报能造成的影响：

- **没有 `userId` 的事件只计数、不参与判定**。
- **按用户去重**：同一个 5 分钟桶里，同一个 `userId` 对同一个构建、同一种结果只计一次。失败率按用户数算，不按事件数算。窗口跨多个桶时按桶求和，所以客户端要保证同一用户、同一构建、同一结果一天最多上报一次（第 8 节），否则频繁打开 App 的用户会被重复计入 `launched`，把失败率拉低。
- **自动动作只停用比例灰度**。最坏情况是伪造的上报让一次灰度提前停下，用户回到全量包，不会把用户推到坏包上；全量发布不会被自动改动。
- 去重在每个 API 实例的内存里做，多个实例之间会有少量重复计数，对阈值判定的影响可以接受。

## 4. 数据

上报和拉包统计一样先在内存里聚合，每 30 秒批量写库，见 `internal/service/artifact_pull_stats.go` 的做法。

```sql
-- 5 分钟一个桶。users 是该桶里去重后的用户数，events 是事件总数（包括没有 userId 的）。
create table release_health_buckets (
    bucket_start timestamptz not null,
    application_id text not null,
    environment_code text not null,
    module_id text not null,
    build_record_id text not null,
    release_id text not null default '',
    channel text not null default '',
    platform text not null,
    outcome text not null,
    reason text not null default '',
    users bigint not null default 0,
    events bigint not null default 0,
    primary key (bucket_start, application_id, environment_code, module_id, build_record_id, release_id, channel, platform, outcome, reason)
);

-- 每个应用、环境一条健康策略，没有记录时不判定、不自动停用。
create table release_health_policies (
    application_id text not null references applications(id) on delete cascade,
    environment_code text not null references environments(code) on delete cascade,
    enabled boolean not null default false,
    window_minutes integer not null default 30,
    min_users integer not null default 50,
    max_failure_rate numeric not null default 0.05,
    min_delta numeric not null default 0.02,
    auto_stop_gray boolean not null default true,
    primary key (application_id, environment_code)
);

-- 每条生效发布最近一次的判定结果，控制台直接读取。
create table release_health (
    release_id uuid primary key references release_records(id) on delete cascade,
    status text not null check (status in ('no_reports', 'insufficient_data', 'healthy', 'unhealthy')),
    launched_users bigint not null,
    failed_users bigint not null,
    failure_rate numeric not null,
    baseline_failure_rate numeric,
    evaluated_at timestamptz not null
);
```

桶数据保留 30 天。`reason` 进主键会增加行数，内存里的待写维度和拉包统计一样设上限。

## 5. 判定

API 进程里每分钟跑一次判定。多实例部署时，用 `pg_try_advisory_lock` 保证同一时间只有一个实例在判定，拿不到锁就跳过这一轮。

对每条生效中的发布（全量和比例灰度），在策略开启的应用和环境下：

1. **窗口**：从 `max(发布创建时间, 当前时间 - window_minutes)` 到当前时间。
2. **失败率**：窗口内这条发布的构建、渠道的 `launch_failed` 用户数 ÷（`launched` + `launch_failed` 用户数）。
3. **基线**：同一模块、环境、渠道、运行时版本下，生效中全量发布在同一窗口里的失败率。判定比例灰度时有基线；判定全量时没有基线。
4. **结论**：
   - 发布创建以来，这条发布的构建、渠道没有收到任何 `launched` 或 `launch_failed` 上报：`no_reports`。只看窗口内是不够的，窗口内没有上报也可能只是流量低，所以这一条看的是发布的整个生命周期。
   - 用户数少于 `min_users`：`insufficient_data`。
   - 失败率 ≥ `max_failure_rate`，且有基线时还要满足"失败率 ≥ 基线 + `min_delta`"：`unhealthy`。
   - 其余：`healthy`。

   有基线时要求比基线高出一截，是为了避免全量本身就有一定失败率时，灰度被误判。

默认值（策略里可改）：窗口 30 分钟，最少 50 个用户，失败率阈值 5%，比基线高 2 个百分点。

## 6. 自动动作

| 发布类型 | 判定为 unhealthy 时 |
| --- | --- |
| 比例灰度，`auto_stop_gray` 开启 | 自动停用（复用 `DeactivateRelease`）。灰度用户回到全量包。操作记录 `deactivated` 的 detail 带 `auto: true`、失败率、用户数和阈值，操作人显示为"系统" |
| 比例灰度，`auto_stop_gray` 关闭 | 只告警 |
| 全量 | 只告警 |

告警在第一期是一条新的操作记录 `health_unhealthy`，只在状态从其他值变为 `unhealthy` 时写一次，控制台在发布详情和列表上醒目展示。以后有了通知适配器，在同一个位置发通知。

**待定**：全量发布不健康时，是否也支持"自动回滚到上一次全量"（作为策略里一个默认关闭的选项）。第一期建议不做，原因是第 3.3 节的伪造上报风险：自动回滚全量的影响面比停用灰度大得多。

## 7. 控制台

- **发布详情**新增「健康」卡片：状态、窗口内的成功和失败用户数、失败率与基线、阈值、判定时间；`verify_failed` 和 `download_failed` 单独列出，有 `verify_failed` 时用警告样式（可能是包被篡改或公钥配置错误）。
- 两种"没有结论"的状态分开显示：
  - `no_reports`：显示"没有收到客户端上报，这条发布不受自动停用保护"，并说明客户端需要接入第 8 节的上报。策略打开时用警告样式。
  - `insufficient_data`：显示"已收到上报，用户数未达到判定下限"，附上当前用户数和下限。
- **发布记录列表**的生效发布显示健康状态标记。
- **应用详情**新增「发布健康」tab，按环境配置策略。

## 8. 客户端需求

整体是可选的（第 2.1 节）。要接入的话，第 1、2 条是最小接入单元，必须一起做；第 3 到 5 条可以后补。

1. 每个模块定义就绪点（建议：模块首屏组件挂载完成），到达后上报 `launched`。同一用户、同一构建一天最多上报一次。
2. 加载新包前写"待确认"标记，到达就绪点后清除；下次启动发现标记还在，就回退到上一个包或内置包，并上报 `launch_failed`。
3. 校验失败上报 `verify_failed`，下载失败上报 `download_failed`。
4. 事件先在本地排队，下次启动或切到后台时批量上报；上报失败下次再发，本地最多保留 200 条。
5. 同一用户、同一构建、同一结果一天最多上报一次（`launched` 见第 1 条，其余结果同理）。

## 9. 接口

| 接口 | 说明 |
| --- | --- |
| `POST /api/v1/artifacts/applications/{slug}/environments/{env}/launch-reports` | 客户端上报，访问令牌鉴权，返回 202 和计入的事件数 |
| `GET /api/v1/releases/{id}/health` | 现场计算一条发布的健康状态，不写库、不触发动作；策略关闭时也按策略参数算，`policyEnabled` 为 false |
| `GET /api/v1/applications/{slug}/release-health-policies` | 应用绑定的每个环境的策略，没保存过的返回默认值 |
| `PUT /api/v1/applications/{slug}/release-health-policies/{environment}` | 保存一个环境的策略，需要 `application:update` |

发布列表和详情新增 `healthStatus`，是最近一次判定的结果，没判定过时为空字符串。自动停用的 `deactivated` 操作记录 detail 带 `auto: true` 和判定数据，操作人为空（控制台显示"系统"）。

实现位置：`internal/service/release_health.go`（聚合、判定、自动停用），`internal/infra/postgres/release_health_repository.go`，判定规则在 `internal/domain/release_health.go` 的 `EvaluateReleaseHealth`。

## 10. 以后：Sentry 指标来源

Sentry 可以按 release 提供崩溃率，能补上"到达就绪点之后才崩溃"的情况。接入方式按项目约定做成适配器：在控制台配置 Sentry 地址、令牌、项目，以及 Forge 构建到 Sentry release 名的映射模板（例如 `{moduleId}@{buildNumber}`，需要客户端用同样的规则设置 Sentry release）。判定时把它当作第二个指标，任一指标不健康就算不健康。这部分不进第一期。

## 11. 测试

- 上报接口：格式校验、超过 50 条、过期事件丢弃、没有 `userId` 的事件不计用户数。
- 聚合：同一用户在同一个桶里去重；多次写库正确叠加。
- 判定：从未收到上报（`no_reports`）、样本不足、超阈值但没超过基线、超过基线；只在状态变化时写操作记录。
- 自动停用：比例灰度被停用后用户回到全量；全量只告警；策略关闭时什么都不做；多实例时只有一个实例在判定。
