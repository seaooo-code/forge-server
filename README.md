# forge-server

`forge-server` 是基于 Echo、PostgreSQL、Redis 的 Go HTTP 服务。

## 启动服务

```bash
go run ./cmd/forge-server
```

构建执行进程独立部署：

```bash
go run ./cmd/forge-worker
```

本地启动后，可以在 Apifox 导入：

```text
http://localhost:8080/openapi.json
```

## 主要接口

- `GET /healthz`：进程存活检查。
- `GET /readyz`：PostgreSQL 和 Redis 就绪检查。
- `GET /api/v1/ping`：API 连通性检查。
- `GET /api/v1/enums`：全局枚举选项。
- `GET /api/v1/environments`：环境列表。
- `GET /api/v1/environments/:code`：环境详情。
- `GET /api/v1/applications`：应用列表。
- `GET /api/v1/applications/options`：应用表单选项。
- `GET /api/v1/applications/:slug`：应用详情。
- `POST /api/v1/applications`：创建应用。
- `PATCH /api/v1/applications/:slug`：更新应用。
- `PATCH /api/v1/applications/:slug/status`：上线或下线应用。
- `GET /api/v1/applications/:slug/delete-check`：检查应用是否可删除。
- `DELETE /api/v1/applications/:slug`：依赖检查通过后彻底删除应用。
- `GET /api/v1/code-sources`：代码源应用安装状态。
- `GET /api/v1/code-sources/:provider/manifest`：获取 GitHub App Manifest 创建参数。
- `GET /api/v1/code-sources/:provider/manifest/callback`：接收 GitHub App Manifest 创建回调并保存 App 凭据。
- `GET /api/v1/code-sources/:provider/install-url`：获取 GitHub App 安装地址。
- `GET /api/v1/code-sources/:provider/install/callback`：接收 GitHub App 安装完成回调。
- `DELETE /api/v1/code-sources/:provider/installations`：删除当前登录用户的 GitHub App 安装授权和仓库缓存。
- `GET /api/v1/code-sources/:provider/repositories/search`：搜索 GitHub App 已授权仓库。
- `GET /api/v1/builds`：查询 RN JS bundle 构建记录列表。
- `GET /api/v1/builds/:id`：查询 RN JS bundle 构建记录详情和产物树。
- `POST /api/v1/builds`：传入模块 id、环境和可选分支，创建 RN JS bundle 构建记录。
- `GET /api/v1/artifacts/applications/:applicationSlug/modules/:moduleId/environments/:environment/builds/:buildId/file?path=...`：下载指定构建产物中的单个文件，支持控制台 session 或 `Authorization: Bearer <token>`。
- `GET /api/v1/artifacts/applications/:applicationSlug/environments/:environment/manifest`：查询应用在指定环境下的模块 manifest 配置，可传 `identifier` 优先解析标识发布；客户端需携带 `Authorization: Bearer <token>`，token 来自 `FORGE_ARTIFACT_ACCESS_TOKENS`。
- `GET /api/v1/storage/configurations`：存储配置列表。
- `GET /api/v1/storage/configurations/:id`：存储配置详情。
- `POST /api/v1/storage/configurations/test`：测试存储配置连通性。
- `POST /api/v1/storage/configurations`：创建存储配置。
- `PATCH /api/v1/storage/configurations/:id`：更新存储配置。
- `DELETE /api/v1/storage/configurations/:id`：删除存储配置。
- `GET /api/v1/feishu/users/search`：搜索飞书用户，用于负责人选择。
- `GET /api/v1/auth/feishu/authorize`：跳转飞书 OAuth 授权页。
- `GET /api/v1/auth/feishu/callback`：交换飞书 OAuth code，获取用户信息并写入 `feishu_users`。
- `GET /api/v1/auth/session`：从服务端 session cookie 返回当前飞书登录会话。
- `POST /api/v1/auth/logout`：删除当前飞书登录会话。
- `GET /openapi.json`：OpenAPI 3.0 文档。

## 数据库迁移

服务启动时会自动执行 `migrations/` 下的 PostgreSQL 迁移文件。也可以手动执行：

```bash
go run ./cmd/forge-migrate
```

## 配置

配置从环境变量读取。启动时如果仓库根目录存在 `.env`，服务会自动加载；`.env.example` 只作为模板，不会被默认加载。

如需指定其他 dotenv 文件：

```bash
ENV_FILE=/path/to/file go run ./cmd/forge-server
```

已存在的进程环境变量优先级高于 dotenv 文件中的值。常用配置见 `.env.example`，主要包括：

- `APP_ENV`、`APP_SHUTDOWN_TIMEOUT`：应用运行环境与关闭超时。
- `HTTP_ADDR`、`HTTP_CORS_ALLOW_*`：HTTP 监听地址和 CORS 配置。
- `POSTGRES_*`：PostgreSQL 连接和连接池配置。
- `REDIS_*`：Redis 连接配置。
- `FEISHU_*`：飞书应用凭证、回调地址、授权范围和调试开关。
- `GITHUB_*`：GitHub App 安装、授权和仓库搜索配置。
- `FORGE_WORKER_*`：构建 worker 的工作目录、轮询间隔、租约、Dockerfile 和产物对象前缀。
- `OTEL_*`：OpenTelemetry 追踪和日志导出配置。

飞书 OAuth 至少需要配置 `FEISHU_APP_ID`、`FEISHU_APP_SECRET`、`FEISHU_REDIRECT_URI`，并且需要在飞书开放平台中配置相同的回调地址。配合 `forge-console` 本地开发时，回调地址使用：

```text
http://127.0.0.1:5173/login/feishu/callback
```

同时保持：

```text
HTTP_CORS_ALLOW_CREDENTIALS=true
```

`FEISHU_OAUTH_SCOPE` 默认保持为空，使用飞书应用已配置的权限；只有需要显式收窄授权范围时再设置。飞书用户搜索使用当前登录用户的 `user_access_token`，需要在飞书开放平台为应用开启对应通讯录权限。搜索结果中的邮箱来自飞书批量获取用户信息接口，应用需要具备 `contact:user.email:readonly` 字段权限才会返回该字段。

飞书开放平台的固定接口地址不通过环境变量暴露，由 `internal/config/feishu.go` 统一管理。

代码源管理采用 GitHub App Manifest 流程：页面点击创建后跳转 GitHub，GitHub 创建 App 后回调 `GITHUB_APP_MANIFEST_CALLBACK_URL`，服务端会换取并保存 App ID、slug、private key 和 webhook secret；随后自动跳转到 GitHub App 安装页。默认本地回调地址为：

```text
http://127.0.0.1:8080/api/v1/code-sources/github/manifest/callback
```

GitHub App 安装完成后，GitHub 会回调 `GITHUB_APP_INSTALL_CALLBACK_URL`，服务端记录 installation 元信息；搜索仓库时会用已保存的 GitHub App 私钥签 JWT，换取 installation access token，并读取该 installation 已授权的仓库。

线上部署时需要把以下地址改成公网可访问域名：

```text
GITHUB_APP_HOMEPAGE_URL
GITHUB_APP_MANIFEST_CALLBACK_URL
GITHUB_APP_INSTALL_CALLBACK_URL
GITHUB_APP_INSTALL_REDIRECT_URL
```

`GITHUB_APP_ID`、`GITHUB_APP_SLUG` 和 `GITHUB_APP_PRIVATE_KEY` 只作为兼容手动创建 GitHub App 的可选兜底配置；默认不需要手动填写。`GITHUB_API_BASE_URL`、`GITHUB_WEB_URL` 和 `GITHUB_API_VERSION` 默认使用 github.com，可按 GitHub Enterprise 环境覆盖。

## 构建 worker

`forge-server` 只负责通过 `POST /api/v1/builds` 创建 `pending` 构建记录；`forge-worker` 通过 PostgreSQL 轮询并用 `FOR UPDATE SKIP LOCKED` 抢占记录，因此可以和 HTTP 服务独立部署，也可以水平扩容多个 worker。

worker 执行流程：

1. 使用模块绑定的 GitHub App installation id 换取 installation access token。
2. 构建记录 id 使用创建时间 `yyyyMMddHHmmss`，按构建记录里的 `branch` 拉取模块 Git 仓库；API 未传分支时回落到模块记录里的 `default_branch`。
3. 在仓库根目录执行 Dockerfile 构建镜像，推荐 Dockerfile 把产物写入最终镜像内 `/app/dist`。
4. 导出构建产物到 worker 临时目录，并上传到环境绑定的 S3 存储配置。worker 默认从临时容器复制 `/app/dist`；也兼容 `FROM scratch` 产物镜像里通过 `COPY --from=<stage> /app/dist/ /` 暴露到根目录的写法，这类镜像会通过 `docker build --output` 直接导出最终产物层。上传时递归上传产物 `dist` 下的文件和目录；对象路径为 `rn-bundles/{应用名}/{模块名}/{环境名}/{yyyyMMddHHmmss}/{dist 内相对路径}`。
5. 回写构建记录状态、错误信息、`artifactKey` 和 `artifactUrl`。

worker 常用配置：

- `FORGE_WORKER_ID`：worker 实例标识，默认使用主机名。
- `FORGE_WORKER_WORK_DIR`：构建临时目录，默认 `/tmp/forge-worker`。
- `FORGE_WORKER_POLL_INTERVAL`：轮询间隔，默认 `5s`。
- `FORGE_WORKER_LEASE_DURATION`：任务租约，默认 `30m`；运行时不会短于构建超时。
- `FORGE_WORKER_BUILD_TIMEOUT`：单次构建超时，默认 `30m`。
- `FORGE_WORKER_DOCKERFILE`：仓库内 Dockerfile 路径，默认 `Dockerfile`。
- `FORGE_WORKER_ARTIFACT_PREFIX`：S3 对象前缀，默认 `rn-bundles`。

## 可观测性

OpenTelemetry 默认关闭。导出追踪到本地 OpenTelemetry Collector：

```bash
OTEL_ENABLED=true
OTEL_SERVICE_NAME=forge-server
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
```

导出追踪和结构化 HTTP 交换日志到 ClickStack 时，将导出地址指向 ClickStack OpenTelemetry Collector。ClickStack Open Source 的采集密钥可在 HyperDX 应用的 Team Settings / API Keys 中获取：

```bash
OTEL_ENABLED=true
OTEL_SERVICE_NAME=forge-server
OTEL_SERVICE_VERSION=local
OTEL_DEPLOYMENT_ENVIRONMENT=local
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
OTEL_EXPORTER_OTLP_HEADERS=authorization=<your-clickstack-ingestion-key>
```

启用可观测性后，Echo 入站请求和飞书出站 HTTP 调用都会写入追踪。每个入站 HTTP 端点还会发送一条结构化 `http.exchange` OTLP 日志，包含原始查询参数、请求头、请求体、响应头和响应体；这些内容不会做脱敏。日志会携带请求上下文，便于通过 trace ID 和 span ID 关联 HTTP span。

## 目录结构

```text
.
├── cmd/
│   ├── forge-migrate/   # 手动执行数据库迁移的入口
│   ├── forge-server/    # HTTP 服务启动入口
│   └── forge-worker/    # RN JS bundle 构建 worker 入口
├── internal/
│   ├── app/             # 应用装配，连接配置、基础设施、服务和 HTTP 服务
│   ├── config/          # 运行时配置解析与校验
│   ├── domain/          # 领域模型、领域错误和领域规则
│   ├── infra/           # 外部基础设施适配
│   │   ├── feishu/      # 飞书 HTTP 客户端
│   │   ├── otel/        # OpenTelemetry 初始化
│   │   ├── postgres/    # PostgreSQL 连接、迁移和仓储实现
│   │   ├── redis/       # Redis 客户端初始化
│   │   └── s3/          # S3 兼容对象存储客户端
│   ├── server/
│   │   └── http/        # Echo 路由、中间件、处理器和 OpenAPI 文档
│   ├── service/         # 业务用例和仓储接口
│   └── worker/          # 构建任务轮询和 git/docker/S3 执行器
├── migrations/          # PostgreSQL 迁移文件
├── AGENTS.md            # 项目开发约束
├── README.md            # 项目说明
├── go.mod               # Go module 定义
└── go.sum               # Go 依赖校验文件
```

目录分层和代码约束以 `AGENTS.md` 为准。新增代码时优先放入现有分层，不为单个文件创建过深目录。
