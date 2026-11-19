#!/bin/sh
# 镜像入口：按 FORGE_ROLE 选择启动的进程，用于不能自定义启动命令的部署平台（例如 Matrix）。
# 不设置或设为 server 时启动镜像默认命令（forge-server），设为 worker 时启动 forge-worker。
# 用 exec 替换当前进程，让 SIGTERM 直接到达 forge-server / forge-worker，优雅退出才会生效。
set -eu

case "${FORGE_ROLE:-server}" in
server)
	exec "$@"
	;;
worker)
	exec /app/forge-worker
	;;
*)
	echo "FORGE_ROLE 只能是 server 或 worker，当前是 \"${FORGE_ROLE}\"" >&2
	exit 1
	;;
esac
