# 小米 RN 接入指南

本文记录 Forge 在小米内部接入 RN 测试环境的约定，属于接入层文档，不属于平台能力。平台能力见 [RN 测试环境接入 P0 方案](rn-test-env-p0-plan.md) 和 [构建适配器方案](build-adapters.md)。本文的内容只能通过配置、构建适配器数据和客户端实现落地，不写进 Forge 主线代码。

两条原则：

1. **兼容现有流程的逻辑做成适配器**：凡是为了兼容晴空塔、YRN 工具链而需要的处理，都作为适配器数据在界面上配置。
2. **现有流程只作参考**：能实现功能时，优先选更好的方案。需要客户端配合修改的地方，统一列在 [客户端需求](xiaomi-rn-client-requirements.md) 中。

## 1. 部署前提

- forge-server 和 forge-worker 部署在公司内网。测试环境只在内网使用，不对外网开放。它们需要能访问：
  - `git.n.xiaomi.com`：拉取代码，接收 webhook 回调；
  - `pkgs.d.xiaomi.net`：npm 私有源；
  - FDS 对象存储（见第 3 节）。
- worker 宿主机需要 Docker 23 或以上版本，并启用 BuildKit。构建适配器的 Dockerfile 模板首行声明 `# syntax=docker/dockerfile:1.10`，因此 worker 需要能访问 Docker Hub 或内网镜像源。
- GitLab 需要能访问 forge-server 的 webhook 地址。

## 2. GitLab 连接

1. 在业务所在的 GitLab Group 中打开 *Settings → Access Tokens*，创建 Group Access Token：
   - 角色：Reporter；
   - scope：`read_api`、`read_repository`；
   - 有效期：按公司规定设置，并指定专人负责续期。
2. 在 Forge 的「代码源 → 新建 GitLab 连接」中，实例地址填 `https://git.n.xiaomi.com`，令牌填上一步创建的 Access Token。
3. 绑定模块后，在 GitLab 项目的 *Settings → Webhooks* 中添加 webhook：URL 和 Secret token 从模块详情页复制，触发事件只勾选 *Push events*。

## 3. 对象存储：FDS

FDS 兼容 S3 API，可以直接用 Forge 的 S3 存储配置接入，不需要新增存储适配器。权限流程参见 FDS 文档《FDS IAM / 团队管理 / 创建桶 / 授权》。

**集群必须支持 S3 协议**。FDS 的 S3 兼容域名见文档《接入方式 → 兼容域名》，国内可用的集群有：武清测试、北京2（c3）、北京3（武清）、北京4（c4）。**北京6（cnbj6）不在列表中**，在那里建 bucket 无法通过 S3 接入。

测试环境当前配置如下（2026-11-16 创建）：

| 项 | 值 |
| --- | --- |
| 集群 | 北京4（C4）`cnbj3-fusion` |
| Bucket | `micar-forge-test`，标准存储，不开放外网访问 |
| Org / IAM 节点 | `App-fe`（CL3085638）/ `xiaomi.car.micar-vip.micar-fe.cl3085638` |
| 程序用户组 | `App-fe / rn`（CI3085642），授予 Read、Write、Read Objects，不授予 Full Control |
| S3 Endpoint | `https://s3-cnbj3-fusion.mi-fds.net`，path-style 和 virtual-hosted 两种访问方式都可用 |
| 原生内网域名 | `cnbj3-fusion-fds.api.xiaomi.net`，Forge 不使用 |

在 Forge 的「存储管理」中新建 S3 存储：Endpoint 和 Bucket 按上表填写，**Endpoint 必须是 S3 兼容域名并带 `https://`，不能填原生域名**。Region 填 `us-east-1`，2026-11-16 已用它完成测试连接以及真实构建的上传和下载。AK/SK 必须使用用户组 **CI3085642** 的密钥，不能用个人默认 org 的密钥。先点「测试连接」，通过后再保存，然后把测试环境绑定到这份存储配置。

注意事项：

- **AK/SK 不会过期，也不会自动轮换**。密钥只能保存在 Forge 的存储配置中，不能写进代码、文档或客户端。
- **生命周期**：建议给 bucket 添加按天过期的规则，例如 60 天，自动清理旧的测试构建产物。
- **下载方式**：bucket 不开外网，对象也是私有的。P0 阶段由 Forge 的下载接口代理下载，`publicBaseUrl` 留空。
- **校验头兼容**：Forge 使用较新的 aws-sdk-go-v2，默认附带 CRC32 校验头。2026-11-16 的实测中上传正常，FDS 可以接受这些校验头，不需要额外开关。

## 4. runtimeVersion 约定

YRN 包和客户端是否兼容，由 `SDKVersion`（RN 引擎版本）、`maxNativeApiLevel`、`moduleIdMapVersion` 三个值决定。客户端在拉取业务包之前能拿到这三个值（已确认）。规则如下：

```text
{SDKVersion}-n{maxNativeApiLevel}-m{moduleIdMapVersion}
例：725-n1-m24
```

- **构建端**：YRN 构建适配器从模块的 `buz.config.json` 或 `com.config.json` 中读取这三个值，拼接后写入 `forge.json`（见第 5 节）。
- **客户端**：按相同规则拼接后上报，具体要求见客户端需求。
- common 包和业务包使用同一个 `runtimeVersion`，因此 Forge 一次请求就能返回整组兼容的包。common 包的模块 ID 表变化时，必须升级 `moduleIdMapVersion`，新旧两组包会自动隔离。
- Forge 只做精确匹配。在测试环境里，旧 App 拿不到新包时只会显示"无可用包"，不会因为拿错包而白屏。
- 已知缺口：`maxCommonApiLevel`（业务包依赖的 common 包 API 版本）不在 runtimeVersion 中。同一条 `moduleIdMapVersion` 线上，如果业务包依赖了更高版本的 common API，Forge 目前无法拦截。后续计划在平台层提供通用的"模块能力版本约束"，在发布时校验。

## 5. YRN 构建适配器

适配器的完整定义放在 [`docs/adapters/yrn-725/`](adapters/yrn-725/README.md)，包括 Dockerfile 模板、构建脚本、参数文件模板，以及可直接导入的 `adapter.json`。适配器作为数据存放在 Forge 中，可以在「平台 → 构建适配器」页面查看和编辑；这些文件只是它的参考副本。Forge 主线代码中不包含 YRN 相关逻辑。

适配器只复刻晴空塔 GitLab CI 中真正负责构建的部分，即"安装依赖、预处理、`yrn-cli build-common` / `build-business`"，不调用 `yrn-build`。`yrn-build`（`@mi/yrn-git-script`）的职责是调用晴空塔接口取参数、把产物上传到 KS3，以及回调晴空塔，这些步骤 Forge 都不需要。

模块的参数示例：

| 参数 | common（`micar-rn-common`） | claw（`micar-rn-claw`） |
| --- | --- | --- |
| `kind` | `common` | `business` |
| `entryFile` | `packages/index.{platform}.js` | `index.{platform}.js` |
| `configFile` | `config/com.config.json` | `config/buz.72.config.json` |
| `installCommand` | `npm i --prefer-offline --force --registry=…` | `yarn` |
| `preBuildCommand` | `cp -Rf node_modules/@micar-fe/micar-rn-common-lib/res res` | `node ./scripts/rn72-build-patch.js --moduleName=claw` |
| `baselineDir` | `first_build_config` | — |

业务包需要 common 包的模块 ID 表（`commonMap`）。Forge 在构建 Feature 模块时会把同环境下生效的 common 产物注入到构建目录（见 [构建适配器方案](build-adapters.md) 第 4 节），构建脚本从 `.forge/deps/common/{runtimeVersion}/{platform}/commonMap.{platform}.json` 中取出 commonMap，再复制到 yrn-cli 要求的 `build-config/` 目录下。所以 common 包的产物里会额外包含 `commonMap.{platform}.json`。

**构建变量**：如果私有 npm 源需要鉴权，在模块上配置密钥变量 `NPM_TOKEN`。

### 5.1 用 GitLab CI 构建（gitlab_ci）

worker 部署在 DevX/Matrix 容器里时没有 Docker，YRN 模块可以改用 `gitlab_ci` 构建方式：Forge 触发业务仓库的 GitLab CI，构建在公司共享 k8s runner 上执行，worker 只取回产物。契约、令牌权限和部署见 [转交 GitLab CI 构建](gitlab-ci-build.md)，YRN 的脚本和 `.gitlab-ci.yml` 改动示例见 [`adapters/yrn-725/gitlab-ci/`](adapters/yrn-725/gitlab-ci/)。

与 docker 适配器相比的差异：

- 构建配方（安装依赖、预处理、`yrn-cli build-*`）在业务仓库的 `scripts/forge/` 里，不再由控制台的适配器渲染；适配器的参数改为模块的构建变量（`YRN_KIND`、`YRN_ENTRY_FILE`、`YRN_CONFIG_FILE`、`YRN_INSTALL_COMMAND`、`YRN_PREBUILD_COMMAND` 等）。
- GitLab 连接的令牌需要 `api` scope，并且在项目上至少是 Developer 角色（第 2 节的 Reporter 加 `read_api` 只够 docker 构建）。
- 业务仓库现有的 `.gitlab-ci.yml` 里，`pre_pre` 会打印全部环境变量，`master_merge_notify_job` 对 `master` 是 `when: always`，都要排除 Forge 触发的 pipeline，示例里有具体写法。
- 业务包依赖 common 包的 `commonMap`：Forge 把依赖的下载地址放进 `FORGE_DEPS`，由 `forge-fetch-deps.js` 下载，目录结构与 docker 构建的依赖注入一致。依赖构建所用的存储配置（FDS）需要有公开访问地址，并且 runner 能访问。

## 6. 与晴空塔的对应关系

| 晴空塔 | Forge | 说明 |
| --- | --- | --- |
| `appKey`（如 `MiCar`） | 应用 slug | |
| `moduleName`（如 `common`、`claw`） | 模块 | 一个 YRN 模块绑定一个 Forge 模块 |
| zip → `{模块}.{平台}.tar` → 文件 | `dist/{平台}/` 下的散文件 | 去掉两层打包。客户端按清单下载，可以按 sha256 复用未变化的文件（例如图片资源） |
| `urlmd5` + config 内的 `YRN_BUNDLE_*_MD5` | 清单中每个文件的 `sha256` | 一层校验覆盖所有文件 |
| `buildnum` / `maketime` / `pack_version` | `buildRecordId`、构建号、`commitSha`、`branch` | 客户端用 `buildRecordId` 判断是否有新版本 |
| `updateType` / `residentMemory` | 不提供 | 测试源由客户端固定生效方式 |
| 一次请求拉取所有 `moduleNames` | 一次请求返回应用下所有模块 | |
| 灰度 uid / 白名单 | 提测单成员 `userId` 与比例灰度 | 测试场景下由提测单决定成员拿哪个分支的构建 |

## 7. 客户端拉包

```
GET {forgeBaseUrl}/api/v1/artifacts/applications/{appSlug}/environments/{env}/manifest
    ?platform={android|ios}&runtimeVersion={第 4 节规则}&userId={登录账号 ID，可选}
Authorization: Bearer {访问令牌}
```

- 访问令牌在 Forge 的「访问令牌」页面生成，只写进测试包。
- 每个文件带 `path`、`url`、`sha256`、`sizeBytes`。客户端下载后先校验 sha256，再交给加载器。
- `missingModules` 列出当前没有可用包的模块及原因：`no_release_for_runtime`、`platform_unavailable`、`release not found`。
- 响应带 `ETag`。下次请求时带上 `If-None-Match: {上次的 ETag}`，清单没变时返回 304 且没有响应体。

客户端需要实现的功能和改动见 [客户端需求](xiaomi-rn-client-requirements.md)，端侧组件的改造方式和 Forge 侧待办见 [客户端对接方案](xiaomi-rn-client-integration-plan.md)。
