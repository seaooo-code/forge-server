package domain

import "errors"

var ErrForbidden = errors.New("forbidden")

type GlobalRole string

const (
	GlobalRoleSuperAdmin    GlobalRole = "SuperAdmin"
	GlobalRolePlatformAdmin GlobalRole = "PlatformAdmin"
	GlobalRoleReleaseAdmin  GlobalRole = "ReleaseAdmin"
	GlobalRoleAuditor       GlobalRole = "Auditor"
)

type AuthorizationAction string

const (
	ActionApplicationCreate       AuthorizationAction = "application:create"
	ActionApplicationUpdate       AuthorizationAction = "application:update"
	ActionApplicationDelete       AuthorizationAction = "application:delete"
	ActionApplicationStatus       AuthorizationAction = "application:status"
	ActionApplicationMemberManage AuthorizationAction = "application:member:manage"
	ActionModuleCreate            AuthorizationAction = "module:create"
	ActionModuleUpdate            AuthorizationAction = "module:update"
	ActionModuleDelete            AuthorizationAction = "module:delete"
	ActionBuildCreate             AuthorizationAction = "build:create"
	ActionReleaseCreate           AuthorizationAction = "release:create"
	ActionReleaseRollback         AuthorizationAction = "release:rollback"
	ActionCodeSourceManage        AuthorizationAction = "code_source:manage"
	ActionStorageManage           AuthorizationAction = "storage:manage"
	ActionEnvironmentManage       AuthorizationAction = "environment:manage"
	ActionAccessTokenManage       AuthorizationAction = "access_token:manage"
)

type AuthorizationResource struct {
	ApplicationID   string
	ApplicationSlug string
	ModuleID        string
	BuildRecordID   string
	ReleaseID       string
}

type GlobalCapabilities struct {
	CanCreateApplication  bool `json:"canCreateApplication"`
	CanManageApplications bool `json:"canManageApplications"`
	CanManageModules      bool `json:"canManageModules"`
	CanCreateBuild        bool `json:"canCreateBuild"`
	CanCreateRelease      bool `json:"canCreateRelease"`
	CanManageCodeSources  bool `json:"canManageCodeSources"`
	CanManageStorage      bool `json:"canManageStorage"`
	CanManageEnvironments bool `json:"canManageEnvironments"`
	CanManageAccessTokens bool `json:"canManageAccessTokens"`
}

type ApplicationCapabilities struct {
	CanEditApplication bool `json:"canEditApplication"`
	CanManageMembers   bool `json:"canManageMembers"`
	CanUpdateStatus    bool `json:"canUpdateStatus"`
	CanCreateBuild     bool `json:"canCreateBuild"`
	CanRelease         bool `json:"canRelease"`
}

func IsValidGlobalRole(role GlobalRole) bool {
	switch role {
	case GlobalRoleSuperAdmin, GlobalRolePlatformAdmin, GlobalRoleReleaseAdmin, GlobalRoleAuditor:
		return true
	default:
		return false
	}
}

func GlobalCapabilitiesForRoles(roles []GlobalRole) GlobalCapabilities {
	var capabilities GlobalCapabilities
	for _, role := range roles {
		switch role {
		case GlobalRoleSuperAdmin:
			return GlobalCapabilities{
				CanCreateApplication:  true,
				CanManageApplications: true,
				CanManageModules:      true,
				CanCreateBuild:        true,
				CanCreateRelease:      true,
				CanManageCodeSources:  true,
				CanManageStorage:      true,
				CanManageEnvironments: true,
				CanManageAccessTokens: true,
			}
		case GlobalRolePlatformAdmin:
			capabilities.CanCreateApplication = true
			capabilities.CanManageApplications = true
			capabilities.CanManageModules = true
			capabilities.CanCreateBuild = true
			capabilities.CanManageCodeSources = true
			capabilities.CanManageStorage = true
			capabilities.CanManageEnvironments = true
			capabilities.CanManageAccessTokens = true
		case GlobalRoleReleaseAdmin:
			capabilities.CanCreateBuild = true
			capabilities.CanCreateRelease = true
		}
	}
	return capabilities
}

func ApplicationCapabilitiesFor(globalRoles []GlobalRole, appRole ApplicationMemberRole) ApplicationCapabilities {
	global := GlobalCapabilitiesForRoles(globalRoles)
	capabilities := ApplicationCapabilities{
		CanEditApplication: global.CanManageApplications,
		CanManageMembers:   global.CanManageApplications,
		CanUpdateStatus:    global.CanManageApplications,
		CanCreateBuild:     global.CanCreateBuild,
		CanRelease:         global.CanCreateRelease,
	}

	switch appRole {
	case ApplicationMemberRoleOwner:
		capabilities.CanEditApplication = true
		capabilities.CanManageMembers = true
		capabilities.CanUpdateStatus = true
		capabilities.CanCreateBuild = true
		capabilities.CanRelease = true
	case ApplicationMemberRoleDeveloper, ApplicationMemberRoleQA:
		capabilities.CanCreateBuild = true
	case ApplicationMemberRoleRelease:
		capabilities.CanRelease = true
	}
	return capabilities
}
