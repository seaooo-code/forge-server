package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"forge-server/internal/config"
)

func TestSearchUsersUsesOfficialGETRequest(t *testing.T) {
	client := NewClient(config.FeishuConfig{
		UserSearchURL: "https://open.feishu.cn/open-apis/search/v1/user",
	}, false)
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet {
				t.Fatalf("expected GET, got %s", r.Method)
			}
			if r.URL.Path != "/open-apis/search/v1/user" {
				t.Fatalf("expected official search path, got %s", r.URL.Path)
			}
			if r.URL.Query().Get("query") != "张三" {
				t.Fatalf("expected query param, got %q", r.URL.Query().Get("query"))
			}
			if r.URL.Query().Get("page_size") != "20" {
				t.Fatalf("expected page_size param, got %q", r.URL.Query().Get("page_size"))
			}
			if r.URL.Query().Get("page_token") != "cursor-in" {
				t.Fatalf("expected page_token param, got %q", r.URL.Query().Get("page_token"))
			}
			if r.Header.Get("Authorization") != "Bearer user-access-token" {
				t.Fatalf("expected bearer token, got %q", r.Header.Get("Authorization"))
			}
			if r.Header.Get("Content-Type") != "application/json; charset=utf-8" {
				t.Fatalf("expected content type, got %q", r.Header.Get("Content-Type"))
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(bytes.NewBufferString(`{
					"code": 0,
					"msg": "success",
					"data": {
						"has_more": true,
						"page_token": "cursor-out",
						"users": [
							{
								"avatar": {
									"avatar_72": "https://example.com/avatar-72.png",
									"avatar_240": "https://example.com/avatar-240.png",
									"avatar_640": "https://example.com/avatar-640.png",
									"avatar_origin": "https://example.com/avatar-origin.png"
								},
								"department_ids": ["od-rn"],
								"name": "张三",
								"open_id": "ou-test",
								"user_id": "user-test"
							}
						]
					}
				}`)),
			}, nil
		}),
	}

	page, err := client.SearchUsers(context.Background(), "user-access-token", "张三", 20, "cursor-in")
	if err != nil {
		t.Fatalf("search users: %v", err)
	}

	if !page.HasMore {
		t.Fatal("expected has more")
	}
	if page.PageToken != "cursor-out" {
		t.Fatalf("expected output page token, got %q", page.PageToken)
	}
	if len(page.Users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(page.Users))
	}
	user := page.Users[0]
	if user.UserID != "user-test" {
		t.Fatalf("expected user_id, got %q", user.UserID)
	}
	if user.OpenID != "ou-test" {
		t.Fatalf("expected open_id, got %q", user.OpenID)
	}
	if user.Name != "张三" {
		t.Fatalf("expected name, got %q", user.Name)
	}
	if user.AvatarURL != "https://example.com/avatar-72.png" {
		t.Fatalf("expected avatar_72 URL, got %q", user.AvatarURL)
	}
	if len(user.DepartmentIDs) != 1 || user.DepartmentIDs[0] != "od-rn" {
		t.Fatalf("expected department ids, got %#v", user.DepartmentIDs)
	}
}

func TestSanitizeRawJSONForLogRedactsValues(t *testing.T) {
	sanitized := sanitizeRawJSONForLog(json.RawMessage(`{
		"name": "张三",
		"department_ids": ["od-rn"],
		"active": true
	}`))

	if strings.Contains(sanitized, "张三") || strings.Contains(sanitized, "od-rn") {
		t.Fatalf("expected values to be redacted, got %s", sanitized)
	}
	if !strings.Contains(sanitized, `"name":"<string>"`) {
		t.Fatalf("expected field names to be preserved, got %s", sanitized)
	}
	if !strings.Contains(sanitized, `"active":true`) {
		t.Fatalf("expected booleans to be preserved, got %s", sanitized)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
