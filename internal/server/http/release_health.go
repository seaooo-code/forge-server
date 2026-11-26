package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type ReleaseHealthService interface {
	RecordLaunchReport(ctx context.Context, report domain.LaunchReport) (int, error)
	GetReleaseHealth(ctx context.Context, releaseID string) (domain.ReleaseHealth, error)
	ListReleaseHealthPolicies(ctx context.Context, applicationSlug string) ([]domain.ReleaseHealthPolicy, error)
	UpdateReleaseHealthPolicy(ctx context.Context, applicationSlug string, policy domain.ReleaseHealthPolicy) ([]domain.ReleaseHealthPolicy, error)
}

type releaseHealthHandler struct {
	authService AuthorizationService
	service     ReleaseHealthService
}

type launchReportRequest struct {
	Platform       string                     `json:"platform"`
	RuntimeVersion string                     `json:"runtimeVersion"`
	Channel        string                     `json:"channel"`
	UserID         string                     `json:"userId"`
	Events         []launchReportEventRequest `json:"events"`
}

type launchReportEventRequest struct {
	ModuleID      string `json:"moduleId"`
	BuildRecordID string `json:"buildRecordId"`
	ReleaseID     string `json:"releaseId"`
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason"`
	OccurredAt    string `json:"occurredAt"`
}

type launchReportResponse struct {
	Accepted int `json:"accepted"`
}

type releaseHealthResponse struct {
	Status              string                      `json:"status"`
	PolicyEnabled       bool                        `json:"policyEnabled"`
	Policy              releaseHealthPolicyResponse `json:"policy"`
	WindowStart         string                      `json:"windowStart"`
	LaunchedUsers       int64                       `json:"launchedUsers"`
	FailedUsers         int64                       `json:"failedUsers"`
	VerifyFailedUsers   int64                       `json:"verifyFailedUsers"`
	DownloadFailedUsers int64                       `json:"downloadFailedUsers"`
	FailureRate         float64                     `json:"failureRate"`
	BaselineFailureRate *float64                    `json:"baselineFailureRate"`
	EvaluatedAt         string                      `json:"evaluatedAt"`
}

type releaseHealthPolicyResponse struct {
	Environment     string  `json:"environment"`
	EnvironmentName string  `json:"environmentName"`
	Configured      bool    `json:"configured"`
	Enabled         bool    `json:"enabled"`
	WindowMinutes   int     `json:"windowMinutes"`
	MinUsers        int     `json:"minUsers"`
	MaxFailureRate  float64 `json:"maxFailureRate"`
	MinDelta        float64 `json:"minDelta"`
	AutoStopGray    bool    `json:"autoStopGray"`
	UpdatedAt       string  `json:"updatedAt"`
}

type releaseHealthPoliciesResponse struct {
	Policies []releaseHealthPolicyResponse `json:"policies"`
}

type releaseHealthPolicyRequest struct {
	Enabled        bool    `json:"enabled"`
	WindowMinutes  int     `json:"windowMinutes"`
	MinUsers       int     `json:"minUsers"`
	MaxFailureRate float64 `json:"maxFailureRate"`
	MinDelta       float64 `json:"minDelta"`
	AutoStopGray   bool    `json:"autoStopGray"`
}

// registerLaunchReportRoutes 注册客户端上报接口，鉴权与 manifest 接口相同。
func registerLaunchReportRoutes(group *echo.Group, service ReleaseHealthService, tokenService ArtifactAccessTokenService) {
	handler := releaseHealthHandler{service: service}
	group.POST("/artifacts/applications/:applicationSlug/environments/:environment/launch-reports", handler.report, requireArtifactToken(tokenService))
}

func registerReleaseHealthRoutes(group *echo.Group, service ReleaseHealthService, authService AuthorizationService) {
	handler := releaseHealthHandler{authService: authService, service: service}
	group.GET("/releases/:id/health", handler.health)
	group.GET("/applications/:slug/release-health-policies", handler.policies)
	group.PUT("/applications/:slug/release-health-policies/:environment", handler.updatePolicy)
}

func (h releaseHealthHandler) report(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release health service unavailable"})
	}
	var request launchReportRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid launch report"})
	}
	report := domain.LaunchReport{
		ApplicationSlug: c.Param("applicationSlug"),
		Environment:     c.Param("environment"),
		Platform:        request.Platform,
		RuntimeVersion:  request.RuntimeVersion,
		Channel:         request.Channel,
		UserID:          request.UserID,
		Events:          make([]domain.LaunchReportEvent, 0, len(request.Events)),
	}
	for _, event := range request.Events {
		item := domain.LaunchReportEvent{
			ModuleID:      event.ModuleID,
			BuildRecordID: event.BuildRecordID,
			ReleaseID:     event.ReleaseID,
			Outcome:       domain.LaunchOutcome(event.Outcome),
			Reason:        event.Reason,
		}
		if event.OccurredAt != "" {
			occurredAt, err := time.Parse(time.RFC3339, event.OccurredAt)
			if err != nil {
				// 时间格式不对的单条事件跳过：零值以外的非法时间交给服务层按过期丢弃。
				occurredAt = time.Unix(0, 0)
			}
			item.OccurredAt = occurredAt
		}
		report.Events = append(report.Events, item)
	}
	accepted, err := h.service.RecordLaunchReport(c.Request().Context(), report)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidLaunchReport):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid launch report"})
		case errors.Is(err, domain.ErrArtifactNotFound):
			return c.JSON(http.StatusNotFound, map[string]string{"error": "artifact not found"})
		default:
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
	}
	return c.JSON(http.StatusAccepted, launchReportResponse{Accepted: accepted})
}

func (h releaseHealthHandler) health(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release health service unavailable"})
	}
	health, err := h.service.GetReleaseHealth(c.Request().Context(), c.Param("id"))
	if err != nil {
		return releaseHealthErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, releaseHealthResponse{
		Status:              string(health.Status),
		PolicyEnabled:       health.PolicyEnabled,
		Policy:              newReleaseHealthPolicyResponse(health.Policy),
		WindowStart:         formatTime(health.WindowStart),
		LaunchedUsers:       health.Counts.Launched,
		FailedUsers:         health.Counts.LaunchFailed,
		VerifyFailedUsers:   health.Counts.VerifyFailed,
		DownloadFailedUsers: health.Counts.DownloadFailed,
		FailureRate:         health.FailureRate,
		BaselineFailureRate: health.BaselineFailureRate,
		EvaluatedAt:         formatTime(health.EvaluatedAt),
	})
}

func (h releaseHealthHandler) policies(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release health service unavailable"})
	}
	policies, err := h.service.ListReleaseHealthPolicies(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return releaseHealthErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newReleaseHealthPoliciesResponse(policies))
}

func (h releaseHealthHandler) updatePolicy(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release health service unavailable"})
	}
	var request releaseHealthPolicyRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release health request"})
	}
	// 策略会自动停用发布，修改权限与应用配置一致。
	if err := authorizeCurrentUser(c, h.authService, domain.ActionApplicationUpdate, domain.AuthorizationResource{ApplicationSlug: c.Param("slug")}); err != nil {
		return err
	}
	session, ok := consoleSession(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	policies, err := h.service.UpdateReleaseHealthPolicy(c.Request().Context(), c.Param("slug"), domain.ReleaseHealthPolicy{
		Environment:     c.Param("environment"),
		Enabled:         request.Enabled,
		WindowMinutes:   request.WindowMinutes,
		MinUsers:        request.MinUsers,
		MaxFailureRate:  request.MaxFailureRate,
		MinDelta:        request.MinDelta,
		AutoStopGray:    request.AutoStopGray,
		UpdatedByUserID: session.User.ID,
	})
	if err != nil {
		return releaseHealthErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newReleaseHealthPoliciesResponse(policies))
}

func newReleaseHealthPoliciesResponse(policies []domain.ReleaseHealthPolicy) releaseHealthPoliciesResponse {
	response := releaseHealthPoliciesResponse{Policies: make([]releaseHealthPolicyResponse, 0, len(policies))}
	for _, policy := range policies {
		response.Policies = append(response.Policies, newReleaseHealthPolicyResponse(policy))
	}
	return response
}

func newReleaseHealthPolicyResponse(policy domain.ReleaseHealthPolicy) releaseHealthPolicyResponse {
	response := releaseHealthPolicyResponse{
		Environment:     policy.Environment,
		EnvironmentName: policy.EnvironmentName,
		Configured:      policy.Configured,
		Enabled:         policy.Enabled,
		WindowMinutes:   policy.WindowMinutes,
		MinUsers:        policy.MinUsers,
		MaxFailureRate:  policy.MaxFailureRate,
		MinDelta:        policy.MinDelta,
		AutoStopGray:    policy.AutoStopGray,
	}
	if policy.UpdatedAt != nil {
		response.UpdatedAt = formatTime(*policy.UpdatedAt)
	}
	return response
}

func releaseHealthErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidReleaseHealthInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release health request"})
	case errors.Is(err, domain.ErrApplicationNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "application not found"})
	case errors.Is(err, domain.ErrEnvironmentNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "environment not found"})
	case errors.Is(err, domain.ErrReleaseNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "release not found"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}
