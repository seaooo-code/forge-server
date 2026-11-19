package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestModuleServiceRejectsInvalidIDOnCreate(t *testing.T) {
	repository := fakeModuleRepository{}
	service := newTestModuleService(&repository)

	input := validModuleInput("bad id")
	_, err := service.CreateModule(context.Background(), input)
	if !errors.Is(err, domain.ErrInvalidModuleInput) {
		t.Fatalf("expected invalid module input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestModuleServiceRejectsMissingRepository(t *testing.T) {
	repository := fakeModuleRepository{}
	service := newTestModuleService(&repository)

	input := validModuleInput("module-profile")
	input.Repository = domain.ModuleRepository{}
	_, err := service.CreateModule(context.Background(), input)
	if !errors.Is(err, domain.ErrInvalidModuleInput) {
		t.Fatalf("expected invalid module input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestModuleServiceRejectsUncachedRepositoryAndInactiveConnection(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*domain.ModuleInput, *fakeModuleCodeSourceStore)
		wantErr error
	}{
		{
			name: "repository not in connection cache",
			mutate: func(input *domain.ModuleInput, _ *fakeModuleCodeSourceStore) {
				input.Repository.ExternalRepositoryID = "999"
			},
			wantErr: domain.ErrInvalidModuleInput,
		},
		{
			name: "unknown connection",
			mutate: func(input *domain.ModuleInput, _ *fakeModuleCodeSourceStore) {
				input.Repository.ConnectionID = "missing"
			},
			wantErr: domain.ErrInvalidModuleInput,
		},
		{
			name: "disabled connection",
			mutate: func(_ *domain.ModuleInput, store *fakeModuleCodeSourceStore) {
				store.connection.Status = domain.CodeSourceConnectionStatusDisabled
			},
			wantErr: domain.ErrCodeSourceConnectionInactive,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := fakeModuleRepository{}
			store := newFakeModuleCodeSourceStore()
			service := NewModuleService(&repository, store, "Dockerfile")

			input := validModuleInput("module-profile")
			tt.mutate(&input, store)
			if _, err := service.CreateModule(context.Background(), input); !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
			if repository.createCalls != 0 {
				t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
			}
		})
	}
}

func TestModuleServiceValidatesBuildConfig(t *testing.T) {
	tests := map[string]domain.ModuleBuildConfig{
		"absolute dockerfile":        {DockerfilePath: "/etc/Dockerfile"},
		"dockerfile escapes repo":    {DockerfilePath: "../Dockerfile"},
		"context escapes repo":       {BuildContext: "apps/../../"},
		"dockerfile is directory":    {DockerfilePath: "."},
		"invalid branch glob":        {AutoBuildBranches: []string{"release/["}, AutoBuildEnvironment: "qa"},
		"branches without env":       {AutoBuildBranches: []string{"main"}},
		"negative preview ttl":       {AutoPreviewTTLHours: -1},
		"preview ttl over one year":  {AutoPreviewTTLHours: 24*365 + 1},
		"windows separator in paths": {DockerfilePath: "apps\\Dockerfile"},
	}
	for name, config := range tests {
		t.Run(name, func(t *testing.T) {
			repository := fakeModuleRepository{}
			service := newTestModuleService(&repository)

			input := validModuleInput("module-profile")
			input.BuildConfig = config
			if _, err := service.CreateModule(context.Background(), input); !errors.Is(err, domain.ErrInvalidModuleInput) {
				t.Fatalf("expected invalid module input, got %v", err)
			}
		})
	}
}

func TestModuleServiceRejectsIDChangeOnUpdate(t *testing.T) {
	repository := fakeModuleRepository{}
	service := newTestModuleService(&repository)

	input := validModuleInput("")
	input.ID = "changed"
	_, err := service.UpdateModule(context.Background(), "module-profile", input)
	if !errors.Is(err, domain.ErrInvalidModuleInput) {
		t.Fatalf("expected invalid module input, got %v", err)
	}
	if repository.updateCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.updateCalls)
	}
}

func TestModuleServiceReturnsSummary(t *testing.T) {
	repository := fakeModuleRepository{
		modules: []domain.Module{
			testServiceModule("module-common", domain.ModuleKindCommon),
			testServiceModule("module-home", domain.ModuleKindFeature),
		},
	}
	service := newTestModuleService(&repository)

	_, summary, err := service.ListModules(context.Background(), domain.ModuleListFilters{})
	if err != nil {
		t.Fatalf("list modules: %v", err)
	}

	if summary.Total != 2 || summary.Common != 1 || summary.Feature != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestModuleServiceRejectsFeatureWithoutCommonModule(t *testing.T) {
	repository := fakeModuleRepository{}
	service := newTestModuleService(&repository)

	input := validModuleInput("module-profile")
	input.CommonModuleID = ""
	_, err := service.CreateModule(context.Background(), input)
	if !errors.Is(err, domain.ErrModuleCommonMissing) {
		t.Fatalf("expected missing common module, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestModuleServiceRejectsCommonWithCommonModule(t *testing.T) {
	repository := fakeModuleRepository{}
	service := newTestModuleService(&repository)

	input := validModuleInput("module-common")
	input.Kind = domain.ModuleKindCommon
	input.CommonModuleID = "module-base"
	_, err := service.CreateModule(context.Background(), input)
	if !errors.Is(err, domain.ErrInvalidModuleInput) {
		t.Fatalf("expected invalid module input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestModuleServiceDeletesThroughRepository(t *testing.T) {
	repository := fakeModuleRepository{}
	service := newTestModuleService(&repository)

	if err := service.DeleteModule(context.Background(), " module-profile "); err != nil {
		t.Fatalf("delete module: %v", err)
	}
	if repository.lastDeleteID != "module-profile" {
		t.Fatalf("expected trimmed id, got %q", repository.lastDeleteID)
	}
}

type fakeModuleRepository struct {
	modules         []domain.Module
	module          domain.Module
	createCalls     int
	updateCalls     int
	listCalls       int
	lastCreateInput domain.ModuleInput
	lastUpdateID    string
	lastUpdateInput domain.ModuleInput
	lastDeleteID    string
}

func (r *fakeModuleRepository) ListModules(_ context.Context, filters domain.ModuleListFilters) ([]domain.Module, domain.ModuleSummary, error) {
	r.listCalls++
	result := make([]domain.Module, 0, len(r.modules))
	summary := domain.ModuleSummary{}
	for _, module := range r.modules {
		if filters.Kind != "" && module.Kind != filters.Kind {
			continue
		}
		result = append(result, module)
		summary.Total++
		if module.Kind == domain.ModuleKindCommon {
			summary.Common++
		} else if module.Kind == domain.ModuleKindFeature {
			summary.Feature++
		}
	}
	return result, summary, nil
}

func (r *fakeModuleRepository) GetModule(_ context.Context, _ string) (domain.Module, error) {
	return r.module, nil
}

func (r *fakeModuleRepository) CreateModule(_ context.Context, input domain.ModuleInput) (domain.Module, error) {
	r.createCalls++
	r.lastCreateInput = input
	return testServiceModule(input.ID, input.Kind), nil
}

func (r *fakeModuleRepository) UpdateModule(_ context.Context, id string, input domain.ModuleInput) (domain.Module, error) {
	r.updateCalls++
	r.lastUpdateID = id
	r.lastUpdateInput = input
	return testServiceModule(id, input.Kind), nil
}

func (r *fakeModuleRepository) DeleteModule(_ context.Context, id string) error {
	r.lastDeleteID = id
	return nil
}

func validModuleInput(id string) domain.ModuleInput {
	return domain.ModuleInput{
		ApplicationID:      "app-mi-car",
		CommonModuleID:     "module-common",
		ID:                 id,
		Kind:               domain.ModuleKindFeature,
		Name:               "profile",
		Owner:              "li.ming",
		OwnerAvatarURL:     "https://example.com/li.ming.png",
		OwnerDepartmentIDs: []string{"od-rn-platform"},
		OwnerEmail:         "li.ming@xiaomi.com",
		OwnerUserID:        "ou-li-ming",
		Repository: domain.ModuleRepository{
			ConnectionID:         "code-source-1",
			ExternalRepositoryID: "1001",
		},
	}
}

func newTestModuleService(repository *fakeModuleRepository) *ModuleService {
	return NewModuleService(repository, newFakeModuleCodeSourceStore(), "docker/Dockerfile.rn")
}

type fakeModuleCodeSourceStore struct {
	connection   domain.CodeSourceConnection
	repositories map[string]domain.CodeSourceRepository
}

func newFakeModuleCodeSourceStore() *fakeModuleCodeSourceStore {
	return &fakeModuleCodeSourceStore{
		connection: domain.CodeSourceConnection{
			ID:       "code-source-1",
			Provider: domain.CodeSourceProviderGitLab,
			Status:   domain.CodeSourceConnectionStatusActive,
		},
		repositories: map[string]domain.CodeSourceRepository{
			"1001": {
				ConnectionID:         "code-source-1",
				ExternalRepositoryID: "1001",
				FullName:             "acme/profile",
				WebURL:               "https://gitlab.example.com/acme/profile",
				CloneURL:             "https://gitlab.example.com/acme/profile.git",
				DefaultBranch:        "main",
			},
		},
	}
}

func (s *fakeModuleCodeSourceStore) GetCodeSourceConnection(_ context.Context, id string) (domain.CodeSourceConnection, error) {
	if id != s.connection.ID {
		return domain.CodeSourceConnection{}, domain.ErrCodeSourceConnectionNotFound
	}
	return s.connection, nil
}

func (s *fakeModuleCodeSourceStore) GetCodeSourceRepository(_ context.Context, connectionID string, externalRepositoryID string) (domain.CodeSourceRepository, error) {
	repository, ok := s.repositories[externalRepositoryID]
	if !ok || connectionID != s.connection.ID {
		return domain.CodeSourceRepository{}, domain.ErrCodeSourceRepositoryNotFound
	}
	return repository, nil
}

func testServiceModule(id string, kind domain.ModuleKind) domain.Module {
	commonModuleID := ""
	commonModuleName := ""
	if kind == domain.ModuleKindFeature {
		commonModuleID = "module-common"
		commonModuleName = "common"
	}

	return domain.Module{
		ApplicationID:      "app-mi-car",
		ApplicationName:    "小米汽车 App",
		ApplicationSlug:    "mi-car",
		CommonModuleID:     commonModuleID,
		CommonModuleName:   commonModuleName,
		CodeSourceProvider: domain.CodeSourceProviderGitLab,
		CreatedAt:          time.Date(2026, 6, 9, 14, 32, 18, 0, time.UTC),
		ID:                 id,
		Kind:               kind,
		Name:               "profile",
		Owner:              "li.ming",
		OwnerUserID:        "ou-li-ming",
		Repository: domain.ModuleRepository{
			ConnectionID:         "code-source-1",
			ExternalRepositoryID: "1001",
			FullName:             "acme/profile",
			DefaultBranch:        "main",
		},
		UpdatedAt: time.Date(2026, 6, 9, 14, 40, 18, 0, time.UTC),
	}
}

func TestModuleServiceAcceptsGitLabCIBuildConfig(t *testing.T) {
	repository := fakeModuleRepository{}
	service := NewModuleService(&repository, newFakeModuleCodeSourceStore(), "Dockerfile")

	input := validModuleInput("module-profile")
	input.BuildConfig = domain.ModuleBuildConfig{BuildMode: domain.BuildModeGitLabCI, CIJob: " forge_build "}
	if _, err := service.CreateModule(context.Background(), input); err != nil {
		t.Fatalf("create module: %v", err)
	}

	config := repository.lastCreateInput.BuildConfig
	if config.BuildMode != domain.BuildModeGitLabCI || config.CIJob != "forge_build" || config.CIArtifactPath != domain.DefaultModuleCIArtifactPath {
		t.Fatalf("expected trimmed job and default artifact path, got %#v", config)
	}
}

func TestModuleServiceValidatesGitLabCIBuildConfig(t *testing.T) {
	tests := map[string]domain.ModuleBuildConfig{
		"missing job":             {BuildMode: domain.BuildModeGitLabCI},
		"job with newline":        {BuildMode: domain.BuildModeGitLabCI, CIJob: "build\nbad"},
		"artifact path escapes":   {BuildMode: domain.BuildModeGitLabCI, CIJob: "forge_build", CIArtifactPath: "../dist"},
		"absolute artifact path":  {BuildMode: domain.BuildModeGitLabCI, CIJob: "forge_build", CIArtifactPath: "/dist"},
		"with adapter":            {BuildMode: domain.BuildModeGitLabCI, CIJob: "forge_build", BuildAdapterID: "yrn"},
		"with adapter params":     {BuildMode: domain.BuildModeGitLabCI, CIJob: "forge_build", BuildAdapterParams: map[string]string{"a": "b"}},
		"dockerfile mode with ci": {BuildMode: domain.BuildModeDockerfile, CIJob: "forge_build"},
		"adapter mode with ci":    {BuildMode: domain.BuildModeAdapter, BuildAdapterID: "yrn", CIArtifactPath: "dist"},
	}
	for name, config := range tests {
		t.Run(name, func(t *testing.T) {
			repository := fakeModuleRepository{}
			service := newTestModuleService(&repository)

			input := validModuleInput("module-profile")
			input.BuildConfig = config
			if _, err := service.CreateModule(context.Background(), input); !errors.Is(err, domain.ErrInvalidModuleInput) {
				t.Fatalf("expected invalid module input, got %v", err)
			}
			if repository.createCalls != 0 {
				t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
			}
		})
	}
}

func TestModuleServiceRejectsGitLabCIOnOtherProviders(t *testing.T) {
	repository := fakeModuleRepository{}
	store := newFakeModuleCodeSourceStore()
	store.connection.Provider = domain.CodeSourceProviderGitHub
	service := NewModuleService(&repository, store, "Dockerfile")

	input := validModuleInput("module-profile")
	input.BuildConfig = domain.ModuleBuildConfig{BuildMode: domain.BuildModeGitLabCI, CIJob: "forge_build"}
	if _, err := service.CreateModule(context.Background(), input); !errors.Is(err, domain.ErrInvalidModuleInput) {
		t.Fatalf("expected invalid module input for a GitHub connection, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}
