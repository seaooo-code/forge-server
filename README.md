# forge-server

`forge-server` 是基于 Echo、PostgreSQL、Redis 的 Go HTTP 服务。

## 启动服务

```bash
go run ./cmd/forge-server
```

本地启动后，可以在 Apifox 导入：

```text
http://localhost:8080/openapi.json
```

## 主要接口

- `GET /healthz`：进程存活检查。
- `GET /readyz`：PostgreSQL 和 Redis 就绪检查。
- `GET /api/v1/ping`：API 连通性检查。
- `GET /api/v1/environments`：环境列表。
- `GET /api/v1/environments/:code`：环境详情。
- `GET /api/v1/applications`：应用列表。
- `GET /api/v1/applications/options`：应用表单选项。
- `GET /api/v1/applications/:slug`：应用详情。
- `POST /api/v1/applications`：创建应用。
- `PATCH /api/v1/applications/:slug`：更新应用。
- `DELETE /api/v1/applications/:slug`：删除应用。
- `GET /api/v1/storage/configurations`：存储配置列表。
- `GET /api/v1/storage/configurations/:id`：存储配置详情。
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
│   └── forge-server/    # HTTP 服务启动入口
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
│   └── service/         # 业务用例和仓储接口
├── migrations/          # PostgreSQL 迁移文件
├── AGENTS.md            # 项目开发约束
├── README.md            # 项目说明
├── go.mod               # Go module 定义
└── go.sum               # Go 依赖校验文件
```

目录分层和代码约束以 `AGENTS.md` 为准。新增代码时优先放入现有分层，不为单个文件创建过深目录。
