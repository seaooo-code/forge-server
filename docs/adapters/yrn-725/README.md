# YRN 725 构建适配器

这是小米 RN 725（YRN）的接入层数据，不属于 Forge 主线代码。它在 Forge 中以构建适配器的形式存储，这个目录保存的是参考副本，方便评审和变更追溯。

| 文件 | 作用 |
| --- | --- |
| `Dockerfile.tmpl` | Dockerfile 模板，使用晴空塔构建镜像执行 `.forge/yrn-build.sh`，产物输出到 `/app/dist`（Forge 构建契约约定的导出目录） |
| `yrn-build.sh` | 辅助文件，内容包括：安装依赖、执行预处理、准备 `build-config`、为每个平台执行一次 `yrn-cli build-*`、解出 zip/tar 写入 `/app/dist/{platform}/`，最后写入 `forge.json` |
| `params.json.tmpl` | 辅助文件，把模块参数和构建信息渲染成 JSON。脚本读取后用单引号导出成环境变量，避免 `$` 和反引号被 shell 展开 |
| `gitlab-ci/` | 同一套构建在 GitLab CI 里的版本（`gitlab_ci` 构建方式）：`yrn-build-ci.sh`、业务仓库 `.gitlab-ci.yml` 的改动示例，见 [转交 GitLab CI 构建](../../gitlab-ci-build.md) |
| `adapter.json` | 由上面三个文件和参数定义组合而成，可以直接 `POST /api/v1/build-adapters` 导入 |

**产物结构**：每个平台目录下的内容与晴空塔包 tar 解开后一致，包括 hbc/bundle、`*.config.json`、`res`、`drawable-mdpi` 或 `assets`。如果是 common 包，还会额外放入 `commonMap.{platform}.json`。runtimeVersion 的格式为 `{SDKVersion}-n{maxNativeApiLevel}-m{moduleIdMapVersion}`。`forge.json` 同时声明能力 `commonApi`：common 包写 `provides`，取 `com.config.json` 的 `commonApiLevel`；业务包写 `requires`，取 buz config 的 `maxCommonApiLevel`（见[发布运维与模块能力约束](../../release-operations.md)）。

**升级适配器**：控制台里的适配器数据和业务仓库里的 `scripts/forge/yrn-build-ci.sh` 都是副本，不会随本目录自动更新。docker 构建要在「平台 → 构建适配器」里更新 `yrn-build.sh`；`gitlab_ci` 构建要把 `gitlab-ci/yrn-build-ci.sh` 同步到业务仓库。更新之前产出的构建没有能力声明，不参与校验。

**与晴空塔的差异**：
- `buildnum` 使用 Forge 模块内的构建号，`maketime` 使用 Forge 的构建开始时间。
- `packVersion`：晴空塔为每次打包分配的版本号。未设置参数时取 `0.0.{构建号}`，同时写入 config 的 `pack_version`，并导出为流水线变量 `packVersion`。
- 晴空塔 CI 注入的流水线变量（`bpsEnv`、`makeTime`、`RN_BUNDLE_ID`、`packVersion`、`PLATFORM`、`moduleName`、`CI_BUILD_REF_NAME`）由脚本导出。业务仓库的 babel 配置（`@micar-fe/micar-rn-api` 的 `injectConstants`）要求这些变量都不能为空，否则构建失败。
- common 包每次都以仓库中的 `first_build_config` 作为基线构建。晴空塔则是用线上 common 包的 commonMap 作为基线。common 的 `.yrnrc` 中 `commonBundleIdType` 为 `md5`，模块 ID 由模块路径计算得出，因此两种基线不会导致 ID 漂移。这一点属于推断，需要对比真实构建的 commonMap 才能确认。

**已知限制**：
- 构建镜像是 linux/amd64 架构，在 arm64 宿主机上要通过模拟运行，速度较慢。
- 构建脚本没有执行晴空塔 CI 中的 `sudo sysctl fs.inotify.max_user_watches`。一次性打包不需要文件监听。
- 没有上传 sourcemap。如果需要，可以在脚本中补上，Sentry 令牌通过密钥变量注入。

## 实测记录（2026-11-17）

- `micar-rn-common`（`master` 分支，`5655b22`）：用 common 构建 #2 测试，结果成功。runtimeVersion 为 `725-n1-m24`，共构建 android、ios 两个平台，每个平台 198 个文件，合计约 24 MB，耗时 5 分 41 秒。宿主机为 arm64，构建镜像在 amd64 模拟下运行。
- `micar-rn-claw`（`master` 分支，`442c891`）：用 claw 构建 #3 测试，结果成功。依赖注入正常，拿到了 common #2，每个平台 5 个文件，耗时 5 分 27 秒。
- 拿 claw 的产物和晴空塔样本 `claw.{android,ios}.tar.zip` 对比：
  - **文件结构一致**：都是 `index.{platform}.hbc`、`buz.config.json`、`res/`，Android 多一个 `drawable-mdpi/`，iOS 多一个 `assets/`。
  - **资源文件一致**：所有资源文件逐字节相同。
  - **config 字段一致**：键集合相同，取值不同的只有本次构建自身的信息，包括 `buildnum`、`maketime`、`pack_version` 和两个 md5。
  - **hbc 自洽**：hbc 的大小和样本不同，因为源码 commit 不同，注入的 `bpsEnv` 等常量也不同。hbc 的 md5 与 config 里的 `YRN_BUNDLE_HBC_MD5` 一致。
