package httpserver

import (
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type storageHandler struct {
	service StorageService
}

type storageConfigurationsResponse struct {
	Configurations []storageConfigurationResponse `json:"configurations"`
}

type storageConfigurationDetailResponse struct {
	Configuration storageConfigurationResponse `json:"configuration"`
}

type storageConfigurationTestResponse struct {
	OK       bool   `json:"ok"`
	TestedAt string `json:"testedAt"`
}

type storageConfigurationResponse struct {
	Adapter        string `json:"adapter"`
	Bucket         string `json:"bucket"`
	Endpoint       string `json:"endpoint"`
	ID             string `json:"id"`
	LastVerifiedAt string `json:"lastVerifiedAt"`
	Name           string `json:"name"`
	OwnerUserID    string `json:"ownerUserId"`
	Owner          string `json:"owner"`
	OwnerEmail     string `json:"ownerEmail"`
	OwnerAvatarURL string `json:"ownerAvatarUrl"`
	PublicBaseURL  string `json:"publicBaseUrl"`
	Region         string `json:"region"`
	Status         string `json:"status"`
}

type storageConfigurationRequest struct {
	AccessKeyID        string `json:"accessKeyId"`
	Adapter            string `json:"adapter"`
	Bucket             string `json:"bucket"`
	Endpoint           string `json:"endpoint"`
	ID                 string `json:"id"`
	Name               string `json:"name"`
	OwnerUserID        string `json:"ownerUserId"`
	Owner              string `json:"owner"`
	OwnerEmail         string `json:"ownerEmail"`
	OwnerAvatarURL     string `json:"ownerAvatarUrl"`
	OwnerFeishuUnionID string `json:"ownerFeishuUnionId"`
	OwnerFeishuOpenID  string `json:"ownerFeishuOpenId"`
	OwnerFeishuUserID  string `json:"ownerFeishuUserId"`
	PublicBaseURL      string `json:"publicBaseUrl"`
	Region             string `json:"region"`
	SecretAccessKey    string `json:"secretAccessKey"`
	Status             string `json:"status"`
}

func registerStorageRoutes(group *echo.Group, service StorageService) {
	handler := storageHandler{service: service}

	group.GET("/storage/configurations", handler.list)
	group.POST("/storage/configurations/test", handler.test)
	group.POST("/storage/configurations", handler.create)
	group.GET("/storage/configurations/:id", handler.detail)
	group.PATCH("/storage/configurations/:id", handler.update)
	group.DELETE("/storage/configurations/:id", handler.delete)
}

func (h storageHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage service unavailable"})
	}

	configurations, err := h.service.ListStorageConfigurations(c.Request().Context())
	if err != nil {
		return storageErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, storageConfigurationsResponse{
		Configurations: newStorageConfigurationResponses(configurations),
	})
}

func (h storageHandler) detail(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage service unavailable"})
	}

	configuration, err := h.service.GetStorageConfiguration(c.Request().Context(), c.Param("id"))
	if err != nil {
		return storageErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, storageConfigurationDetailResponse{
		Configuration: newStorageConfigurationResponse(configuration),
	})
}

func (h storageHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage service unavailable"})
	}

	input, ok := bindStorageConfigurationRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid storage configuration request"})
	}

	configuration, err := h.service.CreateStorageConfiguration(c.Request().Context(), input)
	if err != nil {
		return storageErrorResponse(c, err)
	}

	return c.JSON(http.StatusCreated, storageConfigurationDetailResponse{
		Configuration: newStorageConfigurationResponse(configuration),
	})
}

func (h storageHandler) update(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage service unavailable"})
	}

	input, ok := bindStorageConfigurationRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid storage configuration request"})
	}

	configuration, err := h.service.UpdateStorageConfiguration(c.Request().Context(), c.Param("id"), input)
	if err != nil {
		return storageErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, storageConfigurationDetailResponse{
		Configuration: newStorageConfigurationResponse(configuration),
	})
}

func (h storageHandler) delete(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage service unavailable"})
	}

	configuration, err := h.service.DeleteStorageConfiguration(c.Request().Context(), c.Param("id"))
	if err != nil {
		return storageErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, storageConfigurationDetailResponse{
		Configuration: newStorageConfigurationResponse(configuration),
	})
}

func (h storageHandler) test(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage service unavailable"})
	}

	id, input, ok := bindStorageConfigurationTestRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid storage configuration request"})
	}

	result, err := h.service.TestStorageConfiguration(c.Request().Context(), id, input)
	if err != nil {
		return storageErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, storageConfigurationTestResponse{
		OK:       result.OK,
		TestedAt: formatTime(result.TestedAt),
	})
}

func bindStorageConfigurationRequest(c echo.Context) (domain.StorageConfigurationInput, bool) {
	var request storageConfigurationRequest
	if err := c.Bind(&request); err != nil {
		return domain.StorageConfigurationInput{}, false
	}

	input, ok := newStorageConfigurationInput(request)
	return input, ok
}

func bindStorageConfigurationTestRequest(c echo.Context) (string, domain.StorageConfigurationInput, bool) {
	var request storageConfigurationRequest
	if err := c.Bind(&request); err != nil {
		return "", domain.StorageConfigurationInput{}, false
	}

	input, ok := newStorageConfigurationInput(request)
	return request.ID, input, ok
}

func newStorageConfigurationInput(request storageConfigurationRequest) (domain.StorageConfigurationInput, bool) {
	status, ok := domain.ParseStorageStatus(request.Status)
	if !ok {
		return domain.StorageConfigurationInput{}, false
	}

	return domain.StorageConfigurationInput{
		AccessKeyID:        request.AccessKeyID,
		Adapter:            domain.StorageAdapter(request.Adapter),
		Bucket:             request.Bucket,
		Endpoint:           request.Endpoint,
		Name:               request.Name,
		OwnerUserID:        request.OwnerUserID,
		Owner:              request.Owner,
		OwnerEmail:         request.OwnerEmail,
		OwnerAvatarURL:     request.OwnerAvatarURL,
		OwnerFeishuUnionID: request.OwnerFeishuUnionID,
		OwnerFeishuOpenID:  request.OwnerFeishuOpenID,
		OwnerFeishuUserID:  request.OwnerFeishuUserID,
		PublicBaseURL:      request.PublicBaseURL,
		Region:             request.Region,
		SecretAccessKey:    request.SecretAccessKey,
		Status:             status,
	}, true
}

func storageErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrStorageConfigurationNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "storage configuration not found"})
	case errors.Is(err, domain.ErrInvalidStorageConfigurationInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid storage configuration request"})
	case errors.Is(err, domain.ErrStorageConfigurationTestFailed):
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "storage configuration test failed"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newStorageConfigurationResponses(configurations []domain.StorageConfiguration) []storageConfigurationResponse {
	response := make([]storageConfigurationResponse, 0, len(configurations))
	for _, configuration := range configurations {
		response = append(response, newStorageConfigurationResponse(configuration))
	}
	return response
}

func newStorageConfigurationResponse(configuration domain.StorageConfiguration) storageConfigurationResponse {
	lastVerifiedAt := "未验证"
	if configuration.LastVerifiedAt != nil {
		lastVerifiedAt = formatTime(*configuration.LastVerifiedAt)
	}

	return storageConfigurationResponse{
		Adapter:        string(configuration.Adapter),
		Bucket:         configuration.Bucket,
		Endpoint:       configuration.Endpoint,
		ID:             configuration.ID,
		LastVerifiedAt: lastVerifiedAt,
		Name:           configuration.Name,
		OwnerUserID:    configuration.OwnerUserID,
		Owner:          configuration.Owner,
		OwnerEmail:     configuration.OwnerEmail,
		OwnerAvatarURL: configuration.OwnerAvatarURL,
		PublicBaseURL:  configuration.PublicBaseURL,
		Region:         configuration.Region,
		Status:         configuration.Status.Label(),
	}
}
