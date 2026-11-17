package worker

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"forge-server/internal/domain"
)

// dockerSecretEnvPrefix 是密钥变量传给 docker CLI 时使用的环境变量前缀。
// 加前缀是为了避免用户变量（例如 DOCKER_HOST、PATH）覆盖 docker CLI 自身的环境；Dockerfile 里仍按 id=KEY 挂载。
const dockerSecretEnvPrefix = "FORGE_BUILD_SECRET_"

// dockerBuildPaths 是 docker build 使用的绝对路径，均位于检出的仓库目录内。
type dockerBuildPaths struct {
	repoDir    string
	dockerfile string
	context    string
}

// resolveDockerBuildPaths 使用模块配置的 Dockerfile 路径和构建上下文；模块未配置 Dockerfile 时才使用 FORGE_WORKER_DOCKERFILE 兜底。
// 两者都必须是仓库内的相对路径，拒绝绝对路径和 ..。
func resolveDockerBuildPaths(repoDir string, dockerfilePath string, buildContext string, fallbackDockerfile string) (dockerBuildPaths, error) {
	dockerfile := strings.TrimSpace(dockerfilePath)
	if dockerfile == "" {
		dockerfile = strings.TrimSpace(fallbackDockerfile)
	}
	if dockerfile == "" {
		dockerfile = domain.DefaultModuleDockerfilePath
	}
	contextPath := strings.TrimSpace(buildContext)
	if contextPath == "" {
		contextPath = domain.DefaultModuleBuildContext
	}

	resolved := dockerBuildPaths{repoDir: repoDir}
	for _, item := range []struct {
		label  string
		value  string
		target *string
	}{
		{label: "Dockerfile 路径", value: dockerfile, target: &resolved.dockerfile},
		{label: "构建上下文", value: contextPath, target: &resolved.context},
	} {
		cleaned := path.Clean(item.value)
		if strings.Contains(item.value, "\\") || path.IsAbs(item.value) || filepath.IsAbs(item.value) ||
			cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return dockerBuildPaths{}, fmt.Errorf("%s %q 必须是仓库内的相对路径", item.label, item.value)
		}
		*item.target = filepath.Join(repoDir, filepath.FromSlash(cleaned))
	}
	if resolved.dockerfile == filepath.Clean(repoDir) {
		return dockerBuildPaths{}, fmt.Errorf("Dockerfile 路径 %q 不能是仓库根目录", dockerfile)
	}
	return resolved, nil
}

// verifyDockerBuildPaths 在检出后确认 Dockerfile 是文件、构建上下文是目录，并且解析软链后仍在仓库内，
// 防止仓库用软链把宿主机目录作为构建上下文发送给 docker。dockerfile 为空（adapter 模式）时只校验构建上下文。
func verifyDockerBuildPaths(repoDir string, paths dockerBuildPaths) error {
	realRepo, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		return fmt.Errorf("resolve repo dir: %w", err)
	}
	for _, item := range []struct {
		label   string
		path    string
		wantDir bool
	}{
		{label: "Dockerfile", path: paths.dockerfile},
		{label: "构建上下文", path: paths.context, wantDir: true},
	} {
		if item.path == "" {
			continue
		}
		relative, _ := filepath.Rel(repoDir, item.path)
		realPath, err := filepath.EvalSymlinks(item.path)
		if err != nil {
			return fmt.Errorf("仓库中找不到%s %s", item.label, filepath.ToSlash(relative))
		}
		inside, err := filepath.Rel(realRepo, realPath)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s %s 指向仓库外部", item.label, filepath.ToSlash(relative))
		}
		info, err := os.Stat(realPath)
		if err != nil {
			return fmt.Errorf("仓库中找不到%s %s", item.label, filepath.ToSlash(relative))
		}
		if info.IsDir() != item.wantDir {
			return fmt.Errorf("%s %s 类型不正确", item.label, filepath.ToSlash(relative))
		}
	}
	return nil
}

// dockerBuildVariableOptions 生成 docker build 的变量参数和环境变量：普通变量用 --build-arg，
// 密钥变量用 BuildKit 的 --secret 从环境变量读取，密钥值只出现在返回的 env 中，不会出现在命令行参数里。
// 平台固定注入的 FORGE_* 变量放在最后，用户变量不能覆盖。
func dockerBuildVariableOptions(record domain.BuildRecord, commitSHA string, variables []domain.ModuleBuildVariable) ([]string, []string) {
	sorted := append([]domain.ModuleBuildVariable(nil), variables...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	options := []string{}
	env := []string{"DOCKER_BUILDKIT=1"}
	for _, variable := range sorted {
		if !domain.IsValidBuildVariableKey(variable.Key) || strings.HasPrefix(variable.Key, "FORGE_") {
			continue
		}
		if variable.IsSecret {
			envName := dockerSecretEnvPrefix + variable.Key
			options = append(options, "--secret", "id="+variable.Key+",env="+envName)
			env = append(env, envName+"="+variable.Value)
			continue
		}
		options = append(options, "--build-arg", variable.Key+"="+variable.Value)
	}
	for _, item := range [][2]string{
		{"FORGE_ENVIRONMENT", record.Environment},
		{"FORGE_BRANCH", record.Branch},
		{"FORGE_COMMIT_SHA", commitSHA},
		{"FORGE_BUILD_ID", record.ID},
		{"FORGE_MODULE_ID", record.ModuleID},
		// 依赖产物目录相对构建上下文；只有注入了依赖时目录才存在。
		{domain.BuildDependenciesDirArg, buildDependenciesDirArgValue},
	} {
		options = append(options, "--build-arg", item[0]+"="+item[1])
	}
	return options, env
}

func dockerBuildArgs(paths dockerBuildPaths, imageTag string, buildOptions []string) []string {
	args := append([]string{"build", "--progress", "plain", "--file", paths.dockerfile}, buildOptions...)
	return append(args, "--tag", imageTag, paths.context)
}

func dockerBuildOutputArgs(paths dockerBuildPaths, outputDir string, buildOptions []string) []string {
	args := append([]string{"build", "--progress", "plain", "--file", paths.dockerfile}, buildOptions...)
	return append(args, "--output", "type=local,dest="+outputDir, paths.context)
}
