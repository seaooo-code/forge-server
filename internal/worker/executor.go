package worker

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
	s3infra "forge-server/internal/infra/s3"
	"forge-server/internal/service"
)

// RepositoryCredentialsProvider 通过代码源 provider 获取 clone 凭证，由 service.CodeSourceService 实现。
type RepositoryCredentialsProvider interface {
	CloneCredentials(ctx context.Context, repository domain.ModuleRepository) (domain.GitCredentials, error)
}

// BuildVariableResolver 解析模块在某个环境下生效的构建变量（已解密、已按环境覆盖），由 service.BuildVariableService 实现。
type BuildVariableResolver interface {
	ResolveBuildVariables(ctx context.Context, moduleID string, environmentCode string) ([]domain.ModuleBuildVariable, error)
}

// BuildAdapterProvider 读取构建适配器并把实际使用的版本写回构建记录，由 service.BuildAdapterService 实现。
type BuildAdapterProvider interface {
	GetBuildAdapter(ctx context.Context, id string) (domain.BuildAdapter, error)
	RecordBuildAdapterUsage(ctx context.Context, buildID string, usage domain.BuildAdapterUsage) error
}

const dockerBuildArtifactDir = "/app/dist"

// renderedDockerfileName 是适配器渲染出的 Dockerfile 文件名，写在构建工作目录（仓库之外）。
const renderedDockerfileName = "Dockerfile.adapter"

type Executor struct {
	adapters     BuildAdapterProvider
	cfg          config.WorkerConfig
	credentials  RepositoryCredentialsProvider
	dependencies BuildDependencyProvider
	// dependencyArtifacts 读取依赖构建的清单和文件，为空时使用 infra/s3。
	dependencyArtifacts DependencyArtifactReader
	variables           BuildVariableResolver
	logs                BuildLogStore
	logger              *slog.Logger
	// runner 为空时使用 execCommand，测试可以替换成假实现。
	runner commandRunner
	// logFlushInterval 为 0 时使用 buildLogFlushInterval。
	logFlushInterval time.Duration
}

type ExecutorDependencies struct {
	Adapters    BuildAdapterProvider
	Credentials RepositoryCredentialsProvider
	// Dependencies 为空时不注入依赖产物。
	Dependencies BuildDependencyProvider
	// DependencyArtifacts 为空时使用 infra/s3 按依赖构建的存储配置读取。
	DependencyArtifacts DependencyArtifactReader
	Logs                BuildLogStore
	Variables           BuildVariableResolver
}

type buildWorkspace struct {
	outputDir string
	repoDir   string
	rootDir   string
}

func NewExecutor(cfg config.WorkerConfig, deps ExecutorDependencies, logger *slog.Logger) *Executor {
	dependencyArtifacts := deps.DependencyArtifacts
	if dependencyArtifacts == nil {
		dependencyArtifacts = s3DependencyArtifactReader{}
	}
	return &Executor{
		adapters:            deps.Adapters,
		cfg:                 cfg,
		credentials:         deps.Credentials,
		dependencies:        deps.Dependencies,
		dependencyArtifacts: dependencyArtifacts,
		variables:           deps.Variables,
		logs:                deps.Logs,
		logger:              logger,
	}
}

func (e *Executor) Execute(ctx context.Context, record domain.BuildRecord) (domain.BuildRecordCompletion, error) {
	if err := e.validateBuildTask(record); err != nil {
		return domain.BuildRecordCompletion{}, err
	}

	flushInterval := e.logFlushInterval
	if flushInterval <= 0 {
		flushInterval = buildLogFlushInterval
	}
	logs := newBuildLogStream(ctx, e.logs, e.logger, record.ID, buildLogOptions{
		chunkBytes:    buildLogChunkBytes,
		flushInterval: flushInterval,
		maxBytes:      e.cfg.LogMaxBytes,
	})

	completion, err := e.execute(ctx, record, logs)
	if err != nil {
		logs.Linef("构建失败：%v", err)
	} else {
		logs.Linef("构建成功：commit %s，runtimeVersion %s，平台 %s，共 %d 个文件", completion.CommitSHA, completion.RuntimeVersion, strings.Join(completion.Platforms, ","), completion.FileCount)
	}
	// 必须先写完日志再返回：worker 随后才把构建标记为结束，日志接口据此判断 finished。
	_ = logs.Close()
	return completion, err
}

// execute 按检出、docker 构建、产物校验、生成清单、上传的顺序串行执行，每一步的输出都进入构建日志。
func (e *Executor) execute(ctx context.Context, record domain.BuildRecord, logs *buildLogStream) (domain.BuildRecordCompletion, error) {
	logs.Linef("开始构建 %s（#%d）：模块 %s，环境 %s，分支 %s", record.ID, record.Number, record.ModuleID, record.Environment, record.Branch)

	// 适配器在构建开始时读取，整个构建使用这一刻的版本；停用的适配器直接失败。
	var adapter *domain.BuildAdapter
	if record.BuildMode == domain.BuildModeAdapter {
		loaded, err := e.loadBuildAdapter(ctx, record)
		if err != nil {
			return domain.BuildRecordCompletion{}, err
		}
		adapter = &loaded
		logs.Linef("构建适配器：%s（%s），版本 %d", adapter.Name, adapter.ID, adapter.Version)
	}

	credentials, err := e.credentials.CloneCredentials(ctx, record.Repository)
	if err != nil {
		return domain.BuildRecordCompletion{}, fmt.Errorf("获取仓库凭证失败：%w", err)
	}
	logs.AddSecrets(credentials.Password)

	var variables []domain.ModuleBuildVariable
	if e.variables != nil {
		variables, err = e.variables.ResolveBuildVariables(ctx, record.ModuleID, record.Environment)
		if err != nil {
			return domain.BuildRecordCompletion{}, fmt.Errorf("读取构建变量失败：%w", err)
		}
	}
	for _, variable := range variables {
		if variable.IsSecret {
			logs.AddSecrets(variable.Value)
		}
	}

	workspace, cleanup, err := e.prepareBuildWorkspace(record)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	defer cleanup()

	paths, err := resolveDockerBuildPaths(workspace.repoDir, record.DockerfilePath, record.BuildContext, e.cfg.Dockerfile)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	if adapter != nil {
		// adapter 模式不使用仓库内的 Dockerfile，检出后改为渲染结果。
		paths.dockerfile = ""
	}

	commitSHA, err := e.checkoutRepository(ctx, record, credentials, workspace.repoDir, logs)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	logs.Linef("已检出 commit %s", commitSHA)

	if err := verifyDockerBuildPaths(workspace.repoDir, paths); err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	var adapterFiles map[string]string
	if adapter != nil {
		paths.dockerfile, adapterFiles, err = e.prepareBuildAdapter(ctx, record, *adapter, commitSHA, workspace.rootDir, logs)
		if err != nil {
			return domain.BuildRecordCompletion{}, err
		}
	}

	// Feature 模块注入所属 Common 模块的生效产物；没有需要写入 .forge 的内容时不检查仓库里的 .forge。
	var dependencyPlan service.BuildDependencyPlan
	injectDependencies := false
	if e.dependencies != nil {
		dependencyPlan, injectDependencies, err = e.dependencies.ResolveBuildDependencies(ctx, record)
		if err != nil {
			return domain.BuildRecordCompletion{}, fmt.Errorf("解析依赖构建失败：%w", err)
		}
	}
	if len(adapterFiles) > 0 || injectDependencies {
		if err := ensureForgeDirAvailable(paths.context); err != nil {
			return domain.BuildRecordCompletion{}, err
		}
	}
	if err := writeBuildAdapterFiles(paths.context, adapterFiles); err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	if injectDependencies {
		dependencies, err := e.injectBuildDependencies(ctx, paths.context, dependencyPlan, logs)
		if err != nil {
			return domain.BuildRecordCompletion{}, fmt.Errorf("注入依赖产物失败：%w", err)
		}
		if err := e.dependencies.RecordBuildDependencies(ctx, record.ID, dependencies); err != nil {
			return domain.BuildRecordCompletion{}, fmt.Errorf("记录依赖构建失败：%w", err)
		}
	}
	buildOptions, buildEnv := dockerBuildVariableOptions(record, commitSHA, variables)
	if err := e.exportDockerArtifact(ctx, record.ID+"-"+filepath.Base(workspace.rootDir), paths, buildOptions, buildEnv, workspace.outputDir, logs); err != nil {
		return domain.BuildRecordCompletion{}, err
	}

	artifactRoot, err := buildArtifactRoot(workspace.outputDir)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	forgeManifest, platforms, err := validateArtifactContract(artifactRoot)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	logs.Linef("产物校验通过：runtimeVersion %s，平台 %s", forgeManifest.RuntimeVersion, strings.Join(platforms, ","))

	manifest, err := generateArtifactManifest(artifactRoot, e.artifactKey(record), e.cfg.ArtifactDigests)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	logs.Linef("上传 %d 个文件，共 %d 字节", manifest.FileCount, manifest.TotalSizeBytes)
	if err := e.uploadArtifact(ctx, record, artifactRoot, manifest); err != nil {
		return domain.BuildRecordCompletion{}, fmt.Errorf("上传产物失败：%w", err)
	}

	completion := buildRecordCompletion(record, manifest)
	completion.CommitSHA = commitSHA
	completion.RuntimeVersion = forgeManifest.RuntimeVersion
	completion.Platforms = platforms
	return completion, nil
}

func (e *Executor) validateBuildTask(record domain.BuildRecord) error {
	if record.Repository.ConnectionID == "" {
		return domain.ErrInvalidBuildInput
	}
	if e.credentials == nil {
		return domain.ErrCodeSourceAuthNotConfigured
	}
	if record.BuildMode == domain.BuildModeAdapter && (e.adapters == nil || record.ModuleBuildAdapterID == "") {
		return domain.ErrInvalidBuildInput
	}
	return nil
}

func (e *Executor) loadBuildAdapter(ctx context.Context, record domain.BuildRecord) (domain.BuildAdapter, error) {
	adapter, err := e.adapters.GetBuildAdapter(ctx, record.ModuleBuildAdapterID)
	if err != nil {
		return domain.BuildAdapter{}, fmt.Errorf("读取构建适配器 %s 失败：%w", record.ModuleBuildAdapterID, err)
	}
	if adapter.Status != domain.BuildAdapterStatusActive {
		return domain.BuildAdapter{}, fmt.Errorf("构建适配器 %s（%s）已停用", adapter.Name, adapter.ID)
	}
	return adapter, nil
}

// prepareBuildAdapter 渲染适配器：Dockerfile 写到仓库之外的工作目录，并把适配器版本和渲染结果 sha256 写回构建记录。
// 模板上下文不包含构建变量和密钥。返回渲染后 Dockerfile 的绝对路径和待写入 {构建上下文}/.forge/ 的辅助文件。
func (e *Executor) prepareBuildAdapter(ctx context.Context, record domain.BuildRecord, adapter domain.BuildAdapter, commitSHA string, workDir string, logs *buildLogStream) (string, map[string]string, error) {
	params, err := service.ResolveBuildAdapterParams(adapter.Parameters, record.ModuleBuildAdapterParams, true)
	if err != nil {
		return "", nil, fmt.Errorf("构建适配器参数无效：%w", err)
	}
	buildTime := time.Now().UTC()
	if record.StartedAt != nil {
		buildTime = record.StartedAt.UTC()
	}
	rendered, err := service.RenderBuildAdapter(adapter, domain.BuildAdapterRenderContext{
		Module:      domain.BuildAdapterRenderModule{ID: record.ModuleID, Name: record.ModuleName},
		Environment: record.Environment,
		Branch:      record.Branch,
		CommitSHA:   commitSHA,
		Build:       domain.BuildAdapterRenderBuild{ID: record.ID, Number: record.Number, Time: buildTime.Format(time.RFC3339)},
		Params:      params,
	})
	if err != nil {
		return "", nil, fmt.Errorf("渲染构建适配器失败：%w", err)
	}
	logs.Linef("构建适配器渲染完成：Dockerfile sha256 %s，辅助文件 %d 个", rendered.DockerfileSHA256, len(rendered.Files))

	if err := e.adapters.RecordBuildAdapterUsage(ctx, record.ID, domain.BuildAdapterUsage{
		AdapterID:                adapter.ID,
		AdapterVersion:           adapter.Version,
		RenderedDockerfileSHA256: rendered.DockerfileSHA256,
	}); err != nil {
		return "", nil, fmt.Errorf("记录构建适配器版本失败：%w", err)
	}

	dockerfile := filepath.Join(workDir, renderedDockerfileName)
	if err := os.WriteFile(dockerfile, []byte(rendered.Dockerfile), 0o644); err != nil {
		return "", nil, fmt.Errorf("写入渲染后的 Dockerfile 失败：%w", err)
	}
	return dockerfile, rendered.Files, nil
}

// writeBuildAdapterFiles 把辅助文件写到 {构建上下文}/.forge/。调用前必须已通过 ensureForgeDirAvailable，
// 确认仓库里没有 .forge；适配器没有辅助文件时不创建目录。辅助文件不能写入平台保留的 .forge/deps/。
func writeBuildAdapterFiles(contextDir string, files map[string]string) error {
	forgeDir := filepath.Join(contextDir, domain.BuildAdapterFilesDir)
	for filePath, content := range files {
		if !domain.IsValidBuildAdapterFilePath(filePath) || domain.IsReservedBuildAdapterFilePath(filePath) {
			return fmt.Errorf("构建适配器辅助文件路径 %q 无效", filePath)
		}
		target := filepath.Join(forgeDir, filepath.FromSlash(filePath))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("创建辅助文件目录失败：%w", err)
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return fmt.Errorf("写入辅助文件 %s 失败：%w", filePath, err)
		}
	}
	return nil
}

func (e *Executor) prepareBuildWorkspace(record domain.BuildRecord) (buildWorkspace, func(), error) {
	baseWorkDir := e.workDir()
	if err := os.MkdirAll(baseWorkDir, 0o755); err != nil {
		return buildWorkspace{}, nil, fmt.Errorf("create worker base dir: %w", err)
	}

	workDir, err := os.MkdirTemp(baseWorkDir, record.ID+"-*")
	if err != nil {
		return buildWorkspace{}, nil, fmt.Errorf("create build work dir: %w", err)
	}
	cleanup := func() {
		if err := os.RemoveAll(workDir); err != nil && e.logger != nil {
			e.logger.Warn("remove build work dir failed", "build", record.ID, "path", workDir, "error", err)
		}
	}

	workspace := buildWorkspace{
		outputDir: filepath.Join(workDir, "output"),
		repoDir:   filepath.Join(workDir, "repo"),
		rootDir:   workDir,
	}
	if err := os.MkdirAll(workspace.outputDir, 0o755); err != nil {
		cleanup()
		return buildWorkspace{}, nil, fmt.Errorf("create build output dir: %w", err)
	}
	return workspace, cleanup, nil
}

func buildRecordCompletion(record domain.BuildRecord, manifest domain.ArtifactManifest) domain.BuildRecordCompletion {
	return domain.BuildRecordCompletion{
		ArtifactKey:    manifest.ArtifactKey,
		ArtifactURL:    artifactURL(record.StoragePublicBaseURL, manifest.ArtifactKey),
		ManifestKey:    artifactManifestKey(manifest.ArtifactKey),
		FileCount:      manifest.FileCount,
		TotalSizeBytes: manifest.TotalSizeBytes,
	}
}

func (e *Executor) exportDockerArtifact(ctx context.Context, buildID string, paths dockerBuildPaths, buildOptions []string, buildEnv []string, outputDir string, logs io.Writer) error {
	docker := func(env []string, captureStdout bool, args ...string) (string, error) {
		return e.runCommand(ctx, command{env: env, name: "docker", args: args, log: logs, captureStdout: captureStdout})
	}

	outputPlan := dockerArtifactOutputPlan(paths.repoDir, paths.dockerfile)
	if outputPlan.useBuildOutput {
		_, err := docker(buildEnv, false, dockerBuildOutputArgs(paths, outputDir, buildOptions)...)
		return err
	}

	imageTag := dockerImageTag(buildID)
	if _, err := docker(buildEnv, false, dockerBuildArgs(paths, imageTag, buildOptions)...); err != nil {
		return err
	}
	defer e.cleanupDockerImage(imageTag)

	containerID, err := docker(nil, true, dockerCreateArgs(imageTag)...)
	if err != nil {
		return err
	}
	if containerID == "" {
		return errors.New("docker create returned empty container id")
	}
	defer e.cleanupDockerContainer(containerID)

	_, err = docker(nil, false, dockerCopyArgs(containerID, outputPlan.copySource, outputDir)...)
	return err
}

func dockerCreateArgs(imageTag string) []string {
	return []string{"create", "--entrypoint", "true", imageTag}
}

func dockerCopyArgs(containerID string, artifactSource string, outputDir string) []string {
	return []string{"cp", containerID + ":" + artifactSource, outputDir}
}

type dockerOutputPlan struct {
	useBuildOutput bool
	copySource     string
}

func dockerArtifactOutputPlan(repoDir string, dockerfile string) dockerOutputPlan {
	content, err := os.ReadFile(dockerfilePath(repoDir, dockerfile))
	if err != nil {
		return dockerOutputPlan{copySource: dockerBuildArtifactDir + "/."}
	}
	if dockerfileUsesScratchRootArtifact(string(content)) {
		return dockerOutputPlan{useBuildOutput: true}
	}
	return dockerOutputPlan{copySource: dockerBuildArtifactDir + "/."}
}

func dockerfilePath(repoDir string, dockerfile string) string {
	dockerfile = strings.TrimSpace(dockerfile)
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if filepath.IsAbs(dockerfile) {
		return dockerfile
	}
	return filepath.Join(repoDir, filepath.Clean(dockerfile))
}

type dockerfileInstruction struct {
	name string
	args string
}

func dockerfileUsesScratchRootArtifact(content string) bool {
	finalStageFromScratch := false
	copiesAppDistToRoot := false
	for _, instruction := range dockerfileInstructions(content) {
		switch instruction.name {
		case "FROM":
			finalStageFromScratch = dockerfileFromImage(instruction.args) == "scratch"
			copiesAppDistToRoot = false
		case "COPY":
			if finalStageFromScratch && dockerCopyMovesAppDistToRoot(instruction.args) {
				copiesAppDistToRoot = true
			}
		}
	}
	return finalStageFromScratch && copiesAppDistToRoot
}

func dockerfileInstructions(content string) []dockerfileInstruction {
	var instructions []dockerfileInstruction
	var current strings.Builder
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		if strings.HasSuffix(trimmed, "\\") {
			current.WriteString(strings.TrimSpace(strings.TrimSuffix(trimmed, "\\")))
			continue
		}
		current.WriteString(trimmed)
		instruction := strings.TrimSpace(current.String())
		current.Reset()

		fields := strings.Fields(instruction)
		if len(fields) == 0 {
			continue
		}
		instructions = append(instructions, dockerfileInstruction{
			name: strings.ToUpper(fields[0]),
			args: strings.TrimSpace(instruction[len(fields[0]):]),
		})
	}
	if current.Len() > 0 {
		instruction := strings.TrimSpace(current.String())
		fields := strings.Fields(instruction)
		if len(fields) > 0 {
			instructions = append(instructions, dockerfileInstruction{
				name: strings.ToUpper(fields[0]),
				args: strings.TrimSpace(instruction[len(fields[0]):]),
			})
		}
	}
	return instructions
}

func dockerfileFromImage(args string) string {
	for _, field := range strings.Fields(args) {
		if strings.HasPrefix(field, "--") {
			continue
		}
		return strings.ToLower(field)
	}
	return ""
}

func dockerCopyMovesAppDistToRoot(args string) bool {
	values, hasFrom := dockerCopyValues(args)
	if !hasFrom || len(values) < 2 || cleanDockerPath(values[len(values)-1]) != "/" {
		return false
	}
	for _, source := range values[:len(values)-1] {
		if cleanDockerPath(source) == dockerBuildArtifactDir {
			return true
		}
	}
	return false
}

func dockerCopyValues(args string) ([]string, bool) {
	fields := strings.Fields(args)
	hasFrom := false
	valueStart := 0
	for valueStart < len(fields) {
		field := fields[valueStart]
		if !strings.HasPrefix(field, "--") {
			break
		}
		switch {
		case strings.HasPrefix(field, "--from="):
			hasFrom = true
			valueStart++
		case field == "--from":
			hasFrom = true
			valueStart += 2
		default:
			valueStart++
		}
	}

	remaining := strings.TrimSpace(strings.Join(fields[valueStart:], " "))
	if strings.HasPrefix(remaining, "[") {
		var values []string
		if err := json.Unmarshal([]byte(remaining), &values); err == nil {
			return values, hasFrom
		}
	}

	values := make([]string, 0, len(fields)-valueStart)
	for _, field := range fields[valueStart:] {
		values = append(values, strings.Trim(field, `"'`))
	}
	return values, hasFrom
}

func cleanDockerPath(value string) string {
	value = strings.TrimSpace(strings.Trim(value, `"'`))
	if value == "" {
		return ""
	}
	return path.Clean(value)
}

func dockerImageTag(buildID string) string {
	buildID = strings.ToLower(strings.TrimSpace(buildID))
	var suffix strings.Builder
	for _, value := range buildID {
		switch {
		case value >= 'a' && value <= 'z':
			suffix.WriteRune(value)
		case value >= '0' && value <= '9':
			suffix.WriteRune(value)
		case value == '_' || value == '.' || value == '-':
			suffix.WriteRune(value)
		default:
			suffix.WriteByte('-')
		}
	}

	tag := strings.Trim(suffix.String(), ".-")
	if tag == "" {
		tag = "build"
	}
	if len(tag) > 120 {
		tag = tag[:120]
	}
	return "forge-worker-build:" + tag
}

func (e *Executor) cleanupDockerContainer(containerID string) {
	if _, err := e.runCommand(context.Background(), command{name: "docker", args: []string{"rm", "-f", containerID}}); err != nil && e.logger != nil {
		e.logger.Warn("remove build container failed", "container", containerID, "error", err)
	}
}

func (e *Executor) cleanupDockerImage(imageTag string) {
	if _, err := e.runCommand(context.Background(), command{name: "docker", args: []string{"image", "rm", "-f", imageTag}}); err != nil && e.logger != nil {
		e.logger.Warn("remove build image failed", "image", imageTag, "error", err)
	}
}

// generateArtifactManifest 扫描本地产物生成清单。digests 是 FORGE_ARTIFACT_DIGESTS 配置的算法，sha256 字段始终计算以兼容旧逻辑。
func generateArtifactManifest(artifactRoot string, artifactKey string, digests []string) (domain.ArtifactManifest, error) {
	// manifest 是产物上传和客户端下载的共同索引，这里只扫描本地文件并生成稳定元数据，不访问远端存储。
	files, err := artifactFiles(artifactRoot)
	if err != nil {
		return domain.ArtifactManifest{}, fmt.Errorf("list build artifacts: %w", err)
	}
	if len(files) == 0 {
		return domain.ArtifactManifest{}, errors.New("docker build produced no artifacts")
	}

	manifest := newArtifactManifest(artifactKey)
	for _, artifact := range files {
		objectKey := strings.TrimRight(artifactKey, "/") + "/" + strings.TrimLeft(artifact.key, "/")
		manifestFile, err := artifactManifestFile(artifact, objectKey, digests)
		if err != nil {
			return domain.ArtifactManifest{}, err
		}
		manifest.Files = append(manifest.Files, manifestFile)
		manifest.FileCount++
		manifest.TotalSizeBytes += manifestFile.SizeBytes
	}
	return manifest, nil
}

func (e *Executor) uploadArtifact(ctx context.Context, record domain.BuildRecord, artifactRoot string, manifest domain.ArtifactManifest) error {
	// 上传阶段完全以 manifest 为准，保证对象存储中的文件、manifest.json 和构建记录摘要使用同一份产物索引。
	client, err := s3infra.NewClient(s3infra.Options{
		Region:          record.StorageRegion,
		Bucket:          record.StorageBucket,
		Endpoint:        record.StorageEndpoint,
		AccessKeyID:     record.StorageAccessKeyID,
		SecretAccessKey: record.StorageSecretAccessKey,
	})
	if err != nil {
		return err
	}

	for _, manifestFile := range manifest.Files {
		if err := uploadArtifactFile(ctx, client, artifactRoot, manifestFile); err != nil {
			return err
		}
	}
	return uploadArtifactManifest(ctx, client, manifest)
}

func uploadArtifactFile(ctx context.Context, client *s3infra.Client, artifactRoot string, manifestFile domain.ArtifactManifestFile) error {
	artifactPath, err := artifactLocalPath(artifactRoot, manifestFile.Path)
	if err != nil {
		return err
	}
	file, err := os.Open(artifactPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return client.PutObject(ctx, manifestFile.ObjectKey, file, manifestFile.ContentType)
}

func uploadArtifactManifest(ctx context.Context, client *s3infra.Client, manifest domain.ArtifactManifest) error {
	manifestPayload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode artifact manifest: %w", err)
	}
	return client.PutObject(ctx, artifactManifestKey(manifest.ArtifactKey), strings.NewReader(string(manifestPayload)), "application/json")
}

func artifactLocalPath(artifactRoot string, artifactPath string) (string, error) {
	// manifest 路径未来可能来自独立任务产物，上传前必须阻止越界路径访问 worker 本地文件。
	cleaned := path.Clean(strings.TrimSpace(artifactPath))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, "\\") {
		return "", errors.New("artifact manifest contains invalid file path")
	}
	return filepath.Join(artifactRoot, filepath.FromSlash(cleaned)), nil
}

func newArtifactManifest(artifactKey string) domain.ArtifactManifest {
	return domain.ArtifactManifest{
		Version:     1,
		ArtifactKey: artifactKey,
		GeneratedAt: time.Now().UTC(),
		Files:       []domain.ArtifactManifestFile{},
	}
}

func artifactManifestFile(artifact artifactFile, objectKey string, digests []string) (domain.ArtifactManifestFile, error) {
	file, err := os.Open(artifact.path)
	if err != nil {
		return domain.ArtifactManifestFile{}, err
	}
	defer file.Close()

	sha256Hash := sha256.New()
	hashes := map[string]hash.Hash{"sha256": sha256Hash}
	writers := []io.Writer{sha256Hash}
	for _, algorithm := range digests {
		if _, ok := hashes[algorithm]; ok {
			continue
		}
		switch algorithm {
		case "md5":
			hashes[algorithm] = md5.New()
		default:
			return domain.ArtifactManifestFile{}, fmt.Errorf("unsupported artifact digest algorithm %q", algorithm)
		}
		writers = append(writers, hashes[algorithm])
	}

	size, err := io.Copy(io.MultiWriter(writers...), file)
	if err != nil {
		return domain.ArtifactManifestFile{}, err
	}

	fileDigests := make(map[string]string, len(digests))
	for _, algorithm := range digests {
		fileDigests[algorithm] = fmt.Sprintf("%x", hashes[algorithm].Sum(nil))
	}
	return domain.ArtifactManifestFile{
		Path:        artifact.key,
		ObjectKey:   objectKey,
		SizeBytes:   size,
		ContentType: contentType(artifact.path),
		SHA256:      fmt.Sprintf("%x", sha256Hash.Sum(nil)),
		Digests:     fileDigests,
	}, nil
}

type artifactFile struct {
	key  string
	path string
}

func artifactFiles(artifactRoot string) ([]artifactFile, error) {
	files := make([]artifactFile, 0)
	err := filepath.WalkDir(artifactRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relativePath, err := filepath.Rel(artifactRoot, path)
		if err != nil {
			return err
		}
		files = append(files, artifactFile{
			key:  filepath.ToSlash(relativePath),
			path: path,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// artifactKey 用应用 slug、模块 id 和环境 code 组成对象路径。这些标识创建后不变，
// 也都是 URL 安全的字符；显示名可以随时改名，且可能含中文和空格，不适合放进路径。
func (e *Executor) artifactKey(record domain.BuildRecord) string {
	prefix := strings.Trim(strings.TrimSpace(e.cfg.ArtifactPrefix), "/")
	parts := []string{
		artifactPathSegment(record.ApplicationSlug),
		artifactPathSegment(record.ModuleID),
		artifactPathSegment(record.Environment),
		artifactPathSegment(recordBuildKeySegment(record)),
	}
	if prefix != "" {
		parts = append([]string{prefix}, parts...)
	}
	return strings.Join(parts, "/")
}

func recordBuildKeySegment(record domain.BuildRecord) string {
	id := strings.TrimSpace(record.ID)
	if id != "" {
		return id
	}
	if !record.CreatedAt.IsZero() {
		return record.CreatedAt.Format("20060102150405")
	}
	return id
}

func buildArtifactRoot(outputDir string) (string, error) {
	distDir := filepath.Join(outputDir, "dist")
	info, err := os.Stat(distDir)
	if err == nil {
		if info.IsDir() {
			return distDir, nil
		}
		return "", errors.New("docker build produced dist path that is not a directory")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect build dist output: %w", err)
	}
	return outputDir, nil
}

func artifactPathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", "\x00", "")
	value = strings.Trim(replacer.Replace(value), "/")
	if value == "" {
		return "unknown"
	}
	return value
}

func (e *Executor) workDir() string {
	workDir := strings.TrimSpace(e.cfg.WorkDir)
	if workDir == "" {
		return os.TempDir()
	}
	return workDir
}

func artifactURL(publicBaseURL string, artifactKey string) string {
	publicBaseURL = strings.TrimSpace(publicBaseURL)
	artifactKey = strings.TrimLeft(strings.TrimSpace(artifactKey), "/")
	if publicBaseURL == "" || artifactKey == "" {
		return ""
	}
	return strings.TrimRight(publicBaseURL, "/") + "/" + artifactKey
}

func artifactManifestKey(artifactKey string) string {
	return strings.TrimRight(strings.TrimSpace(artifactKey), "/") + "/manifest.json"
}

func contentType(path string) string {
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}
