package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"forge-server/internal/domain"
)

// capabilityProbeTimeout 是探测单个命令的超时，docker daemon 无响应时不能拖住 worker 启动。
const capabilityProbeTimeout = 10 * time.Second

// BuildModeCapabilities 是 worker 在当前环境下实际能处理的构建方式。
type BuildModeCapabilities struct {
	// Enabled 保持配置的顺序。
	Enabled []domain.BuildMode
	// Disabled 是被自动禁用的构建方式及原因。
	Disabled map[domain.BuildMode]string
}

// DetectBuildModes 按运行环境的能力过滤配置的构建方式：dockerfile 和 adapter 需要 git 和可连接的 Docker daemon，
// 缺少时自动禁用，worker 就不会领取它们并让构建失败；gitlab_ci 只访问代码源接口，不依赖本机环境。
// runner 为空时执行真实命令。只在 worker 启动时探测一次，不含 docker 构建方式的配置不会执行任何探测命令。
func DetectBuildModes(ctx context.Context, configured []domain.BuildMode, runner commandRunner) BuildModeCapabilities {
	if runner == nil {
		runner = execCommand
	}

	capabilities := BuildModeCapabilities{Disabled: map[domain.BuildMode]string{}}
	var dockerReason string
	probed := false
	seen := map[domain.BuildMode]bool{}
	for _, mode := range configured {
		if seen[mode] {
			continue
		}
		seen[mode] = true

		if mode != domain.BuildModeDockerfile && mode != domain.BuildModeAdapter {
			capabilities.Enabled = append(capabilities.Enabled, mode)
			continue
		}
		if !probed {
			dockerReason = probeDockerBuild(ctx, runner)
			probed = true
		}
		if dockerReason != "" {
			capabilities.Disabled[mode] = dockerReason
			continue
		}
		capabilities.Enabled = append(capabilities.Enabled, mode)
	}
	return capabilities
}

// probeDockerBuild 检查 docker 构建需要的 git 和 Docker daemon，返回不能使用的原因，可用时返回空字符串。
func probeDockerBuild(ctx context.Context, runner commandRunner) string {
	for _, probe := range []struct {
		command command
		failure string
	}{
		{command{name: "git", args: []string{"--version"}}, "找不到 git 命令"},
		{command{name: "docker", args: []string{"version", "--format", "{{.Server.Version}}"}, captureStdout: true}, "找不到 docker 命令，或无法连接 Docker daemon"},
	} {
		probeCtx, cancel := context.WithTimeout(ctx, capabilityProbeTimeout)
		output, err := runner(probeCtx, probe.command)
		cancel()
		if err != nil {
			return fmt.Sprintf("%s：%v", probe.failure, err)
		}
		// docker version 在 daemon 不可达时某些版本只打印客户端信息、退出码仍为 0，这时 Server.Version 是空的。
		if probe.command.captureStdout && strings.TrimSpace(output) == "" {
			return probe.failure + "：没有读到 Docker daemon 的版本"
		}
	}
	return ""
}
