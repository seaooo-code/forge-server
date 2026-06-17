package worker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestGitCloneArgsUsesBuildRecordBranch(t *testing.T) {
	got := gitCloneArgs(" release/1.2.0 ", "https://github.com/mi-car/profile.git", "/tmp/repo")
	want := []string{
		"clone",
		"--depth",
		"1",
		"--branch",
		"release/1.2.0",
		"https://github.com/mi-car/profile.git",
		"/tmp/repo",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestGitCloneArgsUsesRemoteDefaultWhenBranchEmpty(t *testing.T) {
	got := gitCloneArgs(" ", "https://github.com/mi-car/profile.git", "/tmp/repo")
	want := []string{
		"clone",
		"--depth",
		"1",
		"https://github.com/mi-car/profile.git",
		"/tmp/repo",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestDockerBuildArgsBuildsTaggedImage(t *testing.T) {
	got := dockerBuildArgs("docker/rn.Dockerfile", "forge-worker-build:build-1")
	want := []string{
		"build",
		"--file",
		"docker/rn.Dockerfile",
		"--tag",
		"forge-worker-build:build-1",
		".",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestDockerBuildOutputArgsExportsFinalImageFilesystem(t *testing.T) {
	got := dockerBuildOutputArgs("Dockerfile", "/tmp/output")
	want := []string{"build", "--file", "Dockerfile", "--output", "type=local,dest=/tmp/output", "."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestDockerCreateArgsOverridesEntrypoint(t *testing.T) {
	got := dockerCreateArgs("forge-worker-build:build-1")
	want := []string{"create", "--entrypoint", "true", "forge-worker-build:build-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestDockerCopyArgsCopiesAppDist(t *testing.T) {
	got := dockerCopyArgs("container-1", "/app/dist/.", "/tmp/output")
	want := []string{"cp", "container-1:/app/dist/.", "/tmp/output"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestDockerArtifactOutputPlanDefaultsToAppDistCopy(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte(`
FROM node:22-bookworm-slim AS bundle
WORKDIR /app
COPY . .
RUN npm run build
`), 0o600); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	got := dockerArtifactOutputPlan(repoDir, "Dockerfile")
	if got.useBuildOutput || got.copySource != "/app/dist/." {
		t.Fatalf("expected app dist copy plan, got %#v", got)
	}
}

func TestDockerArtifactOutputPlanSupportsScratchRootArtifact(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte(`
FROM node:22.13.0-bookworm-slim AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM deps AS bundle
ARG PLATFORM=android
ENV NODE_ENV=production
WORKDIR /app
COPY . .
RUN npm run bundle:${PLATFORM}

FROM scratch AS bundle-output
COPY --from=bundle /app/dist/ /
`), 0o600); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	got := dockerArtifactOutputPlan(repoDir, "Dockerfile")
	if !got.useBuildOutput || got.copySource != "" {
		t.Fatalf("expected build output plan, got %#v", got)
	}
}

func TestDockerArtifactOutputPlanIgnoresNonScratchRootCopy(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte(`
FROM node:22.13.0-bookworm-slim AS bundle
WORKDIR /app
COPY . .
RUN npm run build
COPY --from=bundle /app/dist/ /
`), 0o600); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	got := dockerArtifactOutputPlan(repoDir, "Dockerfile")
	if got.useBuildOutput || got.copySource != "/app/dist/." {
		t.Fatalf("expected app dist copy plan, got %#v", got)
	}
}

func TestDockerImageTagNormalizesBuildID(t *testing.T) {
	got := dockerImageTag(" Build/1 渠道 ")
	want := "forge-worker-build:build-1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestArtifactKeyUsesNamesAndBuildTimestamp(t *testing.T) {
	executor := Executor{cfg: config.WorkerConfig{ArtifactPrefix: "rn-bundles"}}
	got := executor.artifactKey(domain.BuildRecord{
		ID:              "20260617130509",
		ApplicationName: "seaooo",
		ModuleName:      "test-bundle",
		Environment:     "qa",
		EnvironmentName: "QA",
	})
	want := "rn-bundles/seaooo/test-bundle/QA/20260617130509"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestArtifactKeyFallsBackToCreatedAtForLegacyBuildID(t *testing.T) {
	executor := Executor{cfg: config.WorkerConfig{ArtifactPrefix: "rn-bundles"}}
	got := executor.artifactKey(domain.BuildRecord{
		ID:              "build-2bf49b9b9d32b57e",
		ApplicationName: "小米汽车",
		ModuleName:      "测试模块-通用",
		Environment:     "qa",
		EnvironmentName: "QA",
		CreatedAt:       time.Date(2026, 6, 17, 13, 5, 9, 0, time.Local),
	})
	want := "rn-bundles/小米汽车/测试模块-通用/QA/20260617130509"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBuildArtifactRootUsesDistDirectoryWhenPresent(t *testing.T) {
	outputDir := t.TempDir()
	distDir := filepath.Join(outputDir, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatalf("create dist dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "assets"), 0o755); err != nil {
		t.Fatalf("create assets dir: %v", err)
	}

	got, err := buildArtifactRoot(outputDir)
	if err != nil {
		t.Fatalf("build artifact root: %v", err)
	}
	if got != distDir {
		t.Fatalf("expected %q, got %q", distDir, got)
	}
}

func TestArtifactFilesIncludesDistRootFilesAndDirectories(t *testing.T) {
	artifactRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(artifactRoot, "index.bundle"), []byte("bundle"), 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(artifactRoot, "assets"), 0o755); err != nil {
		t.Fatalf("create assets dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactRoot, "assets", "image.png"), []byte("image"), 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	files, err := artifactFiles(artifactRoot)
	if err != nil {
		t.Fatalf("artifact files: %v", err)
	}
	got := make(map[string]bool, len(files))
	for _, file := range files {
		got[file.key] = true
	}
	if len(files) != 2 || !got["index.bundle"] || !got["assets/image.png"] {
		t.Fatalf("expected root and nested files, got %#v", files)
	}
}

func TestArtifactURLJoinsPublicBaseURLAndKey(t *testing.T) {
	got := artifactURL("https://cdn.example.com/assets/", "/rn-bundles/module-profile/qa/build-1")
	want := "https://cdn.example.com/assets/rn-bundles/module-profile/qa/build-1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestContentTypeDefaultsToOctetStream(t *testing.T) {
	if got := contentType("bundle.unknownext"); got != "application/octet-stream" {
		t.Fatalf("expected octet stream, got %q", got)
	}
}

func TestWriteGitAskPassUsesEnvironmentCredentials(t *testing.T) {
	path, err := writeGitAskPass(t.TempDir())
	if err != nil {
		t.Fatalf("write git askpass: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat git askpass: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("expected executable askpass permissions, got %v", info.Mode().Perm())
	}
}

func TestRunCommandRedactsSecretsFromErrorOutput(t *testing.T) {
	err := runCommand(
		context.Background(),
		"",
		nil,
		[]string{"token-value"},
		"sh",
		"-c",
		"printf '%s' 'failed with token-value'; exit 1",
	)
	if err == nil {
		t.Fatal("expected command error")
	}
	if strings.Contains(err.Error(), "token-value") {
		t.Fatalf("expected token to be redacted, got %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("expected redacted marker, got %v", err)
	}
}
