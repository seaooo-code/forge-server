package worker

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
	"forge-server/internal/service"
)

// fakeCIClient 模拟代码源的 CI：pipelines 按调用顺序依次返回状态，最后一个状态重复返回。
type fakeCIClient struct {
	mu sync.Mutex

	createRef  string
	createVars []domain.CIVariable
	createErr  error
	created    domain.CIPipeline

	pipelines   []domain.CIPipeline
	getCalls    int
	getErr      error
	jobs        []domain.CIJob
	trace       string
	artifacts   []byte
	artifactErr error
	canceled    []int64
	creates     int
}

func (c *fakeCIClient) CreatePipeline(_ context.Context, ref string, variables []domain.CIVariable) (domain.CIPipeline, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.creates++
	c.createRef = ref
	c.createVars = variables
	if c.createErr != nil {
		return domain.CIPipeline{}, c.createErr
	}
	return c.created, nil
}

func (c *fakeCIClient) GetPipeline(context.Context, int64) (domain.CIPipeline, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.getErr != nil {
		return domain.CIPipeline{}, c.getErr
	}
	index := min(c.getCalls, len(c.pipelines)-1)
	c.getCalls++
	return c.pipelines[index], nil
}

func (c *fakeCIClient) ListPipelineJobs(context.Context, int64) ([]domain.CIJob, error) {
	return c.jobs, nil
}

func (c *fakeCIClient) JobTrace(_ context.Context, _ int64, offset int64) ([]byte, error) {
	if offset >= int64(len(c.trace)) {
		return nil, nil
	}
	return []byte(c.trace[offset:]), nil
}

func (c *fakeCIClient) DownloadJobArtifacts(_ context.Context, _ int64, dst io.Writer, _ int64) error {
	if c.artifactErr != nil {
		return c.artifactErr
	}
	_, err := dst.Write(c.artifacts)
	return err
}

func (c *fakeCIClient) CancelPipeline(_ context.Context, id int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.canceled = append(c.canceled, id)
	return nil
}

func (c *fakeCIClient) canceledPipelines() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int64(nil), c.canceled...)
}

type fakeCIProvider struct{ client *fakeCIClient }

func (p fakeCIProvider) CIClient(context.Context, domain.ModuleRepository) (service.CIClient, error) {
	return p.client, nil
}

type fakeCIRecorder struct {
	mu        sync.Mutex
	pipelines []int64
	urls      []string
	workers   []string
	err       error
}

func (r *fakeCIRecorder) RecordBuildCIPipeline(_ context.Context, _ string, workerID string, pipelineID int64, pipelineURL string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.pipelines = append(r.pipelines, pipelineID)
	r.urls = append(r.urls, pipelineURL)
	r.workers = append(r.workers, workerID)
	return nil
}

func ciTestRecord() domain.BuildRecord {
	record := executeTestRecord()
	record.BuildMode = domain.BuildModeGitLabCI
	record.CIJob = "forge_build"
	record.CIArtifactPath = "dist"
	record.Number = 7
	record.WorkerID = "worker-1"
	record.DefaultBranch = "master"
	return record
}

func newCITestExecutor(t *testing.T, client *fakeCIClient, recorder *fakeCIRecorder, store BuildLogStore) *Executor {
	t.Helper()
	executor := NewExecutor(config.WorkerConfig{
		WorkDir:            t.TempDir(),
		LogMaxBytes:        1 << 20,
		ArtifactDigests:    []string{"sha256"},
		CIPollInterval:     time.Millisecond,
		CIArtifactMaxBytes: 1 << 20,
	}, ExecutorDependencies{
		CI:          fakeCIProvider{client: client},
		CIPipelines: recorder,
		Logs:        store,
		Variables: fakeVariableResolver{variables: []domain.ModuleBuildVariable{
			{Key: "NPM_TOKEN", Value: "npm-secret-value", IsSecret: true},
			{Key: "API_HOST", Value: "qa.example.com"},
			{Key: "FORGE_BUILD_ID", Value: "spoofed"},
		}},
	}, nil)
	return executor
}

func ciTestSuccessClient(artifacts []byte) *fakeCIClient {
	return &fakeCIClient{
		created:   domain.CIPipeline{ID: 42, URL: "https://gitlab.example.com/rn/app/-/pipelines/42", SHA: testCommitSHA, Status: "pending"},
		pipelines: []domain.CIPipeline{{ID: 42, SHA: testCommitSHA, Status: "running"}, {ID: 42, SHA: testCommitSHA, Status: domain.CIStatusSuccess}},
		jobs:      []domain.CIJob{{ID: 9, Name: "forge_build", Status: domain.CIStatusSuccess, URL: "https://gitlab.example.com/rn/app/-/jobs/9"}},
		trace:     "yarn install\nnpm notice using token npm-secret-value\nbuild done\n",
		artifacts: artifacts,
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func validCIArtifacts(t *testing.T) []byte {
	t.Helper()
	return zipBytes(t, map[string]string{
		"dist/forge.json":                `{"runtimeVersion":"725-n1-m24"}`,
		"dist/android/index.android.hbc": "android-bundle",
		"dist/ios/index.ios.hbc":         "ios-bundle",
	})
}

func variableValue(variables []domain.CIVariable, key string) (string, bool) {
	for _, variable := range variables {
		if variable.Key == key {
			return variable.Value, true
		}
	}
	return "", false
}

func TestExecuteGitLabCIPublishesArtifactsAndRecordsPipeline(t *testing.T) {
	uploaded := map[string]int{}
	var uploadMu sync.Mutex
	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		uploadMu.Lock()
		uploaded[r.URL.Path]++
		uploadMu.Unlock()
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer s3.Close()

	client := ciTestSuccessClient(validCIArtifacts(t))
	recorder := &fakeCIRecorder{}
	store := &fakeBuildLogStore{}
	record := ciTestRecord()
	record.StorageBucket = "bucket"
	record.StorageRegion = "us-east-1"
	record.StorageEndpoint = s3.URL
	record.StorageAccessKeyID = "key"
	record.StorageSecretAccessKey = "secret"
	record.StoragePublicBaseURL = "https://cdn.example.com"

	completion, err := newCITestExecutor(t, client, recorder, store).Execute(context.Background(), record)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if completion.CommitSHA != testCommitSHA || completion.RuntimeVersion != "725-n1-m24" || len(completion.Platforms) != 2 {
		t.Fatalf("unexpected completion: %#v", completion)
	}
	if completion.FileCount != 3 {
		t.Fatalf("expected 3 artifact files, got %d", completion.FileCount)
	}
	uploadMu.Lock()
	uploadCount := len(uploaded)
	uploadMu.Unlock()
	if uploadCount < 4 {
		t.Fatalf("expected files and manifest to be uploaded, got %d objects", uploadCount)
	}

	if client.createRef != "main" || client.creates != 1 {
		t.Fatalf("unexpected pipeline creation: ref=%q creates=%d", client.createRef, client.creates)
	}
	if len(recorder.pipelines) != 1 || recorder.pipelines[0] != 42 || recorder.workers[0] != "worker-1" {
		t.Fatalf("expected pipeline recorded for the lease holder, got %#v %#v", recorder.pipelines, recorder.workers)
	}
	if len(client.canceledPipelines()) != 0 {
		t.Fatalf("successful build must not cancel the pipeline")
	}

	logs := store.content()
	if strings.Contains(logs, "npm-secret-value") || !strings.Contains(logs, "npm notice using token [redacted]") {
		t.Fatalf("expected redacted job trace in log, got %q", logs)
	}
	if !strings.Contains(logs, "build done") || !strings.Contains(logs, "产物校验通过") {
		t.Fatalf("unexpected build log: %q", logs)
	}
}

func TestExecuteGitLabCIPassesPlatformVariablesAndKeepsUserOnesOutOfForgeNamespace(t *testing.T) {
	client := ciTestSuccessClient(zipBytes(t, map[string]string{"dist/x": "x"}))
	recorder := &fakeCIRecorder{}
	record := ciTestRecord()
	start := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)
	record.StartedAt = &start

	_, _ = newCITestExecutor(t, client, recorder, &fakeBuildLogStore{}).Execute(context.Background(), record)

	expected := map[string]string{
		"NPM_TOKEN":          "npm-secret-value",
		"API_HOST":           "qa.example.com",
		"FORGE_BUILD_ID":     record.ID,
		"FORGE_BUILD_NUMBER": "7",
		"FORGE_BUILD_TIME":   "2026-11-19T08:00:00Z",
		"FORGE_MODULE_ID":    "module-1",
		"FORGE_ENVIRONMENT":  "qa",
		"FORGE_BRANCH":       "main",
	}
	for key, want := range expected {
		if got, ok := variableValue(client.createVars, key); !ok || got != want {
			t.Fatalf("variable %s = %q (present=%v), want %q", key, got, ok, want)
		}
	}
	count := 0
	for _, variable := range client.createVars {
		if variable.Key == "FORGE_BUILD_ID" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("user variable must not override FORGE_BUILD_ID, got %d entries", count)
	}
	if _, ok := variableValue(client.createVars, "FORGE_DEPS"); ok {
		t.Fatal("FORGE_DEPS must only be set for modules with dependencies")
	}
}

func TestExecuteGitLabCIUsesDefaultBranchWhenBuildHasNone(t *testing.T) {
	client := ciTestSuccessClient(nil)
	record := ciTestRecord()
	record.Branch = ""

	_, _ = newCITestExecutor(t, client, &fakeCIRecorder{}, &fakeBuildLogStore{}).Execute(context.Background(), record)
	if client.createRef != "master" {
		t.Fatalf("expected default branch, got %q", client.createRef)
	}
	if got, _ := variableValue(client.createVars, "FORGE_BRANCH"); got != "master" {
		t.Fatalf("expected FORGE_BRANCH to use default branch, got %q", got)
	}
}

func TestExecuteGitLabCIReportsFailedPipeline(t *testing.T) {
	client := ciTestSuccessClient(nil)
	client.pipelines = []domain.CIPipeline{{ID: 42, SHA: testCommitSHA, Status: domain.CIStatusFailed, URL: "https://gitlab.example.com/p/42"}}
	client.jobs = []domain.CIJob{
		{ID: 8, Name: "lint", Status: domain.CIStatusFailed},
		{ID: 9, Name: "forge_build", Status: domain.CIStatusSuccess},
	}

	_, err := newCITestExecutor(t, client, &fakeCIRecorder{}, &fakeBuildLogStore{}).Execute(context.Background(), ciTestRecord())
	if err == nil || !strings.Contains(err.Error(), "pipeline #42 失败") || !strings.Contains(err.Error(), "lint") {
		t.Fatalf("expected failed pipeline error naming the failed job, got %v", err)
	}
}

func TestExecuteGitLabCIReportsMissingJobAndSkippedPipeline(t *testing.T) {
	missing := ciTestSuccessClient(nil)
	missing.jobs = []domain.CIJob{{ID: 1, Name: "other", Status: domain.CIStatusSuccess}}
	_, err := newCITestExecutor(t, missing, &fakeCIRecorder{}, &fakeBuildLogStore{}).Execute(context.Background(), ciTestRecord())
	if err == nil || !strings.Contains(err.Error(), `没有找到 job "forge_build"`) || !strings.Contains(err.Error(), "other") {
		t.Fatalf("expected missing job error listing jobs, got %v", err)
	}

	skipped := ciTestSuccessClient(nil)
	skipped.pipelines = []domain.CIPipeline{{ID: 42, SHA: testCommitSHA, Status: domain.CIStatusSkipped}}
	skipped.jobs = nil
	_, err = newCITestExecutor(t, skipped, &fakeCIRecorder{}, &fakeBuildLogStore{}).Execute(context.Background(), ciTestRecord())
	if err == nil || !strings.Contains(err.Error(), "FORGE_BUILD_ID") {
		t.Fatalf("expected skipped pipeline hint about rules, got %v", err)
	}
}

func TestExecuteGitLabCIReportsMissingArtifactsAndDirectory(t *testing.T) {
	noArtifacts := ciTestSuccessClient(nil)
	noArtifacts.artifactErr = domain.ErrCIArtifactsNotFound
	_, err := newCITestExecutor(t, noArtifacts, &fakeCIRecorder{}, &fakeBuildLogStore{}).Execute(context.Background(), ciTestRecord())
	if err == nil || !strings.Contains(err.Error(), "没有 artifacts") {
		t.Fatalf("expected missing artifacts error, got %v", err)
	}

	wrongPath := ciTestSuccessClient(zipBytes(t, map[string]string{"other/forge.json": "{}"}))
	_, err = newCITestExecutor(t, wrongPath, &fakeCIRecorder{}, &fakeBuildLogStore{}).Execute(context.Background(), ciTestRecord())
	if err == nil || !strings.Contains(err.Error(), "没有产物目录 dist") {
		t.Fatalf("expected missing artifact directory error, got %v", err)
	}
}

func TestExecuteGitLabCIRejectsCommitMismatchAndCancelsPipeline(t *testing.T) {
	client := ciTestSuccessClient(nil)
	record := ciTestRecord()
	record.CommitSHA = strings.Repeat("a", 40)

	_, err := newCITestExecutor(t, client, &fakeCIRecorder{}, &fakeBuildLogStore{}).Execute(context.Background(), record)
	if err == nil || !strings.Contains(err.Error(), "分支已前进") {
		t.Fatalf("expected commit mismatch error, got %v", err)
	}
	if got := client.canceledPipelines(); len(got) != 1 || got[0] != 42 {
		t.Fatalf("expected the mismatched pipeline to be canceled, got %v", got)
	}
}

func TestExecuteGitLabCIResumesExistingPipeline(t *testing.T) {
	client := ciTestSuccessClient(nil)
	recorder := &fakeCIRecorder{}
	record := ciTestRecord()
	record.CIPipelineID = 42

	_, _ = newCITestExecutor(t, client, recorder, &fakeBuildLogStore{}).Execute(context.Background(), record)
	if client.creates != 0 || len(recorder.pipelines) != 0 {
		t.Fatalf("resume must not create another pipeline: creates=%d recorded=%v", client.creates, recorder.pipelines)
	}
	if client.getCalls == 0 {
		t.Fatal("expected the existing pipeline to be polled")
	}
}

func TestExecuteGitLabCICreatesNewPipelineWhenRecordedOneIsGone(t *testing.T) {
	client := ciTestSuccessClient(nil)
	client.getErr = domain.ErrCIPipelineNotFound
	recorder := &fakeCIRecorder{}
	record := ciTestRecord()
	record.CIPipelineID = 41

	// 第一次查询 41 不存在，之后创建的新 pipeline 也会被查询；让后续查询恢复正常。
	executor := newCITestExecutor(t, client, recorder, &fakeBuildLogStore{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		client.mu.Lock()
		client.getErr = nil
		client.mu.Unlock()
	}()
	_, _ = executor.Execute(context.Background(), record)
	if client.creates != 1 || len(recorder.pipelines) != 1 {
		t.Fatalf("expected a new pipeline when the recorded one is gone: creates=%d recorded=%v", client.creates, recorder.pipelines)
	}
}

func TestExecuteGitLabCICancelsPipelineOnTimeoutButNotOnLeaseLostOrShutdown(t *testing.T) {
	running := func() *fakeCIClient {
		client := ciTestSuccessClient(nil)
		client.pipelines = []domain.CIPipeline{{ID: 42, SHA: testCommitSHA, Status: "running"}}
		return client
	}

	timeoutClient := running()
	timeoutCtx, cancelTimeout := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelTimeout()
	_, err := newCITestExecutor(t, timeoutClient, &fakeCIRecorder{}, &fakeBuildLogStore{}).Execute(timeoutCtx, ciTestRecord())
	if err == nil || !strings.Contains(err.Error(), "构建超时") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if got := timeoutClient.canceledPipelines(); len(got) != 1 || got[0] != 42 {
		t.Fatalf("expected pipeline to be canceled on timeout, got %v", got)
	}

	for _, cause := range []error{errBuildLeaseLost, errWorkerShutdown} {
		client := running()
		ctx, cancel := context.WithCancelCause(context.Background())
		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel(cause)
		}()
		_, err := newCITestExecutor(t, client, &fakeCIRecorder{}, &fakeBuildLogStore{}).Execute(ctx, ciTestRecord())
		if !errors.Is(err, cause) {
			t.Fatalf("expected %v, got %v", cause, err)
		}
		if got := client.canceledPipelines(); len(got) != 0 {
			t.Fatalf("pipeline must stay alive for the next holder when %v, got canceled %v", cause, got)
		}
	}
}

func TestExecuteGitLabCIGivesUpAfterRepeatedPollFailures(t *testing.T) {
	client := ciTestSuccessClient(nil)
	client.getErr = errors.New("gitlab is upgrading")

	// 没有已有 pipeline，创建成功后轮询一直失败。
	_, err := newCITestExecutor(t, client, &fakeCIRecorder{}, &fakeBuildLogStore{}).Execute(context.Background(), ciTestRecord())
	if err == nil || !strings.Contains(err.Error(), "连续") {
		t.Fatalf("expected repeated poll failure error, got %v", err)
	}
}

func TestExecuteGitLabCIFailsWhenRecordingPipelineFailsAndCancelsIt(t *testing.T) {
	client := ciTestSuccessClient(nil)
	recorder := &fakeCIRecorder{err: domain.ErrBuildLeaseLost}

	_, err := newCITestExecutor(t, client, recorder, &fakeBuildLogStore{}).Execute(context.Background(), ciTestRecord())
	if !errors.Is(err, domain.ErrBuildLeaseLost) {
		t.Fatalf("expected lease lost error, got %v", err)
	}
	if got := client.canceledPipelines(); len(got) != 1 || got[0] != 42 {
		t.Fatalf("expected the orphan pipeline to be canceled, got %v", got)
	}
}

func TestExecuteGitLabCIRequiresCIDependencies(t *testing.T) {
	executor := NewExecutor(config.WorkerConfig{WorkDir: t.TempDir()}, ExecutorDependencies{Logs: &fakeBuildLogStore{}}, nil)
	_, err := executor.Execute(context.Background(), ciTestRecord())
	if !errors.Is(err, domain.ErrCINotSupported) {
		t.Fatalf("expected ErrCINotSupported without CI dependencies, got %v", err)
	}
}

func TestExecuteGitLabCIPassesDependenciesToThePipeline(t *testing.T) {
	client := ciTestSuccessClient(nil)
	first := dependencyTestBuild("a0000000-0000-0000-0000-000000000001", 3, "1.0", "ios", "android")
	first.StoragePublicBaseURL = "https://cdn.example.com/"
	provider := &fakeDependencyProvider{ok: true, plan: service.BuildDependencyPlan{ModuleID: "common", Environment: "qa", Builds: []domain.BuildRecord{first}}}
	executor := newCITestExecutor(t, client, &fakeCIRecorder{}, &fakeBuildLogStore{})
	executor.dependencies = provider
	record := ciTestRecord()
	record.ModuleKind = domain.ModuleKindFeature
	record.CommonModuleID = "common"

	_, _ = executor.Execute(context.Background(), record)

	raw, ok := variableValue(client.createVars, "FORGE_DEPS")
	if !ok {
		t.Fatal("expected FORGE_DEPS to be passed")
	}
	var deps ciDependencies
	if err := json.Unmarshal([]byte(raw), &deps); err != nil {
		t.Fatalf("decode FORGE_DEPS: %v", err)
	}
	if deps.ModuleID != "common" || deps.Environment != "qa" || len(deps.Items) != 1 {
		t.Fatalf("unexpected FORGE_DEPS: %#v", deps)
	}
	item := deps.Items[0]
	if item.RuntimeVersion != "1.0" || item.BuildNumber != 3 || strings.Join(item.Platforms, ",") != "android,ios" {
		t.Fatalf("unexpected dependency item: %#v", item)
	}
	if item.BaseURL != "https://cdn.example.com" || item.ManifestURL != "https://cdn.example.com/"+first.ManifestKey {
		t.Fatalf("unexpected dependency urls: %#v", item)
	}
	if len(provider.recorded) != 1 || len(provider.recorded[0]) != 1 || provider.recorded[0][0].BuildRecordID != first.ID {
		t.Fatalf("expected dependency builds recorded on the build, got %#v", provider.recorded)
	}
}

func TestExecuteGitLabCIRejectsDependencyWithoutPublicURL(t *testing.T) {
	client := ciTestSuccessClient(nil)
	first := dependencyTestBuild("a0000000-0000-0000-0000-000000000001", 3, "1.0", "android")
	provider := &fakeDependencyProvider{ok: true, plan: service.BuildDependencyPlan{ModuleID: "common", Environment: "qa", Builds: []domain.BuildRecord{first}}}
	executor := newCITestExecutor(t, client, &fakeCIRecorder{}, &fakeBuildLogStore{})
	executor.dependencies = provider
	record := ciTestRecord()
	record.ModuleKind = domain.ModuleKindFeature
	record.CommonModuleID = "common"

	_, err := executor.Execute(context.Background(), record)
	if err == nil || !strings.Contains(err.Error(), "公开访问地址") {
		t.Fatalf("expected public url error, got %v", err)
	}
	if client.creates != 0 {
		t.Fatal("pipeline must not be created when dependencies cannot be handed over")
	}
}

func TestExtractArtifactsZipRejectsUnsafeArchives(t *testing.T) {
	cases := map[string]map[string]string{
		"path traversal": {"../evil": "x"},
		"nested escape":  {"dist/../../evil": "x"},
		"absolute path":  {"/etc/evil": "x"},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			zipPath := filepath.Join(t.TempDir(), "a.zip")
			if err := os.WriteFile(zipPath, zipBytes(t, files), 0o600); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(t.TempDir(), "out")
			if err := os.MkdirAll(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := extractArtifactsZip(zipPath, dest, 1<<20); err == nil {
				t.Fatal("expected unsafe archive to be rejected")
			}
		})
	}
}

func TestExtractArtifactsZipEnforcesSizeLimit(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "a.zip")
	if err := os.WriteFile(zipPath, zipBytes(t, map[string]string{"dist/big": strings.Repeat("x", 4096)}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractArtifactsZip(zipPath, t.TempDir(), 1024); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestExtractArtifactsZipWritesFilesAndDirectories(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "a.zip")
	if err := os.WriteFile(zipPath, zipBytes(t, map[string]string{"dist/a/b.txt": "hello"}), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := extractArtifactsZip(zipPath, dest, 1<<20); err != nil {
		t.Fatalf("extract: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dest, "dist", "a", "b.txt"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("unexpected extracted file: %q %v", content, err)
	}
}

func TestCIArtifactRootDefaultsToDistAndAllowsRoot(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outputDir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := ciArtifactRoot(outputDir, "")
	if err != nil || root != filepath.Join(outputDir, "dist") {
		t.Fatalf("expected default dist root, got %q %v", root, err)
	}
	root, err = ciArtifactRoot(outputDir, ".")
	if err != nil || root != outputDir {
		t.Fatalf("expected output dir as root, got %q %v", root, err)
	}
	if _, err := ciArtifactRoot(outputDir, "missing"); err == nil {
		t.Fatal("expected missing directory error")
	}
}
