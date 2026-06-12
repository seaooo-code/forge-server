# 项目约束

## Git 提交

- 提交标题使用格式：`<gitmoji> type(scope): 中文描述`。
- 中文描述不超过 50 字。
- `type` 使用常见约定：`feat`、`fix`、`docs`、`refactor`、`test`、`style`、`perf`、`chore`。
- 常用 gitmoji 映射：`✨ feat`、`🐛 fix`、`📝 docs`、`♻️ refactor`、`✅ test`、`🎨 style`、`⚡️ perf`、`🔧 chore`。
- `scope` 使用改动所属模块或目录名，例如 `api`、`config`、`infra`、`http`。

## 目录规范

- 入口只放在 `cmd/<binary>/main.go`，只能做配置加载、日志、信号处理和应用启动。
- 运行时配置放在 `internal/config`，只负责解析与校验环境变量，不连接外部服务。
- HTTP 传输层放在 `internal/server/http`，Echo 路由、中间件、handler 只做协议适配。
- 外部基础设施适配放在 `internal/infra/<name>`，例如 PostgreSQL、Redis、第三方客户端初始化。
- 业务用例放在 `internal/service`，不得依赖 Echo、HTTP request/response 或具体数据库客户端。
- 领域模型与领域规则放在 `internal/domain`，不得依赖传输层和基础设施层。
- 仓储接口优先由业务层定义；具体实现放在 `internal/infra` 或 `internal/repository`。
- 可被其他仓库复用的稳定库才允许放入 `pkg`；默认不要新建 `pkg`。
- 数据库迁移文件放在 `migrations`，文件名使用递增版本号和清晰动作描述。
- 脚本放在 `scripts`，必须能从仓库根目录执行。
- 文档放在 `docs`，README 只保留启动、配置和关键入口说明。
- 新增目录前先确认不能落入现有分层；不要为单个文件创建过深目录。

## 代码规范

- 不要为四五行且仅单次使用的简单逻辑拆出工具函数；优先在调用处用局部变量或内联条件表达清楚。只有存在复用、稳定领域语义、隔离外部协议或错误码、或能显著降低复杂度时才抽函数。
