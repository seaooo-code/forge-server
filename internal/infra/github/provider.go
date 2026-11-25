package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"forge-server/internal/domain"
)

const (
	cloneUsername          = "x-access-token"
	repositoryFetchPerPage = 100
	// branchFetchMaxPages 限制分支遍历页数，单次搜索最多扫描 1000 个分支。
	branchFetchMaxPages = 10
	signaturePrefix     = "sha256="
	zeroCommitSHA       = "0000000000000000000000000000000000000000"
	branchRefPrefix     = "refs/heads/"
)

// AppConfigStore 读取保存在数据库中的 GitHub App 配置，由 postgres.CodeSourceRepository 实现。
type AppConfigStore interface {
	GetCodeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) (domain.CodeSourceAppConfig, error)
}

// Provider 是 GitHub 的代码源适配器：实现 service.CodeSourceProvider，
// 同时承载 GitHub App 创建与安装流程（service.GitHubAppClient）。
type Provider struct {
	*Client
	store AppConfigStore
}

func NewProvider(client *Client, store AppConfigStore) *Provider {
	return &Provider{Client: client, store: store}
}

func (p *Provider) Provider() domain.CodeSourceProvider {
	return domain.CodeSourceProviderGitHub
}

// AppConfig 优先使用页面创建并写入数据库的 App 配置，其次使用 GITHUB_APP_* 环境变量。
func (p *Provider) AppConfig(ctx context.Context) (domain.CodeSourceAppConfig, bool, error) {
	if p.store != nil {
		config, err := p.store.GetCodeSourceAppConfig(ctx, domain.CodeSourceProviderGitHub)
		if err == nil {
			return config, true, nil
		}
		if !errors.Is(err, domain.ErrCodeSourceAuthNotConfigured) {
			return domain.CodeSourceAppConfig{}, false, err
		}
	}

	fallback := domain.CodeSourceAppConfig{
		AppID:         strings.TrimSpace(p.cfg.AppID),
		AppSlug:       strings.TrimSpace(p.cfg.AppSlug),
		PrivateKey:    strings.TrimSpace(p.cfg.PrivateKey),
		Provider:      domain.CodeSourceProviderGitHub,
		WebhookSecret: p.cfg.WebhookSecret,
	}
	if fallback.AppID == "" || fallback.AppSlug == "" || fallback.PrivateKey == "" {
		return domain.CodeSourceAppConfig{}, false, nil
	}
	return fallback, true, nil
}

func (p *Provider) VerifyConnection(ctx context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceConnectionInfo, error) {
	config, installationID, err := p.connectionContext(ctx, conn)
	if err != nil {
		return domain.CodeSourceConnectionInfo{}, err
	}
	installation, err := p.GetInstallation(ctx, config, installationID)
	if err != nil {
		return domain.CodeSourceConnectionInfo{}, err
	}
	return domain.CodeSourceConnectionInfo{ExternalAccount: installation.AccountLogin}, nil
}

// SearchRepositories 遍历 installation 授权的仓库并在本地按关键词过滤，GitHub 没有按 installation 搜索的接口。
func (p *Provider) SearchRepositories(ctx context.Context, conn domain.CodeSourceConnection, input domain.RepositorySearchInput) (domain.RepositoryPage, error) {
	if input.Page < 1 || input.PageSize < 1 {
		return domain.RepositoryPage{}, domain.ErrInvalidCodeSourceInput
	}
	config, installationID, err := p.connectionContext(ctx, conn)
	if err != nil {
		return domain.RepositoryPage{}, err
	}
	token, err := p.installationAccessToken(ctx, config, installationID)
	if err != nil {
		return domain.RepositoryPage{}, err
	}

	query := strings.ToLower(strings.TrimSpace(input.Query))
	offset := (input.Page - 1) * input.PageSize
	// 多取一条用于判断是否还有下一页。
	limit := offset + input.PageSize + 1
	matched := make([]domain.CodeSourceRepository, 0, limit)
	for page := 1; len(matched) < limit; page++ {
		repositoryPage, err := p.listInstallationRepositories(ctx, token.Token, page, repositoryFetchPerPage)
		if err != nil {
			return domain.RepositoryPage{}, err
		}
		for _, repository := range repositoryPage.Repositories {
			if query == "" ||
				strings.Contains(strings.ToLower(repository.FullName), query) ||
				strings.Contains(strings.ToLower(repository.Description), query) {
				matched = append(matched, repository)
			}
		}
		if !repositoryPage.HasMore {
			break
		}
	}

	if offset >= len(matched) {
		return domain.RepositoryPage{Repositories: []domain.CodeSourceRepository{}}, nil
	}
	end := min(offset+input.PageSize, len(matched))
	return domain.RepositoryPage{
		Repositories: matched[offset:end],
		HasMore:      len(matched) > end,
	}, nil
}

// SearchBranches 分页遍历仓库分支并在本地按名称包含匹配，GitHub 没有按关键词搜索分支的接口。
func (p *Provider) SearchBranches(ctx context.Context, conn domain.CodeSourceConnection, repo domain.ModuleRepository, input domain.BranchSearchInput) ([]string, error) {
	fullName := strings.Trim(strings.TrimSpace(repo.FullName), "/")
	if fullName == "" || input.Limit < 1 {
		return nil, domain.ErrInvalidCodeSourceInput
	}
	config, installationID, err := p.connectionContext(ctx, conn)
	if err != nil {
		return nil, err
	}
	token, err := p.installationAccessToken(ctx, config, installationID)
	if err != nil {
		return nil, err
	}

	query := strings.ToLower(strings.TrimSpace(input.Query))
	matched := make([]string, 0, input.Limit)
	for page := 1; page <= branchFetchMaxPages && len(matched) < input.Limit; page++ {
		branches, hasMore, err := p.listRepositoryBranches(ctx, token.Token, fullName, page, repositoryFetchPerPage)
		if err != nil {
			return nil, err
		}
		for _, branch := range branches {
			if len(matched) < input.Limit && (query == "" || strings.Contains(strings.ToLower(branch), query)) {
				matched = append(matched, branch)
			}
		}
		if !hasMore {
			break
		}
	}
	return matched, nil
}

// ResolveCommit 用提交列表接口按 sha=ref（分支名或 SHA）取最新一条，ref 走查询参数，不用处理路径里的斜杠转义。
func (p *Provider) ResolveCommit(ctx context.Context, conn domain.CodeSourceConnection, repo domain.ModuleRepository, ref string) (domain.BranchCommit, error) {
	fullName := strings.Trim(strings.TrimSpace(repo.FullName), "/")
	ref = strings.TrimSpace(ref)
	if fullName == "" || ref == "" {
		return domain.BranchCommit{}, domain.ErrInvalidCodeSourceInput
	}
	config, installationID, err := p.connectionContext(ctx, conn)
	if err != nil {
		return domain.BranchCommit{}, err
	}
	token, err := p.installationAccessToken(ctx, config, installationID)
	if err != nil {
		return domain.BranchCommit{}, err
	}
	commit, err := p.latestCommit(ctx, token.Token, fullName, ref)
	if err != nil {
		return domain.BranchCommit{}, err
	}
	commit.Branch = ref
	return commit, nil
}

// CompareCommits 用 compare 接口取方向和数量；to 落后时再反向比较一次，列出发布后会被撤掉的提交。
func (p *Provider) CompareCommits(ctx context.Context, conn domain.CodeSourceConnection, repo domain.ModuleRepository, from string, to string) (domain.CommitComparison, error) {
	fullName := strings.Trim(strings.TrimSpace(repo.FullName), "/")
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if fullName == "" || from == "" || to == "" {
		return domain.CommitComparison{}, domain.ErrInvalidCodeSourceInput
	}
	config, installationID, err := p.connectionContext(ctx, conn)
	if err != nil {
		return domain.CommitComparison{}, err
	}
	token, err := p.installationAccessToken(ctx, config, installationID)
	if err != nil {
		return domain.CommitComparison{}, err
	}
	comparison, err := p.compareCommits(ctx, token.Token, fullName, from, to)
	if err != nil || comparison.Status != domain.CommitComparisonBehind {
		return comparison, err
	}
	reversed, err := p.compareCommits(ctx, token.Token, fullName, to, from)
	if err != nil {
		return domain.CommitComparison{}, err
	}
	reversed.Status = domain.CommitComparisonBehind
	reversed.BehindBy = 0
	return reversed, nil
}

func (p *Provider) CloneCredentials(ctx context.Context, conn domain.CodeSourceConnection, _ domain.ModuleRepository) (domain.GitCredentials, error) {
	config, installationID, err := p.connectionContext(ctx, conn)
	if err != nil {
		return domain.GitCredentials{}, err
	}
	token, err := p.installationAccessToken(ctx, config, installationID)
	if err != nil {
		return domain.GitCredentials{}, err
	}
	credentials := domain.GitCredentials{Username: cloneUsername, Password: token.Token}
	if !token.ExpiresAt.IsZero() {
		expiresAt := token.ExpiresAt
		credentials.ExpiresAt = &expiresAt
	}
	return credentials, nil
}

// ParseWebhook 用 App 级 webhook secret 校验 X-Hub-Signature-256，只处理 push 事件；
// 连接由 service 按 installation.id（PushEvent.ExternalConnectionID）定位。
func (p *Provider) ParseWebhook(ctx context.Context, req domain.WebhookRequest) (domain.PushEvent, bool, error) {
	config, configured, err := p.AppConfig(ctx)
	if err != nil {
		return domain.PushEvent{}, false, err
	}
	if !configured || config.WebhookSecret == "" {
		return domain.PushEvent{}, false, domain.ErrCodeSourceAuthNotConfigured
	}
	if !validSignature(config.WebhookSecret, req.Body, req.Header("X-Hub-Signature-256")) {
		return domain.PushEvent{}, false, domain.ErrCodeSourceWebhookUnauthorized
	}
	if req.Header("X-GitHub-Event") != "push" {
		return domain.PushEvent{}, false, nil
	}

	var payload pushPayload
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return domain.PushEvent{}, false, fmt.Errorf("%w: decode github push payload: %w", domain.ErrInvalidCodeSourceWebhookPayload, err)
	}
	branch, isBranch := strings.CutPrefix(payload.Ref, branchRefPrefix)
	if !isBranch || branch == "" || payload.Deleted || payload.After == "" || payload.After == zeroCommitSHA {
		return domain.PushEvent{}, false, nil
	}
	if payload.Installation.ID <= 0 || payload.Repository.ID <= 0 {
		return domain.PushEvent{}, false, fmt.Errorf("%w: github push payload missing installation or repository", domain.ErrInvalidCodeSourceWebhookPayload)
	}

	return domain.PushEvent{
		Provider:             domain.CodeSourceProviderGitHub,
		ExternalConnectionID: strconv.FormatInt(payload.Installation.ID, 10),
		ExternalRepositoryID: strconv.FormatInt(payload.Repository.ID, 10),
		RepositoryFullName:   payload.Repository.FullName,
		Branch:               branch,
		CommitSHA:            payload.After,
		DeliveryID:           req.Header("X-GitHub-Delivery"),
	}, true, nil
}

func (p *Provider) connectionContext(ctx context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceAppConfig, int64, error) {
	installationID, err := strconv.ParseInt(strings.TrimSpace(conn.ExternalID), 10, 64)
	if err != nil || installationID <= 0 {
		return domain.CodeSourceAppConfig{}, 0, fmt.Errorf("%w: github connection requires installation id", domain.ErrInvalidCodeSourceInput)
	}
	config, configured, err := p.AppConfig(ctx)
	if err != nil {
		return domain.CodeSourceAppConfig{}, 0, err
	}
	if !configured {
		return domain.CodeSourceAppConfig{}, 0, domain.ErrCodeSourceAuthNotConfigured
	}
	return config, installationID, nil
}

func validSignature(secret string, body []byte, header string) bool {
	signature, ok := strings.CutPrefix(strings.TrimSpace(header), signaturePrefix)
	if !ok {
		return false
	}
	expected, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), expected)
}

type pushPayload struct {
	After        string `json:"after"`
	Deleted      bool   `json:"deleted"`
	Ref          string `json:"ref"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		FullName string `json:"full_name"`
		ID       int64  `json:"id"`
	} `json:"repository"`
}
