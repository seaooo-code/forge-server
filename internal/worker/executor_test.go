package worker

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
		"--",
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
		"--",
		"https://github.com/mi-car/profile.git",
		"/tmp/repo",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestDockerBuildArgsBuildsTaggedImage(t *testing.T) {
	paths := dockerBuildPaths{repoDir: "/tmp/repo", dockerfile: "/tmp/repo/docker/rn.Dockerfile", context: "/tmp/repo/app"}
	got := dockerBuildArgs(paths, "forge-worker-build:build-1", []string{"--build-arg", "A=1"})
	want := []string{
		"build",
		"--progress",
		"plain",
		"--file",
		"/tmp/repo/docker/rn.Dockerfile",
		"--build-arg",
		"A=1",
		"--tag",
		"forge-worker-build:build-1",
		"/tmp/repo/app",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestDockerBuildOutputArgsExportsFinalImageFilesystem(t *testing.T) {
	paths := dockerBuildPaths{repoDir: "/tmp/repo", dockerfile: "/tmp/repo/Dockerfile", context: "/tmp/repo"}
	got := dockerBuildOutputArgs(paths, "/tmp/output", nil)
	want := []string{"build", "--progress", "plain", "--file", "/tmp/repo/Dockerfile", "--output", "type=local,dest=/tmp/output", "/tmp/repo"}
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

func TestArtifactKeyUsesStableIdentifiersAndBuildID(t *testing.T) {
	executor := Executor{cfg: config.WorkerConfig{ArtifactPrefix: "rn-bundles"}}
	got := executor.artifactKey(domain.BuildRecord{
		ID:              "274c3210-bf60-4c5a-b2bd-6893538c991b",
		ApplicationSlug: "seaooo",
		ApplicationName: "示例 应用",
		ModuleID:        "test-bundle",
		ModuleName:      "测试 模块",
		Environment:     "qa",
		EnvironmentName: "QA 环境",
	})
	// 显示名含中文和空格，路径只用 slug、模块 id、环境 code。
	want := "rn-bundles/seaooo/test-bundle/qa/274c3210-bf60-4c5a-b2bd-6893538c991b"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestArtifactKeyFallsBackToCreatedAtForEmptyBuildID(t *testing.T) {
	executor := Executor{cfg: config.WorkerConfig{ArtifactPrefix: "rn-bundles"}}
	got := executor.artifactKey(domain.BuildRecord{
		ApplicationSlug: "demo-app",
		ModuleID:        "common",
		Environment:     "qa",
		CreatedAt:       time.Date(2026, 6, 17, 13, 5, 9, 0, time.Local),
	})
	want := "rn-bundles/demo-app/common/qa/20260617130509"
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

func TestArtifactManifestFileIncludesNestedMetadata(t *testing.T) {
	artifactRoot := t.TempDir()
	nestedDir := filepath.Join(artifactRoot, "assets", "images", "icons")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}
	content := []byte("image")
	path := filepath.Join(nestedDir, "home.png")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	file, err := artifactManifestFile(artifactFile{
		key:  "assets/images/icons/home.png",
		path: path,
	}, "rn-bundles/app/module/qa/build-1/assets/images/icons/home.png", []string{"sha256", "md5"})
	if err != nil {
		t.Fatalf("artifact manifest file: %v", err)
	}

	if file.Path != "assets/images/icons/home.png" ||
		file.ObjectKey != "rn-bundles/app/module/qa/build-1/assets/images/icons/home.png" ||
		file.SizeBytes != int64(len(content)) ||
		file.ContentType != "image/png" ||
		file.SHA256 != fmt.Sprintf("%x", sha256.Sum256(content)) ||
		file.Digests["sha256"] != file.SHA256 ||
		file.Digests["md5"] != fmt.Sprintf("%x", md5.Sum(content)) {
		t.Fatalf("unexpected manifest file: %+v", file)
	}
}

func TestGenerateArtifactManifestScansArtifacts(t *testing.T) {
	artifactRoot := t.TempDir()
	bundle := []byte("bundle")
	if err := os.WriteFile(filepath.Join(artifactRoot, "index.bundle"), bundle, 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(artifactRoot, "assets"), 0o755); err != nil {
		t.Fatalf("create assets dir: %v", err)
	}
	image := []byte("image")
	if err := os.WriteFile(filepath.Join(artifactRoot, "assets", "home.png"), image, 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	manifest, err := generateArtifactManifest(artifactRoot, "rn-bundles/app/module/qa/build-1", []string{"sha256"})
	if err != nil {
		t.Fatalf("generate artifact manifest: %v", err)
	}

	if manifest.ArtifactKey != "rn-bundles/app/module/qa/build-1" ||
		manifest.FileCount != 2 ||
		manifest.TotalSizeBytes != int64(len(bundle)+len(image)) {
		t.Fatalf("unexpected manifest summary: %+v", manifest)
	}

	files := map[string]domain.ArtifactManifestFile{}
	for _, file := range manifest.Files {
		files[file.Path] = file
	}
	if files["index.bundle"].ObjectKey != "rn-bundles/app/module/qa/build-1/index.bundle" ||
		files["assets/home.png"].ObjectKey != "rn-bundles/app/module/qa/build-1/assets/home.png" {
		t.Fatalf("unexpected manifest files: %+v", manifest.Files)
	}
}

func TestArtifactLocalPathRejectsTraversal(t *testing.T) {
	if _, err := artifactLocalPath(t.TempDir(), "../secret"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestNewArtifactManifestAndManifestKey(t *testing.T) {
	manifest := newArtifactManifest("rn-bundles/app/module/qa/build-1")
	if manifest.Version != 1 || manifest.ArtifactKey != "rn-bundles/app/module/qa/build-1" || len(manifest.Files) != 0 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if got := artifactManifestKey("rn-bundles/app/module/qa/build-1/"); got != "rn-bundles/app/module/qa/build-1/manifest.json" {
		t.Fatalf("unexpected manifest key %q", got)
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
