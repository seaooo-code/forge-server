# 项目约束

## Git 提交

- 提交标题使用格式：`<gitmoji> type(scope): 中文描述`。
- 中文描述不超过 50 字。
- `type` 使用常见约定：`feat`、`fix`、`docs`、`refactor`、`test`、`style`、`perf`、`chore`。
- 常用 gitmoji 映射：`✨ feat`、`🐛 fix`、`📝 docs`、`♻️ refactor`、`✅ test`、`🎨 style`、`⚡️ perf`、`🔧 chore`。
- `scope` 使用改动所属模块或目录名，例如 `api`、`config`、`infra`、`http`。
- 提交标题不加 Markdown 标记（如 `**`）。

## 改动校验

- 代码改动后至少运行 `gofmt -l .`（输出必须为空）、`go build ./...`、`go vet ./...` 与 `go test ./...`，全部通过后再提交。
- `docs/openapi.json` 是 `internal/server/http/openapi.go` 的快照，前端据此生成接口类型。修改接口或 OpenAPI 定义后运行 `UPDATE_OPENAPI_SNAPSHOT=1 go test ./internal/server/http -run TestOpenAPISnapshot` 更新快照，不要手改；快照过期时 `go test` 会失败。
- 本仓库与前端仓库 `../forge-console` 属于同一项目；接口字段或行为变更时，同步检查前端调用方，前端改动遵守 `forge-console/AGENTS.md`。

## 目录规范

- 入口只放在 `cmd/<binary>/main.go`，只能做配置加载、日志、信号处理和应用启动。
- 运行时配置放在 `internal/config`，只负责解析与校验环境变量，不连接外部服务。
- HTTP 传输层放在 `internal/server/http`，Echo 路由、中间件、handler 只做协议适配。
- 外部基础设施适配放在 `internal/infra/<name>`，例如 PostgreSQL、S3、第三方客户端初始化。
- 业务用例放在 `internal/service`，不得依赖 Echo、HTTP request/response 或具体数据库客户端。
- 领域模型与领域规则放在 `internal/domain`，不得依赖传输层和基础设施层。
- 仓储接口优先由业务层定义；具体实现放在 `internal/infra` 或 `internal/repository`。
- 可被其他仓库复用的稳定库才允许放入 `pkg`；默认不要新建 `pkg`。
- 数据库迁移文件放在 `migrations`，文件名使用递增版本号和清晰动作描述。
- 脚本放在 `scripts`，必须能从仓库根目录执行。
- 文档放在 `docs`，README 只保留启动、配置和关键入口说明。
- 新增目录前先确认不能落入现有分层；不要为单个文件创建过深目录。

## 数据库迁移规范

- `migrations/000001_init_schema.sql` 是已部署环境（staging 等）的基线，不再修改。
- 修改表结构时新增递增编号的增量迁移文件，例如 `000002_gitlab_ci_build.sql`，文件名用 6 位序号加动作描述，序号连续。
- API、worker 和 `forge-migrate` 启动时会自动执行尚未记录的迁移（用数据库 advisory lock 串行化，每个文件在一个事务里），不需要手工同步已部署环境的库。
- 迁移要写成可重复执行：`add column if not exists`、`drop constraint if exists` 后再 `add constraint`、`create index if not exists`。已经执行过的迁移文件不能再修改，需要调整就新增文件。
- 迁移必须兼容旧版本进程：部署期间新旧实例会同时访问同一个库，所以先加列、后用列；删除或重命名列要拆成两次发布。
- 本地开发库同样由迁移升级；不要手工 DDL。同步前先确认目标库来自当前项目配置，除非用户明确要求，不清空业务数据。

## 代码规范

- 不要为四五行且仅单次使用的简单逻辑拆出工具函数；优先在调用处用局部变量或内联条件表达清楚。只有存在复用、稳定领域语义、隔离外部协议或错误码、或能显著降低复杂度时才抽函数。
