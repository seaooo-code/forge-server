package httpserver

import (
	"errors"
	"net/http"
	"strconv"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type codeSourceHandler struct {
	authService        FeishuAuthService
	installRedirectURL string
	service            CodeSourceService
}

type codeSourcesResponse struct {
	Apps []codeSourceAppResponse `json:"apps"`
}

type codeSourceAppDetailResponse struct {
	App codeSourceAppResponse `json:"app"`
}

type codeSourceAppResponse struct {
	ConfigurationStatus string `json:"configurationStatus"`
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

type codeSourceRepositorySearchResponse struct {
	Repositories []codeSourceRepositoryResponse `json:"repositories"`
}

type codeSourceRepositoryResponse struct {
	CloneURL       string `json:"cloneUrl"`
	DefaultBranch  string `json:"defaultBranch"`
	Description    string `json:"description"`
	FullName       string `json:"fullName"`
	HTMLURL        string `json:"htmlUrl"`
	ID             string `json:"id"`
	InstallationID string `json:"installationId"`
	Name           string `json:"name"`
	Owner          string `json:"owner"`
	Private        bool   `json:"private"`
	Provider       string `json:"provider"`
	SSHURL         string `json:"sshUrl"`
	UpdatedAt      string `json:"updatedAt"`
}

func registerCodeSourceRoutes(group *echo.Group, publicGroup *echo.Group, service CodeSourceService, authService FeishuAuthService, installRedirectURL string) {
	handler := codeSourceHandler{
		authService:        authService,
		installRedirectURL: installRedirectURL,
		service:            service,
	}

	group.GET("/code-sources", handler.list)
	group.GET("/code-sources/:provider/manifest", handler.manifest)
	publicGroup.GET("/code-sources/:provider/manifest/callback", handler.completeManifest)
	group.GET("/code-sources/:provider/install-url", handler.installURL)
	group.GET("/code-sources/:provider/install/callback", handler.completeInstallation)
	group.DELETE("/code-sources/:provider/installations", handler.deleteInstallations)
	group.GET("/code-sources/:provider/repositories/search", handler.searchRepositories)
}

func (h codeSourceHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source service unavailable"})
	}

	session, ok, err := h.currentSession(c)
	if err != nil || !ok {
		return err
	}

	apps, err := h.service.ListCodeSources(c.Request().Context(), session.User.UnionID)
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, codeSourcesResponse{
		Apps: newCodeSourceAppResponses(apps),
	})
}

func (h codeSourceHandler) manifest(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source service unavailable"})
	}
	if _, ok, err := h.currentSession(c); err != nil || !ok {
		return err
	}

	manifest, err := h.service.GetAppManifest(c.Request().Context(), domain.CodeSourceProvider(c.Param("provider")))
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, codeSourceAppManifestResponse{
		ActionURL: manifest.ActionURL,
		Manifest:  manifest.Payload,
	})
}

func (h codeSourceHandler) installURL(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source service unavailable"})
	}

	session, ok, err := h.currentSession(c)
	if err != nil || !ok {
		return err
	}

	installURL, err := h.service.GetInstallURL(c.Request().Context(), domain.CodeSourceProvider(c.Param("provider")), session.User.UnionID)
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, codeSourceInstallURLResponse{
		URL: installURL.URL,
	})
}

func (h codeSourceHandler) completeManifest(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source service unavailable"})
	}

	installURL, err := h.service.CompleteAppManifest(c.Request().Context(), domain.CodeSourceProvider(c.Param("provider")), c.QueryParam("code"))
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}

	return c.Redirect(http.StatusFound, installURL.URL)
}

func (h codeSourceHandler) completeInstallation(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source service unavailable"})
	}

	session, ok, err := h.currentSession(c)
	if err != nil || !ok {
		return err
	}

	installationID, err := strconv.ParseInt(c.QueryParam("installation_id"), 10, 64)
	if err != nil || installationID <= 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid code source request"})
	}

	_, err = h.service.CompleteInstallation(
		c.Request().Context(),
		domain.CodeSourceProvider(c.Param("provider")),
		session.User.UnionID,
		installationID,
		c.QueryParam("setup_action"),
	)
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}

	return c.Redirect(http.StatusFound, h.installRedirectURL)
}

func (h codeSourceHandler) deleteInstallations(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source service unavailable"})
	}

	session, ok, err := h.currentSession(c)
	if err != nil || !ok {
		return err
	}

	if err := h.service.DeleteInstallations(c.Request().Context(), domain.CodeSourceProvider(c.Param("provider")), session.User.UnionID); err != nil {
		return codeSourceErrorResponse(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func (h codeSourceHandler) searchRepositories(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source service unavailable"})
	}

	session, ok, err := h.currentSession(c)
	if err != nil || !ok {
		return err
	}

	pageSize, ok := codeSourceRepositoryPageSize(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid code source request"})
	}

	result, err := h.service.SearchRepositories(c.Request().Context(), domain.CodeSourceRepositorySearchInput{
		OwnerUnionID: session.User.UnionID,
		PageSize:     pageSize,
		Provider:     domain.CodeSourceProvider(c.Param("provider")),
		Query:        c.QueryParam("query"),
	})
	if err != nil {
		return codeSourceErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, codeSourceRepositorySearchResponse{
		Repositories: newCodeSourceRepositoryResponses(result.Repositories),
	})
}

func codeSourceRepositoryPageSize(c echo.Context) (int, bool) {
	rawPageSize := c.QueryParam("pageSize")
	if rawPageSize == "" {
		rawPageSize = c.QueryParam("page_size")
	}
	if rawPageSize == "" {
		return 0, true
	}
	pageSize, err := strconv.Atoi(rawPageSize)
	if err != nil {
		return 0, false
	}
	return pageSize, true
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
		if errors.Is(err, domain.ErrFeishuSessionNotFound) || errors.Is(err, domain.ErrFeishuSessionExpired) {
			clearFeishuSessionCookie(c)
			return domain.FeishuAuthResult{}, false, c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		if errors.Is(err, domain.ErrFeishuAuthNotConfigured) {
			return domain.FeishuAuthResult{}, false, c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
		}
		if errors.Is(err, domain.ErrFeishuAuthorization) {
			return domain.FeishuAuthResult{}, false, c.JSON(http.StatusBadGateway, map[string]string{"error": "feishu authorization failed"})
		}
		return domain.FeishuAuthResult{}, false, c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	setFeishuSessionCookie(c, session)

	return session, true, nil
}

func codeSourceErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidCodeSourceInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid code source request"})
	case errors.Is(err, domain.ErrCodeSourceAuthNotConfigured):
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source auth not configured"})
	case errors.Is(err, domain.ErrCodeSourceInstallationNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "code source installation not found"})
	case errors.Is(err, domain.ErrCodeSourceProviderNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "code source provider not found"})
	case errors.Is(err, domain.ErrCodeSourceAuthorization):
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "code source authorization failed"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newCodeSourceAppResponses(apps []domain.CodeSourceApp) []codeSourceAppResponse {
	response := make([]codeSourceAppResponse, 0, len(apps))
	for _, app := range apps {
		response = append(response, newCodeSourceAppResponse(app))
	}
	return response
}

func newCodeSourceAppResponse(app domain.CodeSourceApp) codeSourceAppResponse {
	installedAt := ""
	if app.InstalledAt != nil {
		installedAt = formatTime(*app.InstalledAt)
	}

	return codeSourceAppResponse{
		ConfigurationStatus: string(app.ConfigurationStatus),
		Description:         app.Description,
		ID:                  string(app.ID),
		InstalledAt:         installedAt,
		Name:                app.Name,
		Provider:            string(app.Provider),
		Status:              string(app.Status),
	}
}

func newCodeSourceRepositoryResponses(repositories []domain.CodeSourceRepository) []codeSourceRepositoryResponse {
	response := make([]codeSourceRepositoryResponse, 0, len(repositories))
	for _, repository := range repositories {
		response = append(response, newCodeSourceRepositoryResponse(repository))
	}
	return response
}

func newCodeSourceRepositoryResponse(repository domain.CodeSourceRepository) codeSourceRepositoryResponse {
	updatedAt := ""
	if repository.UpdatedAt != nil {
		updatedAt = formatTime(*repository.UpdatedAt)
	}

	return codeSourceRepositoryResponse{
		CloneURL:       repository.CloneURL,
		DefaultBranch:  repository.DefaultBranch,
		Description:    repository.Description,
		FullName:       repository.FullName,
		HTMLURL:        repository.HTMLURL,
		ID:             strconv.FormatInt(repository.RepositoryID, 10),
		InstallationID: strconv.FormatInt(repository.InstallationID, 10),
		Name:           repository.Name,
		Owner:          repository.Owner,
		Private:        repository.Private,
		Provider:       string(repository.Provider),
		SSHURL:         repository.SSHURL,
		UpdatedAt:      updatedAt,
	}
}
