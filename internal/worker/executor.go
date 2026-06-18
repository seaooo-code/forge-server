package worker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
	s3infra "forge-server/internal/infra/s3"
)

type CodeSourceConfigStore interface {
	GetCodeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) (domain.CodeSourceAppConfig, error)
}

type GitHubInstallationTokenClient interface {
	InstallationAccessToken(ctx context.Context, appConfig domain.CodeSourceAppConfig, installationID int64) (string, error)
}

const dockerBuildArtifactDir = "/app/dist"

type Executor struct {
	cfg                  config.WorkerConfig
	codeSourceStore      CodeSourceConfigStore
	fallbackGitHubConfig domain.CodeSourceAppConfig
	githubClient         GitHubInstallationTokenClient
	logger               *slog.Logger
}

func NewExecutor(cfg config.WorkerConfig, codeSourceStore CodeSourceConfigStore, githubClient GitHubInstallationTokenClient, fallbackGitHubConfig domain.CodeSourceAppConfig, logger *slog.Logger) *Executor {
	return &Executor{
		cfg:                  cfg,
		codeSourceStore:      codeSourceStore,
		fallbackGitHubConfig: fallbackGitHubConfig,
		githubClient:         githubClient,
		logger:               logger,
	}
}

func (e *Executor) Execute(ctx context.Context, record domain.BuildRecord) (domain.BuildRecordCompletion, error) {
	if record.RepositoryProvider != domain.CodeSourceProviderGitHub {
		return domain.BuildRecordCompletion{}, domain.ErrInvalidBuildInput
	}
	if e.codeSourceStore == nil || e.githubClient == nil {
		return domain.BuildRecordCompletion{}, domain.ErrCodeSourceAuthNotConfigured
	}

	appConfig, err := e.codeSourceAppConfig(ctx, record.RepositoryProvider)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}

	token, err := e.githubClient.InstallationAccessToken(ctx, appConfig, record.RepositoryInstallationID)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}

	baseWorkDir := e.workDir()
	if err := os.MkdirAll(baseWorkDir, 0o755); err != nil {
		return domain.BuildRecordCompletion{}, fmt.Errorf("create worker base dir: %w", err)
	}

	workDir, err := os.MkdirTemp(baseWorkDir, record.ID+"-*")
	if err != nil {
		return domain.BuildRecordCompletion{}, fmt.Errorf("create build work dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(workDir); err != nil && e.logger != nil {
			e.logger.Warn("remove build work dir failed", "build", record.ID, "path", workDir, "error", err)
		}
	}()

	repoDir := filepath.Join(workDir, "repo")
	outputDir := filepath.Join(workDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return domain.BuildRecordCompletion{}, fmt.Errorf("create build output dir: %w", err)
	}

	if err := e.cloneRepository(ctx, record, token, repoDir); err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	if err := e.buildDockerOutput(ctx, record.ID+"-"+filepath.Base(workDir), repoDir, outputDir); err != nil {
		return domain.BuildRecordCompletion{}, err
	}

	artifactKey := e.artifactKey(record)
	artifactRoot, err := buildArtifactRoot(outputDir)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	manifest, err := e.uploadOutput(ctx, record, artifactRoot, artifactKey)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}

	return domain.BuildRecordCompletion{
		ArtifactKey:    artifactKey,
		ArtifactURL:    artifactURL(record.StoragePublicBaseURL, artifactKey),
		ManifestKey:    artifactManifestKey(artifactKey),
		FileCount:      manifest.FileCount,
		TotalSizeBytes: manifest.TotalSizeBytes,
	}, nil
}

func (e *Executor) cloneRepository(ctx context.Context, record domain.BuildRecord, token string, repoDir string) error {
	cloneURL := strings.TrimSpace(record.RepositoryCloneURL)
	if cloneURL == "" {
		return domain.ErrInvalidBuildInput
	}

	askpassPath, err := writeGitAskPass(filepath.Dir(repoDir))
	if err != nil {
		return err
	}

	return runCommand(ctx, "", []string{
		"GIT_ASKPASS=" + askpassPath,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_USERNAME=x-access-token",
		"GIT_PASSWORD=" + token,
	}, []string{token}, "git", gitCloneArgs(record.Branch, cloneURL, repoDir)...)
}

func gitCloneArgs(branch string, cloneURL string, repoDir string) []string {
	args := []string{"clone", "--depth", "1"}
	if strings.TrimSpace(branch) != "" {
		args = append(args, "--branch", strings.TrimSpace(branch))
	}
	return append(args, cloneURL, repoDir)
}

func (e *Executor) buildDockerOutput(ctx context.Context, buildID string, repoDir string, outputDir string) error {
	dockerfile := strings.TrimSpace(e.cfg.Dockerfile)
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	outputPlan := dockerArtifactOutputPlan(repoDir, dockerfile)
	if outputPlan.useBuildOutput {
		return runCommand(ctx, repoDir, nil, nil, "docker", dockerBuildOutputArgs(dockerfile, outputDir)...)
	}

	imageTag := dockerImageTag(buildID)
	if err := runCommand(ctx, repoDir, nil, nil, "docker", dockerBuildArgs(dockerfile, imageTag)...); err != nil {
		return err
	}
	defer e.cleanupDockerImage(imageTag)

	containerID, err := runCommandOutput(ctx, "", nil, nil, "docker", dockerCreateArgs(imageTag)...)
	if err != nil {
		return err
	}
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return errors.New("docker create returned empty container id")
	}
	defer e.cleanupDockerContainer(containerID)

	return runCommand(ctx, "", nil, nil, "docker", dockerCopyArgs(containerID, outputPlan.copySource, outputDir)...)
}

func dockerBuildArgs(dockerfile string, imageTag string) []string {
	return []string{"build", "--file", dockerfile, "--tag", imageTag, "."}
}

func dockerBuildOutputArgs(dockerfile string, outputDir string) []string {
	return []string{"build", "--file", dockerfile, "--output", "type=local,dest=" + outputDir, "."}
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
	if err := runCommand(context.Background(), "", nil, nil, "docker", "rm", "-f", containerID); err != nil && e.logger != nil {
		e.logger.Warn("remove build container failed", "container", containerID, "error", err)
	}
}

func (e *Executor) cleanupDockerImage(imageTag string) {
	if err := runCommand(context.Background(), "", nil, nil, "docker", "image", "rm", "-f", imageTag); err != nil && e.logger != nil {
		e.logger.Warn("remove build image failed", "image", imageTag, "error", err)
	}
}

func (e *Executor) uploadOutput(ctx context.Context, record domain.BuildRecord, artifactRoot string, artifactKey string) (domain.ArtifactManifest, error) {
	client, err := s3infra.NewClient(s3infra.Options{
		Region:          record.StorageRegion,
		Bucket:          record.StorageBucket,
		Endpoint:        record.StorageEndpoint,
		AccessKeyID:     record.StorageAccessKeyID,
		SecretAccessKey: record.StorageSecretAccessKey,
	})
	if err != nil {
		return domain.ArtifactManifest{}, err
	}

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
		manifestFile, err := artifactManifestFile(artifact, objectKey)
		if err != nil {
			return domain.ArtifactManifest{}, err
		}

		file, err := os.Open(artifact.path)
		if err != nil {
			return domain.ArtifactManifest{}, err
		}
		if err := client.PutObject(ctx, objectKey, file, manifestFile.ContentType); err != nil {
			_ = file.Close()
			return domain.ArtifactManifest{}, err
		}
		if err := file.Close(); err != nil {
			return domain.ArtifactManifest{}, err
		}

		manifest.Files = append(manifest.Files, manifestFile)
		manifest.FileCount++
		manifest.TotalSizeBytes += manifestFile.SizeBytes
	}
	manifestPayload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return domain.ArtifactManifest{}, fmt.Errorf("encode artifact manifest: %w", err)
	}
	if err := client.PutObject(ctx, artifactManifestKey(artifactKey), strings.NewReader(string(manifestPayload)), "application/json"); err != nil {
		return domain.ArtifactManifest{}, err
	}
	return manifest, nil
}

func newArtifactManifest(artifactKey string) domain.ArtifactManifest {
	return domain.ArtifactManifest{
		Version:     1,
		ArtifactKey: artifactKey,
		GeneratedAt: time.Now().UTC(),
		Files:       []domain.ArtifactManifestFile{},
	}
}

func artifactManifestFile(artifact artifactFile, objectKey string) (domain.ArtifactManifestFile, error) {
	file, err := os.Open(artifact.path)
	if err != nil {
		return domain.ArtifactManifestFile{}, err
	}
	defer file.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return domain.ArtifactManifestFile{}, err
	}

	return domain.ArtifactManifestFile{
		Path:        artifact.key,
		ObjectKey:   objectKey,
		SizeBytes:   size,
		ContentType: contentType(artifact.path),
		SHA256:      fmt.Sprintf("%x", hash.Sum(nil)),
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

func (e *Executor) codeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) (domain.CodeSourceAppConfig, error) {
	config, err := e.codeSourceStore.GetCodeSourceAppConfig(ctx, provider)
	if err == nil {
		return config, nil
	}
	if errors.Is(err, domain.ErrCodeSourceAuthNotConfigured) &&
		provider == domain.CodeSourceProviderGitHub &&
		isUsableGitHubAppConfig(e.fallbackGitHubConfig) {
		return e.fallbackGitHubConfig, nil
	}
	return domain.CodeSourceAppConfig{}, err
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

func runCommand(ctx context.Context, dir string, env []string, secrets []string, name string, args ...string) error {
	_, err := runCommandOutput(ctx, dir, env, secrets, name, args...)
	return err
}

func runCommandOutput(ctx context.Context, dir string, env []string, secrets []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return "", fmt.Errorf("run %s: %w", name, err)
		}
		for _, secret := range secrets {
			if strings.TrimSpace(secret) != "" {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
		}
		return "", fmt.Errorf("run %s: %w: %s", name, err, message)
	}
	return strings.TrimSpace(string(output)), nil
}

func writeGitAskPass(dir string) (string, error) {
	path := filepath.Join(dir, "git-askpass.sh")
	script := `#!/bin/sh
case "$1" in
*Username*) printf '%s\n' "$GIT_USERNAME" ;;
*) printf '%s\n' "$GIT_PASSWORD" ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("write git askpass helper: %w", err)
	}
	return path, nil
}

func isUsableGitHubAppConfig(config domain.CodeSourceAppConfig) bool {
	return strings.TrimSpace(config.AppID) != "" &&
		strings.TrimSpace(config.AppSlug) != "" &&
		strings.TrimSpace(config.PrivateKey) != ""
}
