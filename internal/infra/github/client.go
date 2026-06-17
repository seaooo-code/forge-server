package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Client struct {
	cfg        config.GitHubConfig
	httpClient *http.Client
	now        func() time.Time
}

func NewClient(cfg config.GitHubConfig, telemetryEnabled bool) *Client {
	var transport http.RoundTripper = http.DefaultTransport
	if telemetryEnabled {
		transport = otelhttp.NewTransport(transport)
	}

	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
		now: time.Now,
	}
}

func (c *Client) AppManifest() (domain.CodeSourceAppManifest, error) {
	manifest := appManifestRequest{
		CallbackURLs: []string{c.cfg.InstallRedirectURL},
		DefaultPermissions: map[string]string{
			"contents": "read",
			"metadata": "read",
		},
		Description: "Forge Console code source integration.",
		Name:        c.appName(),
		Public:      false,
		RedirectURL: c.cfg.ManifestCallbackURL,
		SetupURL:    c.cfg.InstallCallbackURL,
		URL:         c.appHomepageURL(),
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		return domain.CodeSourceAppManifest{}, fmt.Errorf("%w: encode github app manifest: %w", domain.ErrCodeSourceAuthorization, err)
	}
	return domain.CodeSourceAppManifest{
		ActionURL: c.appManifestURL(),
		Payload:   string(payload),
	}, nil
}

func (c *Client) CompleteAppManifest(ctx context.Context, code string) (domain.CodeSourceAppConfig, error) {
	req, err := c.newAPIRequest(ctx, http.MethodPost, "/app-manifests/"+url.PathEscape(code)+"/conversions", nil, bytes.NewReader([]byte("{}")), "")
	if err != nil {
		return domain.CodeSourceAppConfig{}, err
	}
	req.Header.Del("Authorization")
	req.Header.Set("Content-Type", "application/json")

	var response appManifestConversionResponse
	if err := c.do(req, &response); err != nil {
		return domain.CodeSourceAppConfig{}, err
	}
	if response.ID == 0 || response.PEM == "" || response.Slug == "" {
		return domain.CodeSourceAppConfig{}, fmt.Errorf("%w: github app manifest conversion response missing app credentials", domain.ErrCodeSourceAuthorization)
	}

	return domain.CodeSourceAppConfig{
		AppID:         strconv.FormatInt(response.ID, 10),
		AppSlug:       response.Slug,
		HTMLURL:       response.HTMLURL,
		Name:          response.Name,
		PrivateKey:    response.PEM,
		Provider:      domain.CodeSourceProviderGitHub,
		WebhookSecret: response.WebhookSecret,
	}, nil
}

func (c *Client) InstallationURL(appConfig domain.CodeSourceAppConfig) (string, error) {
	slug := strings.TrimSpace(appConfig.AppSlug)
	if slug == "" {
		return "", domain.ErrCodeSourceAuthNotConfigured
	}

	installURL, err := url.JoinPath(c.webURL(), "apps", slug, "installations", "new")
	if err != nil {
		return "", fmt.Errorf("%w: build github app installation url: %w", domain.ErrCodeSourceAuthorization, err)
	}
	return installURL, nil
}

func (c *Client) ValidateApp(ctx context.Context, appConfig domain.CodeSourceAppConfig) error {
	jwt, err := c.appJWT(appConfig)
	if err != nil {
		return err
	}

	req, err := c.newAPIRequest(ctx, http.MethodGet, "/app", nil, nil, jwt)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: send github api request: %w", domain.ErrCodeSourceAuthorization, err)
	}
	defer resp.Body.Close()
	req.Response = resp

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: github app credentials are no longer valid", domain.ErrCodeSourceAppNotFound)
	}
	return githubAPIError(resp)
}

func (c *Client) GetInstallation(ctx context.Context, appConfig domain.CodeSourceAppConfig, installationID int64) (domain.CodeSourceInstallation, error) {
	jwt, err := c.appJWT(appConfig)
	if err != nil {
		return domain.CodeSourceInstallation{}, err
	}

	req, err := c.newAPIRequest(ctx, http.MethodGet, "/app/installations/"+strconv.FormatInt(installationID, 10), nil, nil, jwt)
	if err != nil {
		return domain.CodeSourceInstallation{}, err
	}

	var response installationResponse
	if err := c.do(req, &response); err != nil {
		return domain.CodeSourceInstallation{}, err
	}

	return domain.CodeSourceInstallation{
		AccountLogin:        response.Account.Login,
		AccountType:         response.Account.Type,
		InstallationID:      response.ID,
		InstalledAt:         response.CreatedAt,
		Provider:            domain.CodeSourceProviderGitHub,
		RepositorySelection: response.RepositorySelection,
		UpdatedAt:           response.UpdatedAt,
	}, nil
}

func (c *Client) DeleteInstallation(ctx context.Context, appConfig domain.CodeSourceAppConfig, installationID int64) error {
	jwt, err := c.appJWT(appConfig)
	if err != nil {
		return err
	}

	req, err := c.newAPIRequest(ctx, http.MethodDelete, "/app/installations/"+strconv.FormatInt(installationID, 10), nil, nil, jwt)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *Client) ListRepositories(ctx context.Context, appConfig domain.CodeSourceAppConfig, installationID int64, page int, pageSize int) (domain.CodeSourceRepositoryPage, error) {
	if page < 1 || pageSize < 1 {
		return domain.CodeSourceRepositoryPage{}, domain.ErrInvalidCodeSourceInput
	}

	token, err := c.installationAccessToken(ctx, appConfig, installationID)
	if err != nil {
		return domain.CodeSourceRepositoryPage{}, err
	}

	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("per_page", strconv.Itoa(pageSize))
	req, err := c.newAPIRequest(ctx, http.MethodGet, "/installation/repositories", query, nil, token)
	if err != nil {
		return domain.CodeSourceRepositoryPage{}, err
	}

	var response repositoriesResponse
	if err := c.do(req, &response); err != nil {
		return domain.CodeSourceRepositoryPage{}, err
	}

	repositories := make([]domain.CodeSourceRepository, 0, len(response.Repositories))
	for _, repository := range response.Repositories {
		repositories = append(repositories, newCodeSourceRepository(installationID, repository))
	}

	return domain.CodeSourceRepositoryPage{
		HasMore:      hasNextPage(req.Response),
		Repositories: repositories,
	}, nil
}

func (c *Client) InstallationAccessToken(ctx context.Context, appConfig domain.CodeSourceAppConfig, installationID int64) (string, error) {
	return c.installationAccessToken(ctx, appConfig, installationID)
}

func (c *Client) installationAccessToken(ctx context.Context, appConfig domain.CodeSourceAppConfig, installationID int64) (string, error) {
	jwt, err := c.appJWT(appConfig)
	if err != nil {
		return "", err
	}

	req, err := c.newAPIRequest(ctx, http.MethodPost, "/app/installations/"+strconv.FormatInt(installationID, 10)+"/access_tokens", nil, bytes.NewReader([]byte("{}")), jwt)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	var response accessTokenResponse
	if err := c.do(req, &response); err != nil {
		return "", err
	}
	if response.Token == "" {
		return "", fmt.Errorf("%w: github installation token response missing token", domain.ErrCodeSourceAuthorization)
	}
	return response.Token, nil
}

func (c *Client) appJWT(appConfig domain.CodeSourceAppConfig) (string, error) {
	appID := strings.TrimSpace(appConfig.AppID)
	privateKey := strings.TrimSpace(appConfig.PrivateKey)
	if appID == "" || privateKey == "" {
		return "", domain.ErrCodeSourceAuthNotConfigured
	}

	key, err := parsePrivateKey([]byte(privateKey))
	if err != nil {
		return "", fmt.Errorf("%w: parse github app private key: %w", domain.ErrCodeSourceAuthorization, err)
	}

	now := c.now().UTC()
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	payload := map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	}

	encodedHeader, err := encodeJWTPart(header)
	if err != nil {
		return "", err
	}
	encodedPayload, err := encodeJWTPart(payload)
	if err != nil {
		return "", err
	}

	signingInput := encodedHeader + "." + encodedPayload
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("%w: sign github app jwt: %w", domain.ErrCodeSourceAuthorization, err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("missing PEM block")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return key, nil
}

func encodeJWTPart(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: encode github app jwt: %w", domain.ErrCodeSourceAuthorization, err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (c *Client) newAPIRequest(ctx context.Context, method string, path string, query url.Values, body io.Reader, bearerToken string) (*http.Request, error) {
	requestURL, err := c.apiURL(path, query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("%w: build github api request: %w", domain.ErrCodeSourceAuthorization, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("X-GitHub-Api-Version", c.apiVersion())
	return req, nil
}

func (c *Client) apiURL(path string, query url.Values) (string, error) {
	baseURL, err := url.Parse(c.apiBaseURL())
	if err != nil {
		return "", fmt.Errorf("%w: parse github api url: %w", domain.ErrCodeSourceAuthorization, err)
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + path
	baseURL.RawQuery = query.Encode()
	return baseURL.String(), nil
}

func (c *Client) do(req *http.Request, target any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: send github api request: %w", domain.ErrCodeSourceAuthorization, err)
	}
	defer resp.Body.Close()
	req.Response = resp

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return githubAPIError(resp)
	}
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("%w: decode github api response: %w", domain.ErrCodeSourceAuthorization, err)
	}
	return nil
}

func githubAPIError(resp *http.Response) error {
	var response errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		response.Message = strings.TrimSpace(resp.Status)
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", domain.ErrCodeSourceInstallationNotFound, response.Message)
	}
	return fmt.Errorf("%w: github status=%d message=%s", domain.ErrCodeSourceAuthorization, resp.StatusCode, response.Message)
}

func hasNextPage(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	return strings.Contains(resp.Header.Get("Link"), `rel="next"`)
}

func newCodeSourceRepository(installationID int64, repository repositoryResponse) domain.CodeSourceRepository {
	return domain.CodeSourceRepository{
		CloneURL:       repository.CloneURL,
		DefaultBranch:  repository.DefaultBranch,
		Description:    repository.Description,
		FullName:       repository.FullName,
		HTMLURL:        repository.HTMLURL,
		InstallationID: installationID,
		Name:           repository.Name,
		Owner:          repository.Owner.Login,
		Private:        repository.Private,
		Provider:       domain.CodeSourceProviderGitHub,
		RepositoryID:   repository.ID,
		SSHURL:         repository.SSHURL,
		UpdatedAt:      repository.updatedAt(),
	}
}

func (r repositoryResponse) updatedAt() *time.Time {
	if r.UpdatedAt.IsZero() {
		return nil
	}
	updatedAt := r.UpdatedAt
	return &updatedAt
}

func (c *Client) apiBaseURL() string {
	value := strings.TrimRight(strings.TrimSpace(c.cfg.APIBaseURL), "/")
	if value == "" {
		return "https://api.github.com"
	}
	return value
}

func (c *Client) appHomepageURL() string {
	value := strings.TrimRight(strings.TrimSpace(c.cfg.AppHomepageURL), "/")
	if value == "" {
		return "http://127.0.0.1:5173"
	}
	return value
}

func (c *Client) appManifestURL() string {
	return strings.TrimRight(c.webURL(), "/") + "/settings/apps/new"
}

func (c *Client) appName() string {
	value := strings.TrimSpace(c.cfg.AppName)
	if value == "" {
		return "Forge Console"
	}
	return value
}

func (c *Client) apiVersion() string {
	value := strings.TrimSpace(c.cfg.APIVersion)
	if value == "" {
		return "2026-03-10"
	}
	return value
}

func (c *Client) webURL() string {
	value := strings.TrimRight(strings.TrimSpace(c.cfg.WebURL), "/")
	if value == "" {
		return "https://github.com"
	}
	return value
}

type githubAccountResponse struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type appManifestRequest struct {
	CallbackURLs       []string          `json:"callback_urls,omitempty"`
	DefaultPermissions map[string]string `json:"default_permissions,omitempty"`
	Description        string            `json:"description,omitempty"`
	Name               string            `json:"name"`
	Public             bool              `json:"public"`
	RedirectURL        string            `json:"redirect_url"`
	SetupURL           string            `json:"setup_url,omitempty"`
	URL                string            `json:"url"`
}

type appManifestConversionResponse struct {
	HTMLURL       string `json:"html_url"`
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	PEM           string `json:"pem"`
	Slug          string `json:"slug"`
	WebhookSecret string `json:"webhook_secret"`
}

type installationResponse struct {
	Account             githubAccountResponse `json:"account"`
	CreatedAt           time.Time             `json:"created_at"`
	ID                  int64                 `json:"id"`
	RepositorySelection string                `json:"repository_selection"`
	UpdatedAt           time.Time             `json:"updated_at"`
}

type accessTokenResponse struct {
	ExpiresAt time.Time `json:"expires_at"`
	Token     string    `json:"token"`
}

type repositoriesResponse struct {
	Repositories []repositoryResponse `json:"repositories"`
	TotalCount   int                  `json:"total_count"`
}

type repositoryResponse struct {
	CloneURL      string                `json:"clone_url"`
	DefaultBranch string                `json:"default_branch"`
	Description   string                `json:"description"`
	FullName      string                `json:"full_name"`
	HTMLURL       string                `json:"html_url"`
	ID            int64                 `json:"id"`
	Name          string                `json:"name"`
	Owner         githubAccountResponse `json:"owner"`
	Private       bool                  `json:"private"`
	SSHURL        string                `json:"ssh_url"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

type errorResponse struct {
	Message string `json:"message"`
}
