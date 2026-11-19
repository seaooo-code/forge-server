# forge-server 镜像：API 进程（forge-server）、迁移工具（forge-migrate）和构建 worker（forge-worker）。
# 镜像里没有 git 和 Docker，worker 在这里只能处理 gitlab_ci 构建，需要 Docker 的 worker 部署方式另定。默认启动 forge-server，设置 FORGE_ROLE=worker 时启动 forge-worker。
# 只用标准 Dockerfile 语法（不依赖 BuildKit 扩展），MIFLOW/kaniko 可直接构建。部署说明见 docs/deployment.md。

# DevX Go 构建镜像，已配置公司 Go 代理（pkgs.d.xiaomi.net go-virtual）。go.mod 要求 Go 1.25。
ARG GO_IMAGE=micr.cloud.mioffice.cn/devx-build-image/golang:1.25-ubuntu24-base
# DevX 精简运行镜像，apt 使用内网源 apt.sys.srv；自带 work 用户（uid 10000）。
ARG RUNTIME_IMAGE=micr.cloud.mioffice.cn/devx-build-image/base:ubuntu-2404-mini

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 静态编译；GOAMD64=v1 覆盖基础镜像里的 v3，避免在不支持 AVX2 的节点上无法启动。
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1
RUN go build -trimpath -ldflags "-s -w" -o /out/forge-server ./cmd/forge-server \
    && go build -trimpath -ldflags "-s -w" -o /out/forge-migrate ./cmd/forge-migrate \
    && go build -trimpath -ldflags "-s -w" -o /out/forge-worker ./cmd/forge-worker

FROM ${RUNTIME_IMAGE}
# ca-certificates：访问飞书、GitLab、FDS 等 HTTPS 服务；tzdata：基础镜像把 /etc/localtime 指向 Asia/Shanghai，但没装时区数据。
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*
# 迁移按相对路径 migrations/ 读取，工作目录必须包含它。
WORKDIR /app
COPY --from=build /out/forge-server /out/forge-migrate /out/forge-worker /app/
COPY migrations/ /app/migrations/
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
USER work
ENV HTTP_ADDR=:8080
EXPOSE 8080
# 部署平台不能自定义启动命令时，用 FORGE_ROLE=worker 让同一个镜像启动 forge-worker。用 sh 调用，不依赖文件的可执行位。
ENTRYPOINT ["/bin/sh", "/app/docker-entrypoint.sh"]
CMD ["/app/forge-server"]
