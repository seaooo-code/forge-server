# 构建适配器方案

## 背景

Forge 的构建契约是：模块构建后导出 `dist/forge.json` 加上 `dist/{platform}/` 目录（见 [P0 方案](rn-test-env-p0-plan.md) 第 1 节）。目前满足这个契约的唯一方式，是在业务仓库里放一个 Dockerfile。

现有 RN 仓库都是用公司已有的打包工具链打包的，仓库里没有 Dockerfile。如果要求每个仓库都加 Dockerfile、维护同样的打包脚本，接入成本高，而且各仓库很快会各写各的。

主流平台的做法是"构建预设"，例如 Vercel 的 Framework Preset、Netlify 的 Build Plugin：平台维护一份可复用的构建配方，业务仓库只选预设、填参数，不需要改仓库。

**原则**：兼容某个具体工具链或公司现有流程的构建逻辑，都以**构建适配器**的形式存储为数据，在控制台里新增和编辑。主线代码只提供通用机制，不出现任何具体工具链的字段名或分支判断。

## 1. 概念

| 概念 | 说明 |
| --- | --- |
| 构建适配器 | 一份可复用的构建配方：Dockerfile 模板、辅助文件、参数定义。平台级资源，由平台管理员维护 |
| 适配器参数 | 适配器声明的可配置项，例如入口文件、配置文件路径。模块选用适配器时填写参数值 |
| 构建方式 | 模块级配置，二选一：`dockerfile` 使用仓库自带的 Dockerfile（现有行为，默认值）；`adapter` 使用某个构建适配器 |

适配器只负责"怎么构建"，产物仍须满足 `forge.json` 契约，平台照常校验。代码源、触发、变量与密钥、日志、发布、拉包这些环节不受影响。

## 2. 数据模型

```sql
create table build_adapters (
    id text primary key,
    name text not null,
    description text not null default '',
    dockerfile_template text not null,
    files jsonb not null default '{}',          -- {相对路径: 模板内容}，渲染后放入构建上下文的 .forge/ 目录
    parameters jsonb not null default '[]',     -- [{key, label, description, required, default}]
    version integer not null default 1,         -- 每次修改 +1
    status text not null check (status in ('active', 'disabled')),
    created_by_user_id text not null references users(id) on delete restrict,
    updated_by_user_id text not null references users(id) on delete restrict,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- modules
build_mode text not null default 'dockerfile' check (build_mode in ('dockerfile', 'adapter')),
build_adapter_id text references build_adapters(id) on delete restrict,
build_adapter_params jsonb not null default '{}',
check ((build_mode = 'dockerfile' and build_adapter_id is null) or (build_mode = 'adapter' and build_adapter_id is not null))

-- build_records：可追溯与可复现
number bigint not null,                         -- 模块内自增构建号，(module_id, number) 唯一
build_adapter_id text,
build_adapter_version integer,
rendered_dockerfile_sha256 text not null default '',
dependency_builds jsonb not null default '[]'   -- 注入的依赖构建，见第 4 节
```

- 参数 key 的规则和构建变量一致：`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`。参数不允许存放密钥，密钥一律走模块的构建变量（`--secret`）。
- 适配器仍被模块引用时不能删除，只能停用。停用后，引用它的模块无法发起新构建，已有的构建和发布不受影响。
- 构建号 `number` 是通用能力，每个模块单调递增，Dockerfile 构建也会分配。

## 3. 渲染

- 使用 Go `text/template`，开启 `missingkey=error`，引用不存在的字段直接报错。函数只开放白名单：`default`、`quote`、`join`、`lower`、`upper`、`trim`、`replace`，不提供读文件、环境变量和网络相关的函数。
- 模板上下文如下：

| 字段 | 说明 |
| --- | --- |
| `.Module.ID` / `.Module.Name` | 模块 |
| `.Environment` | 环境 code |
| `.Branch` / `.CommitSHA` | 本次构建的代码版本 |
| `.Build.ID` / `.Build.Number` / `.Build.Time` | 构建记录 id、模块内构建号、构建开始时间（UTC，RFC 3339） |
| `.Params.<key>` | 模块填写的适配器参数，没填时取默认值 |

- 构建变量和密钥**不进入模板上下文**，仍然通过 `--build-arg` / `--secret` 注入，避免密钥出现在渲染后的 Dockerfile 里。平台固定注入的 `FORGE_*` build-arg 保持不变。
- worker 的处理顺序：检出代码 → 渲染 Dockerfile 到工作目录（仓库之外）→ 把 `files` 渲染到 `{构建上下文}/.forge/`（如果仓库里已经有 `.forge/`，构建失败，避免覆盖仓库内容）→ 注入依赖产物（第 4 节）→ `docker build --file <渲染结果> <构建上下文>`。
- 辅助文件不能放在 `deps/` 下，这个目录留给依赖产物注入，保存和渲染时都会拒绝。
- 构建日志开头会写出适配器名称、版本和渲染后 Dockerfile 的 sha256。渲染失败时，构建失败，错误信息给出出错的模板行号。

## 4. 依赖产物注入

Feature 模块构建时，经常需要读取所属 Common 模块产物里的文件，例如 Common 包导出的模块 ID 表。平台只负责把依赖产物放进构建上下文，**具体读取哪个文件、怎么使用，由仓库的 Dockerfile 或构建适配器自己决定**。Dockerfile 构建和适配器构建都会注入。

### 4.1 触发与选择规则

- 只对 `kind = 'Feature'` 且配置了 `common_module_id` 的模块生效，Common 模块不注入。
- 注入所属 Common 模块在**当前构建环境**下所有生效中的默认路由（`route_type = 'default'`、`status = 'active'`）指向的构建。每个 `runtimeVersion` 最多一条生效默认路由，因此每个运行时版本对应一个构建。定向和百分比路由不注入。
- 路由指向的构建不可用（未成功、没有清单、`runtimeVersion` 或平台不合法）时，构建失败。
- worker 在检出代码之后、`docker build` 之前执行注入；与适配器同时使用时，先写辅助文件，再注入依赖。

### 4.2 目录结构

```text
{构建上下文}/.forge/deps/
  common/
    index.json
    {runtimeVersion}/
      {platform}/
        ...                 # 依赖构建 dist/{platform}/ 下的文件，保持原有相对路径
```

- 只注入平台目录下的文件，`dist/forge.json` 这类根目录文件不注入。
- 平台固定注入 build-arg `FORGE_DEPS_DIR=.forge/deps`（相对构建上下文），所有构建都会传，只有注入了依赖时目录才存在。Dockerfile 里可以写 `ARG FORGE_DEPS_DIR` 后 `COPY ${FORGE_DEPS_DIR}/common/ /deps/common/`。
- 如果仓库的 `.dockerignore` 排除了 `.forge`，依赖产物不会进入构建上下文，需要在 `.dockerignore` 里放开。

`index.json` 描述注入了哪些构建：

```json
{
  "moduleId": "common",
  "environment": "qa",
  "items": [
    {
      "runtimeVersion": "1.0.0",
      "buildRecordId": "8f0c…",
      "buildNumber": 12,
      "commitSha": "3a9e…",
      "platforms": ["android", "ios"]
    }
  ]
}
```

Common 模块在当前环境下没有任何生效默认路由时，只写入 `items` 为空数组的 `index.json`，构建日志记一条警告，构建继续。是否因此失败，由适配器或 Dockerfile 自己判断，例如在 `RUN` 里检查目标运行时版本的目录是否存在。

### 4.3 下载与限制

- 产物从对象存储下载，存储配置取依赖构建记录自己的 `storage_configuration_id`，对象 key 的计算规则与拉包接口相同。下载后逐个文件校验大小和 sha256，不一致时构建失败。
- 单次构建注入的总大小上限由 `FORGE_WORKER_DEPS_MAX_BYTES` 控制，默认 200 MiB。worker 先汇总所有清单的文件大小，超出上限时不下载、直接失败。
- 清单路径必须是相对路径：含 `..` 段、绝对路径、反斜杠时构建失败。目标文件用独占方式创建，不跟随已存在的软链；父目录解析软链后必须仍在依赖目录内。`runtimeVersion` 为 `.`、`..` 或 `index.json` 时不能作为目录名，构建失败。
- `.forge` 冲突规则与辅助文件相同：本次构建需要写入辅助文件或注入依赖、而仓库里已经有 `.forge`（目录、文件或软链）时，构建失败。没有需要写入的内容时不检查。Feature 模块总会写入 `index.json`，所以 Feature 模块的仓库里不能有 `.forge`。

### 4.4 可追溯

- 构建日志列出注入的每个依赖：运行时版本、构建号、短 commit、平台、文件数和大小。
- 注入完成后，worker 把 `[{moduleId, runtimeVersion, buildRecordId}]` 写入 `build_records.dependency_builds`。构建接口返回 `dependencyBuilds`，读取时额外关联出 `moduleName` 和 `buildNumber`，控制台的构建详情页据此链接到依赖构建。

## 5. 接口与界面

- 接口：
  - `GET/POST /api/v1/build-adapters`
  - `GET/PATCH /api/v1/build-adapters/{id}`
  - `POST /api/v1/build-adapters/{id}/disable`
  - `POST /api/v1/build-adapters/{id}/render-preview`：用示例上下文或指定模块渲染一次，返回结果或错误，不执行构建
- 权限：PlatformAdmin 或 SuperAdmin 可以管理；有模块权限的用户可以读取和选用。
- 控制台：
  - **平台 → 构建适配器**：列表展示名称、版本、引用模块数、状态。编辑页包括 Dockerfile 模板、辅助文件（可增删）、参数定义，以及"渲染预览"。
  - **模块构建配置**：构建方式二选一。选择适配器后，按参数定义动态生成表单；选择 Dockerfile 时，保留现有的 Dockerfile 路径配置。

## 6. 边界

- 适配器的执行权限和仓库 Dockerfile 相同，都在构建容器内运行，不会增加 worker 宿主机的暴露面。
- 构建适配器只解决"兼容现有打包工具链"的问题。如果客户端需要兼容其他拉包协议，那属于分发侧的需求，等真正出现时再单独设计，P0 不做。

## 7. 实施与验收

实施顺序：表结构与 domain → 渲染器（含单测）→ 适配器的增删改查与预览接口 → worker 接入 → 前端适配器页面与模块构建方式 → 用适配器构建一次测试仓库。

验收标准：

- 主线代码里没有任何具体工具链的字段名，可以用 `grep -riE "yrn|skytree|buz\.config"` 检查（测试数据除外）。
- 适配器修改后版本号加 1，构建记录能追溯到所用的版本和渲染结果的 sha256。
- 渲染时引用不存在的字段，或使用白名单以外的函数，都会报错。密钥不出现在渲染结果中。
- 仓库里已有 `.forge/` 目录时构建失败；适配器停用后，引用它的模块无法发起新构建。
- 在界面上新建一个适配器，选给测试模块，push 后能构建成功并拉到包。
