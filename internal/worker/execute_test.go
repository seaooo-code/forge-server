package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

type fakeCredentialsProvider struct{ credentials domain.GitCredentials }

func (p fakeCredentialsProvider) CloneCredentials(context.Context, domain.ModuleRepository) (domain.GitCredentials, error) {
	return p.credentials, nil
}

type fakeVariableResolver struct{ variables []domain.ModuleBuildVariable }

func (r fakeVariableResolver) ResolveBuildVariables(context.Context, string, string) ([]domain.ModuleBuildVariable, error) {
	return r.variables, nil
}

// executeTestRunner 模拟 git 和 docker：clone 时写入 Dockerfile，docker build 时把密钥打印到输出，再按 dockerBuild 决定结果。
type executeTestRunner struct {
	dockerBuild func(cmd command) error
	dockerCalls []command
}

func (r *executeTestRunner) run(_ context.Context, cmd command) (string, error) {
	if cmd.name == "git" {
		switch cmd.args[0] {
		case "clone":
			repoDir := cmd.args[len(cmd.args)-1]
			if err := os.MkdirAll(repoDir, 0o755); err != nil {
				return "", err
			}
			return "", os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte("FROM node AS bundle\nFROM scratch\nCOPY --from=bundle /app/dist/ /\n"), 0o600)
		case "rev-parse":
			return testCommitSHA, nil
		}
		return "", nil
	}
	r.dockerCalls = append(r.dockerCalls, cmd)
	for _, value := range cmd.env {
		if secret, ok := strings.CutPrefix(value, dockerSecretEnvPrefix+"NPM_TOKEN="); ok {
			_, _ = fmt.Fprintf(cmd.log, "npm notice using token %s\n", secret)
		}
	}
	return "", r.dockerBuild(cmd)
}

func newExecuteTestExecutor(t *testing.T, runner *executeTestRunner, store BuildLogStore) *Executor {
	t.Helper()
	executor := NewExecutor(config.WorkerConfig{WorkDir: t.TempDir(), LogMaxBytes: 1 << 20, ArtifactDigests: []string{"sha256"}}, ExecutorDependencies{
		Credentials: fakeCredentialsProvider{credentials: domain.GitCredentials{Username: "oauth2", Password: "glpat-repo-secret"}},
		Logs:        store,
		Variables: fakeVariableResolver{variables: []domain.ModuleBuildVariable{
			{Key: "NPM_TOKEN", Value: "npm-secret-value", IsSecret: true},
			{Key: "API_HOST", Value: "qa.example.com"},
		}},
	}, nil)
	executor.runner = runner.run
	return executor
}

func executeTestRecord() domain.BuildRecord {
	return domain.BuildRecord{
		ID:          "274c3210-bf60-4c5a-b2bd-6893538c991b",
		ModuleID:    "module-1",
		Environment: "qa",
		Branch:      "main",
		Repository:  domain.ModuleRepository{ConnectionID: "conn-1", CloneURL: "https://gitlab.example.com/rn/app.git"},
	}
}

func TestExecuteWritesRedactedLogAndShortErrorOnDockerFailure(t *testing.T) {
	store := &fakeBuildLogStore{}
	runner := &executeTestRunner{dockerBuild: func(command) error {
		return errors.New("docker build 执行失败（exit status 1），完整输出见构建日志")
	}}

	_, err := newExecuteTestExecutor(t, runner, store).Execute(context.Background(), executeTestRecord())
	if err == nil {
		t.Fatal("expected build failure")
	}
	if strings.Contains(err.Error(), "npm notice") {
		t.Fatalf("expected short error message, got %v", err)
	}

	logs := store.content()
	if strings.Contains(logs, "npm-secret-value") || strings.Contains(logs, "glpat-repo-secret") {
		t.Fatalf("secret leaked into build log: %q", logs)
	}
	if !strings.Contains(logs, "npm notice using token [redacted]") || !strings.Contains(logs, "[forge] 构建失败") || !strings.Contains(logs, "$ git clone") {
		t.Fatalf("unexpected build log: %q", logs)
	}

	build := runner.dockerCalls[0]
	if !slices.Contains(build.env, "DOCKER_BUILDKIT=1") || strings.Contains(strings.Join(build.args, " "), "npm-secret-value") {
		t.Fatalf("unexpected docker build invocation: %#v %#v", build.args, build.env)
	}
	if !slices.Contains(build.args, "--output") || !slices.Contains(build.args, "FORGE_COMMIT_SHA="+testCommitSHA) {
		t.Fatalf("expected scratch output build with commit arg, got %#v", build.args)
	}
}

func TestExecuteFailsWhenForgeManifestInvalid(t *testing.T) {
	store := &fakeBuildLogStore{}
	runner := &executeTestRunner{dockerBuild: func(cmd command) error {
		outputDir := strings.TrimPrefix(cmd.args[slices.Index(cmd.args, "--output")+1], "type=local,dest=")
		if err := os.MkdirAll(filepath.Join(outputDir, "ios"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outputDir, "ios", "main.jsbundle"), []byte("x"), 0o600); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(outputDir, "forge.json"), []byte(`{"runtimeVersion":"1","extra":true}`), 0o600)
	}}

	_, err := newExecuteTestExecutor(t, runner, store).Execute(context.Background(), executeTestRecord())
	if err == nil || !strings.Contains(err.Error(), `未知字段 "extra"`) {
		t.Fatalf("expected forge.json validation error, got %v", err)
	}
	if !strings.Contains(store.content(), `[forge] 构建失败：产物校验失败`) {
		t.Fatalf("expected validation reason in log, got %q", store.content())
	}
}
