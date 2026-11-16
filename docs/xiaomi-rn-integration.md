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

FDS 兼容 S3 API，可以直接使用 Forge 的 S3 存储配置，不需要新增存储适配器。

1. 在 [FDS 控制台](https://cloud.d.xiaomi.net/fds/buckets?regionType=overseas&regionId=cnbj3-fusion&projectId=521002)（北京4 `cnbj3-fusion`，IAM 节点 `micar-ai`）中创建一个测试环境专用的 Bucket，并获取团队的 AK/SK。
2. 在 Forge 的「存储管理」中新建 S3 存储：
   - Endpoint：S3 兼容域名 `s3-{region}.mi-fds.net`（参见 FDS 文档《8. API 参考》）。`cnbj3-fusion` 对应的具体域名以控制台展示为准。
   - Region：`cnbj3-fusion`；Bucket、AK/SK 填第 1 步的值。
   - 先点「测试连接」，通过后再保存。
3. 下载链接：如果测试设备可以直接访问 FDS 内网域名，可以把 `publicBaseUrl` 配置为 Bucket 的访问地址，客户端直接从 FDS 下载。否则留空，由 Forge 的下载接口代理下载。

**待验证**：Forge 使用的 aws-sdk-go-v2 较新版本，默认会附带 CRC32 校验头。部分 S3 兼容服务不支持这个头，会导致上传失败。如果对接 FDS 时遇到这种情况，需要在存储配置中增加一个通用开关"仅在必需时计算校验和"。这个开关属于 S3 适配器的通用能力，不是针对 FDS 的特殊处理。

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

在 Forge 的「平台 → 构建适配器」中新建「YRN」适配器，需要拉包的 RN 模块都选用它，业务仓库不需要新增 Dockerfile。

**参数**

| key | 说明 | 示例 |
| --- | --- | --- |
| `kind` | `business` 或 `common` | `business` |
| `entryFile` | 入口文件 | `app/index.js` |
| `configFile` | 包配置，可以使用 `{{.Environment}}` | `config/buz.config.{{.Environment}}.json` |
| `platforms` | 逗号分隔 | `android,ios` |
| `nodeImage` | 构建镜像 | `node:20` |

**产物**：每个平台目录的内容，与晴空塔包 tar 解开后客户端实际加载的内容一致，但**不再套 tar 和 zip，也不注入 md5 字段**。Forge 的清单本身已经给出每个文件的 sha256：

```text
dist/
  forge.json                    # {"runtimeVersion": "725-n1-m24"}
  android/
    index.android.hbc
    buz.config.json             # yrn-cli 生成的配置原样保留
    drawable-mdpi/...           # Android 资源目录
  ios/
    index.ios.hbc
    buz.config.json
    assets/res/...              # iOS 资源目录
```

**Dockerfile 模板（草稿）**

```dockerfile
# syntax=docker/dockerfile:1.10
FROM {{.Params.nodeImage}} AS build
WORKDIR /src
COPY . .
RUN --mount=type=secret,id=NPM_TOKEN,env=NPM_TOKEN npm ci
RUN set -eu; \
    for p in $(echo "{{.Params.platforms}}" | tr ',' ' '); do \
      npx yrn-cli build-{{.Params.kind}} --platform "$p" \
        --entry-file "{{.Params.entryFile}}" --config-file "{{.Params.configFile}}"; \
      sh .forge/collect.sh "$p" "/app/dist/$p"; \
    done; \
    node .forge/write-forge-json.js "{{.Params.configFile}}" > /app/dist/forge.json

FROM scratch
COPY --from=build /app/dist/ /
```

适配器的辅助文件：

- `.forge/collect.sh`：把 yrn-cli 的输出整理成上面的平台目录结构。
- `.forge/write-forge-json.js`：读取配置中的三个兼容参数，按第 4 节的规则输出。

**待补**：yrn-cli 的输出目录，以及 Hermes 编译（`.hbc`）、`drawable-mdpi`、`assets/res` 这些产物是由 yrn-cli 生成，还是由晴空塔的打包脚本处理。需要拿到 yrn-cli 的文档或晴空塔打包脚本，才能写完 `collect.sh`。这是 Forge 这边的待办，和客户端无关。

**构建变量**：在模块上配置密钥变量 `NPM_TOKEN`，值为 `pkgs.d.xiaomi.net` 的读取令牌。

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
| 灰度 uid / 白名单 | 定向发布的 `identifier`（分支名） | 测试场景下用"通道"代替人群灰度 |

## 7. 客户端拉包

```
GET {forgeBaseUrl}/api/v1/artifacts/applications/{appSlug}/environments/{env}/manifest
    ?platform={android|ios}&runtimeVersion={第 4 节规则}&identifier={分支名，可选}
Authorization: Bearer {访问令牌}
```

- 访问令牌在 Forge 的「访问令牌」页面生成，只写进测试包。
- 每个文件带 `path`、`url`、`sha256`、`sizeBytes`。客户端下载后先校验 sha256，再交给加载器。
- `missingModules` 列出当前没有可用包的模块及原因：`no_release_for_runtime`、`platform_unavailable`、`release not found`。

客户端需要实现的功能和改动见 [客户端需求](xiaomi-rn-client-requirements.md)。
