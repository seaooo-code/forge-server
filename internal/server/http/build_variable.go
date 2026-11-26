package httpserver

import (
	"context"
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type BuildVariableService interface {
	DeleteBuildVariable(ctx context.Context, moduleID string, key string, environmentCode string) error
	ListBuildVariables(ctx context.Context, moduleID string) ([]domain.ModuleBuildVariable, error)
	UpsertBuildVariable(ctx context.Context, input domain.ModuleBuildVariableInput) (domain.ModuleBuildVariable, error)
}

type buildVariableHandler struct {
	authService AuthorizationService
	service     BuildVariableService
}

type buildVariableUpsertRequest struct {
	Key             string `json:"key"`
	EnvironmentCode string `json:"environmentCode"`
	Value           string `json:"value"`
	IsSecret        bool   `json:"isSecret"`
}

type buildVariablesResponse struct {
	Variables []buildVariableResponse `json:"variables"`
}

type buildVariableDetailResponse struct {
	Variable buildVariableResponse `json:"variable"`
}

// buildVariableResponse 的 Value 对密钥变量固定为 ******，明文只在 worker 构建时解密。
type buildVariableResponse struct {
	Key             string `json:"key"`
	EnvironmentCode string `json:"environmentCode"`
	Value           string `json:"value"`
	IsSecret        bool   `json:"isSecret"`
	UpdatedBy       string `json:"updatedBy"`
	UpdatedByUserID string `json:"updatedByUserId"`
	UpdatedAt       string `json:"updatedAt"`
}

// registerBuildVariableRoutes 注册模块构建变量接口，读写都复用模块管理权限（module:update）。
func registerBuildVariableRoutes(group *echo.Group, service BuildVariableService, authService AuthorizationService) {
	handler := buildVariableHandler{authService: authService, service: service}

	group.GET("/modules/:id/build-variables", handler.list)
	group.PUT("/modules/:id/build-variables", handler.upsert)
	group.DELETE("/modules/:id/build-variables", handler.delete)
}

func (h buildVariableHandler) list(c echo.Context) error {
	if h.service == nil {
		return buildVariableUnavailable(c)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionModuleUpdate, domain.AuthorizationResource{ModuleID: c.Param("id")}); err != nil {
		return err
	}

	variables, err := h.service.ListBuildVariables(c.Request().Context(), c.Param("id"))
	if err != nil {
		return buildVariableErrorResponse(c, err)
	}
	responses := make([]buildVariableResponse, 0, len(variables))
	for _, variable := range variables {
		responses = append(responses, newBuildVariableResponse(variable))
	}
	return c.JSON(http.StatusOK, buildVariablesResponse{Variables: responses})
}

func (h buildVariableHandler) upsert(c echo.Context) error {
	if h.service == nil {
		return buildVariableUnavailable(c)
	}
	var request buildVariableUpsertRequest
	if err := c.Bind(&request); err != nil {
		return buildVariableErrorResponse(c, domain.ErrInvalidBuildVariableInput)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionModuleUpdate, domain.AuthorizationResource{ModuleID: c.Param("id")}); err != nil {
		return err
	}
	session, _ := consoleSession(c)

	variable, err := h.service.UpsertBuildVariable(c.Request().Context(), domain.ModuleBuildVariableInput{
		ModuleID:        c.Param("id"),
		Key:             request.Key,
		Value:           request.Value,
		IsSecret:        request.IsSecret,
		EnvironmentCode: request.EnvironmentCode,
		UpdatedByUserID: session.User.ID,
	})
	if err != nil {
		return buildVariableErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, buildVariableDetailResponse{Variable: newBuildVariableResponse(variable)})
}

func (h buildVariableHandler) delete(c echo.Context) error {
	if h.service == nil {
		return buildVariableUnavailable(c)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionModuleUpdate, domain.AuthorizationResource{ModuleID: c.Param("id")}); err != nil {
		return err
	}

	if err := h.service.DeleteBuildVariable(c.Request().Context(), c.Param("id"), c.QueryParam("key"), c.QueryParam("environmentCode")); err != nil {
		return buildVariableErrorResponse(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func newBuildVariableResponse(variable domain.ModuleBuildVariable) buildVariableResponse {
	return buildVariableResponse{
		Key:             variable.Key,
		EnvironmentCode: variable.EnvironmentCode,
		Value:           variable.Value,
		IsSecret:        variable.IsSecret,
		UpdatedBy:       variable.UpdatedBy,
		UpdatedByUserID: variable.UpdatedByUserID,
		UpdatedAt:       formatTime(variable.UpdatedAt),
	}
}

func buildVariableUnavailable(c echo.Context) error {
	return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build variable service unavailable"})
}

func buildVariableErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidBuildVariableInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid build variable request"})
	case errors.Is(err, domain.ErrModuleNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "module not found"})
	case errors.Is(err, domain.ErrEnvironmentNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "environment not found"})
	case errors.Is(err, domain.ErrBuildVariableNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "build variable not found"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}
