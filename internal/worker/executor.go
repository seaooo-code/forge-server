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
)

// RepositoryCredentialsProvider 通过代码源 provider 获取 clone 凭证，由 service.CodeSourceService 实现。
type RepositoryCredentialsProvider interface {
	CloneCredentials(ctx context.Context, repository domain.ModuleRepository) (domain.GitCredentials, error)
}

// BuildVariableResolver 解析模块在某个环境下生效的构建变量（已解密、已按环境覆盖），由 service.BuildVariableService 实现。
type BuildVariableResolver interface {
	ResolveBuildVariables(ctx context.Context, moduleID string, environmentCode string) ([]domain.ModuleBuildVariable, error)
}

const dockerBuildArtifactDir = "/app/dist"

type Executor struct {
	cfg         config.WorkerConfig
	credentials RepositoryCredentialsProvider
	variables   BuildVariableResolver
	logs        BuildLogStore
	logger      *slog.Logger
	// runner 为空时使用 execCommand，测试可以替换成假实现。
	runner commandRunner
	// logFlushInterval 为 0 时使用 buildLogFlushInterval。
	logFlushInterval time.Duration
}

type ExecutorDependencies struct {
	Credentials RepositoryCredentialsProvider
	Logs        BuildLogStore
	Variables   BuildVariableResolver
}

type buildWorkspace struct {
	outputDir string
	repoDir   string
	rootDir   string
}

func NewExecutor(cfg config.WorkerConfig, deps ExecutorDependencies, logger *slog.Logger) *Executor {
	return &Executor{
		cfg:         cfg,
		credentials: deps.Credentials,
		variables:   deps.Variables,
		logs:        deps.Logs,
		logger:      logger,
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
	logs.Linef("开始构建 %s：模块 %s，环境 %s，分支 %s", record.ID, record.ModuleID, record.Environment, record.Branch)

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

	commitSHA, err := e.checkoutRepository(ctx, record, credentials, workspace.repoDir, logs)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	logs.Linef("已检出 commit %s", commitSHA)

	if err := verifyDockerBuildPaths(workspace.repoDir, paths); err != nil {
		return domain.BuildRecordCompletion{}, err
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

func (e *Executor) artifactKey(record domain.BuildRecord) string {
	prefix := strings.Trim(strings.TrimSpace(e.cfg.ArtifactPrefix), "/")
	environmentName := strings.TrimSpace(record.EnvironmentName)
	if environmentName == "" {
		environmentName = record.Environment
	}
	parts := []string{
		artifactPathSegment(record.ApplicationName),
		artifactPathSegment(record.ModuleName),
		artifactPathSegment(environmentName),
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
