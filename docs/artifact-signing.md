# 包签名

状态：已实现（迁移 `000009_artifact_signing.sql`）。

## 1. 要解决的问题

manifest 里每个文件都有 sha256，客户端能确认文件下载完整、和清单一致。但清单本身是从 Forge 接口下发的，文件放在对象存储或 CDN 上，任何能改动其中一处的人，都可以同时换掉文件和对应的 sha256，客户端发现不了。

包签名让客户端能确认：这份文件列表是这个应用的签名密钥签过的，而且确实属于这个模块、这个环境、这个运行时版本。

签名**不替代** sha256。只给文件列表签一次名，文件列表里带每个文件的 sha256，客户端先验签名，再逐个文件比对 sha256。md5 只是 `FORGE_ARTIFACT_DIGESTS` 可选输出的兼容字段，签名方案不使用它。

## 2. 防护范围

| 能防 | 不能防 |
| --- | --- |
| 对象存储、CDN、传输链路上替换或篡改文件 | 同时拿到数据库和 `FORGE_SECRET_KEY` 的人用私钥签出新包 |
| 把 A 模块的包冒充 B 模块下发，或把测试环境的包下发到生产环境 | 回放这个应用以前签过的旧构建（见第 7 节） |
| 把不同 `runtimeVersion` 的包下发给不兼容的客户端 | 客户端本身被篡改 |

## 3. 密钥

- 每个应用有自己的 Ed25519 密钥对，在控制台生成。私钥用 secretbox（`FORGE_SECRET_KEY`）加密后存在 `application_signing_keys`，任何接口都不返回私钥。
- 公钥以 base64 编码的原始 32 字节展示，客户端打包时内置。`keyId` 是公钥 sha256 的前 16 个十六进制字符，用来在多把公钥里挑出对应的那把。
- 一个应用同一时间最多一把 `active` 密钥，新构建用它签名。生成新密钥时，旧的 active 密钥在同一个事务里转为 `retired`。retired 密钥不再签新构建，但它签过的构建照常有效，客户端应继续信任它，直到这些构建都不再下发。
- 只有 retired 密钥可以删除。删除只是把公钥从控制台移走；客户端是否还信任它，取决于客户端内置的公钥列表。

**轮换步骤**：生成新密钥 → 客户端内置新旧两把公钥并发版 → 旧版客户端占比足够低后，客户端去掉旧公钥。轮换期间，新构建用新密钥签名，旧版客户端会因为不认识新的 `keyId` 拒绝这些包，回退到内置包。所以要先发带新公钥的客户端，再生成新密钥。

## 4. 签名内容

worker 在构建结束、上传产物前（`internal/worker/executor.go` 的 `publishArtifact`，docker 和 gitlab_ci 构建共用），如果应用有 active 密钥，就生成下面这份 JSON，用私钥签名，把 JSON 原文、签名和 `keyId` 写回构建记录。

```json
{
  "version": 1,
  "application": "forge-demo",
  "moduleId": "claw",
  "environment": "test",
  "buildRecordId": "c4938f50-7fc8-4187-a322-412211b4f905",
  "buildNumber": 12,
  "runtimeVersion": "725-n1-m24",
  "commitSha": "d0c0f3c7...",
  "signedAt": "2026-11-24T03:17:02Z",
  "files": [
    { "path": "android/index.android.bundle", "sizeBytes": 123456, "sha256": "..." }
  ]
}
```

- `files[].path` 带平台前缀，和产物目录一致；manifest 下发时去掉了平台前缀，客户端比对时要拼回 `{platform}/{path}`。
- 签名对象是这份 JSON 的**原始字节**。客户端不需要自己重新序列化 JSON，先验签名，再解析。
- 应用没有 active 密钥时不签名，构建日志记一条说明，构建照常成功。有 active 密钥但签名失败（例如私钥解密失败）时，构建失败。

## 5. 下发

manifest 的每个模块新增 `signature`，没有签名时为 `null`：

```json
"signature": {
  "algorithm": "ed25519",
  "keyId": "3fa1c2d4e5b60718",
  "payload": "<签名内容 JSON 的 base64>",
  "signature": "<签名的 base64>"
}
```

提测单和发布命中的构建都会带上它们自己的签名。

### 5.1 客户端校验步骤

1. 用 `keyId` 在内置公钥里找到公钥；找不到就当作校验失败。
2. 对 base64 解码后的 `payload` 字节做 Ed25519 校验。
3. 解析 `payload`，确认 `application`、`environment` 与请求一致，`moduleId`、`buildRecordId` 与模块项一致，`runtimeVersion` 与客户端上报的一致。
4. 下载每个文件后，按 `{platform}/{path}` 找到 `payload.files` 里的对应项，比对 `sizeBytes` 和 `sha256`。

任何一步失败，这个模块按 `missingModules` 处理，回退到内置包。`signature` 为 `null` 时，开启强制校验的客户端同样回退。

## 6. 要求签名

应用有一个开关「只允许发布已签名的构建」（`applications.require_signed_releases`，默认关闭）：

- 打开前，应用必须有 active 密钥。
- 打开后，创建发布和回滚都会拒绝没有签名的构建，返回 409 `release build unsigned`。比例灰度转全量不受影响，因为那个构建已经发布过了。
- 已经生效的发布不受影响；提测单也不受限制，提测单里的未签名构建由客户端自己决定是否接受。

推荐的接入顺序：生成密钥（新构建开始带签名）→ 客户端内置公钥、先只记录校验结果 → 确认线上都是已签名构建后打开开关 → 客户端改为强制校验。

## 7. 已知限制

- **回放旧构建**：回滚本来就要把旧构建重新下发，所以客户端不能简单拒绝"比当前旧"的构建。签名内容里有 `buildNumber` 和 `signedAt`，客户端可以按需要自定规则，例如拒绝早于 App 内置包的构建。
- **私钥托管**：私钥和其他凭证一样由 `FORGE_SECRET_KEY` 加密。以后可以换成 KMS 适配器，签名内容和下发格式不用变。

## 8. 接口

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/applications/{slug}/signing` | 开关状态和密钥列表（公钥、状态、签过的构建数），登录用户可读 |
| `PATCH /api/v1/applications/{slug}/signing` | 修改「只允许发布已签名的构建」；没有 active 密钥时打开返回 409 `signing key required` |
| `POST /api/v1/applications/{slug}/signing/keys` | 生成新的 active 密钥，原 active 密钥转为 retired |
| `DELETE /api/v1/applications/{slug}/signing/keys/{keyId}` | 删除 retired 密钥；删除 active 密钥返回 409 `signing key active` |

修改类接口需要 `application:update` 权限。构建详情新增 `signingKeyId`，未签名时为空字符串。
