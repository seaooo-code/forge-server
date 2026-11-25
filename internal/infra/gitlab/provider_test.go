package gitlab

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"forge-server/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func newTestProvider(t *testing.T, handler func(*http.Request) *http.Response) *Provider {
	t.Helper()
	provider := NewProvider(false)
	provider.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-token" {
			t.Fatalf("expected private token header, got %q", r.Header.Get("PRIVATE-TOKEN"))
		}
		return handler(r), nil
	})}
	return provider
}

func testConnection() domain.CodeSourceConnection {
	return domain.CodeSourceConnection{
		ID:            "code-source-1",
		Provider:      domain.CodeSourceProviderGitLab,
		APIBaseURL:    "https://gitlab.example.com/sub",
		WebBaseURL:    "https://gitlab.example.com/sub",
		Credential:    "glpat-token",
		WebhookSecret: "hook-secret",
	}
}

func TestVerifyConnectionReadsTokenScopesAndUser(t *testing.T) {
	provider := newTestProvider(t, func(r *http.Request) *http.Response {
		switch r.URL.Path {
		case "/sub/api/v4/personal_access_tokens/self":
			return jsonResponse(http.StatusOK, `{"active":true,"revoked":false,"scopes":["read_api","read_repository"],"expires_at":"2027-01-31"}`, nil)
		case "/sub/api/v4/user":
			return jsonResponse(http.StatusOK, `{"username":"group_123_bot"}`, nil)
		}
		t.Fatalf("unexpected path %s", r.URL.Path)
		return nil
	})

	info, err := provider.VerifyConnection(context.Background(), testConnection())
	if err != nil {
		t.Fatalf("verify connection: %v", err)
	}
	if info.ExternalAccount != "group_123_bot" || len(info.Scopes) != 2 {
		t.Fatalf("unexpected info: %+v", info)
	}
	if info.ExpiresAt == nil || info.ExpiresAt.Format("2006-01-02") != "2027-01-31" {
		t.Fatalf("unexpected expiry: %v", info.ExpiresAt)
	}
}

func TestVerifyConnectionRejectsInvalidTokens(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"message":"401 Unauthorized"}`},
		{name: "revoked", status: http.StatusOK, body: `{"active":true,"revoked":true,"scopes":["api"]}`},
		{name: "inactive", status: http.StatusOK, body: `{"active":false,"scopes":["api"]}`},
		{name: "missing read_api", status: http.StatusOK, body: `{"active":true,"scopes":["read_repository"]}`},
		{name: "missing read_repository", status: http.StatusOK, body: `{"active":true,"scopes":["read_api"]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := newTestProvider(t, func(*http.Request) *http.Response {
				return jsonResponse(tt.status, tt.body, nil)
			})
			_, err := provider.VerifyConnection(context.Background(), testConnection())
			if !errors.Is(err, domain.ErrCodeSourceCredentialInvalid) {
				t.Fatalf("expected invalid credential, got %v", err)
			}
		})
	}

	conn := testConnection()
	conn.Credential = ""
	if _, err := NewProvider(false).VerifyConnection(context.Background(), conn); !errors.Is(err, domain.ErrCodeSourceCredentialInvalid) {
		t.Fatalf("expected invalid credential for empty token, got %v", err)
	}
}

func TestSearchRepositoriesMapsProjects(t *testing.T) {
	provider := newTestProvider(t, func(r *http.Request) *http.Response {
		query := r.URL.Query()
		if r.URL.Path != "/sub/api/v4/projects" || query.Get("membership") != "true" || query.Get("simple") != "true" ||
			query.Get("search") != "home" || query.Get("page") != "2" || query.Get("per_page") != "10" {
			t.Fatalf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		header := make(http.Header)
		header.Set("X-Next-Page", "3")
		return jsonResponse(http.StatusOK, `[{"id":42,"path_with_namespace":"rn/home","web_url":"https://gitlab.example.com/rn/home","http_url_to_repo":"https://gitlab.example.com/rn/home.git","default_branch":"main","visibility":"internal","description":"Home"}]`, header)
	})

	page, err := provider.SearchRepositories(context.Background(), testConnection(), domain.RepositorySearchInput{Query: "home", Page: 2, PageSize: 10})
	if err != nil {
		t.Fatalf("search repositories: %v", err)
	}
	if !page.HasMore || len(page.Repositories) != 1 {
		t.Fatalf("unexpected page: %+v", page)
	}
	repository := page.Repositories[0]
	if repository.ExternalRepositoryID != "42" || repository.FullName != "rn/home" || repository.CloneURL != "https://gitlab.example.com/rn/home.git" ||
		repository.DefaultBranch != "main" || !repository.Private {
		t.Fatalf("unexpected repository: %+v", repository)
	}
}

func TestSearchBranchesUsesProjectBranchesAPI(t *testing.T) {
	provider := newTestProvider(t, func(r *http.Request) *http.Response {
		query := r.URL.Query()
		if r.URL.EscapedPath() != "/sub/api/v4/projects/42/repository/branches" || query.Get("search") != "feat" || query.Get("per_page") != "50" {
			t.Fatalf("unexpected request %s?%s", r.URL.EscapedPath(), r.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, `[{"name":"feature/login"},{"name":"feat-home"}]`, nil)
	})

	branches, err := provider.SearchBranches(context.Background(), testConnection(), domain.ModuleRepository{ExternalRepositoryID: "42"}, domain.BranchSearchInput{Query: " feat ", Limit: 50})
	if err != nil {
		t.Fatalf("search branches: %v", err)
	}
	if len(branches) != 2 || branches[0] != "feature/login" || branches[1] != "feat-home" {
		t.Fatalf("unexpected branches: %#v", branches)
	}
}

func TestLatestBranchCommitUsesCommitsAPIWithRefName(t *testing.T) {
	provider := newTestProvider(t, func(r *http.Request) *http.Response {
		query := r.URL.Query()
		if r.URL.EscapedPath() != "/sub/api/v4/projects/42/repository/commits" || query.Get("ref_name") != "feature/login" || query.Get("per_page") != "1" {
			t.Fatalf("unexpected request %s?%s", r.URL.EscapedPath(), r.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, `[{"id":"da1560886d4f094c3e6c9ef40349f7d38b5d27d7","short_id":"da156088","title":"新增登录页","author_name":"Chen","committed_date":"2026-11-25T14:30:00.000+08:00","web_url":"https://gitlab.example.com/rn/home/-/commit/da15608"}]`, nil)
	})

	commit, err := provider.ResolveCommit(context.Background(), testConnection(), domain.ModuleRepository{ExternalRepositoryID: "42"}, " feature/login ")
	if err != nil {
		t.Fatalf("latest branch commit: %v", err)
	}
	if commit.Branch != "feature/login" || commit.SHA != "da1560886d4f094c3e6c9ef40349f7d38b5d27d7" || commit.Title != "新增登录页" ||
		commit.AuthorName != "Chen" || !commit.CommittedAt.Equal(time.Date(2026, 11, 25, 6, 30, 0, 0, time.UTC)) || commit.WebURL == "" {
		t.Fatalf("unexpected commit: %+v", commit)
	}
}

func TestLatestBranchCommitMapsMissingBranch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response *http.Response
	}{
		{"404", jsonResponse(http.StatusNotFound, `{"message":"404 Commit Not Found"}`, nil)},
		{"empty", jsonResponse(http.StatusOK, `[]`, nil)},
	} {
		provider := newTestProvider(t, func(*http.Request) *http.Response { return tc.response })
		if _, err := provider.ResolveCommit(context.Background(), testConnection(), domain.ModuleRepository{ExternalRepositoryID: "42"}, "gone"); !errors.Is(err, domain.ErrCodeSourceBranchNotFound) {
			t.Fatalf("%s: expected branch not found, got %v", tc.name, err)
		}
	}
	// 其他接口的 404 仍然只是授权失败，不受影响。
	provider := newTestProvider(t, func(*http.Request) *http.Response {
		return jsonResponse(http.StatusNotFound, `{"message":"404 Project Not Found"}`, nil)
	})
	_, err := provider.SearchBranches(context.Background(), testConnection(), domain.ModuleRepository{ExternalRepositoryID: "42"}, domain.BranchSearchInput{Limit: 1})
	if !errors.Is(err, domain.ErrCodeSourceAuthorization) || errors.Is(err, domain.ErrCodeSourceBranchNotFound) {
		t.Fatalf("expected authorization error for other 404, got %v", err)
	}
}

func gitlabCommitJSON(sha string, title string) string {
	return `{"id":"` + sha + `","title":"` + title + `","author_name":"Chen","committed_date":"2026-11-25T14:30:00.000+08:00","web_url":"https://gitlab.example.com/c/` + sha + `"}`
}

func TestCompareCommitsUsesRevisionRanges(t *testing.T) {
	cases := []struct {
		name       string
		ahead      string
		behind     string
		status     domain.CommitComparisonStatus
		total      int
		behindBy   int
		firstTitle string
		webSuffix  string
	}{
		{"ahead", `[` + gitlabCommitJSON("c2", "第二个") + `,` + gitlabCommitJSON("c1", "第一个") + `]`, `[]`, domain.CommitComparisonAhead, 2, 0, "第二个", "/-/compare/aaa...bbb"},
		{"behind", `[]`, `[` + gitlabCommitJSON("c3", "被撤掉的") + `]`, domain.CommitComparisonBehind, 1, 1, "被撤掉的", "/-/compare/bbb...aaa"},
		{"diverged", `[` + gitlabCommitJSON("c4", "新分支上的") + `]`, `[` + gitlabCommitJSON("c5", "线上独有的") + `]`, domain.CommitComparisonDiverged, 1, 1, "新分支上的", "/-/compare/aaa...bbb"},
		{"identical", `[]`, `[]`, domain.CommitComparisonIdentical, 0, 0, "", "/-/compare/aaa...bbb"},
	}
	for _, tc := range cases {
		provider := newTestProvider(t, func(r *http.Request) *http.Response {
			if r.URL.EscapedPath() != "/sub/api/v4/projects/42/repository/commits" || r.URL.Query().Get("per_page") != "100" {
				t.Fatalf("unexpected request %s?%s", r.URL.EscapedPath(), r.URL.RawQuery)
			}
			switch r.URL.Query().Get("ref_name") {
			case "aaa..bbb":
				return jsonResponse(http.StatusOK, tc.ahead, nil)
			case "bbb..aaa":
				return jsonResponse(http.StatusOK, tc.behind, nil)
			}
			t.Fatalf("unexpected range %q", r.URL.Query().Get("ref_name"))
			return nil
		})
		comparison, err := provider.CompareCommits(context.Background(), testConnection(), domain.ModuleRepository{ExternalRepositoryID: "42", WebURL: "https://gitlab.example.com/rn/home"}, "aaa", "bbb")
		if err != nil {
			t.Fatalf("%s: compare: %v", tc.name, err)
		}
		if comparison.Status != tc.status || comparison.TotalCommits != tc.total || comparison.BehindBy != tc.behindBy || !comparison.TotalExact ||
			comparison.WebURL != "https://gitlab.example.com/rn/home"+tc.webSuffix {
			t.Fatalf("%s: unexpected comparison: %+v", tc.name, comparison)
		}
		if tc.firstTitle != "" && (len(comparison.Commits) == 0 || comparison.Commits[0].Title != tc.firstTitle) {
			t.Fatalf("%s: unexpected commits: %+v", tc.name, comparison.Commits)
		}
	}
}

func TestCompareCommitsCapsPagesAndCommits(t *testing.T) {
	page := make([]string, 100)
	for index := range page {
		page[index] = gitlabCommitJSON(fmt.Sprintf("c%d", index), "提交")
	}
	body := "[" + strings.Join(page, ",") + "]"
	requests := 0
	provider := newTestProvider(t, func(r *http.Request) *http.Response {
		if r.URL.Query().Get("ref_name") == "bbb..aaa" {
			return jsonResponse(http.StatusOK, `[]`, nil)
		}
		requests++
		header := make(http.Header)
		header.Set("X-Next-Page", "next")
		return jsonResponse(http.StatusOK, body, header)
	})
	comparison, err := provider.CompareCommits(context.Background(), testConnection(), domain.ModuleRepository{ExternalRepositoryID: "42"}, "aaa", "bbb")
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if requests != commitRangeMaxPages || comparison.TotalCommits != commitRangeMaxPages*100 || comparison.TotalExact || len(comparison.Commits) != domain.MaxComparisonCommits || comparison.WebURL != "" {
		t.Fatalf("unexpected capped comparison: requests=%d total=%d exact=%v commits=%d web=%q", requests, comparison.TotalCommits, comparison.TotalExact, len(comparison.Commits), comparison.WebURL)
	}
}

func TestCloneCredentialsUsesOAuth2Username(t *testing.T) {
	credentials, err := NewProvider(false).CloneCredentials(context.Background(), testConnection(), domain.ModuleRepository{})
	if err != nil {
		t.Fatalf("clone credentials: %v", err)
	}
	if credentials.Username != "oauth2" || credentials.Password != "glpat-token" {
		t.Fatalf("unexpected credentials: %+v", credentials)
	}
}

func gitlabWebhookRequest(conn *domain.CodeSourceConnection, event string, token string, body string) domain.WebhookRequest {
	headers := map[string]string{}
	if event != "" {
		headers[textproto.CanonicalMIMEHeaderKey("X-Gitlab-Event")] = event
	}
	if token != "" {
		headers[textproto.CanonicalMIMEHeaderKey("X-Gitlab-Token")] = token
	}
	headers[textproto.CanonicalMIMEHeaderKey("X-Gitlab-Event-UUID")] = "uuid-1"
	return domain.WebhookRequest{Provider: domain.CodeSourceProviderGitLab, Connection: conn, Headers: headers, Body: []byte(body)}
}

const branchPushBody = `{"object_kind":"push","ref":"refs/heads/feature/a","after":"da1560886d4f094c3e6c9ef40349f7d38b5d27d7","project_id":42,"project":{"id":42,"path_with_namespace":"rn/home"}}`

func TestParseWebhookAcceptsPushHook(t *testing.T) {
	conn := testConnection()
	event, ok, err := NewProvider(false).ParseWebhook(context.Background(), gitlabWebhookRequest(&conn, "Push Hook", "hook-secret", branchPushBody))
	if err != nil || !ok {
		t.Fatalf("expected push event, got ok=%v err=%v", ok, err)
	}
	if event.ConnectionID != "code-source-1" || event.ExternalRepositoryID != "42" || event.RepositoryFullName != "rn/home" ||
		event.Branch != "feature/a" || event.CommitSHA != "da1560886d4f094c3e6c9ef40349f7d38b5d27d7" || event.DeliveryID != "uuid-1" {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestParseWebhookRejectsWrongToken(t *testing.T) {
	conn := testConnection()
	for name, token := range map[string]string{"missing": "", "wrong": "other-secret", "prefix": "hook-secre"} {
		t.Run(name, func(t *testing.T) {
			_, ok, err := NewProvider(false).ParseWebhook(context.Background(), gitlabWebhookRequest(&conn, "Push Hook", token, branchPushBody))
			if ok || !errors.Is(err, domain.ErrCodeSourceWebhookUnauthorized) {
				t.Fatalf("expected unauthorized, got ok=%v err=%v", ok, err)
			}
		})
	}

	withoutSecret := testConnection()
	withoutSecret.WebhookSecret = ""
	if _, _, err := NewProvider(false).ParseWebhook(context.Background(), gitlabWebhookRequest(&withoutSecret, "Push Hook", "", branchPushBody)); !errors.Is(err, domain.ErrCodeSourceWebhookUnauthorized) {
		t.Fatalf("expected unauthorized when connection has no secret, got %v", err)
	}
	if _, _, err := NewProvider(false).ParseWebhook(context.Background(), gitlabWebhookRequest(nil, "Push Hook", "hook-secret", branchPushBody)); !errors.Is(err, domain.ErrCodeSourceConnectionNotFound) {
		t.Fatalf("expected connection not found without connection, got %v", err)
	}
}

func TestParseWebhookIgnoresUnsupportedEvents(t *testing.T) {
	conn := testConnection()
	tests := []struct {
		name  string
		event string
		body  string
	}{
		{name: "tag push hook", event: "Tag Push Hook", body: `{"object_kind":"tag_push","ref":"refs/tags/v1","after":"abc","project_id":42}`},
		{name: "merge request hook", event: "Merge Request Hook", body: `{"object_kind":"merge_request"}`},
		{name: "branch deleted", event: "Push Hook", body: `{"object_kind":"push","ref":"refs/heads/main","after":"0000000000000000000000000000000000000000","project_id":42}`},
		{name: "tag ref in push hook", event: "Push Hook", body: `{"object_kind":"push","ref":"refs/tags/v1","after":"abc","project_id":42}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok, err := NewProvider(false).ParseWebhook(context.Background(), gitlabWebhookRequest(&conn, tt.event, "hook-secret", tt.body))
			if err != nil || ok {
				t.Fatalf("expected ignored event, got ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestParseWebhookRejectsMalformedPayload(t *testing.T) {
	conn := testConnection()
	for name, body := range map[string]string{
		"invalid json":       `{`,
		"missing project id": `{"object_kind":"push","ref":"refs/heads/main","after":"abc"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, err := NewProvider(false).ParseWebhook(context.Background(), gitlabWebhookRequest(&conn, "Push Hook", "hook-secret", body))
			if ok || !errors.Is(err, domain.ErrInvalidCodeSourceWebhookPayload) {
				t.Fatalf("expected invalid payload, got ok=%v err=%v", ok, err)
			}
		})
	}
}
