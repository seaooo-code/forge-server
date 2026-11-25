package domain

import (
	"errors"
	"net/textproto"
	"time"
)

var (
	ErrCodeSourceAppNotFound           = errors.New("code source app not found")
	ErrCodeSourceAuthNotConfigured     = errors.New("code source auth not configured")
	ErrCodeSourceAuthorization         = errors.New("code source authorization failed")
	ErrCodeSourceInstallationNotFound  = errors.New("code source installation not found")
	ErrCodeSourceProviderNotFound      = errors.New("code source provider not found")
	ErrInvalidCodeSourceInput          = errors.New("invalid code source request")
	ErrCodeSourceConnectionNotFound    = errors.New("code source connection not found")
	ErrCodeSourceConnectionConflict    = errors.New("code source connection already exists")
	ErrCodeSourceConnectionInUse       = errors.New("code source connection is referenced by modules")
	ErrCodeSourceConnectionInactive    = errors.New("code source connection is not active")
	ErrCodeSourceCredentialInvalid     = errors.New("code source credential is invalid")
	ErrCodeSourceRepositoryNotFound    = errors.New("code source repository not found")
	ErrCodeSourceBranchNotFound        = errors.New("code source branch not found")
	ErrCodeSourceWebhookUnauthorized   = errors.New("code source webhook signature is invalid")
	ErrInvalidCodeSourceWebhookPayload = errors.New("invalid code source webhook payload")
)

type CodeSourceProvider string

const (
	CodeSourceProviderGitHub CodeSourceProvider = "github"
	CodeSourceProviderGitLab CodeSourceProvider = "gitlab"
)

type CodeSourceConnectionStatus string

const (
	CodeSourceConnectionStatusActive   CodeSourceConnectionStatus = "active"
	CodeSourceConnectionStatusDisabled CodeSourceConnectionStatus = "disabled"
	CodeSourceConnectionStatusDeleted  CodeSourceConnectionStatus = "deleted"
)

// CodeSourceConnection 是平台级代码源连接。*Encrypted 字段只在存储层流转；
// Credential、WebhookSecret 是 service 解密后的明文，只交给 provider 使用，不得出现在接口响应和日志里。
type CodeSourceConnection struct {
	ID                     string
	Provider               CodeSourceProvider
	Name                   string
	WebBaseURL             string
	APIBaseURL             string
	ExternalAccount        string
	ExternalID             string
	CredentialEncrypted    string
	CredentialPreview      string
	CredentialExpiresAt    *time.Time
	WebhookSecretEncrypted string
	Status                 CodeSourceConnectionStatus
	CreatedByUserID        string
	CreatedBy              string
	LastVerifiedAt         *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time

	Credential    string
	WebhookSecret string
}

type CodeSourceConnectionInfo struct {
	ExternalAccount string
	Scopes          []string
	ExpiresAt       *time.Time
}

type CodeSourceConnectionCreateInput struct {
	Provider        CodeSourceProvider
	Name            string
	WebBaseURL      string
	APIBaseURL      string
	Credential      string
	CreatedByUserID string
}

type CodeSourceConnectionUpdateInput struct {
	Name       *string
	Credential *string
	Status     *CodeSourceConnectionStatus
}

type CodeSourceConnectionFilters struct {
	Provider CodeSourceProvider
	Status   CodeSourceConnectionStatus
}

type CodeSourceConnectionWebhook struct {
	Provider CodeSourceProvider
	// Path 是 forge-server 上的回调路径，控制台拼接自身 API 地址后展示给用户。
	Path string
	// Secret 为空表示由 provider 统一接收（例如 GitHub App 级 webhook）。
	Secret string
}

// CodeSourceRepository 是各 provider 统一后的仓库结构，同时也是 code_source_repositories 缓存行。
type CodeSourceRepository struct {
	ConnectionID         string
	ExternalRepositoryID string
	FullName             string
	WebURL               string
	CloneURL             string
	DefaultBranch        string
	Private              bool
	Description          string
	LastSyncedAt         time.Time
}

type RepositorySearchInput struct {
	Query    string
	Page     int
	PageSize int
}

type RepositoryPage struct {
	Repositories []CodeSourceRepository
	HasMore      bool
	// Page、PageSize 由 service 回填为实际生效的分页参数。
	Page     int
	PageSize int
}

// BranchSearchInput 按关键词搜索仓库分支，Query 为空时列出全部分支，Limit 是最多返回的分支数。
type BranchSearchInput struct {
	Query string
	Limit int
}

// BranchCommit 是分支当前最新的一次提交，新增构建时展示，帮助确认要构建的代码。
type BranchCommit struct {
	Branch      string
	SHA         string
	Title       string
	AuthorName  string
	CommittedAt time.Time
	WebURL      string
}

// ModuleRepository 是模块绑定的外部仓库。
type ModuleRepository struct {
	ConnectionID         string
	ExternalRepositoryID string
	FullName             string
	WebURL               string
	CloneURL             string
	DefaultBranch        string
}

// GitCredentials 由 worker 通过 GIT_ASKPASS 使用。
type GitCredentials struct {
	Username  string
	Password  string
	ExpiresAt *time.Time
}

// WebhookRequest 是 HTTP 层交给 provider 的原始 webhook 请求，Headers 的键使用规范化的 MIME 头格式。
type WebhookRequest struct {
	Provider CodeSourceProvider
	// ConnectionID 来自回调路径（例如 GitLab 按连接配置的地址），为空表示 provider 级回调。
	ConnectionID string
	// Connection 在 ConnectionID 非空时由 service 预先加载并解密。
	Connection *CodeSourceConnection
	Headers    map[string]string
	Body       []byte
}

func (r WebhookRequest) Header(name string) string {
	return r.Headers[textproto.CanonicalMIMEHeaderKey(name)]
}

// PushEvent 是各 provider webhook 解析后的统一推送事件。
type PushEvent struct {
	Provider     CodeSourceProvider
	ConnectionID string
	// ExternalConnectionID 用于 provider 级回调定位连接，例如 GitHub installation id。
	ExternalConnectionID string
	ExternalRepositoryID string
	RepositoryFullName   string
	Branch               string
	CommitSHA            string
	DeliveryID           string
	// ConnectionCreatedByUserID 是代码源连接创建人，webhook 构建记在其名下。
	ConnectionCreatedByUserID string
}

// PushEventResult 是自动构建规则的处理结果。CreatedBuildIDs 是本次新建的构建，
// DuplicateBuildIDs 是因幂等命中已有 pending/running/succeeded 构建而跳过的构建。
type PushEventResult struct {
	CreatedBuildIDs   []string
	DuplicateBuildIDs []string
}

// Matched 表示事件至少命中了一个模块的自动构建规则（新建或幂等跳过）。
func (r PushEventResult) Matched() bool {
	return len(r.CreatedBuildIDs)+len(r.DuplicateBuildIDs) > 0
}

type WebhookResult struct {
	// Handled 为 false 表示事件被忽略（非 push、连接停用或没有模块匹配），路由返回 204。
	Handled bool
	Event   PushEvent
	Push    PushEventResult
}

// GitHub App 流程专用类型。

type CodeSourceAppStatus string

const (
	CodeSourceAppStatusInstalled    CodeSourceAppStatus = "installed"
	CodeSourceAppStatusNotInstalled CodeSourceAppStatus = "notInstalled"
)

type CodeSourceAppConfigurationStatus string

const (
	CodeSourceAppConfigurationStatusConfigured    CodeSourceAppConfigurationStatus = "configured"
	CodeSourceAppConfigurationStatusNotConfigured CodeSourceAppConfigurationStatus = "notConfigured"
)

type CodeSourceApp struct {
	ConfigurationStatus CodeSourceAppConfigurationStatus
	ConnectionCount     int
	Description         string
	ID                  CodeSourceProvider
	InstalledAt         *time.Time
	Name                string
	Provider            CodeSourceProvider
	Status              CodeSourceAppStatus
}

type CodeSourceInstallURL struct {
	URL string
}

type CodeSourceAppConfig struct {
	AppID         string
	AppSlug       string
	HTMLURL       string
	Name          string
	PrivateKey    string
	Provider      CodeSourceProvider
	WebhookSecret string
}

type CodeSourceAppManifest struct {
	ActionURL string
	Payload   string
}

type CodeSourceInstallation struct {
	AccountLogin        string
	AccountType         string
	APIBaseURL          string
	InstallationID      int64
	InstalledAt         time.Time
	RepositorySelection string
	UpdatedAt           time.Time
	WebBaseURL          string
}

func IsValidCodeSourceProvider(provider CodeSourceProvider) bool {
	return provider == CodeSourceProviderGitHub || provider == CodeSourceProviderGitLab
}

func IsValidCodeSourceConnectionStatus(status CodeSourceConnectionStatus) bool {
	switch status {
	case CodeSourceConnectionStatusActive, CodeSourceConnectionStatusDisabled, CodeSourceConnectionStatusDeleted:
		return true
	default:
		return false
	}
}
