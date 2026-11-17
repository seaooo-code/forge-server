#!/usr/bin/env bash
# YRN 725 构建脚本（接入层数据，由 YRN 适配器写入 .forge/ 下执行，不属于 Forge 主线代码）。
#
# 产出符合 Forge 构建契约的 /app/dist：
#   /app/dist/forge.json                 {"runtimeVersion": "{SDKVersion}-n{maxNativeApiLevel}-m{moduleIdMapVersion}"}
#   /app/dist/{platform}/...             与晴空塔包 tar 解开后的内容一致（hbc/bundle、*.config.json、res、drawable-mdpi 或 assets）
#   /app/dist/{platform}/commonMap.{platform}.json   仅 common 包，供业务包构建时通过依赖注入读取
set -euo pipefail

log() { echo "[yrn-adapter] $*"; }
fail() { echo "[yrn-adapter] 错误：$*" >&2; exit 1; }

export NVM_DIR="$HOME/.nvm"
# shellcheck disable=SC1091
[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"

# 参数由 Forge 渲染为 JSON（.forge/params.json），这里用单引号转义后导出，避免 $ 与反引号被 shell 展开。
eval "$(node -e '
const p = JSON.parse(require("fs").readFileSync(".forge/params.json", "utf8"));
for (const [k, v] of Object.entries(p)) {
  if (!/^[A-Z_]+$/.test(k)) continue;
  console.log(`export ${k}='"'"'${String(v).replace(/'"'"'/g, `'"'"'\\'"'"''"'"'`)}'"'"'`);
}
')"

case "$YRN_KIND" in common|business) ;; *) fail "kind 只能是 common 或 business，当前为 $YRN_KIND" ;; esac
[ -f "$YRN_CONFIG_FILE" ] || fail "找不到包配置 $YRN_CONFIG_FILE"

log "node $(node -v)，npm $(npm -v)"

# 私有 npm 源令牌可选：配置了密钥变量 NPM_TOKEN 时写入用户级 .npmrc。
if [ -n "${NPM_TOKEN:-}" ]; then
  echo "//pkgs.d.xiaomi.net/artifactory/api/npm/mi-npm/:_authToken=${NPM_TOKEN}" >> "$HOME/.npmrc"
fi

export NODE_OPTIONS="${NODE_OPTIONS:---max-old-space-size=8192}"

log "安装依赖：$YRN_INSTALL_COMMAND"
bash -c "$YRN_INSTALL_COMMAND"

if [ -n "$YRN_PREBUILD_COMMAND" ]; then
  log "预处理：$YRN_PREBUILD_COMMAND"
  bash -c "$YRN_PREBUILD_COMMAND"
fi

# 晴空塔为每次打包分配 packVersion（如 11.317.13）；Forge 没有对应概念，未配置时用 0.0.{构建号}，保证非空且递增。
YRN_PACK_VERSION="${YRN_PACK_VERSION:-0.0.$FORGE_BUILD_NUMBER}"

# 写入晴空塔流程中由 yrn-build 注入的字段，客户端可能读取：buildnum、maketime、pack_version。
maketime=$(node -e 'const d=new Date(process.argv[1]);const p=n=>String(n).padStart(2,"0");console.log(`${d.getUTCFullYear()}${p(d.getUTCMonth()+1)}${p(d.getUTCDate())}${p(d.getUTCHours())}${p(d.getUTCMinutes())}${p(d.getUTCSeconds())}`)' "$FORGE_BUILD_TIME")
node -e '
const fs = require("fs");
const [file, buildnum, maketime, packVersion] = process.argv.slice(1);
const data = JSON.parse(fs.readFileSync(file, "utf8"));
data.buildnum = Number(buildnum);
data.maketime = Number(maketime);
data.pack_version = packVersion;
fs.writeFileSync(file, JSON.stringify(data, null, 4));
' "$YRN_CONFIG_FILE" "$FORGE_BUILD_NUMBER" "$maketime" "$YRN_PACK_VERSION"

read -r sdk native mapver pkgname < <(node -e '
const c = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"));
console.log(c.SDKVersion, c.maxNativeApiLevel, c.moduleIdMapVersion, c.name);
' "$YRN_CONFIG_FILE")
runtime="${sdk}-n${native}-m${mapver}"
log "包名 $pkgname，runtimeVersion $runtime"

mkdir -p build-config /app/dist
IFS=',' read -r -a platforms <<< "$YRN_PLATFORMS"

for platform in "${platforms[@]}"; do
  platform=$(echo "$platform" | tr -d ' ')
  [ -n "$platform" ] || continue
  log "===== 平台 $platform ====="

  # 准备 build-config：common 用仓库内基线（首包配置），business 用 Forge 注入的同 runtimeVersion common 产物。
  if [ "$YRN_KIND" = "common" ]; then
    [ -n "$YRN_BASELINE_DIR" ] || fail "common 包需要参数 baselineDir"
    cp "$YRN_BASELINE_DIR/commonMap.$platform.json" "build-config/commonMap.$platform.json"
    cp "$YRN_BASELINE_DIR/unpackInfo.$platform.json" "build-config/unpackInfo.$platform.json"
  else
    dep="${FORGE_DEPS_DIR:-.forge/deps}/common/$runtime/$platform"
    [ -f "$dep/commonMap.$platform.json" ] || fail "未找到 runtimeVersion $runtime 的 common 产物 $dep/commonMap.$platform.json，请先发布同版本的 common 包"
    cp "$dep/commonMap.$platform.json" "build-config/commonMap.$platform.json"
    node -e 'const c=JSON.parse(require("fs").readFileSync(process.argv[1],"utf8"));console.log(JSON.stringify({commonApiLevel:c.maxCommonApiLevel}))' "$YRN_CONFIG_FILE" > "build-config/unpackInfo.$platform.json"
  fi

  # 晴空塔 GitLab CI 注入的流水线变量。业务仓库的 babel 配置（@micar-fe/micar-rn-api 的 injectConstants）会读取它们。
  export bpsEnv="$YRN_BPS_ENV" makeTime="$maketime" RN_BUNDLE_ID="$FORGE_BUILD_NUMBER" packVersion="$YRN_PACK_VERSION"
  export PLATFORM="$platform" moduleName="$pkgname" appName="$YRN_APP_KEY"
  export CI_BUILD_REF_NAME="$FORGE_BRANCH" CI_COMMIT_REF_NAME="$FORGE_BRANCH" CI_COMMIT_SHA="$FORGE_COMMIT_SHA"

  entry="${YRN_ENTRY_FILE//\{platform\}/$platform}"
  [ -f "$entry" ] || fail "找不到入口文件 $entry"
  npx yrn-cli "build-$YRN_KIND" --platform "$platform" --entry-file "$entry" --config-file "$YRN_CONFIG_FILE" --verbose

  if [ "$YRN_KIND" = "common" ]; then pkgdir="build/$platform/base"; archive="base.$platform.tar.zip"; else pkgdir="build/$platform/$pkgname"; archive="$pkgname.$platform.tar.zip"; fi
  [ -d "$pkgdir" ] || pkgdir=$(dirname "$(find "build/$platform" -name "*.$platform.tar.zip" | head -1)")
  [ -f "$pkgdir/$archive" ] || archive=$(basename "$(find "$pkgdir" -maxdepth 1 -name "*.$platform.tar.zip" | head -1)")
  [ -f "$pkgdir/$archive" ] || fail "未找到 $platform 的打包产物（*.$platform.tar.zip）"
  log "产物 $pkgdir/$archive"

  # 解开与晴空塔相同的 zip → tar，得到客户端加载的目录内容。
  work=$(mktemp -d)
  unzip -q "$pkgdir/$archive" -d "$work"
  tarfile=$(find "$work" -name "*.tar" | head -1)
  mkdir -p "/app/dist/$platform"
  tar -xf "$tarfile" -C "/app/dist/$platform"
  rm -rf "$work"

  if [ "$YRN_KIND" = "common" ]; then
    [ -f "$pkgdir/commonMap.$platform.json" ] || fail "common 构建未生成 commonMap.$platform.json"
    cp "$pkgdir/commonMap.$platform.json" "/app/dist/$platform/commonMap.$platform.json"
  fi
  log "$platform 输出：$(find "/app/dist/$platform" -type f | wc -l) 个文件"
done

printf '{"runtimeVersion":"%s"}\n' "$runtime" > /app/dist/forge.json
log "完成：$(cat /app/dist/forge.json)"
