package httpserver

import (
	"context"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type fakeAuthorizationService struct {
	err          error
	capabilities domain.ApplicationCapabilities
	roles        []domain.Role
}

func newAuthorizedTestServer(deps Dependencies) *echo.Echo {
	return newAuthorizedTestServerWithConfig(config.Config{}, deps)
}

func newAuthorizedTestServerWithConfig(cfg config.Config, deps Dependencies) *echo.Echo {
	if deps.FeishuAuthService == nil {
		deps.FeishuAuthService = testConsoleAuthService()
	}
	if deps.AuthorizationService == nil {
		deps.AuthorizationService = fakeAuthorizationService{
			capabilities: domain.ApplicationCapabilitiesFor(
				[]domain.Role{domain.RoleSuperAdmin},
				domain.ApplicationMemberRoleOwner,
			),
			roles: []domain.Role{domain.RoleSuperAdmin},
		}
	}
	return New(cfg, deps)
}

func testConsoleAuthService() *fakeFeishuAuthService {
	return &fakeFeishuAuthService{result: domain.ConsoleSession{
		ExpiresAt:    time.Now().Add(time.Hour),
		SessionToken: "session-token",
		User: domain.User{
			ID:   "ou-test-user",
			Name: "测试用户",
		},
	}}
}

func (s fakeAuthorizationService) ApplicationCapabilities(context.Context, string, string) (domain.ApplicationCapabilities, error) {
	if s.err != nil {
		return domain.ApplicationCapabilities{}, s.err
	}
	return s.capabilities, nil
}

func (s fakeAuthorizationService) Authorize(context.Context, string, domain.AuthorizationAction, domain.AuthorizationResource) error {
	return s.err
}

func (s fakeAuthorizationService) TenantCapabilities(context.Context, string) (domain.TenantCapabilities, []domain.Role, error) {
	if s.err != nil {
		return domain.TenantCapabilities{}, nil, s.err
	}
	roles := s.roles
	if len(roles) == 0 {
		roles = []domain.Role{domain.RoleSuperAdmin}
	}
	return domain.TenantCapabilitiesForRoles(roles), roles, nil
}
