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
rendered_dockerfile_sha256 text not null default ''
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
- worker 的处理顺序：检出代码 → 渲染 Dockerfile 到工作目录（仓库之外）→ 把 `files` 渲染到 `{构建上下文}/.forge/`（如果仓库里已经有 `.forge/`，构建失败，避免覆盖仓库内容）→ `docker build --file <渲染结果> <构建上下文>`。
- 构建日志开头会写出适配器名称、版本和渲染后 Dockerfile 的 sha256。渲染失败时，构建失败，错误信息给出出错的模板行号。

## 4. 接口与界面

- 接口：
  - `GET/POST /api/v1/build-adapters`
  - `GET/PATCH /api/v1/build-adapters/{id}`
  - `POST /api/v1/build-adapters/{id}/disable`
  - `POST /api/v1/build-adapters/{id}/render-preview`：用示例上下文或指定模块渲染一次，返回结果或错误，不执行构建
- 权限：PlatformAdmin 或 SuperAdmin 可以管理；有模块权限的用户可以读取和选用。
- 控制台：
  - **平台 → 构建适配器**：列表展示名称、版本、引用模块数、状态。编辑页包括 Dockerfile 模板、辅助文件（可增删）、参数定义，以及"渲染预览"。
  - **模块构建配置**：构建方式二选一。选择适配器后，按参数定义动态生成表单；选择 Dockerfile 时，保留现有的 Dockerfile 路径配置。

## 5. 边界

- 适配器的执行权限和仓库 Dockerfile 相同，都在构建容器内运行，不会增加 worker 宿主机的暴露面。
- 构建适配器只解决"兼容现有打包工具链"的问题。如果客户端需要兼容其他拉包协议，那属于分发侧的需求，等真正出现时再单独设计，P0 不做。

## 6. 实施与验收

实施顺序：表结构与 domain → 渲染器（含单测）→ 适配器的增删改查与预览接口 → worker 接入 → 前端适配器页面与模块构建方式 → 用适配器构建一次测试仓库。

验收标准：

- 主线代码里没有任何具体工具链的字段名，可以用 `grep -riE "yrn|skytree|buz\.config"` 检查（测试数据除外）。
- 适配器修改后版本号加 1，构建记录能追溯到所用的版本和渲染结果的 sha256。
- 渲染时引用不存在的字段，或使用白名单以外的函数，都会报错。密钥不出现在渲染结果中。
- 仓库里已有 `.forge/` 目录时构建失败；适配器停用后，引用它的模块无法发起新构建。
- 在界面上新建一个适配器，选给测试模块，push 后能构建成功并拉到包。
