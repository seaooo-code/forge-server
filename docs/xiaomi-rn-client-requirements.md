# 客户端需求：测试包接入 Forge 包源

## 背景

Forge 是 RN Bundle 构建与交付平台，部署在公司内网，第一阶段只服务测试环境。它和晴空塔的主要区别有三点：

- push 代码后自动构建；
- 每个分支是一个"通道"，测试同学指定分支名就能拉到这个分支的最新包；
- 不需要人工打包、发版。

线上包继续走晴空塔，本需求**只改测试包**（debug / 测试构建变体），release 构建中不能出现 Forge 相关的入口和令牌。

接入约定见 [小米 RN 接入指南](xiaomi-rn-integration.md)。

## 需求

### R1 包源切换（新功能，必做）

- 在测试包的调试菜单里新增一个「RN 包源」选项，可选：内置包、晴空塔（现有逻辑）、Forge。
- 选择 Forge 后，需要填写或选择：
  - 环境：先固定为 `test`，选项从配置读取；
  - 通道：填分支名，例如 `feature/login`，可以留空。留空时拉取该环境的默认发布。
- 支持扫码填入。扫描 Forge 控制台生成的二维码后，自动填好环境和通道。二维码内容格式由双方另行约定，Forge 这边负责在构建详情页生成二维码。
- 切换后的设置需要持久化，重启 App 后依然生效。调试菜单里要能一键恢复成默认包源。

### R2 拉包协议（必做）

- 接口：`GET /api/v1/artifacts/applications/{appSlug}/environments/{env}/manifest?platform=&runtimeVersion=&identifier=`
- 请求头：`Authorization: Bearer {访问令牌}`。令牌由 Forge 生成，只内置在测试构建中。
- 参数：
  - `platform`：`android` 或 `ios`。
  - `runtimeVersion`：按 `{SDKVersion}-n{maxNativeApiLevel}-m{moduleIdMapVersion}` 拼接，例如 `725-n1-m24`。三个值分别取自客户端的 RN 引擎版本、`nativeApiLevel`，以及当前 common 包配置里的 `moduleIdMapVersion`。
  - `identifier`：通道名，也就是 R1 里填的分支名，需要做 URL 编码。
- 一次请求会返回该应用下所有模块的包，包括 common 包和业务包。不需要像晴空塔那样分两步请求。

### R3 下载与校验（必做，改造现有逻辑）

- 每个模块对应一组文件（`files[]`），每个文件有 `path`、`url`、`sha256`、`sizeBytes`。按 `path` 把文件还原到模块目录下，目录结构和现在晴空塔包 tar 解开后一致，加载器的读取路径不变。
- 校验：每个文件下载后核对 sha256，不一致则判定该模块下载失败。Forge 包**不会**再套 zip 或 tar，也**不会**在 config 里注入 `YRN_BUNDLE_MD5` / `YRN_BUNDLE_HBC_MD5`。所以在 Forge 包源下，加载器不能再依赖这两个字段，完整性统一以 sha256 为准。
- 复用：本地按 sha256 缓存文件。新清单里 sha256 没变的文件直接复用，不重复下载，例如没改过的图片资源。
- 原子性：先下载到临时目录，所有模块都校验通过后再整体切换。任何一个模块失败，就保持当前在用的包不变，并提示失败原因。

### R4 版本判断与生效（必做）

- 用每个模块的 `buildRecordId` 判断是否有新版本，替代晴空塔的 `buildnum`/`maketime`。`buildRecordId` 变化即视为新版本。
- 生效方式：测试源下固定为切换或更新完成后**立即重新加载**（相当于晴空塔的"即时生效"），方便测试同学马上看到效果。Forge 不下发 `updateType`/`residentMemory`。
- 拉包时机：冷启动时拉一次；调试菜单里提供「立即检查更新」按钮。

### R5 缺包处理（必做）

- 响应里的 `missingModules` 会列出没有可用包的模块，原因有三种：
  - `no_release_for_runtime`：这个 runtimeVersion 没有发布；
  - `platform_unavailable`：构建产物里没有这个平台；
  - `release not found`：这个通道没有发布。
- 处理规则：
  - common 包缺失：整组回退到内置包，并提示"Forge 上没有与当前 App 兼容的 common 包"。
  - 只有业务包缺失：这些模块回退到内置包，其余模块照常使用 Forge 包。
- 调试菜单里要展示缺失的模块和原因。

### R6 包信息展示（必做）

在调试菜单里展示当前每个模块的来源，以及 `branch`、`commitSha`（短 SHA）、`buildRecordId`、`runtimeVersion`，方便提 bug 时附上。最好提供"一键复制"。

### R7 网络（约束）

Forge 和对象存储都只能在内网访问，测试设备需要连公司 Wi-Fi 或 VPN。连不上时按 R3 的失败逻辑处理，保持当前包不变。

## 需要客户端确认的问题

1. **各平台实际加载哪些资源目录**：样本包里 Android 同时有 `res/` 和 `drawable-mdpi/`，iOS 同时有 `res/` 和 `assets/res/`，图片重复了一份，比如 `light.png` 有 851 KB。如果加载器只用其中一个目录，构建适配器就只输出那个目录，可以减少包体积和下载量。
2. **加载器对 config 的依赖**：除了 `YRN_BUNDLE_*_MD5`，加载器在运行时还会读取 `buz.config.json` / `com.config.json` 里的哪些字段？比如 `buildnum`、`pack_version` 是否参与逻辑？如果参与，Forge 需要在构建时提供等价的值，由构建适配器写入。
3. **排期与负责人**：R1 到 R6 由谁负责，大致什么时候能完成？

## 验收

- 测试包切到 Forge、填入分支名后，冷启动能加载到该分支最新构建的包。调试菜单里展示的 commit 与 GitLab 一致。
- 同一分支推送新提交并构建完成后，点「立即检查更新」能加载到新包，只重新下载有变化的文件。
- 篡改任意一个文件，导致 sha256 不一致时，不会切换，当前包保持可用。
- 换用 `runtimeVersion` 不匹配的 App，或者请求没有发布的通道时，按 R5 正确回退并提示。
- release 构建中没有 Forge 入口，也没有内置令牌。
