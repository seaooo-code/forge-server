package service

import (
	"context"
	"strings"

	"forge-server/internal/domain"
)

type ModuleRepository interface {
	CreateModule(ctx context.Context, input domain.ModuleInput) (domain.Module, error)
	DeleteModule(ctx context.Context, id string) error
	GetModule(ctx context.Context, id string) (domain.Module, error)
	ListModules(ctx context.Context, filters domain.ModuleListFilters) ([]domain.Module, error)
	UpdateModule(ctx context.Context, id string, input domain.ModuleInput) (domain.Module, error)
}

type ModuleService struct {
	repository ModuleRepository
}

func NewModuleService(repository ModuleRepository) *ModuleService {
	return &ModuleService{repository: repository}
}

func (s *ModuleService) ListModules(ctx context.Context, filters domain.ModuleListFilters) ([]domain.Module, domain.ModuleSummary, error) {
	normalized, err := normalizeModuleListFilters(filters)
	if err != nil {
		return nil, domain.ModuleSummary{}, err
	}

	modules, err := s.repository.ListModules(ctx, normalized)
	if err != nil {
		return nil, domain.ModuleSummary{}, err
	}

	return modules, summarizeModules(modules), nil
}

func (s *ModuleService) GetModule(ctx context.Context, id string) (domain.Module, error) {
	normalizedID := strings.TrimSpace(id)
	if !domain.IsValidModuleID(normalizedID) {
		return domain.Module{}, domain.ErrModuleNotFound
	}
	return s.repository.GetModule(ctx, normalizedID)
}

func (s *ModuleService) CreateModule(ctx context.Context, input domain.ModuleInput) (domain.Module, error) {
	normalized, err := validateModuleInput(input, true)
	if err != nil {
		return domain.Module{}, err
	}
	return s.repository.CreateModule(ctx, normalized)
}

func (s *ModuleService) UpdateModule(ctx context.Context, id string, input domain.ModuleInput) (domain.Module, error) {
	normalizedID := strings.TrimSpace(id)
	if !domain.IsValidModuleID(normalizedID) {
		return domain.Module{}, domain.ErrModuleNotFound
	}

	normalized, err := validateModuleInput(input, false)
	if err != nil {
		return domain.Module{}, err
	}
	return s.repository.UpdateModule(ctx, normalizedID, normalized)
}

func (s *ModuleService) DeleteModule(ctx context.Context, id string) error {
	normalizedID := strings.TrimSpace(id)
	if !domain.IsValidModuleID(normalizedID) {
		return domain.ErrModuleNotFound
	}
	return s.repository.DeleteModule(ctx, normalizedID)
}

func validateModuleInput(input domain.ModuleInput, requireID bool) (domain.ModuleInput, error) {
	owner := normalizeUserInput(domain.UserInput{
		ID:            input.OwnerUserID,
		Name:          input.Owner,
		Email:         input.OwnerEmail,
		AvatarURL:     input.OwnerAvatarURL,
		DepartmentIDs: input.OwnerDepartmentIDs,
		FeishuUnionID: input.OwnerFeishuUnionID,
		FeishuOpenID:  input.OwnerFeishuOpenID,
		FeishuUserID:  input.OwnerFeishuUserID,
	})

	normalized := domain.ModuleInput{
		ApplicationID:      strings.TrimSpace(input.ApplicationID),
		ID:                 strings.TrimSpace(input.ID),
		Kind:               input.Kind,
		Name:               strings.TrimSpace(input.Name),
		Owner:              owner.Name,
		OwnerAvatarURL:     owner.AvatarURL,
		OwnerDepartmentIDs: owner.DepartmentIDs,
		OwnerEmail:         owner.Email,
		OwnerFeishuOpenID:  owner.FeishuOpenID,
		OwnerFeishuUnionID: owner.FeishuUnionID,
		OwnerFeishuUserID:  owner.FeishuUserID,
		OwnerUserID:        owner.ID,
		Repository: domain.ModuleRepositoryBinding{
			Provider:       input.Repository.Provider,
			RepositoryID:   input.Repository.RepositoryID,
			InstallationID: input.Repository.InstallationID,
			FullName:       strings.TrimSpace(input.Repository.FullName),
			HTMLURL:        strings.TrimSpace(input.Repository.HTMLURL),
			CloneURL:       strings.TrimSpace(input.Repository.CloneURL),
			SSHURL:         strings.TrimSpace(input.Repository.SSHURL),
			DefaultBranch:  strings.TrimSpace(input.Repository.DefaultBranch),
		},
		Status: input.Status,
	}

	if requireID && !domain.IsValidModuleID(normalized.ID) {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	if !requireID && normalized.ID != "" {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	if normalized.ApplicationID == "" ||
		normalized.Name == "" ||
		normalized.OwnerUserID == "" ||
		normalized.Owner == "" ||
		normalized.Repository.Provider == "" ||
		normalized.Repository.RepositoryID <= 0 ||
		normalized.Repository.InstallationID <= 0 ||
		normalized.Repository.FullName == "" {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	if !domain.IsValidModuleKind(normalized.Kind) {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	if normalized.Status == "" {
		normalized.Status = domain.ModuleStatusActive
	}
	if !domain.IsValidModuleStatus(normalized.Status) {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	if normalized.Repository.Provider != domain.CodeSourceProviderGitHub {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}

	return normalized, nil
}

func normalizeModuleListFilters(filters domain.ModuleListFilters) (domain.ModuleListFilters, error) {
	normalized := domain.ModuleListFilters{
		Kind:    filters.Kind,
		Keyword: strings.TrimSpace(filters.Keyword),
		Owner:   strings.TrimSpace(filters.Owner),
		Status:  filters.Status,
	}
	if normalized.Kind != "" && !domain.IsValidModuleKind(normalized.Kind) {
		return domain.ModuleListFilters{}, domain.ErrInvalidModuleInput
	}
	if normalized.Status != "" && !domain.IsValidModuleStatus(normalized.Status) {
		return domain.ModuleListFilters{}, domain.ErrInvalidModuleInput
	}
	return normalized, nil
}

func summarizeModules(modules []domain.Module) domain.ModuleSummary {
	summary := domain.ModuleSummary{Total: len(modules)}
	for _, module := range modules {
		switch module.Kind {
		case domain.ModuleKindCommon:
			summary.Common++
		case domain.ModuleKindFeature:
			summary.Feature++
		}
		switch module.Status {
		case domain.ModuleStatusActive:
			summary.Active++
		case domain.ModuleStatusFrozen:
			summary.Frozen++
		}
	}
	return summary
}
