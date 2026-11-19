# 转交 GitLab CI 构建

Forge 有两种构建能力：

| 构建方式 | 谁来构建 | 适用场景 |
| --- | --- | --- |
| `dockerfile` / `adapter` | Forge 的 worker 自己检出代码并 `docker build` | worker 所在机器有 Docker、git 和足够的磁盘 |
| `gitlab_ci` | Forge 触发代码源（GitLab）上的 pipeline，由仓库自带的 `.gitlab-ci.yml` 构建，worker 只负责等待和取回产物 | 公司已有 GitLab CI 和 runner；worker 要部署在没有 Docker 的容器平台 |

`gitlab_ci` 模式下，构建配方在业务仓库里，由业务仓库自己维护；Forge 只定义触发和产物的契约，不关心用什么工具链。只支持 GitLab 代码源。

## 1. 模块配置

在模块的构建配置里选择构建方式 `gitlab_ci`：

| 字段 | 说明 |
| --- | --- |
| `ciJob` | 产出构建产物的 job 名，必须与 `.gitlab-ci.yml` 里的 job 名一致。同一仓库可以用不同的 job 绑定多个模块 |
| `ciArtifactPath` | job artifacts 里产物根目录的相对路径，默认 `dist`。该目录必须满足构建产物契约：根目录只有 `forge.json` 和平台目录（`android`、`ios`、`harmonyos`） |

`gitlab_ci` 模式不使用 Dockerfile 路径、构建上下文和构建适配器，这三项不能同时配置。

## 2. GitLab 令牌权限

触发 pipeline 需要令牌具备 **`api` scope**，并且在项目上至少是 **Developer** 角色。只读令牌（`read_api` + `read_repository`）只能用于 `dockerfile` 和 `adapter` 构建。

令牌权限不足时，构建会失败，错误信息里会提示需要的权限。

## 3. 触发契约

worker 在构建分支（构建没有指定分支时用模块的默认分支）上创建 pipeline，并传入下面的变量。变量类型都是 `env_var`。

| 变量 | 含义 |
| --- | --- |
| `FORGE_BUILD_ID` | 构建记录 UUID。**用它判断是不是 Forge 触发的 pipeline** |
| `FORGE_BUILD_NUMBER` | 模块内递增的构建号 |
| `FORGE_BUILD_TIME` | 构建开始时间，UTC，RFC 3339 |
| `FORGE_MODULE_ID` | 模块 id |
| `FORGE_ENVIRONMENT` | 环境 code |
| `FORGE_BRANCH` | 构建分支 |
| `FORGE_DEPS` | 仅 Feature 模块且所属 Common 模块存在时传入，见第 5 节 |
| 模块的构建变量 | 全部传入，包括密钥变量。`FORGE_` 前缀留给平台，用户变量不能占用 |

构建的 commit 以 pipeline 实际使用的 commit 为准，job 里用 GitLab 自带的 `CI_COMMIT_SHA`。如果构建记录已经指定了 commit（例如 webhook 触发的构建），而分支在 pipeline 创建前又前进了，构建会失败，避免产物和记录对不上。

### 密钥变量的可见性

模块里配置的**密钥变量会作为 pipeline 变量传入**，GitLab 项目里 Developer 及以上的成员能在 pipeline 页面看到它们，通过 API 创建的 pipeline 变量也不能标记为 masked。Forge 会在构建日志里对这些值做脱敏，但这挡不住 GitLab 界面。

- 必须严格保密的值（例如发布用的令牌），放在 GitLab 项目或群组的 **CI/CD 变量**里，并标记 masked 和 protected，不要配置成 Forge 的密钥变量。
- CI 脚本里不要执行 `env`、`printenv` 之类会打印全部变量的命令。

## 4. 产物契约

pipeline 成功，并且 `ciJob` 对应的 job 成功后，worker 下载该 job 的 artifacts，取 `ciArtifactPath` 目录作为产物根目录，之后与 docker 构建完全一致：校验 `forge.json` 和平台目录、生成清单、上传对象存储。

- `.gitlab-ci.yml` 里这个 job 必须配置 `artifacts.paths` 包含该目录，并且 artifacts 没有过期。
- 只会取 `ciJob` 这一个 job 的 artifacts。如果按平台并行构建，需要一个汇总 job（用 `needs` 引用各平台 job 的 artifacts），把产物合并成完整的目录。
- artifacts 压缩包不能包含软链、越界路径和重复文件。下载大小和解压后大小都受 `FORGE_WORKER_CI_ARTIFACT_MAX_BYTES` 限制，默认 1 GiB。

构建日志是 `ciJob` 对应 job 的日志，pipeline 失败时还会列出失败的 job 和 pipeline 链接。

## 5. 依赖产物注入

Feature 模块构建时需要读取所属 Common 模块产物里的文件（例如 `commonMap`）。docker 构建由 worker 把依赖文件放进构建上下文；`gitlab_ci` 构建里 worker 不能写 runner 上的文件，改为：

1. worker 解析出依赖构建（规则与 docker 构建一致：所属 Common 模块在当前环境下生效中的默认路由），把依赖记录到构建记录。
2. 以 `FORGE_DEPS` 变量把依赖的**下载地址**交给 job，JSON 格式：

```json
{
  "moduleId": "common",
  "environment": "qa",
  "items": [
    {
      "runtimeVersion": "725-n1-m24",
      "buildRecordId": "…",
      "buildNumber": 12,
      "commitSha": "3a9e…",
      "platforms": ["android", "ios"],
      "manifestUrl": "https://cdn.example.com/…/manifest.json",
      "baseUrl": "https://cdn.example.com"
    }
  ]
}
```

   `items` 为空表示 Common 模块没有生效中的默认路由，是否因此失败由 job 自己判断。
3. job 运行 [`docs/gitlab-ci/forge-fetch-deps.js`](gitlab-ci/forge-fetch-deps.js)，它按清单下载依赖文件、校验大小和 sha256，写成与 docker 构建相同的目录结构：

```text
.forge/deps/common/index.json
.forge/deps/common/{runtimeVersion}/{platform}/...
```

这要求 **runner 能访问依赖构建所用存储配置的公开访问地址**（`publicBaseUrl`）。依赖构建的存储配置没有公开访问地址时，构建会直接失败，不会创建 pipeline。

## 6. `.gitlab-ci.yml` 写法

最小示例：

```yaml
forge_build:
  stage: build
  rules:
    - if: $FORGE_BUILD_ID          # 只在 Forge 触发的 pipeline 里运行
  script:
    - ./build.sh                   # 产出 dist/forge.json 和 dist/{platform}/
  artifacts:
    paths:
      - dist/
    expire_in: 1 day
```

注意：

- **现有 job 要排除 Forge 的 pipeline。** 通过 API 创建的 pipeline，`CI_PIPELINE_SOURCE` 是 `api`，没有 `rules` 的 job 会一起运行；对默认分支 `when: always` 的通知、发布类 job 也会命中。给这些 job 加上 `- if: $FORGE_BUILD_ID` 加 `when: never`。
- 不要在 job 里打印全部环境变量。
- 想复用同一份构建脚本，可以用 GitLab 的 `include` 引用共享模板。

YRN 725 业务仓库的完整示例见 [`docs/adapters/yrn-725/gitlab-ci/`](adapters/yrn-725/gitlab-ci/)。

## 7. worker 部署

`gitlab_ci` 构建不需要 Docker 和 git，worker 可以直接部署在容器平台。用 `FORGE_WORKER_BUILD_MODES` 指定 worker 处理哪些构建方式，避免没有 Docker 的 worker 领到 docker 构建：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `FORGE_WORKER_BUILD_MODES` | `dockerfile,adapter,gitlab_ci` | 逗号分隔。只处理 CI 构建的 worker 设为 `gitlab_ci` |
| `FORGE_WORKER_CI_POLL_INTERVAL` | `5s` | 轮询 pipeline 状态和 job 日志的间隔，最小 1s |
| `FORGE_WORKER_CI_ARTIFACT_MAX_BYTES` | `1073741824` | artifacts 下载和解压后的大小上限 |

- 构建超时（`FORGE_WORKER_BUILD_TIMEOUT`）到期时，worker 会取消远端 pipeline。
- worker 退出或租约被接管时**不会**取消 pipeline：pipeline ID 已经写回构建记录，下一个领取这个构建的 worker 会续接同一个 pipeline，不会重复触发。所以容器平台的优雅退出时间较短（例如 130 秒）也不影响构建，把 `FORGE_WORKER_SHUTDOWN_GRACE` 设得比它短即可。

## 8. 常见错误

| 现象 | 原因 |
| --- | --- |
| `创建 GitLab pipeline 失败 … 403` | 令牌缺少 `api` scope，或在项目上不是 Developer 角色 |
| `pipeline 没有运行任何 job … rules` | `.gitlab-ci.yml` 里没有 job 的规则匹配 `FORGE_BUILD_ID` |
| `没有找到 job "xxx"` | 模块配置的 `ciJob` 与 `.gitlab-ci.yml` 不一致 |
| `job "xxx" 没有 artifacts` | job 没有配置 `artifacts.paths`，或 artifacts 已过期 |
| `没有产物目录 dist` | artifacts 里没有 `ciArtifactPath` 指定的目录 |
| `分支已前进` | 构建记录指定了 commit，但创建 pipeline 时分支已经前进 |
| `依赖构建 … 没有公开访问地址` | 依赖构建所用的存储配置没有配置 `publicBaseUrl` |
