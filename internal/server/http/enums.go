package httpserver

import (
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type enumsResponse struct {
	ApplicationStatuses []enumOptionResponse `json:"applicationStatuses"`
	ModuleKinds         []enumOptionResponse `json:"moduleKinds"`
	ModuleStatuses      []enumOptionResponse `json:"moduleStatuses"`
	Platforms           []enumOptionResponse `json:"platforms"`
}

type enumOptionResponse struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func registerEnumRoutes(group *echo.Group) {
	group.GET("/enums", func(c echo.Context) error {
		return c.JSON(http.StatusOK, enumsResponse{
			ApplicationStatuses: []enumOptionResponse{
				{Label: domain.ApplicationStatusRunning.Label(), Value: string(domain.ApplicationStatusRunning)},
				{Label: domain.ApplicationStatusMaintenance.Label(), Value: string(domain.ApplicationStatusMaintenance)},
				{Label: domain.ApplicationStatusOffline.Label(), Value: string(domain.ApplicationStatusOffline)},
			},
			ModuleKinds: []enumOptionResponse{
				{Label: "Common Bundle", Value: string(domain.ModuleKindCommon)},
				{Label: "Feature Bundle", Value: string(domain.ModuleKindFeature)},
			},
			ModuleStatuses: []enumOptionResponse{
				{Label: domain.ModuleStatusActive.Label(), Value: string(domain.ModuleStatusActive)},
				{Label: domain.ModuleStatusFrozen.Label(), Value: string(domain.ModuleStatusFrozen)},
			},
			Platforms: []enumOptionResponse{
				{Label: "iOS", Value: "iOS"},
				{Label: "Android", Value: "Android"},
				{Label: "HarmonyOS", Value: "HarmonyOS"},
			},
		})
	})
}
