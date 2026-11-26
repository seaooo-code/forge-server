package service

import (
	"context"
	"errors"
	"strings"
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
		ApplicationID:  "app-mi-car",
		CommonModuleID: "module-common",
		ID:             id,
		Kind:           domain.ModuleKindFeature,
		Name:           "profile",
		OwnerUserID:    "ou-li-ming",
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

type recordingBranchSearcher struct {
	repository domain.ModuleRepository
	query      string
	branch     string
	compared   []string
}

func (s *recordingBranchSearcher) CompareCommits(_ context.Context, repository domain.ModuleRepository, from string, to string) (domain.CommitComparison, error) {
	s.repository = repository
	s.compared = []string{from, to}
	return domain.CommitComparison{Status: domain.CommitComparisonBehind}, nil
}

func (s *recordingBranchSearcher) ResolveCommit(_ context.Context, repository domain.ModuleRepository, branch string) (domain.BranchCommit, error) {
	s.repository = repository
	s.branch = branch
	return domain.BranchCommit{Branch: branch, SHA: "abc"}, nil
}

func (s *recordingBranchSearcher) SearchBranches(_ context.Context, repository domain.ModuleRepository, query string) ([]string, error) {
	s.repository = repository
	s.query = query
	return []string{"main"}, nil
}

func TestModuleServiceSearchBranchesUsesModuleRepository(t *testing.T) {
	repository := fakeModuleRepository{module: testServiceModule("module-home", domain.ModuleKindFeature)}
	searcher := &recordingBranchSearcher{}
	service := NewModuleService(&repository, newFakeModuleCodeSourceStore(), "Dockerfile").WithBranchSearcher(searcher)

	branches, err := service.SearchBranches(context.Background(), "module-home", "ma")
	if err != nil {
		t.Fatalf("search branches: %v", err)
	}
	if len(branches) != 1 || searcher.query != "ma" || searcher.repository != repository.module.Repository {
		t.Fatalf("unexpected search: %#v %+v", branches, searcher)
	}
	if _, err := NewModuleService(&repository, newFakeModuleCodeSourceStore(), "Dockerfile").SearchBranches(context.Background(), "module-home", ""); !errors.Is(err, domain.ErrCodeSourceProviderNotFound) {
		t.Fatalf("expected provider not found without searcher, got %v", err)
	}
}

func TestModuleServiceLatestBranchCommitUsesModuleRepository(t *testing.T) {
	repository := fakeModuleRepository{module: testServiceModule("module-home", domain.ModuleKindFeature)}
	searcher := &recordingBranchSearcher{}
	service := NewModuleService(&repository, newFakeModuleCodeSourceStore(), "Dockerfile").WithBranchSearcher(searcher)

	commit, err := service.LatestBranchCommit(context.Background(), "module-home", "feature/login")
	if err != nil {
		t.Fatalf("latest branch commit: %v", err)
	}
	if commit.SHA != "abc" || searcher.branch != "feature/login" || searcher.repository != repository.module.Repository {
		t.Fatalf("unexpected lookup: %+v %+v", commit, searcher)
	}
	if _, err := NewModuleService(&repository, newFakeModuleCodeSourceStore(), "Dockerfile").LatestBranchCommit(context.Background(), "module-home", "main"); !errors.Is(err, domain.ErrCodeSourceProviderNotFound) {
		t.Fatalf("expected provider not found without searcher, got %v", err)
	}
}

func TestModuleServiceCompareCommitsUsesModuleRepository(t *testing.T) {
	repository := fakeModuleRepository{module: testServiceModule("module-home", domain.ModuleKindFeature)}
	searcher := &recordingBranchSearcher{}
	service := NewModuleService(&repository, newFakeModuleCodeSourceStore(), "Dockerfile").WithBranchSearcher(searcher)

	comparison, err := service.CompareCommits(context.Background(), "module-home", "aaa", "bbb")
	if err != nil || comparison.Status != domain.CommitComparisonBehind || searcher.repository != repository.module.Repository || searcher.compared[1] != "bbb" {
		t.Fatalf("unexpected comparison: %+v %+v %v", comparison, searcher, err)
	}
}

func TestModuleServiceCreatesBuildVariablesWithModule(t *testing.T) {
	repository := fakeModuleRepository{}
	service := newTestModuleService(&repository).WithBuildVariables(NewBuildVariableService(nil, fakeSecretBox{}))

	input := validModuleInput("module-profile")
	input.BuildVariables = []domain.ModuleBuildVariable{
		{Key: " YRN_KIND ", Value: "business", UpdatedByUserID: "ou-li-ming"},
		{Key: "NPM_TOKEN", Value: "secret-token", IsSecret: true, UpdatedByUserID: "ou-li-ming"},
		{Key: "YRN_KIND", Value: "common", EnvironmentCode: "staging", UpdatedByUserID: "ou-li-ming"},
	}
	if _, err := service.CreateModule(context.Background(), input); err != nil {
		t.Fatalf("create module: %v", err)
	}

	saved := repository.lastCreateInput.BuildVariables
	if len(saved) != 3 {
		t.Fatalf("expected 3 build variables passed to the repository, got %#v", saved)
	}
	for _, variable := range saved {
		if variable.ModuleID != "module-profile" || variable.Value != "" || variable.ValueEncrypted == "" {
			t.Fatalf("build variable must be bound to the module and only carry ciphertext, got %#v", variable)
		}
	}
	if saved[0].Key != "YRN_KIND" || saved[0].ValueEncrypted != "enc:business" {
		t.Fatalf("expected trimmed key and encrypted value, got %#v", saved[0])
	}
	if !saved[1].IsSecret || saved[1].ValueEncrypted != "enc:secret-token" {
		t.Fatalf("expected encrypted secret, got %#v", saved[1])
	}
	if saved[2].EnvironmentCode != "staging" {
		t.Fatalf("expected environment override to be kept, got %#v", saved[2])
	}
}

func TestModuleServiceRejectsInvalidBuildVariablesOnCreate(t *testing.T) {
	variable := func(key string, value string, environment string, secret bool) domain.ModuleBuildVariable {
		return domain.ModuleBuildVariable{Key: key, Value: value, EnvironmentCode: environment, IsSecret: secret, UpdatedByUserID: "ou-li-ming"}
	}
	tooMany := make([]domain.ModuleBuildVariable, 0, maxModuleCreateBuildVariables+1)
	for index := 0; index <= maxModuleCreateBuildVariables; index++ {
		tooMany = append(tooMany, variable("VAR_"+string(rune('A'+index%26))+strings.Repeat("X", index/26), "v", "", false))
	}
	tests := map[string][]domain.ModuleBuildVariable{
		"invalid key":          {variable("lower_case", "v", "", false)},
		"reserved prefix":      {variable("FORGE_BRANCH", "v", "", false)},
		"empty secret":         {variable("NPM_TOKEN", "", "", true)},
		"duplicate key":        {variable("YRN_KIND", "a", "", false), variable("YRN_KIND", "b", "", false)},
		"duplicate env key":    {variable("YRN_KIND", "a", "qa", false), variable("YRN_KIND", "b", " qa ", false)},
		"too many variables":   tooMany,
		"missing updated user": {{Key: "YRN_KIND", Value: "a"}},
	}
	for name, variables := range tests {
		t.Run(name, func(t *testing.T) {
			repository := fakeModuleRepository{}
			service := newTestModuleService(&repository).WithBuildVariables(NewBuildVariableService(nil, fakeSecretBox{}))
			input := validModuleInput("module-profile")
			input.BuildVariables = variables
			if _, err := service.CreateModule(context.Background(), input); !errors.Is(err, domain.ErrInvalidBuildVariableInput) {
				t.Fatalf("expected invalid build variable input, got %v", err)
			}
			if repository.createCalls != 0 {
				t.Fatal("module must not be created when its build variables are invalid")
			}
		})
	}
}

func TestModuleServiceAllowsSameKeyForDifferentEnvironments(t *testing.T) {
	repository := fakeModuleRepository{}
	service := newTestModuleService(&repository).WithBuildVariables(NewBuildVariableService(nil, fakeSecretBox{}))
	input := validModuleInput("module-profile")
	input.BuildVariables = []domain.ModuleBuildVariable{
		{Key: "YRN_KIND", Value: "a", UpdatedByUserID: "ou-li-ming"},
		{Key: "YRN_KIND", Value: "b", EnvironmentCode: "qa", UpdatedByUserID: "ou-li-ming"},
	}
	if _, err := service.CreateModule(context.Background(), input); err != nil {
		t.Fatalf("expected one value per environment to be allowed, got %v", err)
	}
}

func TestModuleServiceIgnoresBuildVariablesOnUpdate(t *testing.T) {
	repository := fakeModuleRepository{}
	service := newTestModuleService(&repository).WithBuildVariables(NewBuildVariableService(nil, fakeSecretBox{}))
	input := validModuleInput("")
	input.BuildVariables = []domain.ModuleBuildVariable{{Key: "YRN_KIND", Value: "a", UpdatedByUserID: "ou-li-ming"}}
	if _, err := service.UpdateModule(context.Background(), "module-profile", input); err != nil {
		t.Fatalf("update module: %v", err)
	}
	if len(repository.lastUpdateInput.BuildVariables) != 0 {
		t.Fatalf("update must not pass build variables to the repository, got %#v", repository.lastUpdateInput.BuildVariables)
	}
}
