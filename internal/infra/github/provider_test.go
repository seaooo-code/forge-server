package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

const testWebhookSecret = "app-webhook-secret"

type fakeAppConfigStore struct {
	config domain.CodeSourceAppConfig
	err    error
}

func (s fakeAppConfigStore) GetCodeSourceAppConfig(context.Context, domain.CodeSourceProvider) (domain.CodeSourceAppConfig, error) {
	return s.config, s.err
}

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	client := NewClient(config.GitHubConfig{APIBaseURL: "https://api.github.test"}, false)
	return NewProvider(client, fakeAppConfigStore{config: domain.CodeSourceAppConfig{
		AppID:         "12345",
		AppSlug:       "forge",
		PrivateKey:    testGitHubPrivateKeyPEM(t),
		Provider:      domain.CodeSourceProviderGitHub,
		WebhookSecret: testWebhookSecret,
	}})
}

func signBody(secret string, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func githubWebhookRequest(event string, signature string, body string) domain.WebhookRequest {
	headers := map[string]string{}
	set := func(key, value string) {
		if value != "" {
			headers[textproto.CanonicalMIMEHeaderKey(key)] = value
		}
	}
	set("X-GitHub-Event", event)
	set("X-Hub-Signature-256", signature)
	set("X-GitHub-Delivery", "delivery-1")
	return domain.WebhookRequest{Provider: domain.CodeSourceProviderGitHub, Headers: headers, Body: []byte(body)}
}

func pushBody(ref string, after string, deleted bool) string {
	return fmt.Sprintf(`{"ref":%q,"after":%q,"deleted":%t,"installation":{"id":1001},"repository":{"id":2001,"full_name":"acme/home"}}`, ref, after, deleted)
}

func TestParseWebhookAcceptsSignedPush(t *testing.T) {
	provider := newTestProvider(t)
	body := pushBody("refs/heads/feature/login", "abc123", false)

	event, ok, err := provider.ParseWebhook(context.Background(), githubWebhookRequest("push", signBody(testWebhookSecret, body), body))
	if err != nil || !ok {
		t.Fatalf("expected push event, got ok=%v err=%v", ok, err)
	}
	if event.ExternalConnectionID != "1001" || event.ExternalRepositoryID != "2001" || event.RepositoryFullName != "acme/home" {
		t.Fatalf("unexpected event identity: %+v", event)
	}
	if event.Branch != "feature/login" || event.CommitSHA != "abc123" || event.DeliveryID != "delivery-1" {
		t.Fatalf("unexpected event content: %+v", event)
	}
	if event.ConnectionID != "" {
		t.Fatalf("github provider must leave connection resolution to service, got %q", event.ConnectionID)
	}
}

func TestParseWebhookRejectsInvalidSignature(t *testing.T) {
	provider := newTestProvider(t)
	body := pushBody("refs/heads/main", "abc123", false)

	for name, signature := range map[string]string{
		"missing":        "",
		"wrong secret":   signBody("other-secret", body),
		"tampered body":  signBody(testWebhookSecret, body+" "),
		"no prefix":      strings.TrimPrefix(signBody(testWebhookSecret, body), "sha256="),
		"not hex digest": "sha256=zzzz",
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, err := provider.ParseWebhook(context.Background(), githubWebhookRequest("push", signature, body))
			if ok || !errors.Is(err, domain.ErrCodeSourceWebhookUnauthorized) {
				t.Fatalf("expected unauthorized, got ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestParseWebhookRejectsWhenSecretNotConfigured(t *testing.T) {
	client := NewClient(config.GitHubConfig{AppID: "1", AppSlug: "forge", PrivateKey: "key"}, false)
	provider := NewProvider(client, fakeAppConfigStore{err: domain.ErrCodeSourceAuthNotConfigured})
	body := pushBody("refs/heads/main", "abc123", false)

	_, ok, err := provider.ParseWebhook(context.Background(), githubWebhookRequest("push", signBody("", body), body))
	if ok || !errors.Is(err, domain.ErrCodeSourceAuthNotConfigured) {
		t.Fatalf("expected auth not configured, got ok=%v err=%v", ok, err)
	}
}

func TestParseWebhookIgnoresNonPushAndNonBranchEvents(t *testing.T) {
	provider := newTestProvider(t)
	tests := []struct {
		name  string
		event string
		body  string
	}{
		{name: "ping", event: "ping", body: `{"zen":"hi"}`},
		{name: "pull request", event: "pull_request", body: `{"action":"opened"}`},
		{name: "tag push", event: "push", body: pushBody("refs/tags/v1.0.0", "abc123", false)},
		{name: "branch deleted flag", event: "push", body: pushBody("refs/heads/main", "abc123", true)},
		{name: "branch deleted zero sha", event: "push", body: pushBody("refs/heads/main", zeroCommitSHA, false)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok, err := provider.ParseWebhook(context.Background(), githubWebhookRequest(tt.event, signBody(testWebhookSecret, tt.body), tt.body))
			if err != nil || ok {
				t.Fatalf("expected ignored event, got ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestParseWebhookRejectsMalformedPush(t *testing.T) {
	provider := newTestProvider(t)
	for name, body := range map[string]string{
		"invalid json":         `{`,
		"missing installation": `{"ref":"refs/heads/main","after":"abc","repository":{"id":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, err := provider.ParseWebhook(context.Background(), githubWebhookRequest("push", signBody(testWebhookSecret, body), body))
			if ok || !errors.Is(err, domain.ErrInvalidCodeSourceWebhookPayload) {
				t.Fatalf("expected invalid payload, got ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestProviderAppConfigFallsBackToEnvironment(t *testing.T) {
	client := NewClient(config.GitHubConfig{AppID: "1", AppSlug: "forge", PrivateKey: "key", WebhookSecret: "env-secret"}, false)
	provider := NewProvider(client, fakeAppConfigStore{err: domain.ErrCodeSourceAuthNotConfigured})

	appConfig, configured, err := provider.AppConfig(context.Background())
	if err != nil || !configured {
		t.Fatalf("expected env fallback, got configured=%v err=%v", configured, err)
	}
	if appConfig.AppID != "1" || appConfig.WebhookSecret != "env-secret" {
		t.Fatalf("unexpected fallback config: %+v", appConfig)
	}

	empty := NewProvider(NewClient(config.GitHubConfig{}, false), fakeAppConfigStore{err: domain.ErrCodeSourceAuthNotConfigured})
	if _, configured, err := empty.AppConfig(context.Background()); err != nil || configured {
		t.Fatalf("expected unconfigured app, got configured=%v err=%v", configured, err)
	}
}

func installationTransport(t *testing.T, pages map[string]string) roundTripFunc {
	t.Helper()
	return func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/app/installations/1001/access_tokens":
			return &http.Response{
				StatusCode: http.StatusCreated,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"token":"installation-token","expires_at":"2026-06-12T11:00:00Z"}`)),
			}, nil
		case "/installation/repositories":
			page := r.URL.Query().Get("page")
			header := make(http.Header)
			if _, ok := pages[fmt.Sprint(atoi(page)+1)]; ok {
				header.Set("Link", `<https://api.github.test/installation/repositories?page=next>; rel="next"`)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(pages[page]))}, nil
		default:
			t.Fatalf("unexpected github path %s", r.URL.Path)
			return nil, nil
		}
	}
}

func atoi(value string) int {
	var number int
	_, _ = fmt.Sscanf(value, "%d", &number)
	return number
}

func TestSearchRepositoriesFiltersAcrossPages(t *testing.T) {
	provider := newTestProvider(t)
	provider.httpClient = &http.Client{Transport: installationTransport(t, map[string]string{
		"1": `{"repositories":[{"id":1,"full_name":"acme/home"},{"id":2,"full_name":"acme/payments"}]}`,
		"2": `{"repositories":[{"id":3,"full_name":"acme/home-lite"},{"id":4,"full_name":"acme/docs","description":"home docs"}]}`,
	})}
	conn := domain.CodeSourceConnection{Provider: domain.CodeSourceProviderGitHub, ExternalID: "1001"}

	first, err := provider.SearchRepositories(context.Background(), conn, domain.RepositorySearchInput{Query: "HOME", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("search repositories: %v", err)
	}
	if len(first.Repositories) != 2 || first.Repositories[0].ExternalRepositoryID != "1" || first.Repositories[1].ExternalRepositoryID != "3" || !first.HasMore {
		t.Fatalf("unexpected first page: %+v", first)
	}

	second, err := provider.SearchRepositories(context.Background(), conn, domain.RepositorySearchInput{Query: "home", Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("search repositories: %v", err)
	}
	if len(second.Repositories) != 1 || second.Repositories[0].ExternalRepositoryID != "4" || second.HasMore {
		t.Fatalf("unexpected second page: %+v", second)
	}
}

func TestSearchBranchesFiltersAcrossPagesUpToLimit(t *testing.T) {
	provider := newTestProvider(t)
	pages := map[string]string{
		"1": `[{"name":"main"},{"name":"feature/Login"}]`,
		"2": `[{"name":"release"},{"name":"feature/home"},{"name":"feature/pay"}]`,
	}
	var requestedPages []string
	provider.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/app/installations/1001/access_tokens":
			return &http.Response{StatusCode: http.StatusCreated, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"token":"installation-token"}`))}, nil
		case "/repos/acme/home/branches":
			if r.Header.Get("Authorization") != "Bearer installation-token" {
				t.Fatalf("unexpected authorization header %q", r.Header.Get("Authorization"))
			}
			page := r.URL.Query().Get("page")
			requestedPages = append(requestedPages, page)
			header := make(http.Header)
			if _, ok := pages[fmt.Sprint(atoi(page)+1)]; ok {
				header.Set("Link", `<https://api.github.test/repos/acme/home/branches?page=next>; rel="next"`)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(pages[page]))}, nil
		default:
			t.Fatalf("unexpected github path %s", r.URL.Path)
			return nil, nil
		}
	})}
	conn := domain.CodeSourceConnection{Provider: domain.CodeSourceProviderGitHub, ExternalID: "1001"}

	branches, err := provider.SearchBranches(context.Background(), conn, domain.ModuleRepository{FullName: "acme/home"}, domain.BranchSearchInput{Query: "FEATURE", Limit: 2})
	if err != nil {
		t.Fatalf("search branches: %v", err)
	}
	if len(branches) != 2 || branches[0] != "feature/Login" || branches[1] != "feature/home" {
		t.Fatalf("unexpected branches: %#v", branches)
	}
	if len(requestedPages) != 2 {
		t.Fatalf("expected to stop after page 2, requested %v", requestedPages)
	}
}

func TestLatestBranchCommitUsesCommitsAPI(t *testing.T) {
	provider := newTestProvider(t)
	status, body := http.StatusOK, `[{"sha":"da1560886d4f094c3e6c9ef40349f7d38b5d27d7","html_url":"https://github.com/acme/home/commit/da15608","commit":{"message":"新增登录页\n\n详细说明","author":{"name":"Chen"},"committer":{"date":"2026-11-25T06:30:00Z"}}}]`
	provider.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/app/installations/1001/access_tokens":
			return &http.Response{StatusCode: http.StatusCreated, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"token":"installation-token"}`))}, nil
		case "/repos/acme/home/commits":
			if r.URL.Query().Get("sha") != "feature/login" || r.URL.Query().Get("per_page") != "1" || r.Header.Get("Authorization") != "Bearer installation-token" {
				t.Fatalf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected github path %s", r.URL.Path)
			return nil, nil
		}
	})}
	conn := domain.CodeSourceConnection{Provider: domain.CodeSourceProviderGitHub, ExternalID: "1001"}
	repo := domain.ModuleRepository{FullName: "acme/home"}

	commit, err := provider.LatestBranchCommit(context.Background(), conn, repo, "feature/login")
	if err != nil {
		t.Fatalf("latest branch commit: %v", err)
	}
	if commit.Branch != "feature/login" || commit.SHA != "da1560886d4f094c3e6c9ef40349f7d38b5d27d7" || commit.Title != "新增登录页" ||
		commit.AuthorName != "Chen" || !commit.CommittedAt.Equal(time.Date(2026, 11, 25, 6, 30, 0, 0, time.UTC)) || commit.WebURL == "" {
		t.Fatalf("unexpected commit: %+v", commit)
	}

	for _, tc := range []struct {
		status int
		body   string
	}{
		{http.StatusNotFound, `{"message":"Not Found"}`},
		{http.StatusUnprocessableEntity, `{"message":"No commit found for SHA: feature/login"}`},
		{http.StatusOK, `[]`},
	} {
		status, body = tc.status, tc.body
		if _, err := provider.LatestBranchCommit(context.Background(), conn, repo, "feature/login"); !errors.Is(err, domain.ErrCodeSourceBranchNotFound) {
			t.Fatalf("status %d: expected branch not found, got %v", tc.status, err)
		}
	}
}

func TestCloneCredentialsUsesInstallationToken(t *testing.T) {
	provider := newTestProvider(t)
	provider.httpClient = &http.Client{Transport: installationTransport(t, nil)}

	credentials, err := provider.CloneCredentials(context.Background(), domain.CodeSourceConnection{ExternalID: "1001"}, domain.ModuleRepository{})
	if err != nil {
		t.Fatalf("clone credentials: %v", err)
	}
	if credentials.Username != "x-access-token" || credentials.Password != "installation-token" || credentials.ExpiresAt == nil {
		t.Fatalf("unexpected credentials: %+v", credentials)
	}

	if _, err := provider.CloneCredentials(context.Background(), domain.CodeSourceConnection{}, domain.ModuleRepository{}); !errors.Is(err, domain.ErrInvalidCodeSourceInput) {
		t.Fatalf("expected invalid input without installation id, got %v", err)
	}
}
