package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

type fakeDependencyProvider struct {
	plan     service.BuildDependencyPlan
	ok       bool
	recorded [][]domain.BuildDependency
}

func (p *fakeDependencyProvider) ResolveBuildDependencies(context.Context, domain.BuildRecord) (service.BuildDependencyPlan, bool, error) {
	return p.plan, p.ok, nil
}

func (p *fakeDependencyProvider) RecordBuildDependencies(_ context.Context, _ string, dependencies []domain.BuildDependency) error {
	p.recorded = append(p.recorded, dependencies)
	return nil
}

// fakeDependencyArtifacts 按构建 id 返回清单，按对象 key 返回文件内容；测试直接改 objects 模拟内容被篡改。
type fakeDependencyArtifacts struct {
	manifests map[string]domain.ArtifactManifest
	objects   map[string][]byte
	// puts 记录 PutArtifactObject 写入的 {存储 bucket}/{key}，presignErr 非空时预签名失败。
	puts       []string
	presignErr error
	presignTTL time.Duration
}

func (r *fakeDependencyArtifacts) PutArtifactObject(_ context.Context, record domain.BuildRecord, objectKey string, body []byte, _ string) error {
	if r.objects == nil {
		r.objects = map[string][]byte{}
	}
	r.objects[objectKey] = body
	r.puts = append(r.puts, record.StorageBucket+"/"+objectKey)
	return nil
}

// PresignArtifactObject 返回 https://presigned.example/{bucket}/{key}?ttl=...，测试据此断言用的是哪个存储。
func (r *fakeDependencyArtifacts) PresignArtifactObject(_ context.Context, record domain.BuildRecord, objectKey string, ttl time.Duration) (string, error) {
	if r.presignErr != nil {
		return "", r.presignErr
	}
	r.presignTTL = ttl
	return "https://presigned.example/" + record.StorageBucket + "/" + objectKey + "?ttl=" + ttl.String(), nil
}

func (r *fakeDependencyArtifacts) ReadArtifactManifest(_ context.Context, record domain.BuildRecord) (domain.ArtifactManifest, error) {
	manifest, ok := r.manifests[record.ID]
	if !ok {
		return domain.ArtifactManifest{}, errors.New("manifest not found")
	}
	return manifest, nil
}

func (r *fakeDependencyArtifacts) GetArtifactObject(_ context.Context, _ domain.BuildRecord, objectKey string) (domain.ArtifactObject, error) {
	content, ok := r.objects[objectKey]
	if !ok {
		return domain.ArtifactObject{}, errors.New("object not found: " + objectKey)
	}
	return domain.ArtifactObject{Body: io.NopCloser(bytes.NewReader(content))}, nil
}

// add 往依赖构建的清单里加一个文件，清单的大小和 sha256 按 content 计算。
func (r *fakeDependencyArtifacts) add(build domain.BuildRecord, filePath string, content string) {
	if r.manifests == nil {
		r.manifests = map[string]domain.ArtifactManifest{}
		r.objects = map[string][]byte{}
	}
	sum := sha256.Sum256([]byte(content))
	manifest := r.manifests[build.ID]
	file := domain.ArtifactManifestFile{
		Path:      filePath,
		ObjectKey: build.ArtifactKey + "/" + filePath,
		SizeBytes: int64(len(content)),
		SHA256:    hex.EncodeToString(sum[:]),
	}
	manifest.Files = append(manifest.Files, file)
	r.manifests[build.ID] = manifest
	r.objects[file.ObjectKey] = []byte(content)
}

func dependencyTestBuild(id string, number int64, runtimeVersion string, platforms ...string) domain.BuildRecord {
	return domain.BuildRecord{
		ID:             id,
		Number:         number,
		ModuleID:       "common",
		Environment:    "qa",
		Status:         domain.BuildStatusSucceeded,
		CommitSHA:      strings.Repeat(id[:1], 40),
		RuntimeVersion: runtimeVersion,
		Platforms:      platforms,
		ArtifactKey:    "rn-bundles/app/common/qa/" + id,
		ManifestKey:    "rn-bundles/app/common/qa/" + id + "/manifest.json",
	}
}

func featureExecuteTestRecord() domain.BuildRecord {
	record := executeTestRecord()
	record.ModuleKind = domain.ModuleKindFeature
	record.CommonModuleID = "common"
	return record
}

// standardDependencies 准备两个运行时版本的 Common 构建：1.0 有 android 和 ios，2.0 只有 android。
func standardDependencies() (*fakeDependencyProvider, *fakeDependencyArtifacts) {
	first := dependencyTestBuild("a0000000-0000-0000-0000-000000000001", 3, "1.0", "android", "ios")
	second := dependencyTestBuild("b0000000-0000-0000-0000-000000000002", 7, "2.0", "android")
	artifacts := &fakeDependencyArtifacts{}
	artifacts.add(first, "forge.json", `{"runtimeVersion":"1.0"}`)
	artifacts.add(first, "android/ids.json", `{"a":1}`)
	artifacts.add(first, "android/res/drawable/x.png", "png")
	artifacts.add(first, "ios/ids.json", `{"i":1}`)
	artifacts.add(second, "forge.json", `{"runtimeVersion":"2.0"}`)
	artifacts.add(second, "android/ids.json", `{"a":2}`)
	provider := &fakeDependencyProvider{ok: true, plan: service.BuildDependencyPlan{
		ModuleID:    "common",
		Environment: "qa",
		Builds:      []domain.BuildRecord{first, second},
	}}
	return provider, artifacts
}

func newDependencyTestExecutor(t *testing.T, runner *executeTestRunner, store BuildLogStore, provider *fakeDependencyProvider, artifacts *fakeDependencyArtifacts) *Executor {
	t.Helper()
	executor := newExecuteTestExecutor(t, runner, store)
	executor.dependencies = provider
	executor.dependencyArtifacts = artifacts
	return executor
}

// stopAfterDockerBuild 在 docker build 时把构建上下文里的 .forge/deps 拷贝成 map，再让构建失败结束测试。
func stopAfterDockerBuild(snapshot map[string]string) func(cmd command) error {
	return func(cmd command) error {
		depsDir := filepath.Join(cmd.args[len(cmd.args)-1], ".forge", "deps")
		_ = filepath.WalkDir(depsDir, func(filePath string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			content, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			relative, _ := filepath.Rel(depsDir, filePath)
			snapshot[filepath.ToSlash(relative)] = string(content)
			return nil
		})
		return errors.New("stop after docker build")
	}
}

func TestExecuteInjectsCommonDependencies(t *testing.T) {
	store := &fakeBuildLogStore{}
	provider, artifacts := standardDependencies()
	snapshot := map[string]string{}
	runner := &executeTestRunner{dockerBuild: stopAfterDockerBuild(snapshot)}

	_, err := newDependencyTestExecutor(t, runner, store, provider, artifacts).Execute(context.Background(), featureExecuteTestRecord())
	if err == nil || !strings.Contains(err.Error(), "stop after docker build") {
		t.Fatalf("expected fake docker failure, got %v", err)
	}

	wantFiles := map[string]string{
		"common/1.0/android/ids.json":                   `{"a":1}`,
		"common/1.0/android/res/drawable/x.png":         "png",
		"common/1.0/ios/ids.json":                       `{"i":1}`,
		"common/2.0/android/ids.json":                   `{"a":2}`,
		"common/" + domain.BuildDependencyIndexFileName: "",
	}
	for name, content := range wantFiles {
		got, ok := snapshot[name]
		if !ok {
			t.Fatalf("missing injected file %s, got %v", name, snapshot)
		}
		if content != "" && got != content {
			t.Fatalf("unexpected content of %s: %q", name, got)
		}
	}
	if len(snapshot) != len(wantFiles) {
		t.Fatalf("root files such as forge.json must not be injected, got %v", snapshot)
	}

	var index dependencyIndex
	if err := json.Unmarshal([]byte(snapshot["common/index.json"]), &index); err != nil {
		t.Fatalf("decode index.json: %v", err)
	}
	if index.ModuleID != "common" || index.Environment != "qa" || len(index.Items) != 2 {
		t.Fatalf("unexpected index: %+v", index)
	}
	first := index.Items[0]
	if first.RuntimeVersion != "1.0" || first.BuildNumber != 3 || first.BuildRecordID != provider.plan.Builds[0].ID ||
		first.CommitSHA != provider.plan.Builds[0].CommitSHA || !slices.Equal(first.Platforms, []string{"android", "ios"}) {
		t.Fatalf("unexpected first index item: %+v", first)
	}
	if !slices.Equal(index.Items[1].Platforms, []string{"android"}) {
		t.Fatalf("unexpected second index item: %+v", index.Items[1])
	}

	if len(provider.recorded) != 1 || !slices.Equal(provider.recorded[0], []domain.BuildDependency{
		{ModuleID: "common", RuntimeVersion: "1.0", BuildRecordID: provider.plan.Builds[0].ID},
		{ModuleID: "common", RuntimeVersion: "2.0", BuildRecordID: provider.plan.Builds[1].ID},
	}) {
		t.Fatalf("unexpected recorded dependencies: %#v", provider.recorded)
	}
	if !slices.Contains(runner.dockerCalls[0].args, "FORGE_DEPS_DIR=.forge/deps") {
		t.Fatalf("expected FORGE_DEPS_DIR build arg, got %#v", runner.dockerCalls[0].args)
	}
	logs := store.content()
	if !strings.Contains(logs, "runtimeVersion 1.0，构建 #3（commit aaaaaaa），平台 android,ios，3 个文件，17 字节") ||
		!strings.Contains(logs, "runtimeVersion 2.0，构建 #7（commit bbbbbbb），平台 android，1 个文件，7 字节") {
		t.Fatalf("expected dependency summary in log, got %q", logs)
	}
}

func TestExecuteAdapterModeInjectsDependenciesNextToAdapterFiles(t *testing.T) {
	store := &fakeBuildLogStore{}
	provider, artifacts := standardDependencies()
	snapshot := map[string]string{}
	var adapterFile string
	stop := stopAfterDockerBuild(snapshot)
	runner := &executeTestRunner{dockerBuild: func(cmd command) error {
		content, _ := os.ReadFile(filepath.Join(cmd.args[len(cmd.args)-1], ".forge", "build.js"))
		adapterFile = string(content)
		return stop(cmd)
	}}
	executor := newDependencyTestExecutor(t, runner, store, provider, artifacts)
	executor.adapters = &fakeBuildAdapterProvider{adapter: testWorkerBuildAdapter()}
	record := adapterExecuteTestRecord()
	record.ModuleKind = domain.ModuleKindFeature
	record.CommonModuleID = "common"

	_, err := executor.Execute(context.Background(), record)
	if err == nil || !strings.Contains(err.Error(), "stop after docker build") {
		t.Fatalf("expected fake docker failure, got %v", err)
	}
	if adapterFile == "" || snapshot["common/2.0/android/ids.json"] != `{"a":2}` {
		t.Fatalf("expected adapter files and dependencies together, got %q %v", adapterFile, snapshot)
	}
}

func TestExecuteWritesEmptyIndexWhenCommonHasNoDefaultRoute(t *testing.T) {
	store := &fakeBuildLogStore{}
	provider := &fakeDependencyProvider{ok: true, plan: service.BuildDependencyPlan{ModuleID: "common", Environment: "qa"}}
	snapshot := map[string]string{}
	runner := &executeTestRunner{dockerBuild: stopAfterDockerBuild(snapshot)}

	_, err := newDependencyTestExecutor(t, runner, store, provider, &fakeDependencyArtifacts{}).Execute(context.Background(), featureExecuteTestRecord())
	if err == nil || !strings.Contains(err.Error(), "stop after docker build") {
		t.Fatalf("expected build to continue to docker, got %v", err)
	}
	if len(snapshot) != 1 {
		t.Fatalf("expected only index.json, got %v", snapshot)
	}
	var index map[string]any
	if err := json.Unmarshal([]byte(snapshot["common/index.json"]), &index); err != nil {
		t.Fatalf("decode index.json: %v", err)
	}
	if items, ok := index["items"].([]any); !ok || len(items) != 0 || index["moduleId"] != "common" {
		t.Fatalf("expected empty items array, got %v", index)
	}
	if len(provider.recorded) != 1 || len(provider.recorded[0]) != 0 {
		t.Fatalf("expected empty dependency record, got %#v", provider.recorded)
	}
	if !strings.Contains(store.content(), "警告：所属 Common 模块 common 在环境 qa 下没有生效中的默认路由") {
		t.Fatalf("expected warning in log, got %q", store.content())
	}
}

func TestExecuteFailsBeforeDockerWhenDependencyInvalid(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(provider *fakeDependencyProvider, artifacts *fakeDependencyArtifacts, executor *Executor)
		want    string
	}{
		{
			name: "sha256 mismatch",
			prepare: func(provider *fakeDependencyProvider, artifacts *fakeDependencyArtifacts, _ *Executor) {
				build := provider.plan.Builds[0]
				artifacts.objects[build.ArtifactKey+"/android/ids.json"] = []byte(`{"b":1}`)
			},
			want: "sha256 校验失败",
		},
		{
			name: "size mismatch",
			prepare: func(provider *fakeDependencyProvider, artifacts *fakeDependencyArtifacts, _ *Executor) {
				build := provider.plan.Builds[0]
				artifacts.objects[build.ArtifactKey+"/android/ids.json"] = []byte(`{"a":1} and more`)
			},
			want: "大小与清单不一致",
		},
		{
			name: "total size limit",
			prepare: func(_ *fakeDependencyProvider, _ *fakeDependencyArtifacts, executor *Executor) {
				executor.cfg.DepsMaxBytes = 10
			},
			want: "FORGE_WORKER_DEPS_MAX_BYTES",
		},
		{
			name: "runtime version as parent dir",
			prepare: func(provider *fakeDependencyProvider, _ *fakeDependencyArtifacts, _ *Executor) {
				provider.plan.Builds[1].RuntimeVersion = ".."
			},
			want: "不能作为依赖目录名",
		},
		{
			name: "path traversal",
			prepare: func(provider *fakeDependencyProvider, artifacts *fakeDependencyArtifacts, _ *Executor) {
				artifacts.add(provider.plan.Builds[1], "android/../../escape.txt", "x")
			},
			want: "非法路径",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeBuildLogStore{}
			provider, artifacts := standardDependencies()
			runner := &executeTestRunner{dockerBuild: func(command) error { return nil }}
			executor := newDependencyTestExecutor(t, runner, store, provider, artifacts)
			tt.prepare(provider, artifacts, executor)

			_, err := executor.Execute(context.Background(), featureExecuteTestRecord())
			if err == nil || !strings.Contains(err.Error(), "注入依赖产物失败") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q failure, got %v", tt.want, err)
			}
			if len(runner.dockerCalls) != 0 || len(provider.recorded) != 0 {
				t.Fatalf("expected no docker build and no record, got %d calls %v", len(runner.dockerCalls), provider.recorded)
			}
		})
	}
}

func TestDependencyFilePath(t *testing.T) {
	platforms := []string{"android", "ios"}
	for _, value := range []string{"", "../x", "android/../../x", "android/../ios/x", "/android/x", "android\\x", "android/./../x"} {
		if _, _, _, err := dependencyFilePath(value, platforms); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
	for _, value := range []string{"forge.json", "android", "harmonyos/x.js", "other/x.js"} {
		if _, _, ok, err := dependencyFilePath(value, platforms); err != nil || ok {
			t.Fatalf("expected %q to be skipped, got ok=%v err=%v", value, ok, err)
		}
	}
	got, platform, ok, err := dependencyFilePath("ios/./assets/a.png", platforms)
	if err != nil || !ok || got != "ios/assets/a.png" || platform != "ios" {
		t.Fatalf("unexpected result %q %q %v %v", got, platform, ok, err)
	}
}

func TestDownloadDependencyFileDoesNotFollowSymlinks(t *testing.T) {
	build := dependencyTestBuild("a0000000-0000-0000-0000-000000000001", 1, "1.0", "android")
	artifacts := &fakeDependencyArtifacts{}
	artifacts.add(build, "android/ids.json", "x")
	file := artifacts.manifests[build.ID].Files[0]

	depsRoot := t.TempDir()
	outside := t.TempDir()
	realDepsRoot, _ := filepath.EvalSymlinks(depsRoot)

	// 目标文件是指向依赖目录外的软链：O_EXCL 拒绝写入，外部文件不变。
	outsideFile := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(outsideFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(depsRoot, "1.0", "android"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(depsRoot, "1.0", "android", "ids.json")); err != nil {
		t.Fatal(err)
	}
	if err := downloadDependencyFile(context.Background(), artifacts, realDepsRoot, filepath.Join(depsRoot, "1.0", "android", "ids.json"), build, file); err == nil {
		t.Fatal("expected symlink target to be rejected")
	}
	if content, _ := os.ReadFile(outsideFile); string(content) != "keep" {
		t.Fatalf("file outside deps dir was modified: %q", content)
	}

	// 父目录是指向依赖目录外的软链：拒绝写入。
	if err := os.Symlink(outside, filepath.Join(depsRoot, "2.0")); err != nil {
		t.Fatal(err)
	}
	err := downloadDependencyFile(context.Background(), artifacts, realDepsRoot, filepath.Join(depsRoot, "2.0", "ids.json"), build, file)
	if err == nil || !strings.Contains(err.Error(), "依赖目录外部") {
		t.Fatalf("expected symlinked directory to be rejected, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "ids.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file escaped deps dir: %v", err)
	}
}

func TestExecuteForgeDirConflictOnlyWhenWriting(t *testing.T) {
	t.Run("feature module with repo .forge fails", func(t *testing.T) {
		provider, artifacts := standardDependencies()
		runner := &forgeDirRunner{executeTestRunner: executeTestRunner{dockerBuild: func(command) error { return nil }}}
		executor := newDependencyTestExecutor(t, &runner.executeTestRunner, &fakeBuildLogStore{}, provider, artifacts)
		executor.runner = runner.run

		_, err := executor.Execute(context.Background(), featureExecuteTestRecord())
		if err == nil || !strings.Contains(err.Error(), "已存在 .forge") {
			t.Fatalf("expected .forge conflict, got %v", err)
		}
		if len(runner.dockerCalls) != 0 || len(provider.recorded) != 0 {
			t.Fatal("expected no docker build and no record")
		}
	})

	t.Run("dockerfile build without dependencies ignores repo .forge", func(t *testing.T) {
		provider := &fakeDependencyProvider{}
		runner := &forgeDirRunner{executeTestRunner: executeTestRunner{dockerBuild: func(command) error { return errors.New("stop after docker build") }}}
		executor := newDependencyTestExecutor(t, &runner.executeTestRunner, &fakeBuildLogStore{}, provider, &fakeDependencyArtifacts{})
		executor.runner = runner.run

		_, err := executor.Execute(context.Background(), executeTestRecord())
		if err == nil || !strings.Contains(err.Error(), "stop after docker build") {
			t.Fatalf("expected build to reach docker, got %v", err)
		}
		if len(provider.recorded) != 0 {
			t.Fatalf("expected no dependency record, got %v", provider.recorded)
		}
	})
}
