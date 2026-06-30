package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrApplicationNotFound     = errors.New("application not found")
	ErrApplicationSlugConflict = errors.New("application slug already exists")
	ErrApplicationInUse        = errors.New("application is referenced by modules")
	ErrApplicationLastOwner    = errors.New("application last owner")
	ErrInvalidApplicationInput = errors.New("invalid application request")
)

type ApplicationStatus string

const (
	ApplicationStatusRunning     ApplicationStatus = "running"
	ApplicationStatusMaintenance ApplicationStatus = "maintenance"
	ApplicationStatusOffline     ApplicationStatus = "offline"
)

type ApplicationEnvironmentStatus string

const (
	ApplicationEnvironmentStatusHealthy     ApplicationEnvironmentStatus = "healthy"
	ApplicationEnvironmentStatusGray        ApplicationEnvironmentStatus = "gray"
	ApplicationEnvironmentStatusMaintenance ApplicationEnvironmentStatus = "maintenance"
)

type ApplicationMemberRole string

const (
	ApplicationMemberRoleOwner     ApplicationMemberRole = "Owner"
	ApplicationMemberRoleDeveloper ApplicationMemberRole = "Developer"
	ApplicationMemberRoleQA        ApplicationMemberRole = "QA"
	ApplicationMemberRoleRelease   ApplicationMemberRole = "Release"
)

type Application struct {
	ID                 string
	Name               string
	Slug               string
	Platform           string
	OwnerUserID        string
	Owner              string
	OwnerEmail         string
	OwnerAvatarURL     string
	OwnerDepartmentIDs []string
	ModuleCount        int
	EnvironmentCount   int
	CreatedAt          time.Time
	Status             ApplicationStatus
}

type ApplicationSummary struct {
	Total       int
	Running     int
	Maintenance int
	Offline     int
}

type ApplicationListFilters struct {
	Platform string
	Status   ApplicationStatus
	Owner    string
	Keyword  string
}

type ApplicationDetailMetric struct {
	ID     string
	Label  string
	Value  string
	Detail string
	Tone   string
}

type ApplicationEnvironmentBinding struct {
	ID          string
	Environment string
	Traffic     string
	UpdatedAt   time.Time
	Status      ApplicationEnvironmentStatus
}

type ApplicationMember struct {
	AvatarURL string
	Email     string
	ID        string
	Name      string
	Role      ApplicationMemberRole
	Team      string
	UserID    string
}

type ApplicationModule struct {
	CommonModuleID   string
	CommonModuleName string
	ID               string
	Kind             string
	Name             string
	Owner            string
	Repository       string
}

type ClientIntegrationField struct {
	Name        string
	Description string
	Required    bool
	Type        string
	Example     string
}

type ClientIntegrationEndpoint struct {
	BaseURL        string
	Path           string
	Method         string
	Description    string
	ExampleURL     string
	RequestFields  []ClientIntegrationField
	ResponseFields []ClientIntegrationField
}

type ClientIntegrationInfo struct {
	Endpoint ClientIntegrationEndpoint
}

type ApplicationDetail struct {
	Application        Application
	ID                 string
	AppKey             string
	Slug               string
	Name               string
	Description        string
	OwnerUserID        string
	Owner              string
	OwnerEmail         string
	OwnerAvatarURL     string
	OwnerDepartmentIDs []string
	Metrics            []ApplicationDetailMetric
	Environments       []ApplicationEnvironmentBinding
	Modules            []ApplicationModule
	Members            []ApplicationMember
	Capabilities       ApplicationCapabilities
	ClientIntegration  ClientIntegrationInfo
}

type ApplicationEnvironmentInput struct {
	Environment string
}

type ApplicationMemberInput struct {
	Role   ApplicationMemberRole
	UserID string
}

type ApplicationInput struct {
	Slug               string
	Name               string
	Platform           string
	OwnerUserID        string
	Owner              string
	OwnerEmail         string
	OwnerAvatarURL     string
	OwnerDepartmentIDs []string
	OwnerFeishuUnionID string
	OwnerFeishuOpenID  string
	OwnerFeishuUserID  string
	Description        string
	Status             ApplicationStatus
	Environments       []ApplicationEnvironmentInput
	Members            []ApplicationMemberInput
}

type ApplicationOptionItem struct {
	Label string
	Value string
}

type ApplicationOptions struct {
	ApplicationStatuses []ApplicationOptionItem
	Environments        []ApplicationOptionItem
	Owners              []ApplicationOptionItem
	Platforms           []ApplicationOptionItem
}

type ApplicationDeleteDependency struct {
	ID          string
	Name        string
	Type        string
	Description string
}

type ApplicationDeleteCheck struct {
	Deletable    bool
	Dependencies []ApplicationDeleteDependency
	Message      string
}

func (s ApplicationStatus) Label() string {
	switch s {
	case ApplicationStatusRunning:
		return "运行中"
	case ApplicationStatusMaintenance:
		return "维护中"
	case ApplicationStatusOffline:
		return "已下线"
	default:
		return string(s)
	}
}

func (s ApplicationEnvironmentStatus) Label() string {
	switch s {
	case ApplicationEnvironmentStatusHealthy:
		return "健康"
	case ApplicationEnvironmentStatusGray:
		return "灰度中"
	case ApplicationEnvironmentStatusMaintenance:
		return "维护中"
	default:
		return string(s)
	}
}

func IsValidApplicationSlug(slug string) bool {
	if slug == "" || strings.TrimSpace(slug) != slug {
		return false
	}
	if slug[0] == '-' || slug[len(slug)-1] == '-' {
		return false
	}

	previousDash := false
	for _, char := range slug {
		switch {
		case char >= 'a' && char <= 'z':
			previousDash = false
		case char >= '0' && char <= '9':
			previousDash = false
		case char == '-':
			if previousDash {
				return false
			}
			previousDash = true
		default:
			return false
		}
	}

	return true
}

func IsValidApplicationStatus(status ApplicationStatus) bool {
	switch status {
	case ApplicationStatusRunning, ApplicationStatusMaintenance, ApplicationStatusOffline:
		return true
	default:
		return false
	}
}

func IsValidApplicationMemberRole(role ApplicationMemberRole) bool {
	switch role {
	case ApplicationMemberRoleOwner, ApplicationMemberRoleDeveloper, ApplicationMemberRoleQA, ApplicationMemberRoleRelease:
		return true
	default:
		return false
	}
}
