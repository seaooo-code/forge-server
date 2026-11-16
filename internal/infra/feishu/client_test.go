package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestSearchUsersUsesOfficialGETRequest(t *testing.T) {
	client := NewClient(config.FeishuConfig{
		UserSearchURL: "https://open.feishu.cn/open-apis/search/v1/user",
		UserBatchURL:  "https://open.feishu.cn/open-apis/contact/v3/users/batch",
	}, false)
	requestCount := 0
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requestCount++
			switch r.URL.Path {
			case "/open-apis/search/v1/user":
				if r.Method != http.MethodGet {
					t.Fatalf("expected search GET, got %s", r.Method)
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
								},
								{
									"avatar": {},
									"department_ids": [],
									"name": "李四",
									"open_id": "ou-second",
									"user_id": "user-second"
								}
							]
						}
					}`)),
				}, nil
			case "/open-apis/contact/v3/users/batch":
				if r.Method != http.MethodGet {
					t.Fatalf("expected batch GET, got %s", r.Method)
				}
				if r.Header.Get("Authorization") != "Bearer user-access-token" {
					t.Fatalf("expected bearer token for batch, got %q", r.Header.Get("Authorization"))
				}
				if r.URL.Query().Get("user_id_type") != "open_id" {
					t.Fatalf("expected user_id_type=open_id, got %q", r.URL.Query().Get("user_id_type"))
				}
				if r.URL.Query().Get("department_id_type") != "open_department_id" {
					t.Fatalf("expected department_id_type=open_department_id, got %q", r.URL.Query().Get("department_id_type"))
				}
				userIDs := r.URL.Query()["user_ids"]
				if len(userIDs) != 2 || userIDs[0] != "ou-test" || userIDs[1] != "ou-second" {
					t.Fatalf("expected repeated user_ids, got %#v", userIDs)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(bytes.NewBufferString(`{
						"code": 0,
						"msg": "success",
						"data": {
							"items": [
								{"open_id": "ou-test", "email": "zhangsan@example.com"},
								{"open_id": "ou-second", "email": "lisi@example.com"}
							]
						}
					}`)),
				}, nil
			default:
				t.Fatalf("unexpected request path %s", r.URL.Path)
				return nil, nil
			}
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
	if requestCount != 2 {
		t.Fatalf("expected search and batch requests, got %d", requestCount)
	}
	if len(page.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(page.Users))
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
	if user.Email != "zhangsan@example.com" {
		t.Fatalf("expected email from batch user response, got %q", user.Email)
	}
	if page.Users[1].Email != "lisi@example.com" {
		t.Fatalf("expected second email from batch user response, got %q", page.Users[1].Email)
	}
}

func TestAppendUserEmailsChunksBatchRequestsAtOfficialLimit(t *testing.T) {
	client := NewClient(config.FeishuConfig{
		UserBatchURL: "https://open.feishu.cn/open-apis/contact/v3/users/batch",
	}, false)
	requestCount := 0
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requestCount++
			userIDs := r.URL.Query()["user_ids"]
			if len(userIDs) == 0 || len(userIDs) > feishuUserBatchLimit {
				t.Fatalf("expected 1-%d user_ids, got %d", feishuUserBatchLimit, len(userIDs))
			}

			items := make([]userBatchItem, 0, len(userIDs))
			for _, userID := range userIDs {
				items = append(items, userBatchItem{
					OpenID: userID,
					Email:  userID + "@example.com",
				})
			}
			var body bytes.Buffer
			if err := json.NewEncoder(&body).Encode(userBatchResponse{
				Code: 0,
				Msg:  "success",
				Data: userBatchData{Items: items},
			}); err != nil {
				t.Fatalf("encode response: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(&body),
			}, nil
		}),
	}

	users := make([]domain.FeishuUserSearchResult, feishuUserBatchLimit+1)
	for index := range users {
		users[index].OpenID = "ou-test-" + strconv.Itoa(index)
	}

	if err := client.appendUserEmails(context.Background(), "user-access-token", users); err != nil {
		t.Fatalf("append user emails: %v", err)
	}

	if requestCount != 2 {
		t.Fatalf("expected 2 batch requests, got %d", requestCount)
	}
	if users[0].Email != "ou-test-0@example.com" {
		t.Fatalf("expected first email, got %q", users[0].Email)
	}
	if users[feishuUserBatchLimit].Email != "ou-test-50@example.com" {
		t.Fatalf("expected chunked email, got %q", users[feishuUserBatchLimit].Email)
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

func TestSearchUsersReturnsResultsWhenEmailEnrichmentUnauthorized(t *testing.T) {
	client := NewClient(config.FeishuConfig{
		AppID:         "cli_test",
		AppSecret:     "secret",
		UserSearchURL: "https://open.feishu.cn/open-apis/search/v1/user",
		UserBatchURL:  "https://open.feishu.cn/open-apis/contact/v3/users/batch",
	}, false)
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var body string
			switch r.URL.Path {
			case "/open-apis/search/v1/user":
				body = `{"code":0,"msg":"success","data":{"users":[{"open_id":"ou-1","name":"张三"}],"has_more":false}}`
			case "/open-apis/contact/v3/users/batch":
				body = `{"code":99991679,"msg":"Unauthorized. You do not have permission to perform the requested operation on the resource."}`
			default:
				t.Fatalf("unexpected request %s", r.URL.Path)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		}),
	}

	page, err := client.SearchUsers(context.Background(), "user-access-token", "张", 20, "")
	if err != nil {
		t.Fatalf("search should succeed without email permission: %v", err)
	}
	if len(page.Users) != 1 || page.Users[0].Name != "张三" || page.Users[0].Email != "" {
		t.Fatalf("expected user without email, got %#v", page.Users)
	}
}
