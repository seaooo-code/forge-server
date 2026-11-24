# 小米 RN 客户端对接方案

本文以客户端现有的热更新设计为基础，说明测试包接入 Forge 时，端侧每个组件要怎么改、Forge 要补哪些能力。客户端设计取自飞书文档《鸿蒙RN热更新技术方案》（王凯，2025-11-11，[链接](https://mi.feishu.cn/wiki/AXt7wBYYYijvbAktTrbcOZvonuf)），Android、iOS 与鸿蒙对齐。

本文和另外两份文档的分工：

- [小米 RN 接入指南](xiaomi-rn-integration.md)：部署、存储、runtimeVersion 规则、构建适配器；
- [客户端需求](xiaomi-rn-client-requirements.md)：客户端要实现的功能（R1–R7）和验收标准；
- 本文：把 R1–R7 对应到端侧现有的组件，并列出 Forge 侧的待办。

范围和需求文档保持一致：第一阶段只改测试包，线上包继续走晴空塔。

后续能力（第一阶段不要求客户端实现）：

- [发布渠道](channels.md)：内测等单独打包的 App 拉包时带 `channel`。
- [包签名](artifact-signing.md)：manifest 的模块项带 `signature`，客户端内置公钥后可以校验文件列表是 Forge 签过的，校验步骤见该文第 5.1 节。

## 1. 现状：晴空塔与客户端的交互

```text
React 代码 → bundle.js → 字节码（Android hbc / 鸿蒙 abc）→ 推送晴空塔（CDN + 版本信息入库）

App 编译时：拉包脚本带本地 bundle 版本号 → 版本更新服务 → 落后则下载 → 预置进安装包（内置包）
App 运行时：拉包时机触发 → 带本地版本号请求 → 版本号落后？
                                              ├─ 否：无更新（错误码 2 Bundle no change）
                                              └─ 是：下发新包 CDN 地址 → 下载 → zip md5 → 解压 → bundle md5
                                                     → 更新注册信息 → 按生效策略（重启生效 / 静默生效）加载
```

- **拉包时机**：应用冷启动、前后台切换、联网监听、定时拉包、RN 页面加载。
- **重试**：包损坏时重新拉取。
- **内置包与已下载包比较新旧**：依次比较 `maxNativeApiLevel`、`maxCommonJSApiLevel`、`maketime`。
- **端侧组件**：Bundle Manager 是唯一对接 Bundle Server 的组件，由 `RawLoader`（内置包）、`CloudLoader`（在线包，持有 `BundleRegistry`）和 `FileCleaner` 组成。按 Provider 分，`DefaultAssetsProvider` 提供内置包，`UpgradeableProvider` 提供可升级包。
- **错误码**：0–99 框架，100–199 Bundle，200–299 引擎，300–399 JS。

## 2. 两边的差异和结论

| 方面 | 晴空塔 | Forge | 结论 |
| --- | --- | --- | --- |
| 判断有没有更新 | 端上报本地版本，服务端比较 | 服务端返回当前生效的完整清单，端上比较每个模块的 `buildRecordId` | 沿用 Forge 的做法（R4）。服务端不需要知道端上有什么，提测单、灰度、全量都是同一套逻辑 |
| 产物形态 | 一个模块一个 zip，里面再套 tar | 散文件，每个文件带 sha256 | 沿用 Forge 的做法（R3）。没变的文件可以按 sha256 复用，不需要做 zip 适配器 |
| 完整性校验 | zip md5，加上 config 里的 `YRN_BUNDLE_*_MD5` | 每个文件的 sha256 | Forge 包源统一用 sha256 校验 |
| 兼容性 | 端上比较 apiLevel | 服务端按 `runtimeVersion` 精确匹配 | 测试包不需要跟内置包比新旧，见第 3.4 节 |
| 下载地址 | CDN | 第一阶段由 Forge 代理下载，`publicBaseUrl` 留空 | 测试流量小，代理下载够用；以后上线再配 CDN 地址，代码不用改 |
| 没有更新时 | 返回空，对应错误码 2 | manifest 带 `ETag`，客户端带 `If-None-Match` 请求时，内容没变就返回 304 且没有响应体。不带时每次返回完整清单，实测单个平台约 93 KB | 已支持，见第 4.1 节 |
| 拉包时机 | 五种 | 需求只要求冷启动、回到前台、手动刷新 | 联网监听和定时拉包在 Forge 包源下关闭，见第 3.5 节 |
| 鸿蒙平台 | 已规划 | 平台枚举里已经有 `harmonyos`，但 YRN 适配器默认只构建 `android,ios` | **待确认**，见第 5 节 |

需求文档 R3 有一句需要更正。实际产物的 `com.config.json` 里**仍然有** `YRN_BUNDLE_MD5` 和 `YRN_BUNDLE_HBC_MD5`，这两个字段是 yrn-cli 构建时写进去的，不是 Forge 注入的。R3 的要求不变：Forge 不保证这两个字段一定存在，所以 Forge 包源下的完整性校验统一用 sha256。

## 3. 端侧改造：组件对照

### 3.1 包源配置（`RNConfig` / Config.Environment）

- 在 Environment 里新增包源类型，可选内置包、晴空塔、Forge，对应 R1。
- Forge 包源需要的配置：`forgeBaseUrl`、`appSlug`、环境 `test`、访问令牌，都只放在测试构建变体里。
- release 构建里不能编译进 Forge 相关的 Loader 和令牌。

### 3.2 拉包（`CloudLoader`）

- 新增 Forge 的实现，放在晴空塔请求旁边，按包源选择用哪一个。不要在晴空塔请求里加判断分支。
- 请求 manifest 时带 `platform`、`runtimeVersion`、`userId`，对应 R2。
  - `platform` 取值为 `android`、`ios`、`harmonyos`。
  - `runtimeVersion` 按接入指南第 4 节的规则拼接。
- 一次请求就拿到 common 包和所有业务包。原来"先加载 common、再加载业务包"的顺序（`loadCommonBundle` / `loadBusinessBundle`）放在加载阶段保持不变，拉包阶段不再分两次请求。
- 保存上一次响应的 `ETag`，下次请求时带上 `If-None-Match`。服务端返回 304 时，等同于错误码 2（Bundle no change），流程直接结束。ETag 只能和上次请求时相同的 `platform`、`runtimeVersion`、`userId` 一起使用：换了账号或 App 版本后，旧 ETag 不会再匹配，服务端会返回完整清单。

### 3.3 注册信息（`BundleRegistry`）

每个模块记录这些信息，供 R4 判断新版本、R6 展示包信息：

- `buildRecordId`、`source`、`testPlan.name`；
- `branch`、`commitSha`、`runtimeVersion`；
- 文件列表，每个文件的 `path` 和 `sha256`。

判断规则：`buildRecordId` 没变就跳过这个模块；变了就按文件列表做增量下载。

### 3.4 下载与校验（`UpgradeableProvider` 的下载链路）

把原来的"下载 zip → zip md5 → 解压 → bundle md5"换成：

1. 对比新旧文件列表，sha256 已经在本地缓存里的文件直接复用；
2. 其余文件下载到临时目录，逐个核对 `sizeBytes` 和 `sha256`；
3. 所有模块都校验通过后，整体切换，再更新 `BundleRegistry`；有任何一个模块失败，就保持当前在用的包不变（R3 的原子性要求）。

内置包和下载包比新旧的逻辑（`versionGreaterThan`）在 Forge 包源下**不执行**。测试同学选 Forge 包源，就是要用 Forge 上的包；兼容性已经由服务端按 `runtimeVersion` 精确匹配保证，旧 App 只会拿到"无可用包"，不会拿到不兼容的包。只有 `missingModules` 列出的模块才回退到内置包（R5）。

### 3.5 拉包时机

| 时机 | Forge 包源 | 说明 |
| --- | --- | --- |
| 应用冷启动 | 开 | R4 |
| 前后台切换（回到前台） | 开 | R6：研发在提测单里换了构建后，测试同学能尽快拿到 |
| RN 页面加载 | 关 | 页面加载过程中换包会让同一次会话里新旧代码混用，冷启动和回到前台已经够及时 |
| 联网监听 | 关 | 测试设备只在内网使用 |
| 定时拉包 | 关 | 同上，提测场景不需要 |
| 调试菜单「检查更新」 | 开 | R4、R6 |

### 3.6 生效策略

Forge 包源下固定为下载完成后**立即重新加载**（R4），不读晴空塔的 `updateType`、`residentMemory`，也不走"静默生效"。

### 3.7 文件清理（`FileCleaner`）

切换成功后，删除新文件列表里没有引用到的缓存文件，以及失败后残留的临时目录。sha256 缓存只保留当前在用的这一份包，不保留历史版本。

### 3.8 错误码对照

Forge 包源下沿用现有错误码，不新增号段：

| 场景 | 错误码 | 说明 |
| --- | --- | --- |
| 返回 304，或者所有模块的 `buildRecordId` 都没变 | 2 Bundle no change | |
| 文件的 sha256 或大小对不上 | 104 File md5 check error | 名字里写的是 md5，在 Forge 包源下表示 sha256 校验失败 |
| 网络失败、5xx | 106 Net error | |
| 401（令牌无效或过期） | 106 Net error | 调试菜单里单独提示"Forge 令牌无效"，方便排查 |
| 400（参数不合法） | 106 Net error | 一般是 `runtimeVersion` 拼错或平台取值不对，调试菜单里显示请求参数 |
| 模块出现在 `missingModules` 里 | 100 File not exist | 按 R5 回退到内置包，同时展示原因 |
| 原因是 `common_incompatible` | 100 File not exist | 业务包要求的 common API 版本高于这次拿到的 common 包，按 R5 回退该业务包 |
| 103 File unzip error | 不会出现 | Forge 包源不需要解压 |

## 4. Forge 侧待办

### 4.1 manifest 支持条件请求（已实现，通用能力）

客户端在冷启动和每次回到前台都会请求 manifest。现在每次都返回完整清单，单个平台约 93 KB（198 个文件），绝大多数时候内容没变。

实现方式：

- manifest 响应带上 `ETag`，取值是响应体的哈希；同时设置 `Cache-Control: no-cache`，要求每次都向服务端确认。
- 请求带 `If-None-Match`，并且和当前 ETag 一致时，返回 304，响应体为空。按 HTTP 规范做弱比较，支持 `W/` 前缀、逗号分隔的多个值和 `*`。

这样客户端协议不用改，不带 `If-None-Match` 的老客户端也照常能用。这是通用的 HTTP 能力，任何接入方都用得上，放在主线实现，不做成适配器。比较是在服务端算出响应之后做的，所以数据库查询照常执行，省下的是传输量和客户端的解析。

代码位置：`internal/server/http/artifact.go` 的 `manifest` handler 和 `etagMatches`；接口定义在 `internal/server/http/openapi.go`，新增了 `If-None-Match` 请求头、`ETag` 响应头和 304 响应。

### 4.2 鸿蒙平台的构建（待确认后再做）

Forge 的平台枚举已经支持 `harmonyos`。YRN 适配器的 `YRN_PLATFORMS` 默认是 `android,ios`，要出鸿蒙包，需要确认第 5 节的问题 1。如果要做，改动只在适配器的参考脚本和模块构建变量里，Forge 主线不用改。

### 4.3 正式环境的下载地址（以后再做）

如果以后线上包也迁到 Forge，在存储配置里填好 `publicBaseUrl`（CDN 或对象存储的公开地址）。manifest 里的文件 `url` 会自动换成这个地址（`internal/service/artifact.go` 的 `artifactFileURL`），客户端不用改。第一阶段不需要。

### 4.4 编译期预置（第一阶段不做）

晴空塔支持 App 编译时用拉包脚本预置内置包。Forge 的 manifest 接口用访问令牌就能调用，CI 脚本可以直接复用。但第一阶段内置包仍然来自晴空塔，所以先不做。

## 5. 需要客户端确认的问题

在[客户端需求](xiaomi-rn-client-requirements.md)已列问题的基础上，补充：

1. **鸿蒙包怎么构建**：鸿蒙端用的 RN 引擎版本（`SDKVersion`）是不是也是 725？yrn-cli 能不能直接输出 `bundle.abc`？产物的目录结构和 config 字段跟 Android 一致吗？
2. **字段名**：技术方案代码里比较的是 `maxCommonJSApiLevel`，但实际 common 包的 `com.config.json` 里字段名是 `commonApiLevel`。端侧实际读取的是哪一个？
3. **三端代码是否共用**：Android、iOS 的 Bundle Manager 和鸿蒙的结构是否一致？如果一致，第 3 节的改造可以三端按同一份设计实现。

## 6. 落地顺序

1. Forge：部署包含 4.1 条件请求的服务端版本（对老客户端没有影响）。
2. 客户端：3.1、3.2、3.3、3.4，完成后可以在测试包里跑通拉包、校验和切换。
3. 客户端：3.5–3.8，以及需求文档里 R5、R6 的调试菜单展示。
4. 联调：按[客户端需求](xiaomi-rn-client-requirements.md)的验收项逐条验证，用 staging 上的 `test-app` 和 `common` 模块（runtimeVersion `725-n1-m24`）。
5. 鸿蒙：第 5 节问题 1 确认后，再推进 4.2。
