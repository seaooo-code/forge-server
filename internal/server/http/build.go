package httpserver

import (
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type buildHandler struct {
	authService AuthorizationService
	service     BuildService
}

// buildCreateRequest 只创建手动构建；webhook 构建由 PushEventHandler 在服务端创建。
type buildCreateRequest struct {
	ModuleID    string `json:"moduleId"`
	Environment string `json:"environment"`
	Branch      string `json:"branch"`
}

type buildRecordDetailResponse struct {
	Build buildRecordResponse `json:"build"`
}

type buildRecordsResponse struct {
	Builds     []buildRecordResponse      `json:"builds"`
	Pagination paginationResponse         `json:"pagination"`
	Summary    buildRecordSummaryResponse `json:"summary"`
}

type buildRecordResponse struct {
	ApplicationName          string                    `json:"applicationName"`
	ArtifactTree             *artifactTreeResponse     `json:"artifactTree,omitempty"`
	ArtifactKey              string                    `json:"artifactKey"`
	ArtifactURL              string                    `json:"artifactUrl"`
	CreatedAt                string                    `json:"createdAt"`
	CreatedBy                *userSummaryResponse      `json:"createdBy"`
	Branch                   string                    `json:"branch"`
	DefaultBranch            string                    `json:"defaultBranch"`
	DependencyBuilds         []buildDependencyResponse `json:"dependencyBuilds"`
	Environment              string                    `json:"environment"`
	EnvironmentName          string                    `json:"environmentName"`
	ErrorMessage             string                    `json:"errorMessage"`
	FinishedAt               string                    `json:"finishedAt"`
	ID                       string                    `json:"id"`
	Number                   int64                     `json:"number"`
	BuildAdapterID           string                    `json:"buildAdapterId"`
	BuildAdapterVersion      *int                      `json:"buildAdapterVersion"`
	RenderedDockerfileSHA256 string                    `json:"renderedDockerfileSha256"`
	CIPipelineID             int64                     `json:"ciPipelineId"`
	CIPipelineURL            string                    `json:"ciPipelineUrl"`
	ModuleID                 string                    `json:"moduleId"`
	ModuleName               string                    `json:"moduleName"`
	ManifestKey              string                    `json:"manifestKey"`
	SigningKeyID             string                    `json:"signingKeyId"`
	FileCount                int                       `json:"fileCount"`
	TotalSizeBytes           int64                     `json:"totalSizeBytes"`
	QueuedAt                 string                    `json:"queuedAt"`
	CodeSourceConnectionID   string                    `json:"codeSourceConnectionId"`
	CommitSHA                string                    `json:"commitSha"`
	CommitTitle              string                    `json:"commitTitle"`
	CommitAuthorName         string                    `json:"commitAuthorName"`
	CommittedAt              string                    `json:"committedAt"`
	Platforms                []string                  `json:"platforms"`
	RepositoryFullName       string                    `json:"repositoryFullName"`
	RepositoryID             string                    `json:"repositoryId"`
	RepositoryProvider       string                    `json:"repositoryProvider"`
	RepositoryWebURL         string                    `json:"repositoryWebUrl"`
	RuntimeVersion           string                    `json:"runtimeVersion"`
	ProvidedCapabilities     map[string]int64          `json:"providedCapabilities"`
	RequiredCapabilities     map[string]int64          `json:"requiredCapabilities"`
	StartedAt                string                    `json:"startedAt"`
	Status                   string                    `json:"status"`
	StorageConfigurationID   string                    `json:"storageConfigurationId"`
	StorageConfigurationName string                    `json:"storageConfigurationName"`
	TriggerID                string                    `json:"triggerId"`
	TriggerType              string                    `json:"triggerType"`
	UpdatedAt                string                    `json:"updatedAt"`
	WorkerID                 string                    `json:"workerId"`
}

// buildDependencyResponse 是构建时注入的依赖构建。buildNumber 和 moduleName 在依赖构建或模块不存在时为零值。
type buildDependencyResponse struct {
	BuildNumber    int64  `json:"buildNumber"`
	BuildRecordID  string `json:"buildRecordId"`
	ModuleID       string `json:"moduleId"`
	ModuleName     string `json:"moduleName"`
	RuntimeVersion string `json:"runtimeVersion"`
}

// userSummaryResponse 是创建人等关联用户的展示字段；用户为空时整个对象为 null。
type userSummaryResponse struct {
	AvatarURL string `json:"avatarUrl"`
	ID        string `json:"id"`
	Name      string `json:"name"`
}

type artifactTreeResponse struct {
	FileCount      int                        `json:"fileCount"`
	TotalSizeBytes int64                      `json:"totalSizeBytes"`
	Nodes          []artifactTreeNodeResponse `json:"nodes"`
}

type artifactTreeNodeResponse struct {
	Children    []artifactTreeNodeResponse `json:"children,omitempty"`
	ContentType string                     `json:"contentType,omitempty"`
	Name        string                     `json:"name"`
	ObjectKey   string                     `json:"objectKey,omitempty"`
	Path        string                     `json:"path"`
	SHA256      string                     `json:"sha256,omitempty"`
	SizeBytes   int64                      `json:"sizeBytes"`
	Type        string                     `json:"type"`
}

type buildRecordSummaryResponse struct {
	Failed    int `json:"failed"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Succeeded int `json:"succeeded"`
	Total     int `json:"total"`
}

func registerBuildRoutes(group *echo.Group, service BuildService, authService AuthorizationService) {
	handler := buildHandler{authService: authService, service: service}

	group.GET("/builds", handler.list)
	group.POST("/builds", handler.create)
	group.GET("/builds/:id", handler.get)
}

func (h buildHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build service unavailable"})
	}

	pagination, ok := requestPagination(c)
	if !ok {
		return buildErrorResponse(c, domain.ErrInvalidBuildInput)
	}

	records, summary, err := h.service.ListBuildRecords(c.Request().Context(), domain.BuildRecordFilters{
		ApplicationID: c.QueryParam("applicationId"),
		Environment:   c.QueryParam("environment"),
		ModuleID:      c.QueryParam("moduleId"),
		Pagination:    pagination,
		Status:        domain.BuildStatus(c.QueryParam("status")),
		TriggerType:   domain.BuildTriggerType(c.QueryParam("triggerType")),
	})
	if err != nil {
		return buildErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, buildRecordsResponse{
		Builds:     newBuildRecordResponses(records),
		Pagination: newPaginationResponse(pagination, summary.Total),
		Summary:    newBuildRecordSummaryResponse(summary),
	})
}

func (h buildHandler) get(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build service unavailable"})
	}

	record, err := h.service.GetBuildRecord(c.Request().Context(), c.Param("id"))
	if err != nil {
		return buildErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, buildRecordDetailResponse{Build: newBuildRecordDetailResponse(record)})
}

func (h buildHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build service unavailable"})
	}

	var request buildCreateRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid build request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionBuildCreate, domain.AuthorizationResource{ModuleID: request.ModuleID}); err != nil {
		return err
	}
	// authorizeCurrentUser 已校验会话和用户 id，这里只取值。
	session, _ := consoleSession(c)

	record, err := h.service.CreateBuildRecord(c.Request().Context(), domain.BuildRecordInput{
		ModuleID:        request.ModuleID,
		Environment:     request.Environment,
		Branch:          request.Branch,
		TriggerType:     domain.BuildTriggerTypeManual,
		CreatedByUserID: session.User.ID,
	})
	if err != nil {
		return buildErrorResponse(c, err)
	}

	return c.JSON(http.StatusCreated, buildRecordDetailResponse{Build: newBuildRecordResponse(record)})
}

func buildErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidBuildInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid build request"})
	case errors.Is(err, domain.ErrModuleNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "module not found"})
	case errors.Is(err, domain.ErrEnvironmentNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "environment not found"})
	case errors.Is(err, domain.ErrStorageConfigurationNotFound):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "storage configuration not found"})
	case errors.Is(err, domain.ErrBuildAdapterDisabled):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "build adapter is disabled"})
	case errors.Is(err, domain.ErrBuildRecordNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "build record not found"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newBuildRecordResponses(records []domain.BuildRecord) []buildRecordResponse {
	responses := make([]buildRecordResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, newBuildRecordResponse(record))
	}
	return responses
}

func newBuildRecordResponse(record domain.BuildRecord) buildRecordResponse {
	return newBuildRecordResponseWithTree(record, false)
}

func newBuildRecordDetailResponse(record domain.BuildRecord) buildRecordResponse {
	return newBuildRecordResponseWithTree(record, true)
}

func newBuildRecordResponseWithTree(record domain.BuildRecord, includeArtifactTree bool) buildRecordResponse {
	response := buildRecordResponse{
		ApplicationName:          record.ApplicationName,
		ArtifactKey:              record.ArtifactKey,
		ArtifactURL:              record.ArtifactURL,
		CreatedAt:                formatTime(record.CreatedAt),
		Branch:                   record.Branch,
		DefaultBranch:            record.DefaultBranch,
		Environment:              record.Environment,
		EnvironmentName:          record.EnvironmentName,
		ErrorMessage:             record.ErrorMessage,
		FinishedAt:               formatOptionalTime(record.FinishedAt),
		ID:                       record.ID,
		Number:                   record.Number,
		BuildAdapterID:           record.BuildAdapterID,
		BuildAdapterVersion:      record.BuildAdapterVersion,
		RenderedDockerfileSHA256: record.RenderedDockerfileSHA256,
		CIPipelineID:             record.CIPipelineID,
		CIPipelineURL:            record.CIPipelineURL,
		ModuleID:                 record.ModuleID,
		ModuleName:               record.ModuleName,
		ManifestKey:              record.ManifestKey,
		SigningKeyID:             record.Signature.KeyID,
		FileCount:                record.FileCount,
		TotalSizeBytes:           record.TotalSizeBytes,
		QueuedAt:                 formatTime(record.QueuedAt),
		CodeSourceConnectionID:   record.Repository.ConnectionID,
		CommitSHA:                record.CommitSHA,
		CommitTitle:              record.CommitTitle,
		CommitAuthorName:         record.CommitAuthorName,
		CommittedAt:              formatOptionalTime(record.CommittedAt),
		Platforms:                nonNilStrings(record.Platforms),
		RepositoryFullName:       record.Repository.FullName,
		RepositoryID:             record.Repository.ExternalRepositoryID,
		RepositoryProvider:       string(record.CodeSourceProvider),
		RepositoryWebURL:         record.Repository.WebURL,
		RuntimeVersion:           record.RuntimeVersion,
		ProvidedCapabilities:     nonNilCapabilities(record.ProvidedCapabilities),
		RequiredCapabilities:     nonNilCapabilities(record.RequiredCapabilities),
		StartedAt:                formatOptionalTime(record.StartedAt),
		Status:                   string(record.Status),
		StorageConfigurationID:   record.StorageConfigurationID,
		StorageConfigurationName: record.StorageConfigurationName,
		TriggerID:                record.TriggerID,
		TriggerType:              string(record.TriggerType),
		UpdatedAt:                formatTime(record.UpdatedAt),
		WorkerID:                 record.WorkerID,
	}
	response.DependencyBuilds = make([]buildDependencyResponse, 0, len(record.DependencyBuilds))
	for _, dependency := range record.DependencyBuilds {
		response.DependencyBuilds = append(response.DependencyBuilds, buildDependencyResponse{
			BuildNumber:    dependency.BuildNumber,
			BuildRecordID:  dependency.BuildRecordID,
			ModuleID:       dependency.ModuleID,
			ModuleName:     dependency.ModuleName,
			RuntimeVersion: dependency.RuntimeVersion,
		})
	}
	if record.CreatedBy != nil {
		response.CreatedBy = &userSummaryResponse{AvatarURL: record.CreatedBy.AvatarURL, ID: record.CreatedBy.ID, Name: record.CreatedBy.Name}
	}
	if includeArtifactTree {
		tree := newArtifactTreeResponse(record.ArtifactTree)
		response.ArtifactTree = &tree
	}
	return response
}

func newArtifactTreeResponse(tree domain.ArtifactTree) artifactTreeResponse {
	nodes := make([]artifactTreeNodeResponse, 0, len(tree.Nodes))
	for _, node := range tree.Nodes {
		nodes = append(nodes, newArtifactTreeNodeResponse(node))
	}
	return artifactTreeResponse{
		FileCount:      tree.FileCount,
		TotalSizeBytes: tree.TotalSizeBytes,
		Nodes:          nodes,
	}
}

func newArtifactTreeNodeResponse(node domain.ArtifactTreeNode) artifactTreeNodeResponse {
	children := make([]artifactTreeNodeResponse, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, newArtifactTreeNodeResponse(child))
	}
	return artifactTreeNodeResponse{
		Children:    children,
		ContentType: node.ContentType,
		Name:        node.Name,
		ObjectKey:   node.ObjectKey,
		Path:        node.Path,
		SHA256:      node.SHA256,
		SizeBytes:   node.SizeBytes,
		Type:        node.Type,
	}
}

func newBuildRecordSummaryResponse(summary domain.BuildRecordSummary) buildRecordSummaryResponse {
	return buildRecordSummaryResponse{
		Failed:    summary.Failed,
		Pending:   summary.Pending,
		Running:   summary.Running,
		Succeeded: summary.Succeeded,
		Total:     summary.Total,
	}
}

// nonNilStrings 让空切片序列化为 [] 而不是 null。
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// nonNilCapabilities 让没有能力声明的构建在响应里返回空对象而不是 null。
func nonNilCapabilities(capabilities domain.Capabilities) map[string]int64 {
	if capabilities == nil {
		return map[string]int64{}
	}
	return capabilities
}
