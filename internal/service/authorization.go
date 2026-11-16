package service

import (
	"context"
	"strings"

	"forge-server/internal/domain"
)

type AuthorizationRepository interface {
	ApplicationIDByBuildID(ctx context.Context, buildID string) (string, error)
	ApplicationIDByModuleID(ctx context.Context, moduleID string) (string, error)
	ApplicationIDByReleaseID(ctx context.Context, releaseID string) (string, error)
	ApplicationIDBySlug(ctx context.Context, slug string) (string, error)
	GetApplicationRole(ctx context.Context, userID string, applicationID string) (domain.ApplicationMemberRole, error)
	ListApplicationRoles(ctx context.Context, userID string) ([]domain.ApplicationMemberRole, error)
	ListGlobalRoles(ctx context.Context, userID string) ([]domain.GlobalRole, error)
}

type AuthorizationService struct {
	repository AuthorizationRepository
}

func NewAuthorizationService(repository AuthorizationRepository) *AuthorizationService {
	return &AuthorizationService{repository: repository}
}

func (s *AuthorizationService) ListGlobalRoles(ctx context.Context, userID string) ([]domain.GlobalRole, error) {
	if s == nil || s.repository == nil {
		return nil, nil
	}
	return s.repository.ListGlobalRoles(ctx, strings.TrimSpace(userID))
}

func (s *AuthorizationService) GlobalCapabilities(ctx context.Context, userID string) (domain.GlobalCapabilities, []domain.GlobalRole, error) {
	roles, err := s.ListGlobalRoles(ctx, userID)
	if err != nil {
		return domain.GlobalCapabilities{}, nil, err
	}
	return domain.GlobalCapabilitiesForRoles(roles), roles, nil
}

func (s *AuthorizationService) ApplicationCapabilities(ctx context.Context, userID string, applicationID string) (domain.ApplicationCapabilities, error) {
	if s == nil || s.repository == nil {
		return domain.ApplicationCapabilities{}, nil
	}

	roles, err := s.repository.ListGlobalRoles(ctx, strings.TrimSpace(userID))
	if err != nil {
		return domain.ApplicationCapabilities{}, err
	}
	appRole, err := s.repository.GetApplicationRole(ctx, strings.TrimSpace(userID), strings.TrimSpace(applicationID))
	if err != nil {
		return domain.ApplicationCapabilities{}, err
	}
	return domain.ApplicationCapabilitiesFor(roles, appRole), nil
}

func (s *AuthorizationService) Authorize(ctx context.Context, userID string, action domain.AuthorizationAction, resource domain.AuthorizationResource) error {
	if s == nil || s.repository == nil {
		return domain.ErrForbidden
	}

	normalizedUserID := strings.TrimSpace(userID)
	if normalizedUserID == "" {
		return domain.ErrForbidden
	}

	roles, err := s.repository.ListGlobalRoles(ctx, normalizedUserID)
	if err != nil {
		return err
	}
	if globalActionAllowed(roles, action) {
		return nil
	}

	applicationID, err := s.resolveApplicationID(ctx, resource)
	if err != nil {
		return err
	}
	if applicationID == "" {
		// 平台级资源（例如代码源连接）没有所属应用时，只要用户在任一应用里具备该动作即可。
		if action != domain.ActionCodeSourceUse {
			return domain.ErrForbidden
		}
		appRoles, err := s.repository.ListApplicationRoles(ctx, normalizedUserID)
		if err != nil {
			return err
		}
		for _, appRole := range appRoles {
			if applicationActionAllowed(appRole, action) {
				return nil
			}
		}
		return domain.ErrForbidden
	}

	appRole, err := s.repository.GetApplicationRole(ctx, normalizedUserID, applicationID)
	if err != nil {
		return err
	}
	if applicationActionAllowed(appRole, action) {
		return nil
	}
	return domain.ErrForbidden
}

func (s *AuthorizationService) resolveApplicationID(ctx context.Context, resource domain.AuthorizationResource) (string, error) {
	if applicationID := strings.TrimSpace(resource.ApplicationID); applicationID != "" {
		return applicationID, nil
	}
	if slug := strings.TrimSpace(resource.ApplicationSlug); slug != "" {
		return s.repository.ApplicationIDBySlug(ctx, slug)
	}
	if moduleID := strings.TrimSpace(resource.ModuleID); moduleID != "" {
		return s.repository.ApplicationIDByModuleID(ctx, moduleID)
	}
	if buildID := strings.TrimSpace(resource.BuildRecordID); buildID != "" {
		return s.repository.ApplicationIDByBuildID(ctx, buildID)
	}
	if releaseID := strings.TrimSpace(resource.ReleaseID); releaseID != "" {
		return s.repository.ApplicationIDByReleaseID(ctx, releaseID)
	}
	return "", nil
}

func globalActionAllowed(roles []domain.GlobalRole, action domain.AuthorizationAction) bool {
	for _, role := range roles {
		switch role {
		case domain.GlobalRoleSuperAdmin:
			return true
		case domain.GlobalRolePlatformAdmin:
			switch action {
			case domain.ActionApplicationCreate,
				domain.ActionApplicationUpdate,
				domain.ActionApplicationDelete,
				domain.ActionApplicationStatus,
				domain.ActionApplicationMemberManage,
				domain.ActionModuleCreate,
				domain.ActionModuleUpdate,
				domain.ActionModuleDelete,
				domain.ActionBuildCreate,
				domain.ActionCodeSourceManage,
				domain.ActionCodeSourceUse,
				domain.ActionStorageManage,
				domain.ActionEnvironmentManage,
				domain.ActionAccessTokenManage:
				return true
			}
		case domain.GlobalRoleReleaseAdmin:
			switch action {
			case domain.ActionBuildCreate, domain.ActionReleaseCreate, domain.ActionReleaseRollback:
				return true
			}
		}
	}
	return false
}

func applicationActionAllowed(role domain.ApplicationMemberRole, action domain.AuthorizationAction) bool {
	switch role {
	case domain.ApplicationMemberRoleOwner:
		switch action {
		case domain.ActionApplicationUpdate,
			domain.ActionApplicationStatus,
			domain.ActionApplicationMemberManage,
			domain.ActionModuleCreate,
			domain.ActionModuleUpdate,
			domain.ActionModuleDelete,
			domain.ActionCodeSourceUse,
			domain.ActionBuildCreate,
			domain.ActionReleaseCreate,
			domain.ActionReleaseRollback:
			return true
		}
	case domain.ApplicationMemberRoleDeveloper, domain.ApplicationMemberRoleQA:
		return action == domain.ActionBuildCreate
	case domain.ApplicationMemberRoleRelease:
		return action == domain.ActionReleaseCreate || action == domain.ActionReleaseRollback
	}
	return false
}
