package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type codeSourceHandler struct {
	authService        FeishuAuthService
	authorization      AuthorizationService
	installRedirectURL string
	service            CodeSourceService
}

type codeSourcesResponse struct {
	Apps []codeSourceAppResponse `json:"apps"`
}

type codeSourceAppResponse struct {
	ConfigurationStatus string `json:"configurationStatus"`
	ConnectionCount     int    `json:"connectionCount"`
	Description         string `json:"description"`
	ID                  string `json:"id"`
	InstalledAt         string `json:"installedAt,omitempty"`
	Name                string `json:"name"`
	Provider            string `json:"provider"`
	Status              string `json:"status"`
}

type codeSourceInstallURLResponse struct {
	URL string `json:"url"`
}

type codeSourceAppManifestResponse struct {
	ActionURL string `json:"actionUrl"`
	Manifest  string `json:"manifest"`
}

type codeSourceConnectionsResponse struct {
	Connections []codeSourceConnectionResponse `json:"connections"`
}

type codeSourceConnectionDetailResponse struct {
	Connection codeSourceConnectionResponse `json:"connection"`
}

// codeSourceConnectionResponse 不包含凭证和 webhook secret，只返回凭证预览。
type codeSourceConnectionResponse struct {
	APIBaseURL          string `json:"apiBaseUrl"`
	CreatedAt           string `json:"createdAt"`
	CreatedBy           string `json:"createdBy"`
	CreatedByUserID     string `json:"createdByUserId"`
	CredentialExpiresAt string `json:"credentialExpiresAt"`
	CredentialPreview   string `json:"credentialPreview"`
	ExternalAccount     string `json:"externalAccount"`
	ExternalID          string `json:"externalId"`
	ID                  string `json:"id"`
	LastVerifiedAt      string `json:"lastVerifiedAt"`
	Name                string `json:"name"`
	Provider            string `json:"provider"`
	Status              string `json:"status"`
	UpdatedAt           string `json:"updatedAt"`
	WebBaseURL          string `json:"webBaseUrl"`
}

type codeSourceConnectionCreateRequest struct {
	APIBaseURL string `json:"apiBaseUrl"`
	Credential string `json:"credential"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	WebBaseURL string `json:"webBaseUrl"`
}

type codeSourceConnectionUpdateRequest struct {
	Credential *string `json:"credential"`
	Name       *string `json:"name"`
	Status     *string `json:"status"`
}

type codeSourceConnectionWebhookResponse struct {
	Path     string `json:"path"`
	Provider string `json:"provider"`
	Secret   string `json:"secret"`
}

type codeSourceRepositorySearchResponse struct {
	HasMore      bool                           `json:"hasMore"`
	Page         int                            `json:"page"`
	PageSize     int                            `json:"pageSize"`
	Repositories []codeSourceRepositoryResponse `json:"repositories"`
}

type codeSourceRepositoryResponse struct {
	CloneURL             string `json:"cloneUrl"`
	ConnectionID         string `json:"connectionId"`
	DefaultBranch        string `json:"defaultBranch"`
	Description          string `json:"description"`
	ExternalRepositoryID string `json:"externalRepositoryId"`
	FullName             string `json:"fullName"`
	LastSyncedAt         string `json:"lastSyncedAt"`
	Private              bool   `json:"private"`
	WebURL               string `json:"webUrl"`
}

func registerCodeSourceRoutes(group *echo.Group, publicGroup *echo.Group, service CodeSourceService, authService FeishuAuthService, authorization AuthorizationService, installRedirectURL string) {
	handler := codeSourceHandler{
		authService:        authService,
		authorization:      authorization,
		installRedirectURL: installRedirectURL,
		service:            service,
	}

	// GitHub App 创建与安装流程。
	group.GET("/code-sources", handler.list)
	group.GET("/code-sources/github/manifest", handler.manifest)
	publicGroup.GET("/code-sources/github/manifest/callback", handler.completeManifest)
	group.GET("/code-sources/github/install-url", handler.installURL)
	group.GET("/code-sources/github/install/callback", handler.completeInstallation)

	// 平台级代码源连接：PlatformAdmin / SuperAdmin 管理，有模块权限的人可读取和使用。
	group.GET("/code-source-connections", handler.listConnections)
	group.POST("/code-source-connections", handler.createConnection)
	group.GET("/code-source-connections/:id", handler.getConnection)
	group.PATCH("/code-source-connections/:id", handler.updateConnection)
	group.DELETE("/code-source-connections/:id", handler.deleteConnection)
	group.POST("/code-source-connections/:id/verify", handler.verifyConnection)
	group.GET("/code-source-connections/:id/repositories", handler.searchRepositories)
	group.GET("/code-source-connections/:id/webhook", handler.connectionWebhook)
}

func (h codeSourceHandler) list(c echo.Context) error {
	if h.service == nil {
		return codeSourceUnavailable(c)
	}
	if _, ok, err := h.currentSession(c); err != nil || !ok {
		return err
	}

	apps, err := h.service.ListCodeSources(c.Request().Context())
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}

	response := codeSourcesResponse{Apps: make([]codeSourceAppResponse, 0, len(apps))}
	for _, app := range apps {
		response.Apps = append(response.Apps, codeSourceAppResponse{
			ConfigurationStatus: string(app.ConfigurationStatus),
			ConnectionCount:     app.ConnectionCount,
			Description:         app.Description,
			ID:                  string(app.ID),
			InstalledAt:         formatOptionalTime(app.InstalledAt),
			Name:                app.Name,
			Provider:            string(app.Provider),
			Status:              string(app.Status),
		})
	}
	return c.JSON(http.StatusOK, response)
}

func (h codeSourceHandler) manifest(c echo.Context) error {
	if _, ok, err := h.authorize(c, domain.ActionCodeSourceManage); err != nil || !ok {
		return err
	}

	manifest, err := h.service.GetAppManifest(c.Request().Context())
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, codeSourceAppManifestResponse{
		ActionURL: manifest.ActionURL,
		Manifest:  manifest.Payload,
	})
}

func (h codeSourceHandler) installURL(c echo.Context) error {
	if _, ok, err := h.authorize(c, domain.ActionCodeSourceManage); err != nil || !ok {
		return err
	}

	installURL, err := h.service.GetInstallURL(c.Request().Context())
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, codeSourceInstallURLResponse{URL: installURL.URL})
}

func (h codeSourceHandler) completeManifest(c echo.Context) error {
	if h.service == nil {
		return codeSourceUnavailable(c)
	}

	installURL, err := h.service.CompleteAppManifest(c.Request().Context(), c.QueryParam("code"))
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}
	return c.Redirect(http.StatusFound, installURL.URL)
}

func (h codeSourceHandler) completeInstallation(c echo.Context) error {
	userID, ok, err := h.authorize(c, domain.ActionCodeSourceManage)
	if err != nil || !ok {
		return err
	}

	installationID, err := strconv.ParseInt(c.QueryParam("installation_id"), 10, 64)
	if err != nil || installationID <= 0 {
		return codeSourceErrorResponse(c, domain.ErrInvalidCodeSourceInput)
	}
	if _, err := h.service.CompleteInstallation(c.Request().Context(), userID, installationID, c.QueryParam("setup_action")); err != nil {
		return codeSourceErrorResponse(c, err)
	}
	return c.Redirect(http.StatusFound, h.installRedirectURL)
}

func (h codeSourceHandler) listConnections(c echo.Context) error {
	if _, ok, err := h.authorize(c, domain.ActionCodeSourceUse); err != nil || !ok {
		return err
	}

	connections, err := h.service.ListConnections(c.Request().Context(), domain.CodeSourceConnectionFilters{
		Provider: domain.CodeSourceProvider(strings.TrimSpace(c.QueryParam("provider"))),
		Status:   domain.CodeSourceConnectionStatus(strings.TrimSpace(c.QueryParam("status"))),
	})
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}

	response := codeSourceConnectionsResponse{Connections: make([]codeSourceConnectionResponse, 0, len(connections))}
	for _, conn := range connections {
		response.Connections = append(response.Connections, newCodeSourceConnectionResponse(conn))
	}
	return c.JSON(http.StatusOK, response)
}

func (h codeSourceHandler) createConnection(c echo.Context) error {
	userID, ok, err := h.authorize(c, domain.ActionCodeSourceManage)
	if err != nil || !ok {
		return err
	}

	var request codeSourceConnectionCreateRequest
	if err := c.Bind(&request); err != nil {
		return codeSourceErrorResponse(c, domain.ErrInvalidCodeSourceInput)
	}
	conn, err := h.service.CreateConnection(c.Request().Context(), domain.CodeSourceConnectionCreateInput{
		Provider:        domain.CodeSourceProvider(strings.TrimSpace(request.Provider)),
		Name:            request.Name,
		WebBaseURL:      request.WebBaseURL,
		APIBaseURL:      request.APIBaseURL,
		Credential:      request.Credential,
		CreatedByUserID: userID,
	})
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, codeSourceConnectionDetailResponse{Connection: newCodeSourceConnectionResponse(conn)})
}

func (h codeSourceHandler) getConnection(c echo.Context) error {
	if _, ok, err := h.authorize(c, domain.ActionCodeSourceUse); err != nil || !ok {
		return err
	}

	conn, err := h.service.GetConnection(c.Request().Context(), c.Param("id"))
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, codeSourceConnectionDetailResponse{Connection: newCodeSourceConnectionResponse(conn)})
}

func (h codeSourceHandler) updateConnection(c echo.Context) error {
	if _, ok, err := h.authorize(c, domain.ActionCodeSourceManage); err != nil || !ok {
		return err
	}

	var request codeSourceConnectionUpdateRequest
	if err := c.Bind(&request); err != nil {
		return codeSourceErrorResponse(c, domain.ErrInvalidCodeSourceInput)
	}
	input := domain.CodeSourceConnectionUpdateInput{Name: request.Name, Credential: request.Credential}
	if request.Status != nil {
		status := domain.CodeSourceConnectionStatus(strings.TrimSpace(*request.Status))
		input.Status = &status
	}

	conn, err := h.service.UpdateConnection(c.Request().Context(), c.Param("id"), input)
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, codeSourceConnectionDetailResponse{Connection: newCodeSourceConnectionResponse(conn)})
}

func (h codeSourceHandler) deleteConnection(c echo.Context) error {
	if _, ok, err := h.authorize(c, domain.ActionCodeSourceManage); err != nil || !ok {
		return err
	}

	if err := h.service.DeleteConnection(c.Request().Context(), c.Param("id")); err != nil {
		return codeSourceErrorResponse(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h codeSourceHandler) verifyConnection(c echo.Context) error {
	if _, ok, err := h.authorize(c, domain.ActionCodeSourceManage); err != nil || !ok {
		return err
	}

	conn, err := h.service.VerifyConnection(c.Request().Context(), c.Param("id"))
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, codeSourceConnectionDetailResponse{Connection: newCodeSourceConnectionResponse(conn)})
}

func (h codeSourceHandler) searchRepositories(c echo.Context) error {
	if _, ok, err := h.authorize(c, domain.ActionCodeSourceUse); err != nil || !ok {
		return err
	}

	page, pageOK := optionalIntQuery(c, "page")
	pageSize, pageSizeOK := optionalIntQuery(c, "pageSize")
	if !pageOK || !pageSizeOK {
		return codeSourceErrorResponse(c, domain.ErrInvalidCodeSourceInput)
	}
	input := domain.RepositorySearchInput{Query: c.QueryParam("query"), Page: page, PageSize: pageSize}

	result, err := h.service.SearchRepositories(c.Request().Context(), c.Param("id"), input)
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}

	response := codeSourceRepositorySearchResponse{
		HasMore:      result.HasMore,
		Page:         result.Page,
		PageSize:     result.PageSize,
		Repositories: make([]codeSourceRepositoryResponse, 0, len(result.Repositories)),
	}
	for _, repository := range result.Repositories {
		response.Repositories = append(response.Repositories, codeSourceRepositoryResponse{
			CloneURL:             repository.CloneURL,
			ConnectionID:         repository.ConnectionID,
			DefaultBranch:        repository.DefaultBranch,
			Description:          repository.Description,
			ExternalRepositoryID: repository.ExternalRepositoryID,
			FullName:             repository.FullName,
			LastSyncedAt:         formatTime(repository.LastSyncedAt),
			Private:              repository.Private,
			WebURL:               repository.WebURL,
		})
	}
	return c.JSON(http.StatusOK, response)
}

func (h codeSourceHandler) connectionWebhook(c echo.Context) error {
	if _, ok, err := h.authorize(c, domain.ActionCodeSourceUse); err != nil || !ok {
		return err
	}

	webhook, err := h.service.GetConnectionWebhook(c.Request().Context(), c.Param("id"))
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, codeSourceConnectionWebhookResponse{
		Path:     webhook.Path,
		Provider: string(webhook.Provider),
		Secret:   webhook.Secret,
	})
}

// authorize 校验会话与平台级代码源权限，返回当前用户的平台 users.id；ok=false 时响应已写出。
func (h codeSourceHandler) authorize(c echo.Context, action domain.AuthorizationAction) (string, bool, error) {
	if h.service == nil {
		return "", false, codeSourceUnavailable(c)
	}
	session, ok, err := h.currentSession(c)
	if err != nil || !ok {
		return "", false, err
	}
	if err := authorizeCurrentUser(c, h.authorization, action, domain.AuthorizationResource{}); err != nil {
		return "", false, err
	}
	return domain.ConsoleUserIDFromFeishu(session.User), true, nil
}

func (h codeSourceHandler) currentSession(c echo.Context) (domain.FeishuAuthResult, bool, error) {
	if session, ok := consoleSession(c); ok {
		return session, true, nil
	}
	if h.authService == nil {
		return domain.FeishuAuthResult{}, false, c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}

	sessionToken, ok := feishuSessionToken(c)
	if !ok {
		return domain.FeishuAuthResult{}, false, c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	session, err := h.authService.GetSession(c.Request().Context(), sessionToken)
	if err != nil {
		if errors.Is(err, domain.ErrFeishuSessionNotFound) ||
			errors.Is(err, domain.ErrFeishuSessionExpired) ||
			errors.Is(err, domain.ErrFeishuAuthorization) {
			clearFeishuSessionCookie(c)
			return domain.FeishuAuthResult{}, false, c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		if errors.Is(err, domain.ErrFeishuAuthNotConfigured) {
			return domain.FeishuAuthResult{}, false, c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
		}
		return domain.FeishuAuthResult{}, false, c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	setFeishuSessionCookie(c, session)
	c.Set(consoleSessionContextKey, session)

	return session, true, nil
}

func optionalIntQuery(c echo.Context, name string) (int, bool) {
	raw := strings.TrimSpace(c.QueryParam(name))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil
}

func codeSourceUnavailable(c echo.Context) error {
	return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source service unavailable"})
}

func codeSourceErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidCodeSourceInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid code source request"})
	case errors.Is(err, domain.ErrCodeSourceCredentialInvalid):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "code source credential is invalid"})
	case errors.Is(err, domain.ErrCodeSourceConnectionInactive):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "code source connection is not active"})
	case errors.Is(err, domain.ErrCodeSourceConnectionConflict):
		return c.JSON(http.StatusConflict, map[string]string{"error": "code source connection already exists"})
	case errors.Is(err, domain.ErrCodeSourceConnectionInUse):
		return c.JSON(http.StatusConflict, map[string]string{"error": "code source connection is referenced by modules"})
	case errors.Is(err, domain.ErrCodeSourceAuthNotConfigured):
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source auth not configured"})
	case errors.Is(err, domain.ErrCodeSourceConnectionNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "code source connection not found"})
	case errors.Is(err, domain.ErrCodeSourceRepositoryNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "code source repository not found"})
	case errors.Is(err, domain.ErrCodeSourceInstallationNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "code source installation not found"})
	case errors.Is(err, domain.ErrCodeSourceProviderNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "code source provider not found"})
	case errors.Is(err, domain.ErrCodeSourceAuthorization):
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "code source authorization failed"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newCodeSourceConnectionResponse(conn domain.CodeSourceConnection) codeSourceConnectionResponse {
	return codeSourceConnectionResponse{
		APIBaseURL:          conn.APIBaseURL,
		CreatedAt:           formatTime(conn.CreatedAt),
		CreatedBy:           conn.CreatedBy,
		CreatedByUserID:     conn.CreatedByUserID,
		CredentialExpiresAt: formatOptionalTime(conn.CredentialExpiresAt),
		CredentialPreview:   conn.CredentialPreview,
		ExternalAccount:     conn.ExternalAccount,
		ExternalID:          conn.ExternalID,
		ID:                  conn.ID,
		LastVerifiedAt:      formatOptionalTime(conn.LastVerifiedAt),
		Name:                conn.Name,
		Provider:            string(conn.Provider),
		Status:              string(conn.Status),
		UpdatedAt:           formatTime(conn.UpdatedAt),
		WebBaseURL:          conn.WebBaseURL,
	}
}
