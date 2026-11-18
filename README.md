# Forge Server

Forge Server 是 RN Bundle 交付平台的后端服务，负责应用与模块管理、GitHub 代码源授权、构建任务编排、S3 产物存储、灰度发布路由和客户端 manifest 分发。

## 系统组成

```mermaid
flowchart LR
  Console["Forge Console"] -->|"Feishu session / REST"| API["forge-server"]
  Client["RN Client"] -->|"Bearer token / manifest"| API
  API --> PostgreSQL[(PostgreSQL)]
  API --> GitHub["GitHub App API"]
  Worker["forge-worker"] --> PostgreSQL
  Worker --> GitHub
  Worker --> Docker["Git + Docker"]
  Worker --> S3["S3-compatible storage"]
  API --> S3
```

仓库提供三个独立入口：

| 进程 | 职责 |
| --- | --- |
| `cmd/forge-server` | 启动 Echo HTTP API、自动迁移数据库、维护飞书会话和发布路由 |
| `cmd/forge-worker` | 从 PostgreSQL 抢占待构建任务，执行 Git、Docker 和 S3 上传 |
| `cmd/forge-migrate` | 仅执行 PostgreSQL 数据库迁移 |

## 核心能力

- 应用、成员、环境、模块管理，以及基于全局角色和应用成员的权限校验
- GitHub App Manifest 创建、安装授权和仓库搜索
- RN Bundle 构建记录、执行状态和产物树管理
- S3 兼容对象存储配置与连通性检查
- 全量、比例灰度和定向灰度发布
- 面向客户端的应用 manifest 与单文件产物下载
- 飞书 OAuth 登录、用户搜索和 session 管理
- OpenAPI 3.0 文档、健康检查和 OpenTelemetry 导出

## 本地运行

### 环境要求

- Go 1.25 或更高版本
- PostgreSQL 15 或更高版本
- 飞书开放平台应用
- 运行 `forge-worker` 时还需要 Git、可用的 Docker daemon 和 S3 兼容对象存储

### 准备配置

```bash
cp .env.example .env
```

默认配置连接：

```text
PostgreSQL  postgres://postgres:postgres@127.0.0.1:5432/forge_server?sslmode=disable
HTTP        http://127.0.0.1:8080
```

PostgreSQL 中需要提前创建 `forge_server` 数据库。API 启动时会连接 PostgreSQL 并执行迁移，数据库不可用会导致初始化失败。

飞书登录不需要环境变量。首次启动时租户表为空，日志会打印一行 `platform not initialized` 和 `setupToken`；打开控制台会进入「初始化平台」页面，填入令牌和第一个飞书应用的 App ID、App Secret 即可。之后第一个登录的用户成为 SuperAdmin，其他租户在「平台 → 飞书租户」维护。

每个飞书应用都要把 `{前端地址}/login/feishu/callback`（本地是 `http://127.0.0.1:5173/login/feishu/callback`）加进重定向 URL 白名单。与本地 `forge-console` 联调时同时保持：

```dotenv
HTTP_CORS_ALLOW_ORIGINS=http://127.0.0.1:5173,http://localhost:5173
HTTP_CORS_ALLOW_CREDENTIALS=true
```

登录申请的权限固定为 `internal/infra/feishu` 里的 `OAuthScope`，每个租户的飞书应用后台都要开通：`offline_access`（「持续访问已授权数据」，没有它不返回 refresh token，登录态约 2 小时失效）、`contact:user:search`（「搜索用户」，负责人搜索；缺少时飞书返回 `99991679`）、`contact:contact.base:readonly` 和 `contact:user.email:readonly`（补全邮箱，可选，缺失时搜索结果不带邮箱）。后台新开通的权限需要用户重新授权后才生效。负责人搜索使用当前登录用户的 `user_access_token`；如需返回邮箱，还要为应用开通 `contact:user.email:readonly` 字段权限。

### 启动 API

```bash
go run ./cmd/forge-server
```

服务和 worker 启动时都会自动执行 `migrations/`。只需要手动迁移时运行：

```bash
go run ./cmd/forge-migrate
```

### 启动构建 Worker

```bash
go run ./cmd/forge-worker
```

API 与 worker 使用同一个 PostgreSQL。worker 通过 `FOR UPDATE SKIP LOCKED` 抢占任务，可以独立部署和水平扩容。

## 配置

配置来自进程环境变量。仓库根目录存在 `.env` 时会自动加载，也可显式指定其他文件：

```bash
ENV_FILE=/path/to/forge.env go run ./cmd/forge-server
```

已存在的进程环境变量优先于 dotenv 文件。完整字段与默认值见 [`.env.example`](.env.example)。

| 前缀 | 用途 |
| --- | --- |
| `APP_*` | 运行环境和优雅关闭超时 |
| `HTTP_*` | 监听地址、CORS origin、header 和 credentials |
| `POSTGRES_*` | PostgreSQL DSN 和连接池；`POSTGRES_PASSWORD_COMMAND` 可选，连接前执行命令取密码 |
| `FEISHU_*` | 只剩调试开关 `FEISHU_DEBUG_LOG_SEARCH_RESPONSE`；租户在控制台维护，回调地址由前端传入，scope 写在代码里 |
| `GITHUB_*` | GitHub App、回调地址、API 与 Web 地址 |
| `FORGE_WORKER_*` | worker ID、轮询、并发、租约、退出宽限期、构建超时、工作目录、产物前缀和依赖产物注入上限（`FORGE_WORKER_DEPS_MAX_BYTES`），扩缩容见 [worker 扩缩容](docs/worker-scaling.md) |
| `FORGE_METRICS_TOKEN` | `GET /metrics` 的可选 Bearer 令牌 |
| `OTEL_*` | OTLP trace 与 log 导出 |

## GitHub 代码源

代码源管理使用 GitHub App Manifest：

1. Forge Console 请求 manifest 并跳转 GitHub 创建 App。
2. GitHub 回调服务端，服务端保存 App ID、slug、private key 和 webhook secret。
3. 用户安装 GitHub App 并选择授权仓库。
4. 服务端使用 App JWT 换取 installation access token，搜索仓库或供 worker 拉取代码。

Manifest 默认申请 `Contents: Read-only` 和 `Metadata: Read-only` 仓库权限。安装 App 时必须把构建所需仓库纳入授权范围。

本地默认回调为：

```text
http://127.0.0.1:8080/api/v1/code-sources/github/manifest/callback
http://127.0.0.1:8080/api/v1/code-sources/github/install/callback
```

线上部署必须把 `GITHUB_APP_HOMEPAGE_URL`、`GITHUB_APP_MANIFEST_CALLBACK_URL`、`GITHUB_APP_INSTALL_CALLBACK_URL` 和 `GITHUB_APP_INSTALL_REDIRECT_URL` 改成公网可访问地址。

`GITHUB_APP_ID`、`GITHUB_APP_SLUG` 和 `GITHUB_APP_PRIVATE_KEY` 仅用于兼容手动创建的 GitHub App；默认流程会从数据库读取页面创建的 App 配置。

## 构建与发布链路

1. `POST /api/v1/builds` 创建 `pending` 构建记录。
2. worker 使用模块绑定的 GitHub installation ID 申请临时 token，并按指定分支浅克隆仓库。
3. worker 在仓库根目录执行 Dockerfile。默认从最终镜像的 `/app/dist` 导出产物；最终镜像为 `scratch` 且把 `/app/dist` 复制到根目录时，改用 `docker build --output` 导出。
4. worker 生成文件 manifest，将产物上传到环境绑定的 S3 存储，并回写构建状态和产物摘要。
5. 控制台基于成功构建创建全量或灰度发布；客户端携带访问令牌请求应用 manifest，服务端按环境、标识和灰度比例解析当前版本。

默认对象路径：

```text
rn-bundles/{应用 slug}/{模块 id}/{环境 code}/{构建记录 ID}/{dist 内相对路径}
```

构建失败时优先查看构建详情中的 `workerId` 和 `errorMessage`。GitHub clone 认证由 worker 动态申请 installation token，与平台“访问令牌”不是同一套凭证。

## 鉴权边界

| 请求类型 | 鉴权方式 |
| --- | --- |
| Forge Console 管理接口 | 飞书登录 session cookie，并叠加全局角色或应用成员权限 |
| 客户端应用 manifest | `Authorization: Bearer <artifact-access-token>` |
| 单文件产物下载 | 控制台 session 或 artifact access token |
| Worker 拉取 GitHub 仓库 | GitHub App installation access token |

系统中首位完成飞书登录的用户会被初始化为 `SuperAdmin`。后续权限由全局角色和应用成员角色共同决定。

## 部署

容器镜像、环境变量、Matrix 部署空间和健康检查配置见 [部署说明](docs/deployment.md)。worker 的部署方式另定，扩缩容机制见 [worker 扩缩容](docs/worker-scaling.md)。

## API 与健康检查

启动后可访问：

| 地址 | 说明 |
| --- | --- |
| `GET /healthz` | 进程存活检查 |
| `GET /readyz` | PostgreSQL 就绪检查 |
| `GET /metrics` | 构建队列指标（Prometheus 文本格式），见 [worker 扩缩容](docs/worker-scaling.md) |
| `GET /api/v1/ping` | API 连通性检查 |
| `GET /openapi.json` | 完整 OpenAPI 3.0 文档，可直接导入 Apifox |

业务接口统一位于 `/api/v1`。接口字段、查询参数、响应结构和状态码以运行时生成的 `/openapi.json` 为准。

仓库内 [`docs/openapi.json`](docs/openapi.json) 是同一文档的快照，供 `forge-console` 通过 `bun run gen:api` 生成接口类型。修改接口后运行以下命令更新快照：

```bash
UPDATE_OPENAPI_SNAPSHOT=1 go test ./internal/server/http -run TestOpenAPISnapshot
```

## 可观测性

OpenTelemetry 默认关闭。启用 OTLP HTTP 导出：

```dotenv
OTEL_ENABLED=true
OTEL_SERVICE_NAME=forge-server
OTEL_SERVICE_VERSION=local
OTEL_DEPLOYMENT_ENVIRONMENT=local
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
```

可使用 `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 和 `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` 分别覆盖 trace 与 log 地址，使用对应的 `*_HEADERS` 传递采集密钥。

启用后会采集 Echo 入站请求、飞书出站请求和结构化 `http.exchange` 日志。当前 HTTP exchange 日志包含原始查询参数、header、请求体和响应体，**不会自动脱敏**；生产环境启用前必须评估敏感信息暴露风险。

## 项目结构

```text
.
├── cmd/
│   ├── forge-server/     # HTTP API 入口
│   ├── forge-worker/     # 构建 worker 入口
│   └── forge-migrate/    # 手动数据库迁移入口
├── internal/
│   ├── app/              # 应用装配和进程生命周期
│   ├── config/           # 环境变量解析与校验
│   ├── domain/           # 领域模型、错误和规则
│   ├── infra/            # PostgreSQL、GitHub、GitLab、飞书、S3、OTel 适配
│   ├── server/http/      # Echo handler、中间件、路由和 OpenAPI
│   ├── service/          # 业务用例和仓储接口
│   └── worker/           # 任务轮询、Git、Docker 和 S3 执行器
├── migrations/           # 当前完整 PostgreSQL schema
├── .env.example          # 本地配置模板
├── AGENTS.md             # 开发与分层约束
└── README.md             # 项目入口文档
```

## 验证

```bash
go fmt ./...
go test ./...
go vet ./...
```

目录分层与提交规范以 [`AGENTS.md`](AGENTS.md) 为准。项目上线前数据库只维护 `migrations/000001_init_schema.sql` 这一份最新完整 schema；修改结构时还需要同步当前开发数据库。
