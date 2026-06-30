package service

import (
	"context"
	"errors"
	"testing"

	"forge-server/internal/domain"
)

func TestAuthorizationServiceRoleMatrix(t *testing.T) {
	tests := []struct {
		name    string
		roles   []domain.GlobalRole
		appRole domain.ApplicationMemberRole
		action  domain.AuthorizationAction
		wantErr error
	}{
		{
			name:    "super admin can manage members",
			roles:   []domain.GlobalRole{domain.GlobalRoleSuperAdmin},
			action:  domain.ActionApplicationMemberManage,
			wantErr: nil,
		},
		{
			name:    "plain user cannot create application",
			action:  domain.ActionApplicationCreate,
			wantErr: domain.ErrForbidden,
		},
		{
			name:    "owner can manage application members",
			appRole: domain.ApplicationMemberRoleOwner,
			action:  domain.ActionApplicationMemberManage,
			wantErr: nil,
		},
		{
			name:    "developer can create build",
			appRole: domain.ApplicationMemberRoleDeveloper,
			action:  domain.ActionBuildCreate,
			wantErr: nil,
		},
		{
			name:    "developer cannot manage members",
			appRole: domain.ApplicationMemberRoleDeveloper,
			action:  domain.ActionApplicationMemberManage,
			wantErr: domain.ErrForbidden,
		},
		{
			name:    "release member can create release",
			appRole: domain.ApplicationMemberRoleRelease,
			action:  domain.ActionReleaseCreate,
			wantErr: nil,
		},
		{
			name:    "qa can create build",
			appRole: domain.ApplicationMemberRoleQA,
			action:  domain.ActionBuildCreate,
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := fakeAuthorizationRepository{
				applicationID: "app-mi-car",
				appRole:       tt.appRole,
				globalRoles:   tt.roles,
			}
			service := NewAuthorizationService(&repository)

			err := service.Authorize(context.Background(), "ou-user-1", tt.action, domain.AuthorizationResource{ApplicationID: "app-mi-car"})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestAuthorizationServiceResolvesBuildApplication(t *testing.T) {
	repository := fakeAuthorizationRepository{
		applicationID: "app-mi-car",
		appRole:       domain.ApplicationMemberRoleDeveloper,
	}
	service := NewAuthorizationService(&repository)

	err := service.Authorize(context.Background(), "ou-user-1", domain.ActionBuildCreate, domain.AuthorizationResource{BuildRecordID: "build-1"})
	if err != nil {
		t.Fatalf("authorize build create: %v", err)
	}
	if repository.lastBuildID != "build-1" {
		t.Fatalf("expected build id resolution, got %q", repository.lastBuildID)
	}
}

func TestAuthorizationServiceApplicationCapabilities(t *testing.T) {
	repository := fakeAuthorizationRepository{
		applicationID: "app-mi-car",
		appRole:       domain.ApplicationMemberRoleOwner,
	}
	service := NewAuthorizationService(&repository)

	capabilities, err := service.ApplicationCapabilities(context.Background(), "ou-user-1", "app-mi-car")
	if err != nil {
		t.Fatalf("application capabilities: %v", err)
	}
	if !capabilities.CanManageMembers || !capabilities.CanCreateBuild || !capabilities.CanRelease {
		t.Fatalf("expected owner capabilities, got %#v", capabilities)
	}
}

type fakeAuthorizationRepository struct {
	applicationID string
	appRole       domain.ApplicationMemberRole
	globalRoles   []domain.GlobalRole
	lastBuildID   string
}

func (r *fakeAuthorizationRepository) ApplicationIDBySlug(context.Context, string) (string, error) {
	return r.applicationID, nil
}

func (r *fakeAuthorizationRepository) ApplicationIDByModuleID(context.Context, string) (string, error) {
	return r.applicationID, nil
}

func (r *fakeAuthorizationRepository) ApplicationIDByBuildID(_ context.Context, buildID string) (string, error) {
	r.lastBuildID = buildID
	return r.applicationID, nil
}

func (r *fakeAuthorizationRepository) ApplicationIDByReleaseID(context.Context, string) (string, error) {
	return r.applicationID, nil
}

func (r *fakeAuthorizationRepository) GetApplicationRole(context.Context, string, string) (domain.ApplicationMemberRole, error) {
	return r.appRole, nil
}

func (r *fakeAuthorizationRepository) ListGlobalRoles(context.Context, string) ([]domain.GlobalRole, error) {
	return r.globalRoles, nil
}
