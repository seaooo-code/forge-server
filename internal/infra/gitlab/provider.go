// Package gitlab 实现 GitLab（gitlab.com 与任意自建实例）的代码源适配器。
// 连接使用 Group / Project Access Token（scope 为 read_api + read_repository）。
package gitlab

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"forge-server/internal/domain"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	apiPathPrefix       = "/api/v4"
	cloneUsername       = "oauth2"
	pushHookEvent       = "Push Hook"
	branchRefPrefix     = "refs/heads/"
	maxResponseBytes    = 4 << 20
	expiresAtDateLayout = "2006-01-02"
)

type Provider struct {
	httpClient *http.Client
}

func NewProvider(telemetryEnabled bool) *Provider {
	var transport http.RoundTripper = http.DefaultTransport
	if telemetryEnabled {
		transport = otelhttp.NewTransport(transport)
	}
	return &Provider{httpClient: &http.Client{Timeout: 10 * time.Second, Transport: transport}}
}

func (p *Provider) Provider() domain.CodeSourceProvider {
	return domain.CodeSourceProviderGitLab
}

// VerifyConnection 通过 /personal_access_tokens/self 读取令牌的 scope 和过期时间，
// 再通过 /user 读取令牌所属用户（Group / Project Access Token 对应的 bot 用户）。
func (p *Provider) VerifyConnection(ctx context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceConnectionInfo, error) {
	if strings.TrimSpace(conn.Credential) == "" {
		return domain.CodeSourceConnectionInfo{}, fmt.Errorf("%w: gitlab access token is required", domain.ErrCodeSourceCredentialInvalid)
	}

	var token accessTokenResponse
	if _, err := p.get(ctx, conn, "/personal_access_tokens/self", nil, &token); err != nil {
		return domain.CodeSourceConnectionInfo{}, err
	}
	if !token.Active || token.Revoked {
		return domain.CodeSourceConnectionInfo{}, fmt.Errorf("%w: gitlab access token is inactive or revoked", domain.ErrCodeSourceCredentialInvalid)
	}
	hasAPI := slices.Contains(token.Scopes, "api")
	if !hasAPI && !slices.Contains(token.Scopes, "read_api") {
		return domain.CodeSourceConnectionInfo{}, fmt.Errorf("%w: gitlab access token requires read_api scope", domain.ErrCodeSourceCredentialInvalid)
	}
	if !hasAPI && !slices.Contains(token.Scopes, "read_repository") && !slices.Contains(token.Scopes, "write_repository") {
		return domain.CodeSourceConnectionInfo{}, fmt.Errorf("%w: gitlab access token requires read_repository scope", domain.ErrCodeSourceCredentialInvalid)
	}

	var user userResponse
	if _, err := p.get(ctx, conn, "/user", nil, &user); err != nil {
		return domain.CodeSourceConnectionInfo{}, err
	}

	info := domain.CodeSourceConnectionInfo{ExternalAccount: user.Username, Scopes: token.Scopes}
	if token.ExpiresAt != "" {
		// GitLab 令牌在过期日当天 UTC 零点失效。
		expiresAt, err := time.Parse(expiresAtDateLayout, token.ExpiresAt)
		if err != nil {
			return domain.CodeSourceConnectionInfo{}, fmt.Errorf("%w: parse gitlab token expires_at: %w", domain.ErrCodeSourceAuthorization, err)
		}
		info.ExpiresAt = &expiresAt
	}
	return info, nil
}

func (p *Provider) SearchRepositories(ctx context.Context, conn domain.CodeSourceConnection, input domain.RepositorySearchInput) (domain.RepositoryPage, error) {
	if input.Page < 1 || input.PageSize < 1 {
		return domain.RepositoryPage{}, domain.ErrInvalidCodeSourceInput
	}
	query := url.Values{}
	query.Set("membership", "true")
	query.Set("simple", "true")
	query.Set("order_by", "last_activity_at")
	query.Set("page", strconv.Itoa(input.Page))
	query.Set("per_page", strconv.Itoa(input.PageSize))
	if search := strings.TrimSpace(input.Query); search != "" {
		query.Set("search", search)
		query.Set("search_namespaces", "true")
	}

	var projects []projectResponse
	header, err := p.get(ctx, conn, "/projects", query, &projects)
	if err != nil {
		return domain.RepositoryPage{}, err
	}

	repositories := make([]domain.CodeSourceRepository, 0, len(projects))
	for _, project := range projects {
		repositories = append(repositories, domain.CodeSourceRepository{
			ExternalRepositoryID: strconv.FormatInt(project.ID, 10),
			FullName:             project.PathWithNamespace,
			WebURL:               project.WebURL,
			CloneURL:             project.HTTPURLToRepo,
			DefaultBranch:        project.DefaultBranch,
			// simple=true 时部分版本不返回 visibility，未知时按私有处理。
			Private:     project.Visibility != "public",
			Description: project.Description,
		})
	}
	return domain.RepositoryPage{
		Repositories: repositories,
		HasMore:      strings.TrimSpace(header.Get("X-Next-Page")) != "",
	}, nil
}

func (p *Provider) CloneCredentials(_ context.Context, conn domain.CodeSourceConnection, _ domain.ModuleRepository) (domain.GitCredentials, error) {
	if strings.TrimSpace(conn.Credential) == "" {
		return domain.GitCredentials{}, fmt.Errorf("%w: gitlab access token is required", domain.ErrCodeSourceCredentialInvalid)
	}
	return domain.GitCredentials{
		Username:  cloneUsername,
		Password:  conn.Credential,
		ExpiresAt: conn.CredentialExpiresAt,
	}, nil
}

// ParseWebhook 用常量时间比较校验 X-Gitlab-Token，只处理 Push Hook；删除分支（after 全为 0）和 tag 推送一律忽略。
func (p *Provider) ParseWebhook(_ context.Context, req domain.WebhookRequest) (domain.PushEvent, bool, error) {
	if req.Connection == nil {
		return domain.PushEvent{}, false, domain.ErrCodeSourceConnectionNotFound
	}
	secret := req.Connection.WebhookSecret
	token := req.Header("X-Gitlab-Token")
	if secret == "" || subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		return domain.PushEvent{}, false, domain.ErrCodeSourceWebhookUnauthorized
	}
	if req.Header("X-Gitlab-Event") != pushHookEvent {
		return domain.PushEvent{}, false, nil
	}

	var payload pushPayload
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return domain.PushEvent{}, false, fmt.Errorf("%w: decode gitlab push payload: %w", domain.ErrInvalidCodeSourceWebhookPayload, err)
	}
	branch, isBranch := strings.CutPrefix(payload.Ref, branchRefPrefix)
	if payload.ObjectKind != "push" || !isBranch || branch == "" || strings.Trim(payload.After, "0") == "" {
		return domain.PushEvent{}, false, nil
	}
	projectID := payload.ProjectID
	if projectID == 0 {
		projectID = payload.Project.ID
	}
	if projectID <= 0 {
		return domain.PushEvent{}, false, fmt.Errorf("%w: gitlab push payload missing project id", domain.ErrInvalidCodeSourceWebhookPayload)
	}

	return domain.PushEvent{
		Provider:             domain.CodeSourceProviderGitLab,
		ConnectionID:         req.Connection.ID,
		ExternalRepositoryID: strconv.FormatInt(projectID, 10),
		RepositoryFullName:   payload.Project.PathWithNamespace,
		Branch:               branch,
		CommitSHA:            payload.After,
		DeliveryID:           req.Header("X-Gitlab-Event-UUID"),
	}, true, nil
}

func (p *Provider) get(ctx context.Context, conn domain.CodeSourceConnection, path string, query url.Values, target any) (http.Header, error) {
	baseURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(conn.APIBaseURL), "/"))
	if err != nil || baseURL.Host == "" {
		return nil, fmt.Errorf("%w: invalid gitlab api base url", domain.ErrInvalidCodeSourceInput)
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + apiPathPrefix + path
	baseURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build gitlab api request: %w", domain.ErrCodeSourceAuthorization, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("PRIVATE-TOKEN", conn.Credential)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: send gitlab api request: %w", domain.ErrCodeSourceAuthorization, err)
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, maxResponseBytes)

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: gitlab rejected access token", domain.ErrCodeSourceCredentialInvalid)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		var response errorResponse
		_ = json.NewDecoder(body).Decode(&response)
		return nil, fmt.Errorf("%w: gitlab status=%d message=%s", domain.ErrCodeSourceAuthorization, resp.StatusCode, response.message())
	}
	if err := json.NewDecoder(body).Decode(target); err != nil {
		return nil, fmt.Errorf("%w: decode gitlab api response: %w", domain.ErrCodeSourceAuthorization, err)
	}
	return resp.Header, nil
}

type accessTokenResponse struct {
	Active    bool     `json:"active"`
	ExpiresAt string   `json:"expires_at"`
	Revoked   bool     `json:"revoked"`
	Scopes    []string `json:"scopes"`
}

type userResponse struct {
	Username string `json:"username"`
}

type projectResponse struct {
	DefaultBranch     string `json:"default_branch"`
	Description       string `json:"description"`
	HTTPURLToRepo     string `json:"http_url_to_repo"`
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	Visibility        string `json:"visibility"`
	WebURL            string `json:"web_url"`
}

type pushPayload struct {
	After      string `json:"after"`
	ObjectKind string `json:"object_kind"`
	ProjectID  int64  `json:"project_id"`
	Ref        string `json:"ref"`
	Project    struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
}

type errorResponse struct {
	Message any    `json:"message"`
	Error   string `json:"error"`
}

func (r errorResponse) message() string {
	if r.Error != "" {
		return r.Error
	}
	if r.Message != nil {
		return fmt.Sprint(r.Message)
	}
	return ""
}
