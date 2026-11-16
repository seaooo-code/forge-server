# 小米 RN 接入指南

本文是 Forge 在小米内部接入 RN 测试环境时的约定，属于接入层文档，不属于平台能力。平台能力见 [RN 测试环境接入 P0 方案](rn-test-env-p0-plan.md)。这里写的内容只能通过配置、Dockerfile 和客户端实现来落地，不允许把它们写进 Forge 的代码里。

## 1. 部署前提

- forge-server 和 forge-worker 部署在公司内网，并且能访问：
  - `git.n.xiaomi.com`：拉取代码，接收 webhook 回调；
  - `pkgs.d.xiaomi.net`：npm 私有源；
  - 测试环境使用的对象存储。
- worker 所在宿主机的 Docker 版本要求 ≥ 23，并启用 BuildKit（Docker 23 起默认启用）。
- 第 5 节 Dockerfile 用到的 `--mount=type=secret,...,env=` 写法要求 Dockerfile 语法 ≥ 1.10，所以文件第一行要声明 `# syntax=docker/dockerfile:1.10`。这一行会让 BuildKit 拉取 `docker/dockerfile:1.10` 前端镜像，worker 需要能访问 Docker Hub 或内网镜像源。
- GitLab 需要能访问 forge-server 的 webhook 地址。

## 2. GitLab 连接

1. 在业务所在的 GitLab Group 下，打开 *Settings → Access Tokens*，创建 Group Access Token：
   - 角色：Reporter（只需要读取代码）；
   - scope：`read_api`、`read_repository`；
   - 有效期：按公司规定设置，并指定一位负责续期的同学。
2. 在 Forge 的「代码源 → 新建 GitLab 连接」中填写：
   - 实例地址：`https://git.n.xiaomi.com`；
   - 令牌：第 1 步创建的 Access Token。
3. 绑定模块之后，到 GitLab 项目的 *Settings → Webhooks* 中添加 webhook：
   - URL 和 Secret token：从模块详情页复制；
   - 触发事件：只勾选 *Push events*。

## 3. runtimeVersion 约定

YRN 体系通过三个参数判断一个包和客户端是否兼容：`rnEngineVersion`、`maxNativeApiLevel`、`moduleIdMapVersion`。接入 Forge 时，把这三个参数拼成一个字符串，作为 `runtimeVersion`：

```text
{rnEngineVersion}-n{maxNativeApiLevel}-m{moduleIdMapVersion}
例：610-n3-m3
```

- 构建端：Dockerfile 从包配置（例如 `config/buz.config.*.json` 里的 `SDKVersion`、`maxNativeApiLevel`、`moduleIdMapVersion`）读取这三个值，拼好后写入 `dist/forge.json`。
- 客户端：用相同规则拼接后作为参数上报。
- common 包和业务包使用同一个 `runtimeVersion`。common 包的模块 ID 表发生变化时，必须升级 `moduleIdMapVersion`。这样新旧两组包会自动隔离，互不影响。
- 与晴空塔的区别：晴空塔按"小于等于"匹配，Forge 只做精确匹配。在测试环境里，旧 App 拿不到新包，只会显示"无可用包"。

## 4. 构建变量

| key | 类型 | 说明 |
| --- | --- | --- |
| `NPM_TOKEN` | 密钥 | `pkgs.d.xiaomi.net` 的读取令牌 |

平台会自动注入 `FORGE_ENVIRONMENT`。Dockerfile 用它来选择对应环境的配置文件，例如 `buz.config.${FORGE_ENVIRONMENT}.json`。因此 Forge 中的环境 code 需要和仓库里配置文件名中的环境段保持一致。

## 5. Dockerfile 模板

下面以业务包为例。产物路径要等第 6 节确认之后才能定下来。

```dockerfile
# syntax=docker/dockerfile:1.10
FROM node:20 AS build
ARG FORGE_ENVIRONMENT
WORKDIR /src
COPY package.json package-lock.json .npmrc ./
RUN --mount=type=secret,id=NPM_TOKEN,env=NPM_TOKEN npm ci
COPY . .
RUN for p in android ios; do \
      npx yrn-cli build-business --platform "$p" --entry-file app/index.js \
        --config-file "config/buz.config.${FORGE_ENVIRONMENT}.json" && \
      ./scripts/collect-bundle.sh "$p" "/app/dist/$p"; \
    done && \
    node scripts/write-forge-json.js "config/buz.config.${FORGE_ENVIRONMENT}.json" > /app/dist/forge.json

FROM scratch
COPY --from=build /app/dist /
```

- common 包把 `build-business` 换成 `build-common`。如果 common 包和业务包在同一个仓库里，可以各写一个 Dockerfile，分别绑定为两个模块。
- `write-forge-json.js` 负责读取配置里的三个兼容参数，并按第 3 节的规则输出 `{"runtimeVersion": "..."}`。
- 密钥变量在 Dockerfile 里按变量 key 挂载（`id=NPM_TOKEN`）。worker 实际传给 docker CLI 的环境变量名是 `FORGE_BUILD_SECRET_NPM_TOKEN`，Dockerfile 不需要关心。

## 6. 产物格式（待客户端确认）

以下是推断，还没有核实：现有 YRN 加载器从晴空塔拿到的包是"每个模块、每个平台一个压缩包"，包里有 bundle、`res` 目录，以及带 `YRN_BUNDLE_MD5` 的 config。如果确实如此，最省事的做法是让 `collect-bundle.sh` 产出和晴空塔一样的压缩包，放到 `dist/{platform}/` 下。这样客户端只需要替换"从哪里拿包"这一层。

需要和客户端同学确认的问题：

1. 加载器需要的是单个 zip 还是散文件？zip 里的目录结构是什么样的？config 有哪些字段？
2. 完整性校验用 md5 还是别的算法？如果必须用 md5，就在 Forge 里开启清单的 `md5` 摘要（这是平台的通用配置）。
3. 客户端在拉包时能否读到 `rnEngineVersion`、`maxNativeApiLevel`、`moduleIdMapVersion`？
4. 测试包怎样切换到 Forge 源？是用调试菜单开关，还是做成单独的构建变体？identifier（分支名）在哪里输入？
5. common 包和业务包的加载顺序是什么？common 缺失时，业务包是否直接不加载？

## 7. 客户端拉包

```
GET {forgeBaseUrl}/api/v1/artifacts/applications/{appSlug}/environments/{env}/manifest
    ?platform={android|ios|harmony}&runtimeVersion={第 3 节规则}&identifier={分支名，可选}
Authorization: Bearer {访问令牌}
```

- 访问令牌在 Forge 的「访问令牌」页面生成，只写进测试包里。
- 返回的每个文件都带有 `url`、`sha256` 和 `digests`。`digests` 按算法名给出摘要（例如 `{"sha256": "...", "md5": "..."}`），开启 md5 需要在 worker 上配置 `FORGE_ARTIFACT_DIGESTS=sha256,md5`。客户端下载后先做校验，再交给加载器。
- `missingModules` 里的模块表示当前没有可用的包，客户端按第 6 节第 5 条确认的失败策略处理。
