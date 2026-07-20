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
	service := NewModuleService(&repository)

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
	service := NewModuleService(&repository)

	input := validModuleInput("module-profile")
	input.Repository = domain.ModuleRepositoryBinding{}
	_, err := service.CreateModule(context.Background(), input)
	if !errors.Is(err, domain.ErrInvalidModuleInput) {
		t.Fatalf("expected invalid module input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestModuleServiceRejectsUnsupportedRepositoryProvider(t *testing.T) {
	repository := fakeModuleRepository{}
	service := NewModuleService(&repository)

	input := validModuleInput("module-profile")
	input.Repository.Provider = domain.CodeSourceProvider("gitlab")
	_, err := service.CreateModule(context.Background(), input)
	if !errors.Is(err, domain.ErrInvalidModuleInput) {
		t.Fatalf("expected invalid module input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestModuleServiceNormalizesCreateInput(t *testing.T) {
	repository := fakeModuleRepository{}
	service := NewModuleService(&repository)

	input := validModuleInput(" module-profile ")
	input.ID = "module-profile"
	input.Name = " profile "
	input.ApplicationID = " app-mi-car "
	input.OwnerUserID = " ou-li-ming "
	input.Owner = " li.ming "
	input.Repository.FullName = " mi-car/profile "

	_, err := service.CreateModule(context.Background(), input)
	if err != nil {
		t.Fatalf("create module: %v", err)
	}

	if repository.lastCreateInput.Name != "profile" ||
		repository.lastCreateInput.ApplicationID != "app-mi-car" ||
		repository.lastCreateInput.OwnerUserID != "ou-li-ming" ||
		repository.lastCreateInput.Repository.FullName != "mi-car/profile" {
		t.Fatalf("unexpected normalized input: %#v", repository.lastCreateInput)
	}
}

func TestModuleServiceRejectsIDChangeOnUpdate(t *testing.T) {
	repository := fakeModuleRepository{}
	service := NewModuleService(&repository)

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
	service := NewModuleService(&repository)

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
	service := NewModuleService(&repository)

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
	service := NewModuleService(&repository)

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
	service := NewModuleService(&repository)

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
		Repository: domain.ModuleRepositoryBinding{
			Provider:       domain.CodeSourceProviderGitHub,
			RepositoryID:   1001,
			InstallationID: 2001,
			FullName:       "mi-car/profile",
			HTMLURL:        "https://github.com/mi-car/profile",
			CloneURL:       "https://github.com/mi-car/profile.git",
			SSHURL:         "git@github.com:mi-car/profile.git",
			DefaultBranch:  "main",
		},
	}
}

func testServiceModule(id string, kind domain.ModuleKind) domain.Module {
	commonModuleID := ""
	commonModuleName := ""
	if kind == domain.ModuleKindFeature {
		commonModuleID = "module-common"
		commonModuleName = "common"
	}

	return domain.Module{
		ApplicationID:            "app-mi-car",
		ApplicationName:          "小米汽车 App",
		ApplicationSlug:          "mi-car",
		CommonModuleID:           commonModuleID,
		CommonModuleName:         commonModuleName,
		CreatedAt:                time.Date(2026, 6, 9, 14, 32, 18, 0, time.UTC),
		DefaultBranch:            "main",
		ID:                       id,
		Kind:                     kind,
		Name:                     "profile",
		Owner:                    "li.ming",
		OwnerUserID:              "ou-li-ming",
		RepositoryFullName:       "mi-car/profile",
		RepositoryID:             1001,
		RepositoryInstallationID: 2001,
		RepositoryProvider:       domain.CodeSourceProviderGitHub,
		UpdatedAt:                time.Date(2026, 6, 9, 14, 40, 18, 0, time.UTC),
	}
}
