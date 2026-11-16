package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

type fakeBuildAdapterRepository struct {
	adapters      map[string]domain.BuildAdapter
	lastUpdate    domain.BuildAdapterInput
	lastVersion   int
	updateCalls   int
	statusChanges []domain.BuildAdapterStatus
}

func newFakeBuildAdapterRepository(adapters ...domain.BuildAdapter) *fakeBuildAdapterRepository {
	repository := &fakeBuildAdapterRepository{adapters: map[string]domain.BuildAdapter{}}
	for _, adapter := range adapters {
		repository.adapters[adapter.ID] = adapter
	}
	return repository
}

func (r *fakeBuildAdapterRepository) CreateBuildAdapter(_ context.Context, input domain.BuildAdapterInput) (domain.BuildAdapter, error) {
	adapter := domain.BuildAdapter{ID: "adapter-new", Name: input.Name, DockerfileTemplate: input.DockerfileTemplate, Files: input.Files, Parameters: input.Parameters, Version: 1, Status: domain.BuildAdapterStatusActive}
	r.adapters[adapter.ID] = adapter
	return adapter, nil
}

func (r *fakeBuildAdapterRepository) GetBuildAdapter(_ context.Context, id string) (domain.BuildAdapter, error) {
	adapter, ok := r.adapters[id]
	if !ok {
		return domain.BuildAdapter{}, domain.ErrBuildAdapterNotFound
	}
	return adapter, nil
}

func (r *fakeBuildAdapterRepository) ListBuildAdapters(context.Context, domain.BuildAdapterFilters) ([]domain.BuildAdapter, error) {
	return nil, nil
}

func (r *fakeBuildAdapterRepository) SetBuildAdapterStatus(_ context.Context, id string, status domain.BuildAdapterStatus, _ string) (domain.BuildAdapter, error) {
	adapter := r.adapters[id]
	adapter.Status = status
	r.adapters[id] = adapter
	r.statusChanges = append(r.statusChanges, status)
	return adapter, nil
}

func (r *fakeBuildAdapterRepository) UpdateBuildAdapter(_ context.Context, id string, expectedVersion int, input domain.BuildAdapterInput) (domain.BuildAdapter, error) {
	r.updateCalls++
	r.lastUpdate = input
	r.lastVersion = expectedVersion
	adapter := r.adapters[id]
	adapter.Name, adapter.DockerfileTemplate, adapter.Files, adapter.Parameters = input.Name, input.DockerfileTemplate, input.Files, input.Parameters
	adapter.Version++
	r.adapters[id] = adapter
	return adapter, nil
}

func testBuildAdapter(id string, status domain.BuildAdapterStatus) domain.BuildAdapter {
	return domain.BuildAdapter{
		ID:                 id,
		Name:               "Node bundle",
		DockerfileTemplate: "FROM node:20\nRUN node {{ .Params.entry }}\n",
		Parameters:         []domain.BuildAdapterParameter{{Key: "entry", Required: true}, {Key: "flavor", Default: "release"}},
		Version:            3,
		Status:             status,
	}
}

func adapterModuleInput(adapterID string, params map[string]string) domain.ModuleInput {
	input := validModuleInput("module-profile")
	input.BuildConfig = domain.ModuleBuildConfig{BuildMode: domain.BuildModeAdapter, BuildAdapterID: adapterID, BuildAdapterParams: params}
	return input
}

func TestModuleServiceValidatesBuildAdapter(t *testing.T) {
	adapters := newFakeBuildAdapterRepository(testBuildAdapter("adapter-active", domain.BuildAdapterStatusActive), testBuildAdapter("adapter-disabled", domain.BuildAdapterStatusDisabled))
	tests := []struct {
		name    string
		input   domain.ModuleInput
		wantErr error
	}{
		{name: "valid", input: adapterModuleInput("adapter-active", map[string]string{"entry": "index.js"})},
		{name: "missing adapter id", input: adapterModuleInput("", nil), wantErr: domain.ErrInvalidModuleInput},
		{name: "unknown adapter", input: adapterModuleInput("adapter-missing", map[string]string{"entry": "index.js"}), wantErr: domain.ErrInvalidModuleInput},
		{name: "disabled adapter", input: adapterModuleInput("adapter-disabled", map[string]string{"entry": "index.js"}), wantErr: domain.ErrBuildAdapterDisabled},
		{name: "missing required param", input: adapterModuleInput("adapter-active", map[string]string{"flavor": "debug"}), wantErr: domain.ErrInvalidBuildAdapterInput},
		{name: "undeclared param", input: adapterModuleInput("adapter-active", map[string]string{"entry": "a", "unknown": "b"}), wantErr: domain.ErrInvalidBuildAdapterInput},
		{name: "invalid param key", input: adapterModuleInput("adapter-active", map[string]string{"entry": "a", "bad-key": "b"}), wantErr: domain.ErrInvalidBuildAdapterInput},
		{name: "unknown mode", input: func() domain.ModuleInput {
			input := validModuleInput("module-profile")
			input.BuildConfig.BuildMode = "script"
			return input
		}(), wantErr: domain.ErrInvalidModuleInput},
		{name: "dockerfile mode with adapter", input: func() domain.ModuleInput {
			input := adapterModuleInput("adapter-active", nil)
			input.BuildConfig.BuildMode = domain.BuildModeDockerfile
			return input
		}(), wantErr: domain.ErrInvalidModuleInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := fakeModuleRepository{}
			service := newTestModuleService(&repository).WithBuildAdapters(adapters)
			_, err := service.CreateModule(context.Background(), tt.input)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				if repository.lastCreateInput.BuildConfig.BuildMode != domain.BuildModeAdapter || repository.lastCreateInput.BuildConfig.BuildAdapterParams["entry"] != "index.js" {
					t.Fatalf("unexpected saved build config: %#v", repository.lastCreateInput.BuildConfig)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
			if repository.createCalls != 0 {
				t.Fatal("expected repository not to be called")
			}
		})
	}
}

func TestModuleServiceDefaultsToDockerfileMode(t *testing.T) {
	repository := fakeModuleRepository{}
	if _, err := newTestModuleService(&repository).CreateModule(context.Background(), validModuleInput("module-profile")); err != nil {
		t.Fatalf("create: %v", err)
	}
	config := repository.lastCreateInput.BuildConfig
	if config.BuildMode != domain.BuildModeDockerfile || config.BuildAdapterID != "" || len(config.BuildAdapterParams) != 0 {
		t.Fatalf("unexpected build config: %#v", config)
	}
}

func TestModuleServiceKeepsDisabledAdapterAlreadySelected(t *testing.T) {
	adapters := newFakeBuildAdapterRepository(testBuildAdapter("adapter-disabled", domain.BuildAdapterStatusDisabled))
	repository := fakeModuleRepository{}
	repository.module = testServiceModule("module-profile", domain.ModuleKindFeature)
	repository.module.BuildConfig.BuildAdapterID = "adapter-disabled"
	service := newTestModuleService(&repository).WithBuildAdapters(adapters)

	input := adapterModuleInput("adapter-disabled", map[string]string{"entry": "index.js"})
	input.ID = ""
	if _, err := service.UpdateModule(context.Background(), "module-profile", input); err != nil {
		t.Fatalf("expected update with unchanged disabled adapter to pass, got %v", err)
	}

	repository.module.BuildConfig.BuildAdapterID = "adapter-other"
	if _, err := service.UpdateModule(context.Background(), "module-profile", input); !errors.Is(err, domain.ErrBuildAdapterDisabled) {
		t.Fatalf("expected switching to disabled adapter to fail, got %v", err)
	}
}

func TestBuildAdapterServiceUpdateMergesPatchWithOptimisticVersion(t *testing.T) {
	repository := newFakeBuildAdapterRepository(testBuildAdapter("adapter-1", domain.BuildAdapterStatusActive))
	service := NewBuildAdapterService(BuildAdapterServiceDependencies{Repository: repository})

	template := "FROM node:22\nRUN node {{ .Params.entry }}\n"
	updated, err := service.UpdateBuildAdapter(context.Background(), "adapter-1", domain.BuildAdapterPatch{DockerfileTemplate: &template, UserID: "user-1"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if repository.lastVersion != 3 || updated.Version != 4 || repository.lastUpdate.Name != "Node bundle" || len(repository.lastUpdate.Parameters) != 2 {
		t.Fatalf("unexpected update: version %d -> %d, input %#v", repository.lastVersion, updated.Version, repository.lastUpdate)
	}

	broken := "FROM node\nRUN {{ .Params.entry | printf }}\n"
	_, err = service.UpdateBuildAdapter(context.Background(), "adapter-1", domain.BuildAdapterPatch{DockerfileTemplate: &broken, UserID: "user-1"})
	var renderErr *domain.BuildAdapterRenderError
	if !errors.As(err, &renderErr) || renderErr.Line != 2 || repository.updateCalls != 1 {
		t.Fatalf("expected render error at line 2 without saving, got %v", err)
	}
}

func TestBuildAdapterServiceUpdateRejectsStaleExpectedVersion(t *testing.T) {
	repository := newFakeBuildAdapterRepository(testBuildAdapter("adapter-1", domain.BuildAdapterStatusActive))
	service := NewBuildAdapterService(BuildAdapterServiceDependencies{Repository: repository})
	name := "renamed"

	// 编辑页加载的是版本 2，期间被他人保存到了版本 3。
	_, err := service.UpdateBuildAdapter(context.Background(), "adapter-1", domain.BuildAdapterPatch{Name: &name, ExpectedVersion: 2, UserID: "user-1"})
	if !errors.Is(err, domain.ErrBuildAdapterVersionConflict) || repository.updateCalls != 0 {
		t.Fatalf("expected version conflict without saving, got %v (calls %d)", err, repository.updateCalls)
	}

	if _, err := service.UpdateBuildAdapter(context.Background(), "adapter-1", domain.BuildAdapterPatch{Name: &name, ExpectedVersion: 3, UserID: "user-1"}); err != nil {
		t.Fatalf("matching expected version should update: %v", err)
	}
}

func TestBuildAdapterServicePreview(t *testing.T) {
	repository := newFakeBuildAdapterRepository(testBuildAdapter("adapter-1", domain.BuildAdapterStatusActive))
	modules := &fakeModuleRepository{module: testServiceModule("module-profile", domain.ModuleKindFeature)}
	modules.module.BuildConfig.BuildAdapterID = "adapter-1"
	modules.module.BuildConfig.BuildAdapterParams = map[string]string{"entry": "saved.js"}
	service := NewBuildAdapterService(BuildAdapterServiceDependencies{Repository: repository, Modules: modules})
	service.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("CST", 8*3600)) }

	result, err := service.PreviewBuildAdapter(context.Background(), "adapter-1", domain.BuildAdapterPreviewInput{Params: map[string]string{"entry": "sample.js"}})
	if err != nil || result.Dockerfile != "FROM node:20\nRUN node sample.js\n" {
		t.Fatalf("unexpected sample preview %q, %v", result.Dockerfile, err)
	}

	result, err = service.PreviewBuildAdapter(context.Background(), "adapter-1", domain.BuildAdapterPreviewInput{ModuleID: "module-profile"})
	if err != nil || result.Dockerfile != "FROM node:20\nRUN node saved.js\n" {
		t.Fatalf("unexpected module preview %q, %v", result.Dockerfile, err)
	}

	if _, err := service.PreviewBuildAdapter(context.Background(), "adapter-1", domain.BuildAdapterPreviewInput{}); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
		t.Fatalf("expected missing required param error, got %v", err)
	}
	if _, err := service.PreviewBuildAdapter(context.Background(), "missing", domain.BuildAdapterPreviewInput{}); !errors.Is(err, domain.ErrBuildAdapterNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestBuildAdapterServicePreviewDraft(t *testing.T) {
	modules := &fakeModuleRepository{module: testServiceModule("module-profile", domain.ModuleKindFeature)}
	modules.module.BuildConfig.BuildAdapterID = "adapter-1"
	modules.module.BuildConfig.BuildAdapterParams = map[string]string{"entry": "saved.js", "removed": "x"}
	service := NewBuildAdapterService(BuildAdapterServiceDependencies{Repository: newFakeBuildAdapterRepository(), Modules: modules})
	draft := domain.BuildAdapterInput{
		DockerfileTemplate: "FROM node:22\nRUN node {{ .Params.entry }}\n",
		Files:              map[string]string{"conf/env.txt": "{{ .Environment }}"},
		Parameters:         []domain.BuildAdapterParameter{{Key: "entry", Required: true}},
	}

	result, err := service.PreviewBuildAdapterDraft(context.Background(), draft, domain.BuildAdapterPreviewInput{Params: map[string]string{"entry": "draft.js"}})
	if err != nil || result.Dockerfile != "FROM node:22\nRUN node draft.js\n" || result.Files["conf/env.txt"] != "test" {
		t.Fatalf("unexpected draft preview %#v, %v", result, err)
	}

	// 未传 params 时沿用模块已保存的参数，并忽略草稿未声明的参数。
	result, err = service.PreviewBuildAdapterDraft(context.Background(), draft, domain.BuildAdapterPreviewInput{ModuleID: "module-profile"})
	if err != nil || result.Dockerfile != "FROM node:22\nRUN node saved.js\n" {
		t.Fatalf("unexpected module draft preview %q, %v", result.Dockerfile, err)
	}

	// 显式传入的参数仍严格校验。
	if _, err := service.PreviewBuildAdapterDraft(context.Background(), draft, domain.BuildAdapterPreviewInput{Params: map[string]string{"entry": "a", "other": "b"}}); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
		t.Fatalf("expected undeclared param error, got %v", err)
	}

	broken := draft
	broken.Files = map[string]string{"../escape.sh": "x"}
	if _, err := service.PreviewBuildAdapterDraft(context.Background(), broken, domain.BuildAdapterPreviewInput{Params: map[string]string{"entry": "a"}}); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
		t.Fatalf("expected invalid draft path error, got %v", err)
	}
	broken = draft
	broken.DockerfileTemplate = "FROM node\n{{ printf \"x\" }}\n"
	_, err = service.PreviewBuildAdapterDraft(context.Background(), broken, domain.BuildAdapterPreviewInput{Params: map[string]string{"entry": "a"}})
	var renderErr *domain.BuildAdapterRenderError
	if !errors.As(err, &renderErr) || renderErr.Line != 2 {
		t.Fatalf("expected render error at line 2, got %v", err)
	}
}
