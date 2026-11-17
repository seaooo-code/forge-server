package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

type fakeBuildAdapterProvider struct {
	adapter domain.BuildAdapter
	usages  []domain.BuildAdapterUsage
}

func (p *fakeBuildAdapterProvider) GetBuildAdapter(_ context.Context, id string) (domain.BuildAdapter, error) {
	if id != p.adapter.ID {
		return domain.BuildAdapter{}, domain.ErrBuildAdapterNotFound
	}
	return p.adapter, nil
}

func (p *fakeBuildAdapterProvider) RecordBuildAdapterUsage(_ context.Context, _ string, usage domain.BuildAdapterUsage) error {
	p.usages = append(p.usages, usage)
	return nil
}

func testWorkerBuildAdapter() domain.BuildAdapter {
	return domain.BuildAdapter{
		ID:      "adapter-1",
		Name:    "Node bundle",
		Version: 5,
		Status:  domain.BuildAdapterStatusActive,
		DockerfileTemplate: "FROM node:20 AS bundle\n" +
			"WORKDIR /app\n" +
			"COPY . .\n" +
			"RUN node .forge/build.js {{ quote .Params.entry }} {{ .Build.Number }}\n" +
			"FROM scratch\n" +
			"COPY --from=bundle /app/dist/ /\n",
		Files:      map[string]string{"build.js": "// {{ .Module.ID }}@{{ .CommitSHA }}\n", "conf/env.txt": "{{ .Environment }}"},
		Parameters: []domain.BuildAdapterParameter{{Key: "entry", Required: true}},
	}
}

func adapterExecuteTestRecord() domain.BuildRecord {
	record := executeTestRecord()
	record.Number = 12
	record.BuildMode = domain.BuildModeAdapter
	record.ModuleBuildAdapterID = "adapter-1"
	record.ModuleBuildAdapterParams = map[string]string{"entry": "src/index.js"}
	return record
}

func TestExecuteAdapterModeRendersDockerfileOutsideRepo(t *testing.T) {
	store := &fakeBuildLogStore{}
	provider := &fakeBuildAdapterProvider{adapter: testWorkerBuildAdapter()}
	var renderedDockerfile, forgeFile string
	runner := &executeTestRunner{dockerBuild: func(cmd command) error {
		dockerfile := cmd.args[slices.Index(cmd.args, "--file")+1]
		content, err := os.ReadFile(dockerfile)
		if err != nil {
			return err
		}
		renderedDockerfile = string(content)
		contextDir := cmd.args[len(cmd.args)-1]
		data, err := os.ReadFile(filepath.Join(contextDir, ".forge", "build.js"))
		if err != nil {
			return err
		}
		forgeFile = string(data)
		if _, err := os.Stat(filepath.Join(contextDir, ".forge", "conf", "env.txt")); err != nil {
			return err
		}
		return errors.New("stop after docker build")
	}}
	executor := newExecuteTestExecutor(t, runner, store)
	executor.adapters = provider

	_, err := executor.Execute(context.Background(), adapterExecuteTestRecord())
	if err == nil || !strings.Contains(err.Error(), "stop after docker build") {
		t.Fatalf("expected fake docker failure, got %v", err)
	}

	build := runner.dockerCalls[0]
	dockerfile := build.args[slices.Index(build.args, "--file")+1]
	contextDir := build.args[len(build.args)-1]
	if filepath.Base(dockerfile) != renderedDockerfileName || strings.HasPrefix(dockerfile, contextDir+string(filepath.Separator)) {
		t.Fatalf("expected rendered Dockerfile outside repo, got %s (context %s)", dockerfile, contextDir)
	}
	// scratch 导出判断作用于渲染后的 Dockerfile：使用 --output 而不是 docker create/cp。
	if !slices.Contains(build.args, "--output") || !slices.Contains(build.args, "FORGE_BUILD_ID="+adapterExecuteTestRecord().ID) {
		t.Fatalf("unexpected docker build args: %#v", build.args)
	}
	if !strings.Contains(renderedDockerfile, `RUN node .forge/build.js "src/index.js" 12`) || strings.Contains(renderedDockerfile, "npm-secret-value") {
		t.Fatalf("unexpected rendered Dockerfile: %q", renderedDockerfile)
	}
	if forgeFile != "// module-1@"+testCommitSHA+"\n" {
		t.Fatalf("unexpected rendered helper file: %q", forgeFile)
	}
	if len(provider.usages) != 1 || provider.usages[0].AdapterVersion != 5 || len(provider.usages[0].RenderedDockerfileSHA256) != 64 {
		t.Fatalf("unexpected recorded usage: %#v", provider.usages)
	}
	logs := store.content()
	if !strings.Contains(logs, "构建适配器：Node bundle（adapter-1），版本 5") || !strings.Contains(logs, "sha256 "+provider.usages[0].RenderedDockerfileSHA256) {
		t.Fatalf("expected adapter info in log, got %q", logs)
	}
	if strings.Contains(logs, "npm-secret-value") {
		t.Fatalf("secret leaked into build log: %q", logs)
	}
}

func TestExecuteAdapterModeFailsWhenAdapterDisabled(t *testing.T) {
	store := &fakeBuildLogStore{}
	adapter := testWorkerBuildAdapter()
	adapter.Status = domain.BuildAdapterStatusDisabled
	runner := &executeTestRunner{dockerBuild: func(command) error { return nil }}
	executor := newExecuteTestExecutor(t, runner, store)
	executor.adapters = &fakeBuildAdapterProvider{adapter: adapter}

	_, err := executor.Execute(context.Background(), adapterExecuteTestRecord())
	if err == nil || !strings.Contains(err.Error(), "已停用") {
		t.Fatalf("expected disabled adapter failure, got %v", err)
	}
	if len(runner.dockerCalls) != 0 {
		t.Fatalf("expected no docker build, got %d calls", len(runner.dockerCalls))
	}
}

func TestExecuteAdapterModeFailsOnRenderError(t *testing.T) {
	store := &fakeBuildLogStore{}
	adapter := testWorkerBuildAdapter()
	adapter.DockerfileTemplate = "FROM node\nRUN {{ .Params.missing }}\n"
	runner := &executeTestRunner{dockerBuild: func(command) error { return nil }}
	executor := newExecuteTestExecutor(t, runner, store)
	executor.adapters = &fakeBuildAdapterProvider{adapter: adapter}

	_, err := executor.Execute(context.Background(), adapterExecuteTestRecord())
	if err == nil || !strings.Contains(err.Error(), "Dockerfile 第 2 行") {
		t.Fatalf("expected render error with line, got %v", err)
	}
	if len(runner.dockerCalls) != 0 {
		t.Fatal("expected no docker build")
	}
}

func TestEnsureForgeDirAvailableRejectsExistingForgeDir(t *testing.T) {
	for name, prepare := range map[string]func(dir string) error{
		"directory": func(dir string) error { return os.Mkdir(filepath.Join(dir, ".forge"), 0o755) },
		"file":      func(dir string) error { return os.WriteFile(filepath.Join(dir, ".forge"), []byte("x"), 0o600) },
		"symlink":   func(dir string) error { return os.Symlink(t.TempDir(), filepath.Join(dir, ".forge")) },
	} {
		t.Run(name, func(t *testing.T) {
			contextDir := t.TempDir()
			if err := prepare(contextDir); err != nil {
				t.Fatal(err)
			}
			err := ensureForgeDirAvailable(contextDir)
			if err == nil || !strings.Contains(err.Error(), "已存在 .forge") {
				t.Fatalf("expected .forge conflict, got %v", err)
			}
		})
	}

	contextDir := t.TempDir()
	if err := ensureForgeDirAvailable(contextDir); err != nil {
		t.Fatalf("expected empty context to be available, got %v", err)
	}
	if err := writeBuildAdapterFiles(contextDir, map[string]string{"../escape.txt": "x"}); err == nil {
		t.Fatal("expected invalid path to fail")
	}
	if err := writeBuildAdapterFiles(contextDir, map[string]string{"deps/common/index.json": "{}"}); err == nil {
		t.Fatal("expected reserved deps path to fail")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(contextDir), "escape.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file escaped context dir: %v", err)
	}
}

func TestExecuteAdapterModeFailsWhenRepoHasForgeDir(t *testing.T) {
	store := &fakeBuildLogStore{}
	runner := &forgeDirRunner{executeTestRunner: executeTestRunner{dockerBuild: func(command) error { return nil }}}
	executor := newExecuteTestExecutor(t, &runner.executeTestRunner, store)
	executor.runner = runner.run
	executor.adapters = &fakeBuildAdapterProvider{adapter: testWorkerBuildAdapter()}

	_, err := executor.Execute(context.Background(), adapterExecuteTestRecord())
	if err == nil || !strings.Contains(err.Error(), "已存在 .forge") {
		t.Fatalf("expected .forge conflict, got %v", err)
	}
	if len(runner.dockerCalls) != 0 {
		t.Fatal("expected no docker build")
	}
}

// forgeDirRunner 在 clone 时额外创建仓库自带的 .forge 目录。
type forgeDirRunner struct {
	executeTestRunner
}

func (r *forgeDirRunner) run(ctx context.Context, cmd command) (string, error) {
	output, err := r.executeTestRunner.run(ctx, cmd)
	if err == nil && cmd.name == "git" && cmd.args[0] == "clone" {
		err = os.MkdirAll(filepath.Join(cmd.args[len(cmd.args)-1], ".forge"), 0o755)
	}
	return output, err
}
