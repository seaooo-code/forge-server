package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const feishuUserBatchLimit = 50

// OAuthScope 是登录时申请的用户权限，由平台功能决定：
//   - offline_access：返回 refresh token，登录态可以续期；
//   - contact:user:search：搜索负责人；
//   - contact:contact.base:readonly、contact:user.email:readonly：批量补全负责人邮箱（缺失时搜索照常返回，只是没有邮箱）。
//
// 新增依赖用户授权的功能时在这里追加；每个租户的飞书应用后台也要开通对应权限，用户重新授权后生效。
const OAuthScope = "offline_access contact:user:search contact:contact.base:readonly contact:user.email:readonly"

type Client struct {
	cfg        config.FeishuConfig
	httpClient *http.Client
}

func NewClient(cfg config.FeishuConfig, telemetryEnabled bool) *Client {
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
	}
}

// AuthorizationURL 生成指定租户应用的授权地址。redirectURI 由调用方校验，必须在应用的重定向白名单里。
func (c *Client) AuthorizationURL(app domain.FeishuApp, redirectURI string, state string) (string, error) {
	if !c.configured(app) || redirectURI == "" {
		return "", domain.ErrFeishuAuthNotConfigured
	}

	authURL, err := url.Parse(c.cfg.AuthURL)
	if err != nil {
		return "", fmt.Errorf("%w: parse authorization url: %w", domain.ErrFeishuAuthorization, err)
	}

	values := authURL.Query()
	values.Set("client_id", app.AppID)
	values.Set("response_type", "code")
	values.Set("redirect_uri", redirectURI)
	values.Set("state", state)
	values.Set("scope", OAuthScope)
	authURL.RawQuery = values.Encode()

	return authURL.String(), nil
}

// ExchangeCode 用授权码换取 user access token。redirectURI 必须与发起授权时一致。
func (c *Client) ExchangeCode(ctx context.Context, app domain.FeishuApp, redirectURI string, code string) (domain.FeishuToken, error) {
	if !c.configured(app) {
		return domain.FeishuToken{}, domain.ErrFeishuAuthNotConfigured
	}

	body := tokenRequest{
		GrantType:    "authorization_code",
		ClientID:     app.AppID,
		ClientSecret: app.AppSecret,
		Code:         code,
		RedirectURI:  redirectURI,
	}

	return c.requestToken(ctx, body)
}

func (c *Client) RefreshAccessToken(ctx context.Context, app domain.FeishuApp, refreshToken string) (domain.FeishuToken, error) {
	if !c.configured(app) {
		return domain.FeishuToken{}, domain.ErrFeishuAuthNotConfigured
	}

	body := tokenRequest{
		GrantType:    "refresh_token",
		ClientID:     app.AppID,
		ClientSecret: app.AppSecret,
		RefreshToken: refreshToken,
	}

	return c.requestToken(ctx, body)
}

func (c *Client) GetUserInfo(ctx context.Context, accessToken string) (domain.FeishuUser, error) {
	if c.cfg.UserInfoURL == "" {
		return domain.FeishuUser{}, domain.ErrFeishuAuthNotConfigured
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.UserInfoURL, nil)
	if err != nil {
		return domain.FeishuUser{}, fmt.Errorf("%w: build user info request: %w", domain.ErrFeishuAuthorization, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.FeishuUser{}, fmt.Errorf("%w: request user info: %w", domain.ErrFeishuAuthorization, err)
	}
	defer resp.Body.Close()

	var response userInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return domain.FeishuUser{}, fmt.Errorf("%w: decode user info response: %w", domain.ErrFeishuAuthorization, err)
	}

	if response.Code != 0 || response.Data.UnionID == "" {
		return domain.FeishuUser{}, fmt.Errorf("%w: user info error code=%d msg=%s", domain.ErrFeishuAuthorization, response.Code, response.Msg)
	}

	return domain.FeishuUser{
		UnionID:         response.Data.UnionID,
		OpenID:          response.Data.OpenID,
		UserID:          response.Data.UserID,
		Name:            response.Data.Name,
		EnName:          response.Data.EnName,
		AvatarURL:       response.Data.AvatarURL,
		AvatarThumb:     response.Data.AvatarThumb,
		AvatarMiddle:    response.Data.AvatarMiddle,
		AvatarBig:       response.Data.AvatarBig,
		Email:           response.Data.Email,
		EnterpriseEmail: response.Data.EnterpriseEmail,
		Mobile:          response.Data.Mobile,
		TenantKey:       response.Data.TenantKey,
		EmployeeNo:      response.Data.EmployeeNo,
	}, nil
}

func (c *Client) SearchUsers(ctx context.Context, accessToken string, query string, pageSize int, pageToken string) (domain.FeishuUserSearchPage, error) {
	if c.cfg.UserSearchURL == "" {
		return domain.FeishuUserSearchPage{}, domain.ErrFeishuAuthNotConfigured
	}
	if accessToken == "" {
		return domain.FeishuUserSearchPage{}, domain.ErrFeishuAuthorization
	}

	searchURL, err := url.Parse(c.cfg.UserSearchURL)
	if err != nil {
		return domain.FeishuUserSearchPage{}, fmt.Errorf("%w: parse user search url: %w", domain.ErrFeishuAuthorization, err)
	}
	values := searchURL.Query()
	values.Set("query", query)
	values.Set("page_size", strconv.Itoa(pageSize))
	if pageToken = strings.TrimSpace(pageToken); pageToken != "" {
		values.Set("page_token", pageToken)
	}
	searchURL.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL.String(), nil)
	if err != nil {
		return domain.FeishuUserSearchPage{}, fmt.Errorf("%w: build user search request: %w", domain.ErrFeishuAuthorization, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.FeishuUserSearchPage{}, fmt.Errorf("%w: send user search request: %w", domain.ErrFeishuAuthorization, err)
	}
	defer resp.Body.Close()

	var response userSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return domain.FeishuUserSearchPage{}, fmt.Errorf("%w: decode user search response: %w", domain.ErrFeishuAuthorization, err)
	}
	if c.cfg.DebugLogSearchResponse {
		logUserSearchResponse(response)
	}
	if response.Code != 0 {
		return domain.FeishuUserSearchPage{}, &domain.FeishuAPIError{
			Code:     response.Code,
			Endpoint: "search.v1.user",
			Msg:      response.Msg,
		}
	}

	users := make([]domain.FeishuUserSearchResult, 0, len(response.Data.Users))
	for _, rawItem := range response.Data.Users {
		var item userSearchItem
		if err := json.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		departmentIDs := item.DepartmentIDs
		if departmentIDs == nil {
			departmentIDs = []string{}
		}
		users = append(users, domain.FeishuUserSearchResult{
			OpenID:        item.OpenID,
			UserID:        item.UserID,
			Name:          item.Name,
			AvatarURL:     item.avatarURL(),
			DepartmentIDs: departmentIDs,
		})
	}
	// 邮箱只是补充信息：应用或用户未授权通讯录字段权限时，仍返回搜索结果，只是不带邮箱。
	// 只有配置缺失这类非飞书接口错误才让整个搜索失败。
	if err := c.appendUserEmails(ctx, accessToken, users); err != nil {
		var apiErr *domain.FeishuAPIError
		if !errors.As(err, &apiErr) {
			return domain.FeishuUserSearchPage{}, err
		}
		slog.WarnContext(ctx, "feishu user search: skip email enrichment", "code", apiErr.Code, "endpoint", apiErr.Endpoint, "msg", apiErr.Msg)
	}

	return domain.FeishuUserSearchPage{
		Users:     users,
		HasMore:   response.Data.HasMore,
		PageToken: response.Data.PageToken,
	}, nil
}

func (c *Client) appendUserEmails(ctx context.Context, accessToken string, users []domain.FeishuUserSearchResult) error {
	if len(users) == 0 {
		return nil
	}
	if c.cfg.UserBatchURL == "" {
		return domain.ErrFeishuAuthNotConfigured
	}

	openIDs := make([]string, 0, len(users))
	for _, user := range users {
		if user.OpenID != "" {
			openIDs = append(openIDs, user.OpenID)
		}
	}
	if len(openIDs) == 0 {
		return nil
	}

	emails := make(map[string]string, len(openIDs))
	for start := 0; start < len(openIDs); start += feishuUserBatchLimit {
		end := start + feishuUserBatchLimit
		if end > len(openIDs) {
			end = len(openIDs)
		}
		batchEmails, err := c.fetchUserEmails(ctx, accessToken, openIDs[start:end])
		if err != nil {
			return err
		}
		for openID, email := range batchEmails {
			emails[openID] = email
		}
	}

	for index := range users {
		if email := emails[users[index].OpenID]; email != "" {
			users[index].Email = email
		}
	}
	return nil
}

func (c *Client) fetchUserEmails(ctx context.Context, accessToken string, openIDs []string) (map[string]string, error) {
	batchURL, err := url.Parse(c.cfg.UserBatchURL)
	if err != nil {
		return nil, fmt.Errorf("%w: parse user batch url: %w", domain.ErrFeishuAuthorization, err)
	}
	values := batchURL.Query()
	values.Set("user_id_type", "open_id")
	values.Set("department_id_type", "open_department_id")
	for _, openID := range openIDs {
		values.Add("user_ids", openID)
	}
	batchURL.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, batchURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build user batch request: %w", domain.ErrFeishuAuthorization, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: send user batch request: %w", domain.ErrFeishuAuthorization, err)
	}
	defer resp.Body.Close()

	var response userBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("%w: decode user batch response: %w", domain.ErrFeishuAuthorization, err)
	}
	if response.Code != 0 {
		return nil, &domain.FeishuAPIError{
			Code:     response.Code,
			Endpoint: "contact.v3.user.batch",
			Msg:      response.Msg,
		}
	}

	emails := make(map[string]string, len(response.Data.Items))
	for _, item := range response.Data.Items {
		if item.OpenID != "" {
			emails[item.OpenID] = item.Email
		}
	}
	return emails, nil
}

func (c *Client) configured(app domain.FeishuApp) bool {
	return app.AppID != "" && app.AppSecret != "" && c.cfg.TokenURL != "" && c.cfg.UserInfoURL != "" && c.cfg.UserBatchURL != "" && c.cfg.AuthURL != ""
}

func (c *Client) postJSON(ctx context.Context, requestURL string, body any, out any) error {
	return c.postJSONWithAuth(ctx, requestURL, body, "", out)
}

func (c *Client) postJSONWithAuth(ctx context.Context, requestURL string, body any, bearerToken string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: encode request: %w", domain.ErrFeishuAuthorization, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: build request: %w", domain.ErrFeishuAuthorization, err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: send request: %w", domain.ErrFeishuAuthorization, err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decode response: %w", domain.ErrFeishuAuthorization, err)
	}

	return nil
}

func (c *Client) requestToken(ctx context.Context, body tokenRequest) (domain.FeishuToken, error) {
	var response tokenResponse
	if err := c.postJSON(ctx, c.cfg.TokenURL, body, &response); err != nil {
		return domain.FeishuToken{}, err
	}

	if response.Code != 0 || response.AccessToken == "" {
		return domain.FeishuToken{}, fmt.Errorf("%w: token error code=%d error=%s description=%s", domain.ErrFeishuAuthorization, response.Code, response.Error, response.ErrorDescription)
	}

	return domain.FeishuToken{
		AccessToken:           response.AccessToken,
		ExpiresIn:             response.ExpiresIn,
		RefreshToken:          response.RefreshToken,
		RefreshTokenExpiresIn: response.RefreshTokenExpiresIn,
		TokenType:             response.TokenType,
		Scope:                 response.Scope,
	}, nil
}

type tokenRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Code         string `json:"code,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type tokenResponse struct {
	Code                  int    `json:"code"`
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	TokenType             string `json:"token_type"`
	Scope                 string `json:"scope"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

type userInfoResponse struct {
	Code int          `json:"code"`
	Msg  string       `json:"msg"`
	Data userInfoData `json:"data"`
}

type userInfoData struct {
	Name            string `json:"name"`
	EnName          string `json:"en_name"`
	AvatarURL       string `json:"avatar_url"`
	AvatarThumb     string `json:"avatar_thumb"`
	AvatarMiddle    string `json:"avatar_middle"`
	AvatarBig       string `json:"avatar_big"`
	OpenID          string `json:"open_id"`
	UnionID         string `json:"union_id"`
	Email           string `json:"email"`
	EnterpriseEmail string `json:"enterprise_email"`
	UserID          string `json:"user_id"`
	Mobile          string `json:"mobile"`
	TenantKey       string `json:"tenant_key"`
	EmployeeNo      string `json:"employee_no"`
}

type userSearchResponse struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	Data userSearchData `json:"data"`
}

type userSearchData struct {
	HasMore   bool              `json:"has_more"`
	PageToken string            `json:"page_token"`
	Users     []json.RawMessage `json:"users"`
}

type userSearchItem struct {
	Avatar        userSearchAvatar `json:"avatar"`
	DepartmentIDs []string         `json:"department_ids"`
	Name          string           `json:"name"`
	OpenID        string           `json:"open_id"`
	UserID        string           `json:"user_id"`
}

type userSearchAvatar struct {
	Avatar72     string `json:"avatar_72"`
	Avatar240    string `json:"avatar_240"`
	Avatar640    string `json:"avatar_640"`
	AvatarOrigin string `json:"avatar_origin"`
}

type userBatchResponse struct {
	Code int           `json:"code"`
	Msg  string        `json:"msg"`
	Data userBatchData `json:"data"`
}

type userBatchData struct {
	Items []userBatchItem `json:"items"`
}

type userBatchItem struct {
	OpenID string `json:"open_id"`
	Email  string `json:"email"`
}

func (i userSearchItem) avatarURL() string {
	switch {
	case i.Avatar.Avatar72 != "":
		return i.Avatar.Avatar72
	case i.Avatar.Avatar240 != "":
		return i.Avatar.Avatar240
	case i.Avatar.Avatar640 != "":
		return i.Avatar.Avatar640
	default:
		return i.Avatar.AvatarOrigin
	}
}

func logUserSearchResponse(response userSearchResponse) {
	users := make([]string, 0, len(response.Data.Users))
	for _, raw := range response.Data.Users {
		users = append(users, sanitizeRawJSONForLog(raw))
	}

	slog.Default().Debug(
		"feishu user search response",
		"code", response.Code,
		"msg", response.Msg,
		"has_more", response.Data.HasMore,
		"page_token_present", response.Data.PageToken != "",
		"user_count", len(response.Data.Users),
		"users", users,
	)
}

func sanitizeRawJSONForLog(raw json.RawMessage) string {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "<invalid-json>"
	}

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(sanitizeJSONValueForLog(value)); err != nil {
		return "<marshal-failed>"
	}
	return strings.TrimSpace(buffer.String())
}

func sanitizeJSONValueForLog(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = sanitizeJSONValueForLog(item)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, sanitizeJSONValueForLog(item))
		}
		return result
	case string:
		if typed == "" {
			return ""
		}
		return "<string>"
	case json.Number:
		return "<number>"
	case bool:
		return typed
	case nil:
		return nil
	default:
		return "<value>"
	}
}
