package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forge-server/internal/domain"
)

type fakeBuildVariableService struct {
	variables  []domain.ModuleBuildVariable
	err        error
	lastInput  domain.ModuleBuildVariableInput
	lastDelete [3]string
}

func (s *fakeBuildVariableService) DeleteBuildVariable(_ context.Context, moduleID string, key string, environmentCode string) error {
	s.lastDelete = [3]string{moduleID, key, environmentCode}
	return s.err
}

func (s *fakeBuildVariableService) ListBuildVariables(context.Context, string) ([]domain.ModuleBuildVariable, error) {
	return s.variables, s.err
}

func (s *fakeBuildVariableService) UpsertBuildVariable(_ context.Context, input domain.ModuleBuildVariableInput) (domain.ModuleBuildVariable, error) {
	s.lastInput = input
	if s.err != nil {
		return domain.ModuleBuildVariable{}, s.err
	}
	return domain.ModuleBuildVariable{
		ModuleID:        input.ModuleID,
		Key:             input.Key,
		Value:           "******",
		IsSecret:        input.IsSecret,
		EnvironmentCode: input.EnvironmentCode,
		UpdatedByUserID: input.UpdatedByUserID,
		UpdatedBy:       "测试用户",
		UpdatedAt:       time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC),
	}, nil
}

func TestListBuildVariables(t *testing.T) {
	service := &fakeBuildVariableService{variables: []domain.ModuleBuildVariable{
		{Key: "API_HOST", Value: "qa.example.com", EnvironmentCode: "qa", UpdatedBy: "海鸥", UpdatedByUserID: "ou-1"},
		{Key: "NPM_TOKEN", Value: "******", IsSecret: true, UpdatedBy: "海鸥", UpdatedByUserID: "ou-1"},
	}}
	e := newAuthorizedTestServer(Dependencies{BuildVariableService: service})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules/module-1/build-variables", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response buildVariablesResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Variables) != 2 || response.Variables[0].EnvironmentCode != "qa" || response.Variables[1].Value != "******" ||
		!response.Variables[1].IsSecret || response.Variables[1].UpdatedBy != "海鸥" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestUpsertBuildVariableUsesCurrentUser(t *testing.T) {
	service := &fakeBuildVariableService{}
	e := newAuthorizedTestServer(Dependencies{BuildVariableService: service})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/modules/module-1/build-variables", jsonBody(buildVariableUpsertRequest{
		Key:             "NPM_TOKEN",
		EnvironmentCode: "qa",
		Value:           "npm-secret-value",
		IsSecret:        true,
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "npm-secret-value") {
		t.Fatalf("secret leaked in response: %s", rec.Body.String())
	}
	if service.lastInput.ModuleID != "module-1" || service.lastInput.UpdatedByUserID != "ou-test-user" || service.lastInput.Value != "npm-secret-value" || !service.lastInput.IsSecret {
		t.Fatalf("unexpected service input: %+v", service.lastInput)
	}
}

func TestDeleteBuildVariable(t *testing.T) {
	service := &fakeBuildVariableService{}
	e := newAuthorizedTestServer(Dependencies{BuildVariableService: service})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/modules/module-1/build-variables?key=API_HOST&environmentCode=qa", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.lastDelete != [3]string{"module-1", "API_HOST", "qa"} {
		t.Fatalf("unexpected delete: %v", service.lastDelete)
	}
}

func TestBuildVariableRoutesRequireModulePermission(t *testing.T) {
	service := &fakeBuildVariableService{}
	e := newAuthorizedTestServer(Dependencies{
		BuildVariableService: service,
		AuthorizationService: fakeAuthorizationService{err: domain.ErrForbidden},
	})

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/modules/module-1/build-variables?key=API_HOST", strings.NewReader(`{"key":"API_HOST","value":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		addTestConsoleSession(req)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: expected 403, got %d", method, rec.Code)
		}
	}
	if service.lastInput.Key != "" || service.lastDelete[0] != "" {
		t.Fatal("service must not be called without permission")
	}
}

func TestBuildVariableErrorMapping(t *testing.T) {
	for err, status := range map[error]int{
		domain.ErrInvalidBuildVariableInput: http.StatusBadRequest,
		domain.ErrModuleNotFound:            http.StatusNotFound,
		domain.ErrEnvironmentNotFound:       http.StatusNotFound,
	} {
		e := newAuthorizedTestServer(Dependencies{BuildVariableService: &fakeBuildVariableService{err: err}})
		req := httptest.NewRequest(http.MethodPut, "/api/v1/modules/module-1/build-variables", strings.NewReader(`{"key":"api","value":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		addTestConsoleSession(req)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%v: expected %d, got %d", err, status, rec.Code)
		}
	}
}
