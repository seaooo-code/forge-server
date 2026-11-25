package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"forge-server/internal/domain"
)

const (
	defaultRepositorySearchPageSize = 20
	maxRepositorySearchPageSize     = 100
	defaultBranchSearchLimit        = 50
	maxCodeSourceConnectionNameLen  = 100
	webhookPathPrefix               = "/api/v1/webhooks/"
)

// CodeSourceProvider 是代码托管方适配器。核心逻辑只依赖这个接口，实现放在 internal/infra/<provider>。
type CodeSourceProvider interface {
	Provider() domain.CodeSourceProvider
	// VerifyConnection 校验连接是否可用，返回账号、权限范围、凭证过期时间等信息。
	VerifyConnection(ctx context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceConnectionInfo, error)
	// SearchRepositories 列出该连接可访问的仓库，返回统一的仓库结构。
	SearchRepositories(ctx context.Context, conn domain.CodeSourceConnection, input domain.RepositorySearchInput) (domain.RepositoryPage, error)
	// SearchBranches 按关键词搜索仓库分支名，最多返回 input.Limit 条。
	SearchBranches(ctx context.Context, conn domain.CodeSourceConnection, repo domain.ModuleRepository, input domain.BranchSearchInput) ([]string, error)
	// LatestBranchCommit 返回分支当前最新的提交；分支不存在时返回 ErrCodeSourceBranchNotFound。
	LatestBranchCommit(ctx context.Context, conn domain.CodeSourceConnection, repo domain.ModuleRepository, branch string) (domain.BranchCommit, error)
	// CloneCredentials 提供 clone 凭证，worker 通过 GIT_ASKPASS 使用。
	CloneCredentials(ctx context.Context, conn domain.CodeSourceConnection, repo domain.ModuleRepository) (domain.GitCredentials, error)
	// ParseWebhook 校验 webhook 来源并解析出统一事件；不需要处理的事件返回 ok=false。
	ParseWebhook(ctx context.Context, req domain.WebhookRequest) (event domain.PushEvent, ok bool, err error)
}

// CodeSourceProviderRegistry 按 connection.provider 分发到具体适配器。
type CodeSourceProviderRegistry struct {
	providers map[domain.CodeSourceProvider]CodeSourceProvider
}

func NewCodeSourceProviderRegistry(providers ...CodeSourceProvider) *CodeSourceProviderRegistry {
	registry := &CodeSourceProviderRegistry{providers: map[domain.CodeSourceProvider]CodeSourceProvider{}}
	for _, provider := range providers {
		if provider != nil {
			registry.providers[provider.Provider()] = provider
		}
	}
	return registry
}

func (r *CodeSourceProviderRegistry) Provider(provider domain.CodeSourceProvider) (CodeSourceProvider, error) {
	if r != nil {
		if implementation, ok := r.providers[provider]; ok {
			return implementation, nil
		}
	}
	return nil, domain.ErrCodeSourceProviderNotFound
}

// SecretBox 加解密落库的敏感字段，实现见 internal/infra/secretbox。
type SecretBox interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

// PushEventHandler 接收 provider 解析后的统一推送事件，负责匹配自动构建规则并幂等创建构建，实现见 BuildTriggerService。
// webhook 请求会同步等待它返回，实现里不要做耗时操作。
type PushEventHandler interface {
	HandlePushEvent(ctx context.Context, event domain.PushEvent) (domain.PushEventResult, error)
}

type CodeSourceStore interface {
	GetCodeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) (domain.CodeSourceAppConfig, error)
	UpsertCodeSourceAppConfig(ctx context.Context, config domain.CodeSourceAppConfig) (domain.CodeSourceAppConfig, error)
	DeleteCodeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) error

	ListCodeSourceConnections(ctx context.Context, filters domain.CodeSourceConnectionFilters) ([]domain.CodeSourceConnection, error)
	// GetCodeSourceConnection 不返回已删除的连接。
	GetCodeSourceConnection(ctx context.Context, id string) (domain.CodeSourceConnection, error)
	// FindCodeSourceConnectionByExternalID 按 provider 级外部 id（例如 GitHub installation id）查找未删除的连接。
	FindCodeSourceConnectionByExternalID(ctx context.Context, provider domain.CodeSourceProvider, externalID string) (domain.CodeSourceConnection, error)
	CreateCodeSourceConnection(ctx context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceConnection, error)
	UpdateCodeSourceConnection(ctx context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceConnection, error)
	// DeleteCodeSourceConnection 软删除连接；仍被模块引用时返回 ErrCodeSourceConnectionInUse。
	DeleteCodeSourceConnection(ctx context.Context, id string) error

	UpsertCodeSourceRepositories(ctx context.Context, repositories []domain.CodeSourceRepository) error
	GetCodeSourceRepository(ctx context.Context, connectionID string, externalRepositoryID string) (domain.CodeSourceRepository, error)
}

type CodeSourceService struct {
	githubApp   GitHubAppClient
	now         func() time.Time
	pushHandler PushEventHandler
	registry    *CodeSourceProviderRegistry
	secrets     SecretBox
	store       CodeSourceStore
}

type CodeSourceServiceDependencies struct {
	GitHubApp   GitHubAppClient
	PushHandler PushEventHandler
	Registry    *CodeSourceProviderRegistry
	Secrets     SecretBox
	Store       CodeSourceStore
}

func NewCodeSourceService(deps CodeSourceServiceDependencies) *CodeSourceService {
	return &CodeSourceService{
		githubApp:   deps.GitHubApp,
		now:         time.Now,
		pushHandler: deps.PushHandler,
		registry:    deps.Registry,
		secrets:     deps.Secrets,
		store:       deps.Store,
	}
}

func (s *CodeSourceService) ListConnections(ctx context.Context, filters domain.CodeSourceConnectionFilters) ([]domain.CodeSourceConnection, error) {
	if filters.Provider != "" && !domain.IsValidCodeSourceProvider(filters.Provider) {
		return nil, domain.ErrInvalidCodeSourceInput
	}
	if filters.Status != "" && (filters.Status == domain.CodeSourceConnectionStatusDeleted || !domain.IsValidCodeSourceConnectionStatus(filters.Status)) {
		return nil, domain.ErrInvalidCodeSourceInput
	}
	return s.store.ListCodeSourceConnections(ctx, filters)
}

func (s *CodeSourceService) GetConnection(ctx context.Context, id string) (domain.CodeSourceConnection, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.CodeSourceConnection{}, domain.ErrCodeSourceConnectionNotFound
	}
	return s.store.GetCodeSourceConnection(ctx, id)
}

func (s *CodeSourceService) CreateConnection(ctx context.Context, input domain.CodeSourceConnectionCreateInput) (domain.CodeSourceConnection, error) {
	provider, err := s.registry.Provider(input.Provider)
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}
	name := strings.TrimSpace(input.Name)
	createdBy := strings.TrimSpace(input.CreatedByUserID)
	webBaseURL, webOK := normalizeCodeSourceBaseURL(input.WebBaseURL)
	apiBaseURL, apiOK := webBaseURL, webOK
	if strings.TrimSpace(input.APIBaseURL) != "" {
		apiBaseURL, apiOK = normalizeCodeSourceBaseURL(input.APIBaseURL)
	}
	if name == "" || len([]rune(name)) > maxCodeSourceConnectionNameLen || createdBy == "" || !webOK || !apiOK {
		return domain.CodeSourceConnection{}, domain.ErrInvalidCodeSourceInput
	}

	webhookSecret, err := randomWebhookSecret()
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}
	conn := domain.CodeSourceConnection{
		Provider:        input.Provider,
		Name:            name,
		WebBaseURL:      webBaseURL,
		APIBaseURL:      apiBaseURL,
		Status:          domain.CodeSourceConnectionStatusActive,
		CreatedByUserID: createdBy,
		Credential:      strings.TrimSpace(input.Credential),
		WebhookSecret:   webhookSecret,
	}
	if err := s.verifyAndApply(ctx, provider, &conn); err != nil {
		return domain.CodeSourceConnection{}, err
	}
	if err := s.sealConnection(&conn); err != nil {
		return domain.CodeSourceConnection{}, err
	}

	saved, err := s.store.CreateCodeSourceConnection(ctx, conn)
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}
	return saved, nil
}

func (s *CodeSourceService) UpdateConnection(ctx context.Context, id string, input domain.CodeSourceConnectionUpdateInput) (domain.CodeSourceConnection, error) {
	conn, err := s.GetConnection(ctx, id)
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > maxCodeSourceConnectionNameLen {
			return domain.CodeSourceConnection{}, domain.ErrInvalidCodeSourceInput
		}
		conn.Name = name
	}
	if input.Status != nil {
		if *input.Status != domain.CodeSourceConnectionStatusActive && *input.Status != domain.CodeSourceConnectionStatusDisabled {
			return domain.CodeSourceConnection{}, domain.ErrInvalidCodeSourceInput
		}
		conn.Status = *input.Status
	}
	if input.Credential != nil {
		provider, err := s.registry.Provider(conn.Provider)
		if err != nil {
			return domain.CodeSourceConnection{}, err
		}
		conn.Credential = strings.TrimSpace(*input.Credential)
		if err := s.verifyAndApply(ctx, provider, &conn); err != nil {
			return domain.CodeSourceConnection{}, err
		}
		encrypted, err := s.encrypt(conn.Credential)
		if err != nil {
			return domain.CodeSourceConnection{}, err
		}
		conn.CredentialEncrypted = encrypted
		conn.CredentialPreview = credentialPreview(conn.Credential)
		conn.Credential = ""
	}

	return s.store.UpdateCodeSourceConnection(ctx, conn)
}

func (s *CodeSourceService) DeleteConnection(ctx context.Context, id string) error {
	conn, err := s.GetConnection(ctx, id)
	if err != nil {
		return err
	}
	return s.store.DeleteCodeSourceConnection(ctx, conn.ID)
}

func (s *CodeSourceService) VerifyConnection(ctx context.Context, id string) (domain.CodeSourceConnection, error) {
	conn, provider, err := s.openConnection(ctx, id, false)
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}
	if err := s.verifyAndApply(ctx, provider, &conn); err != nil {
		return domain.CodeSourceConnection{}, err
	}
	conn.Credential = ""
	conn.WebhookSecret = ""
	return s.store.UpdateCodeSourceConnection(ctx, conn)
}

func (s *CodeSourceService) SearchRepositories(ctx context.Context, connectionID string, input domain.RepositorySearchInput) (domain.RepositoryPage, error) {
	normalized := domain.RepositorySearchInput{
		Query:    strings.TrimSpace(input.Query),
		Page:     input.Page,
		PageSize: input.PageSize,
	}
	if normalized.Page == 0 {
		normalized.Page = 1
	}
	if normalized.PageSize == 0 {
		normalized.PageSize = defaultRepositorySearchPageSize
	}
	if normalized.Page < 1 || normalized.PageSize < 1 || normalized.PageSize > maxRepositorySearchPageSize {
		return domain.RepositoryPage{}, domain.ErrInvalidCodeSourceInput
	}

	conn, provider, err := s.openConnection(ctx, connectionID, true)
	if err != nil {
		return domain.RepositoryPage{}, err
	}
	page, err := provider.SearchRepositories(ctx, conn, normalized)
	if err != nil {
		return domain.RepositoryPage{}, err
	}

	syncedAt := s.now().UTC()
	for index := range page.Repositories {
		page.Repositories[index].ConnectionID = conn.ID
		page.Repositories[index].LastSyncedAt = syncedAt
	}
	if err := s.store.UpsertCodeSourceRepositories(ctx, page.Repositories); err != nil {
		return domain.RepositoryPage{}, err
	}
	if page.Repositories == nil {
		page.Repositories = []domain.CodeSourceRepository{}
	}
	page.Page = normalized.Page
	page.PageSize = normalized.PageSize
	return page, nil
}

func (s *CodeSourceService) GetConnectionWebhook(ctx context.Context, id string) (domain.CodeSourceConnectionWebhook, error) {
	conn, _, err := s.openConnection(ctx, id, false)
	if err != nil {
		return domain.CodeSourceConnectionWebhook{}, err
	}
	// 有连接级 secret 的连接按连接接收回调；没有的由 provider 统一接收（例如 GitHub App）。
	webhook := domain.CodeSourceConnectionWebhook{
		Provider: conn.Provider,
		Path:     webhookPathPrefix + string(conn.Provider),
		Secret:   conn.WebhookSecret,
	}
	if conn.WebhookSecret != "" {
		webhook.Path += "/" + url.PathEscape(conn.ID)
	}
	return webhook, nil
}

// SearchBranches 通过模块绑定仓库所在的连接搜索分支名，最多返回 defaultBranchSearchLimit 条。
func (s *CodeSourceService) SearchBranches(ctx context.Context, repository domain.ModuleRepository, query string) ([]string, error) {
	conn, provider, err := s.openConnection(ctx, repository.ConnectionID, true)
	if err != nil {
		return nil, err
	}
	branches, err := provider.SearchBranches(ctx, conn, repository, domain.BranchSearchInput{
		Query: strings.TrimSpace(query),
		Limit: defaultBranchSearchLimit,
	})
	if err != nil {
		return nil, err
	}
	if branches == nil {
		branches = []string{}
	}
	return branches, nil
}

// LatestBranchCommit 通过模块绑定仓库所在的连接查询分支最新提交。
func (s *CodeSourceService) LatestBranchCommit(ctx context.Context, repository domain.ModuleRepository, branch string) (domain.BranchCommit, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return domain.BranchCommit{}, domain.ErrInvalidCodeSourceInput
	}
	conn, provider, err := s.openConnection(ctx, repository.ConnectionID, true)
	if err != nil {
		return domain.BranchCommit{}, err
	}
	return provider.LatestBranchCommit(ctx, conn, repository, branch)
}

// CloneCredentials 供 worker 获取仓库 clone 凭证。
func (s *CodeSourceService) CloneCredentials(ctx context.Context, repository domain.ModuleRepository) (domain.GitCredentials, error) {
	conn, provider, err := s.openConnection(ctx, repository.ConnectionID, true)
	if err != nil {
		return domain.GitCredentials{}, err
	}
	return provider.CloneCredentials(ctx, conn, repository)
}

// HandleWebhook 交给 provider 校验并解析 webhook，再把统一的 PushEvent 交给 PushEventHandler。
func (s *CodeSourceService) HandleWebhook(ctx context.Context, req domain.WebhookRequest) (domain.WebhookResult, error) {
	provider, err := s.registry.Provider(req.Provider)
	if err != nil {
		return domain.WebhookResult{}, err
	}

	req.ConnectionID = strings.TrimSpace(req.ConnectionID)
	req.Connection = nil
	if req.ConnectionID != "" {
		conn, err := s.store.GetCodeSourceConnection(ctx, req.ConnectionID)
		if err != nil {
			return domain.WebhookResult{}, err
		}
		if conn.Provider != req.Provider {
			return domain.WebhookResult{}, domain.ErrCodeSourceConnectionNotFound
		}
		if err := s.openSecrets(&conn); err != nil {
			return domain.WebhookResult{}, err
		}
		req.Connection = &conn
	}

	event, ok, err := provider.ParseWebhook(ctx, req)
	if err != nil || !ok {
		return domain.WebhookResult{}, err
	}

	event.Provider = req.Provider
	var conn domain.CodeSourceConnection
	switch {
	case req.Connection != nil:
		conn = *req.Connection
	case strings.TrimSpace(event.ExternalConnectionID) != "":
		conn, err = s.store.FindCodeSourceConnectionByExternalID(ctx, req.Provider, strings.TrimSpace(event.ExternalConnectionID))
		if err != nil {
			return domain.WebhookResult{}, err
		}
	default:
		return domain.WebhookResult{}, domain.ErrInvalidCodeSourceWebhookPayload
	}
	if conn.Status != domain.CodeSourceConnectionStatusActive {
		return domain.WebhookResult{}, nil
	}
	event.ConnectionID = conn.ID
	event.ConnectionCreatedByUserID = conn.CreatedByUserID

	if s.pushHandler == nil {
		return domain.WebhookResult{Event: event}, nil
	}
	pushResult, err := s.pushHandler.HandlePushEvent(ctx, event)
	if err != nil {
		return domain.WebhookResult{}, err
	}
	return domain.WebhookResult{Handled: pushResult.Matched(), Event: event, Push: pushResult}, nil
}

// openConnection 加载未删除的连接并解密凭证；requireActive 为 true 时拒绝已停用的连接。
func (s *CodeSourceService) openConnection(ctx context.Context, id string, requireActive bool) (domain.CodeSourceConnection, CodeSourceProvider, error) {
	conn, err := s.GetConnection(ctx, id)
	if err != nil {
		return domain.CodeSourceConnection{}, nil, err
	}
	if requireActive && conn.Status != domain.CodeSourceConnectionStatusActive {
		return domain.CodeSourceConnection{}, nil, domain.ErrCodeSourceConnectionInactive
	}
	provider, err := s.registry.Provider(conn.Provider)
	if err != nil {
		return domain.CodeSourceConnection{}, nil, err
	}
	if err := s.openSecrets(&conn); err != nil {
		return domain.CodeSourceConnection{}, nil, err
	}
	return conn, provider, nil
}

func (s *CodeSourceService) openSecrets(conn *domain.CodeSourceConnection) error {
	credential, err := s.decrypt(conn.CredentialEncrypted)
	if err != nil {
		return fmt.Errorf("decrypt code source credential: %w", err)
	}
	webhookSecret, err := s.decrypt(conn.WebhookSecretEncrypted)
	if err != nil {
		return fmt.Errorf("decrypt code source webhook secret: %w", err)
	}
	conn.Credential = credential
	conn.WebhookSecret = webhookSecret
	return nil
}

// sealConnection 加密明文字段并清空明文，之后才能交给存储层。
func (s *CodeSourceService) sealConnection(conn *domain.CodeSourceConnection) error {
	credential, err := s.encrypt(conn.Credential)
	if err != nil {
		return err
	}
	webhookSecret, err := s.encrypt(conn.WebhookSecret)
	if err != nil {
		return err
	}
	conn.CredentialEncrypted = credential
	conn.CredentialPreview = credentialPreview(conn.Credential)
	conn.WebhookSecretEncrypted = webhookSecret
	conn.Credential = ""
	conn.WebhookSecret = ""
	return nil
}

func (s *CodeSourceService) verifyAndApply(ctx context.Context, provider CodeSourceProvider, conn *domain.CodeSourceConnection) error {
	info, err := provider.VerifyConnection(ctx, *conn)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if account := strings.TrimSpace(info.ExternalAccount); account != "" {
		conn.ExternalAccount = account
	}
	conn.CredentialExpiresAt = info.ExpiresAt
	conn.LastVerifiedAt = &now
	return nil
}

func (s *CodeSourceService) encrypt(plaintext string) (string, error) {
	if s.secrets == nil {
		return "", domain.ErrCodeSourceAuthNotConfigured
	}
	ciphertext, err := s.secrets.Encrypt(plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt code source secret: %w", err)
	}
	return ciphertext, nil
}

func (s *CodeSourceService) decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if s.secrets == nil {
		return "", domain.ErrCodeSourceAuthNotConfigured
	}
	return s.secrets.Decrypt(ciphertext)
}

func normalizeCodeSourceBaseURL(raw string) (string, bool) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	return value, true
}

func credentialPreview(credential string) string {
	if credential == "" {
		return ""
	}
	if len(credential) <= 12 {
		return "****"
	}
	return credential[:4] + "****" + credential[len(credential)-4:]
}

func randomWebhookSecret() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
