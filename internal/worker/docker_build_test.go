package worker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

func TestResolveDockerBuildPathsUsesModuleConfig(t *testing.T) {
	got, err := resolveDockerBuildPaths("/work/repo", "apps/rn/Dockerfile", "apps/rn", "Dockerfile.global")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.dockerfile != "/work/repo/apps/rn/Dockerfile" || got.context != "/work/repo/apps/rn" {
		t.Fatalf("unexpected paths: %+v", got)
	}
}

func TestResolveDockerBuildPathsFallsBackToGlobalDockerfile(t *testing.T) {
	got, err := resolveDockerBuildPaths("/work/repo", " ", "", "docker/rn.Dockerfile")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.dockerfile != "/work/repo/docker/rn.Dockerfile" || got.context != "/work/repo" {
		t.Fatalf("unexpected paths: %+v", got)
	}

	got, err = resolveDockerBuildPaths("/work/repo", "", "", "")
	if err != nil || got.dockerfile != "/work/repo/Dockerfile" {
		t.Fatalf("expected default Dockerfile, got %+v %v", got, err)
	}
}

func TestResolveDockerBuildPathsRejectsPathsOutsideRepo(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		context    string
	}{
		{name: "absolute dockerfile", dockerfile: "/etc/Dockerfile", context: "."},
		{name: "parent dockerfile", dockerfile: "../Dockerfile", context: "."},
		{name: "nested parent dockerfile", dockerfile: "apps/../../Dockerfile", context: "."},
		{name: "absolute context", dockerfile: "Dockerfile", context: "/"},
		{name: "parent context", dockerfile: "Dockerfile", context: ".."},
		{name: "backslash", dockerfile: `apps\Dockerfile`, context: "."},
		{name: "repo root as dockerfile", dockerfile: ".", context: "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := resolveDockerBuildPaths("/work/repo", tt.dockerfile, tt.context, ""); err == nil {
				t.Fatalf("expected rejection, got %+v", got)
			}
		})
	}
}

func TestVerifyDockerBuildPathsRejectsSymlinkOutsideRepo(t *testing.T) {
	repoDir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(repoDir, "ctx")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	paths, err := resolveDockerBuildPaths(repoDir, "Dockerfile", ".", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := verifyDockerBuildPaths(repoDir, paths); err != nil {
		t.Fatalf("expected repo paths to pass, got %v", err)
	}

	paths, _ = resolveDockerBuildPaths(repoDir, "Dockerfile", "ctx", "")
	if err := verifyDockerBuildPaths(repoDir, paths); err == nil || !strings.Contains(err.Error(), "仓库外部") {
		t.Fatalf("expected outside symlink rejection, got %v", err)
	}

	paths, _ = resolveDockerBuildPaths(repoDir, "missing/Dockerfile", ".", "")
	if err := verifyDockerBuildPaths(repoDir, paths); err == nil {
		t.Fatal("expected missing Dockerfile error")
	}
}

func TestDockerBuildVariableOptionsKeepsSecretsOutOfArgs(t *testing.T) {
	record := domain.BuildRecord{ID: "build-1", ModuleID: "module-1", Environment: "qa", Branch: "feature/login"}
	options, env := dockerBuildVariableOptions(record, testCommitSHA, []domain.ModuleBuildVariable{
		{Key: "NPM_TOKEN", Value: "npm-secret-value", IsSecret: true},
		{Key: "API_HOST", Value: "qa.example.com"},
		{Key: "FORGE_BRANCH", Value: "spoofed"},
	})

	want := []string{
		"--build-arg", "API_HOST=qa.example.com",
		"--secret", "id=NPM_TOKEN,env=FORGE_BUILD_SECRET_NPM_TOKEN",
		"--build-arg", "FORGE_ENVIRONMENT=qa",
		"--build-arg", "FORGE_BRANCH=feature/login",
		"--build-arg", "FORGE_COMMIT_SHA=" + testCommitSHA,
		"--build-arg", "FORGE_BUILD_ID=build-1",
		"--build-arg", "FORGE_MODULE_ID=module-1",
		"--build-arg", "FORGE_DEPS_DIR=.forge/deps",
	}
	if !slices.Equal(options, want) {
		t.Fatalf("expected %#v, got %#v", want, options)
	}
	if strings.Contains(strings.Join(options, " "), "npm-secret-value") || strings.Contains(strings.Join(options, " "), "spoofed") {
		t.Fatalf("secret or reserved override leaked into args: %#v", options)
	}
	if !slices.Contains(env, "DOCKER_BUILDKIT=1") || !slices.Contains(env, "FORGE_BUILD_SECRET_NPM_TOKEN=npm-secret-value") {
		t.Fatalf("unexpected env: %#v", env)
	}
}
