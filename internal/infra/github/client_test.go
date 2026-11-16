package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestInstallationURLUsesConfiguredGitHubAppSlug(t *testing.T) {
	client := NewClient(config.GitHubConfig{
		AppSlug: "forge-console",
		WebURL:  "https://github.example.com",
	}, false)

	installURL, err := client.InstallationURL(domain.CodeSourceAppConfig{AppSlug: "forge-console"})
	if err != nil {
		t.Fatalf("installation url: %v", err)
	}

	expected := "https://github.example.com/apps/forge-console/installations/new"
	if installURL != expected {
		t.Fatalf("expected %q, got %q", expected, installURL)
	}
}

func TestAppManifestUsesConfiguredURLs(t *testing.T) {
	client := NewClient(config.GitHubConfig{
		AppHomepageURL:      "https://forge.example.com",
		AppName:             "Forge Test",
		InstallCallbackURL:  "https://api.forge.example.com/api/v1/code-sources/github/install/callback",
		InstallRedirectURL:  "https://forge.example.com/code-sources",
		ManifestCallbackURL: "https://api.forge.example.com/api/v1/code-sources/github/manifest/callback",
		WebURL:              "https://github.example.com",
	}, false)

	manifest, err := client.AppManifest()
	if err != nil {
		t.Fatalf("app manifest: %v", err)
	}
	if manifest.ActionURL != "https://github.example.com/settings/apps/new" {
		t.Fatalf("unexpected action url %q", manifest.ActionURL)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(manifest.Payload), &payload); err != nil {
		t.Fatalf("decode manifest payload: %v", err)
	}
	if payload["name"] != "Forge Test" || payload["redirect_url"] != "https://api.forge.example.com/api/v1/code-sources/github/manifest/callback" {
		t.Fatalf("unexpected manifest payload: %+v", payload)
	}
	if payload["setup_url"] != "https://api.forge.example.com/api/v1/code-sources/github/install/callback" {
		t.Fatalf("unexpected setup url: %+v", payload)
	}
	if _, ok := payload["hook_attributes"]; ok {
		t.Fatalf("expected no webhook without public webhook url: %+v", payload)
	}
}

func TestAppManifestSubscribesPushWhenWebhookURLConfigured(t *testing.T) {
	client := NewClient(config.GitHubConfig{WebhookURL: "https://forge.example.com/api/v1/webhooks/github"}, false)

	manifest, err := client.AppManifest()
	if err != nil {
		t.Fatalf("app manifest: %v", err)
	}
	var payload struct {
		DefaultEvents  []string `json:"default_events"`
		HookAttributes struct {
			Active bool   `json:"active"`
			URL    string `json:"url"`
		} `json:"hook_attributes"`
	}
	if err := json.Unmarshal([]byte(manifest.Payload), &payload); err != nil {
		t.Fatalf("decode manifest payload: %v", err)
	}
	if len(payload.DefaultEvents) != 1 || payload.DefaultEvents[0] != "push" {
		t.Fatalf("expected push event subscription, got %+v", payload.DefaultEvents)
	}
	if !payload.HookAttributes.Active || payload.HookAttributes.URL != "https://forge.example.com/api/v1/webhooks/github" {
		t.Fatalf("unexpected hook attributes: %+v", payload.HookAttributes)
	}
}

func TestValidateAppUsesGitHubAppJWT(t *testing.T) {
	client := NewClient(config.GitHubConfig{
		APIBaseURL: "https://api.github.test",
		APIVersion: "2026-03-10",
	}, false)
	client.now = func() time.Time {
		return time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	}

	requests := 0
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			if r.Method != http.MethodGet || r.URL.Path != "/app" {
				t.Fatalf("expected app GET, got %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
				t.Fatalf("expected github api version, got %q", r.Header.Get("X-GitHub-Api-Version"))
			}
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				t.Fatalf("expected jwt bearer auth, got %q", r.Header.Get("Authorization"))
			}
			jwt := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if parts := strings.Split(jwt, "."); len(parts) != 3 {
				t.Fatalf("expected jwt with 3 parts, got %q", jwt)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"id":12345,"slug":"forge-console"}`)),
			}, nil
		}),
	}

	err := client.ValidateApp(context.Background(), domain.CodeSourceAppConfig{
		AppID:      "12345",
		PrivateKey: testGitHubPrivateKeyPEM(t),
	})
	if err != nil {
		t.Fatalf("validate app: %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected one app validation request, got %d", requests)
	}
}

func TestValidateAppMapsDeletedGitHubAppToAppNotFound(t *testing.T) {
	client := NewClient(config.GitHubConfig{
		APIBaseURL: "https://api.github.test",
	}, false)
	client.now = func() time.Time {
		return time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	}
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"message":"Bad credentials"}`)),
			}, nil
		}),
	}

	err := client.ValidateApp(context.Background(), domain.CodeSourceAppConfig{
		AppID:      "12345",
		PrivateKey: testGitHubPrivateKeyPEM(t),
	})
	if !errors.Is(err, domain.ErrCodeSourceAppNotFound) {
		t.Fatalf("expected deleted app error, got %v", err)
	}
}

func TestListRepositoriesUsesInstallationAccessToken(t *testing.T) {
	client := NewClient(config.GitHubConfig{
		APIBaseURL: "https://api.github.test",
		APIVersion: "2026-03-10",
		AppID:      "12345",
		PrivateKey: testGitHubPrivateKeyPEM(t),
	}, false)
	client.now = func() time.Time {
		return time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	}

	requests := 0
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Fatalf("expected github accept header, got %q", r.Header.Get("Accept"))
			}
			if r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
				t.Fatalf("expected github api version, got %q", r.Header.Get("X-GitHub-Api-Version"))
			}

			switch r.URL.Path {
			case "/app/installations/1001/access_tokens":
				if r.Method != http.MethodPost {
					t.Fatalf("expected token POST, got %s", r.Method)
				}
				if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
					t.Fatalf("expected jwt bearer auth, got %q", r.Header.Get("Authorization"))
				}
				jwt := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				if parts := strings.Split(jwt, "."); len(parts) != 3 {
					t.Fatalf("expected jwt with 3 parts, got %q", jwt)
				}

				return &http.Response{
					StatusCode: http.StatusCreated,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"token":"installation-token","expires_at":"2026-06-12T11:00:00Z"}`)),
				}, nil
			case "/installation/repositories":
				if r.Method != http.MethodGet {
					t.Fatalf("expected repositories GET, got %s", r.Method)
				}
				if r.Header.Get("Authorization") != "Bearer installation-token" {
					t.Fatalf("expected installation bearer auth, got %q", r.Header.Get("Authorization"))
				}
				if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("per_page") != "100" {
					t.Fatalf("unexpected repository query: %s", r.URL.RawQuery)
				}

				header := make(http.Header)
				header.Set("Link", `<https://api.github.test/installation/repositories?page=2>; rel="next"`)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     header,
					Body: io.NopCloser(strings.NewReader(`{
						"total_count": 1,
						"repositories": [
							{
								"id": 2001,
								"name": "home",
								"full_name": "mi-car/home",
								"owner": {"login": "mi-car", "type": "Organization"},
								"private": true,
								"html_url": "https://github.com/mi-car/home",
								"clone_url": "https://github.com/mi-car/home.git",
								"ssh_url": "git@github.com:mi-car/home.git",
								"default_branch": "main",
								"description": "Home module",
								"updated_at": "2026-06-12T10:00:00Z"
							}
						]
					}`)),
				}, nil
			default:
				t.Fatalf("unexpected github path %s", r.URL.Path)
				return nil, nil
			}
		}),
	}

	page, err := client.ListRepositories(context.Background(), domain.CodeSourceAppConfig{
		AppID:      "12345",
		PrivateKey: testGitHubPrivateKeyPEM(t),
	}, 1001, 1, 100)
	if err != nil {
		t.Fatalf("list repositories: %v", err)
	}

	if requests != 2 {
		t.Fatalf("expected token and repository requests, got %d", requests)
	}
	if !page.HasMore {
		t.Fatal("expected next page from link header")
	}
	if len(page.Repositories) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(page.Repositories))
	}
	repository := page.Repositories[0]
	if repository.ExternalRepositoryID != "2001" || repository.FullName != "mi-car/home" || repository.WebURL != "https://github.com/mi-car/home" {
		t.Fatalf("unexpected repository: %+v", repository)
	}
	if repository.CloneURL != "https://github.com/mi-car/home.git" || repository.DefaultBranch != "main" || !repository.Private {
		t.Fatalf("unexpected repository clone metadata: %+v", repository)
	}
}

func testGitHubPrivateKeyPEM(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
