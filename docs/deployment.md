# 部署说明

本文说明 forge-server（API 进程）的镜像和容器部署。worker 需要访问 Docker daemon，部署方式另定，不在本镜像内；扩缩容机制见 [worker 扩缩容](worker-scaling.md)。前端部署见 forge-console 的 `docs/deployment.md`。

## 1. 镜像

根目录 [`Dockerfile`](../Dockerfile) 分两个阶段：

| 阶段 | 镜像 | 说明 |
| --- | --- | --- |
| 构建 | `micr.cloud.mioffice.cn/devx-build-image/golang:1.25-ubuntu24-base` | DevX Go 1.25 镜像，已配置公司 Go 代理。静态编译 `forge-server` 和 `forge-migrate`（`CGO_ENABLED=0`，`GOAMD64=v1`） |
| 运行 | `micr.cloud.mioffice.cn/devx-build-image/base:ubuntu-2404-mini` | 从内网 apt 源安装 `ca-certificates` 和 `tzdata`，以 `work` 用户（uid 10000）运行 |

- 镜像内容：`/app/forge-server`、`/app/forge-migrate`、`/app/migrations/`，工作目录 `/app`，默认启动 `forge-server`，监听 `8080`。
- 镜像里不包含 `.env`，所有配置通过环境变量注入（`.dockerignore` 排除了 `.env*`）。
- 只使用标准 Dockerfile 语法，MIFLOW（kaniko）和本地 `docker build` 都能构建。

本地构建并试运行（Apple Silicon 上需要 `--platform linux/amd64`）：

```bash
docker build --platform linux/amd64 -t forge-server:local .
docker run --rm -p 18082:8080 --env-file <环境变量文件> forge-server:local
```

## 2. 依赖

| 依赖 | 要求 |
| --- | --- |
| PostgreSQL | 15 及以上（用到 `unique nulls not distinct`），通过 `POSTGRES_DSN` 配置 |
| 出站网络 | 飞书开放平台、`git.n.xiaomi.com`（代码源 API）、FDS S3 兼容域名 |
| 入站网络 | GitLab 能访问 webhook 地址 `https://<域名>/api/v1/webhooks/gitlab/{连接 ID}`（模块详情页可复制）；浏览器经前端域名的 `/api/` 转发访问 |

## 3. 环境变量

完整列表见 [`.env.example`](../.env.example)。部署时至少需要：

| 变量 | 说明 |
| --- | --- |
| `APP_ENV` | 环境名，例如 `test` |
| `POSTGRES_DSN` | 数据库连接串 |
| `FORGE_SECRET_KEY` | 加密代码源凭证、webhook secret、密钥变量的主密钥（32 字节 base64）。**部署后不能更换**，否则已加密的数据无法解密 |
| `HTTP_CORS_ALLOW_ORIGINS` | 同域部署时填前端域名即可；前后端不同域时必须填前端域名 |
| `FORGE_METRICS_TOKEN` | 可选，`/metrics` 的 Bearer 令牌 |

`FORGE_SECRET_KEY`、`POSTGRES_DSN` 放在平台的密钥配置里，不要写进镜像、代码仓库或流水线变量明文。

### KeyCenter 数据库密码（小米内部）

公司数据库平台不提供明文密码，只给用 SID 加密的密文。主程序通过通用的 `POSTGRES_PASSWORD_COMMAND` 在连接前取密码，小米内部用 [`tools/kc-password`](../tools/kc-password) 调 KeyCenter 解密。它是独立 module，KeyCenter SDK 是内网私有模块，不进入主 module 依赖；对应镜像用 [`Dockerfile.xiaomi`](../Dockerfile.xiaomi) 构建。

1. 数据库平台的「账号」Tab，对读写账号点【复制连接配置】→ Golang 格式，得到明文账号和 KeyCenter 密文密码。迁移需要写权限，用读写账号。
2. Matrix 部署页配置 SID（输入小米人 6 位动态验证码），`kc-agent` sidecar 起来后，容器内 `127.0.0.1:9988` 就是 agent。容器环境不需要认证密钥，也不需要 IP 白名单。
3. 配置环境变量：

| 变量 | 值 |
| --- | --- |
| `POSTGRES_DSN` | `postgres://<账号>@<地址>/<库名>?sslmode=<按实例要求>`，不写密码 |
| `POSTGRES_PASSWORD_COMMAND` | `/app/kc-password` |
| `KEYCENTER_SID` | 数据库账号绑定的 SID，例如 `forge-staging` |
| `POSTGRES_PASSWORD_CIPHER` | 复制出来的密文 |
| `KEYCENTER_AGENT_HOST`、`KEYCENTER_AGENT_PORT` | 可选，默认 `127.0.0.1:9988` |

`forge-server`、`forge-migrate`、`forge-worker` 共用这套配置。解密只发生在进程启动时，数据库换密码后需要更新密文并重启。

本地调试可以把 `KEYCENTER_AGENT_HOST` 设为 `kcagent.sec.xiaomi.com`（公共 agent，仅限办公网本地开发，只连测试环境，禁止在 staging、生产、CI 使用）。要求本机 IP 已关联小米人账号、账号是 SID 的管理员或发布者，且测试环境有已开启并分发到公共 agent IP 的认证密钥，详见 KeyCenter 文档《本地开发环境 - 有认证公共 KC agent 使用文档》。

## 4. 部署到 Matrix

### 4.1 部署空间

| 项 | 值 |
| --- | --- |
| 端口 | `8080` |
| 规格 | 通用型超微型（0.2 核 / 1G）或微型（0.5 核 / 2G）。API 进程只处理请求和日志清理，构建由 worker 执行 |
| 实例数 | 首次部署先起 1 个实例（见 4.3），之后按需增加 |

健康检查（都用 http，端口 `8080`）：

| 检查 | 路径 | 建议参数 |
| --- | --- | --- |
| 启动检查 | `/healthz` | 首次延迟 2 秒，每隔 3 秒，失败阈值 20 次，超时 2 秒 |
| 存活检查 | `/healthz` | 每隔 10 秒，失败阈值 3 次，超时 2 秒 |
| 就绪检查 | `/readyz` | 每隔 5 秒，失败阈值 3 次，超时 3 秒 |

存活检查不能用 `/readyz`：`/readyz` 会检查 PostgreSQL，数据库短暂不可用时会让所有实例被反复重启。

### 4.2 转发规则

与前端共用一个域名，`/api/` 转发到 forge-server，其余路径转发到 forge-console（见前端部署说明）。`/healthz`、`/readyz`、`/metrics`、`/openapi.json` 不需要对外暴露；需要外部采集 `/metrics` 时单独配置转发并设置 `FORGE_METRICS_TOKEN`。

### 4.3 数据库迁移

- forge-server 启动时会自动执行 `migrations/` 下尚未执行的迁移，每个迁移文件在一个事务里执行。
- 多个实例同时首次启动时，可能同时执行同一个迁移，其中一个会因为表已存在而启动失败，重启后恢复正常。首次部署先起 1 个实例，或者先单独执行迁移：

```bash
/app/forge-migrate
```

`forge-migrate` 加载与 forge-server 相同的配置，环境变量要一并提供（包括 `FORGE_SECRET_KEY`）。

### 4.4 其他

- 日志为 JSON，写 stdout。
- 收到 SIGTERM 后在 `APP_SHUTDOWN_TIMEOUT`（默认 10 秒）内优雅退出。
- session cookie 目前没有设置 `Secure` 属性，HTTPS 下可以正常使用；后续建议在 HTTPS 部署时加上。

### 4.5 飞书登录租户

飞书登录不需要环境变量。首次部署后：

1. 在服务日志里找到 `platform not initialized` 这一行，复制其中的 `setupToken`。令牌只在当前进程内有效，重启后会重新生成；所以首次部署只运行 1 个实例。
2. 打开控制台，会进入「初始化平台」页面。填写令牌、租户名称和第一个飞书应用的 App ID、App Secret。
3. 回到登录页登录。**第一个登录的用户会成为 SuperAdmin**，请自己先完成登录。之后初始化入口永久关闭，其他租户在「平台 → 飞书租户」维护。

- 每个租户对应一个飞书租户里的自建应用。登录页列出所有启用中的租户，只有一个时不显示选择框。
- App Secret 用 `FORGE_SECRET_KEY` 加密存库，接口不返回。App ID 创建后不能修改：同一个人在不同应用下的 open_id 不同，换应用应新建租户。
- 不同租户的账号互不相通：同一个人在两个租户下是两个用户，全局角色和应用成员要分别授予；负责人搜索只搜当前登录租户的通讯录。
- 停用租户会立即让它的所有会话失效；平台至少要保留一个启用中的租户。
- 每个租户的飞书应用都要把 `https://<前端域名>/login/feishu/callback` 加进重定向 URL 白名单（回调地址由前端按自身域名传入，后端只接受这个路径），并开通 `offline_access`、`contact:user:search`、`contact:contact.base:readonly`、`contact:user.email:readonly`（见 README 飞书登录一节）。

## 5. 验证

1. `GET /healthz` 返回 `{"status":"ok"}`；
2. `GET /readyz` 返回 `ready`，`postgres` 是 `ok`；
3. 经前端域名访问 `GET /api/v1/ping`，返回 forge-server 的 JSON；
4. 走一遍飞书登录；
5. 在 GitLab 项目里发送一次 webhook 测试请求，确认能收到。
