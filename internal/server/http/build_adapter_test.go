package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"forge-server/internal/domain"
)

type fakeBuildAdapterService struct {
	createInput  domain.BuildAdapterInput
	createErr    error
	previewInput domain.BuildAdapterPreviewInput
	draftInput   domain.BuildAdapterInput
	previewErr   error
	patch        domain.BuildAdapterPatch
}

func (s *fakeBuildAdapterService) CreateBuildAdapter(_ context.Context, input domain.BuildAdapterInput) (domain.BuildAdapter, error) {
	s.createInput = input
	if s.createErr != nil {
		return domain.BuildAdapter{}, s.createErr
	}
	return domain.BuildAdapter{ID: "adapter-1", Name: input.Name, Version: 1, Status: domain.BuildAdapterStatusActive, Parameters: input.Parameters, Files: input.Files}, nil
}

func (s *fakeBuildAdapterService) DisableBuildAdapter(_ context.Context, id string, _ string) (domain.BuildAdapter, error) {
	return domain.BuildAdapter{ID: id, Status: domain.BuildAdapterStatusDisabled}, nil
}

func (s *fakeBuildAdapterService) EnableBuildAdapter(_ context.Context, id string, _ string) (domain.BuildAdapter, error) {
	return domain.BuildAdapter{ID: id, Status: domain.BuildAdapterStatusActive}, nil
}

func (s *fakeBuildAdapterService) GetBuildAdapter(_ context.Context, id string) (domain.BuildAdapter, error) {
	return domain.BuildAdapter{ID: id, ModuleCount: 2}, nil
}

func (s *fakeBuildAdapterService) ListBuildAdapters(context.Context, domain.BuildAdapterFilters) ([]domain.BuildAdapter, error) {
	return []domain.BuildAdapter{{ID: "adapter-1", ModuleCount: 3}}, nil
}

func (s *fakeBuildAdapterService) PreviewBuildAdapter(_ context.Context, _ string, input domain.BuildAdapterPreviewInput) (domain.BuildAdapterRenderResult, error) {
	s.previewInput = input
	if s.previewErr != nil {
		return domain.BuildAdapterRenderResult{}, s.previewErr
	}
	return domain.BuildAdapterRenderResult{Dockerfile: "FROM node\n", DockerfileSHA256: "abc", Files: map[string]string{"a.sh": "x"}}, nil
}

func (s *fakeBuildAdapterService) PreviewBuildAdapterDraft(ctx context.Context, draft domain.BuildAdapterInput, input domain.BuildAdapterPreviewInput) (domain.BuildAdapterRenderResult, error) {
	s.draftInput = draft
	return s.PreviewBuildAdapter(ctx, "", input)
}

func (s *fakeBuildAdapterService) UpdateBuildAdapter(_ context.Context, id string, patch domain.BuildAdapterPatch) (domain.BuildAdapter, error) {
	s.patch = patch
	return domain.BuildAdapter{ID: id, Version: 2}, nil
}

func serveBuildAdapterRequest(t *testing.T, deps Dependencies, method string, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, jsonBody(body))
		req.Header.Set("Content-Type", "application/json")
	}
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	newAuthorizedTestServer(deps).ServeHTTP(rec, req)
	return rec
}

func TestCreateBuildAdapter(t *testing.T) {
	service := &fakeBuildAdapterService{}
	rec := serveBuildAdapterRequest(t, Dependencies{BuildAdapterService: service}, http.MethodPost, "/api/v1/build-adapters", map[string]any{
		"name":               "Node bundle",
		"dockerfileTemplate": "FROM node\n",
		"files":              map[string]string{"build.sh": "echo"},
		"parameters":         []map[string]any{{"key": "entry", "required": true}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.createInput.UserID != "ou-test-user" || service.createInput.Files["build.sh"] != "echo" || !service.createInput.Parameters[0].Required {
		t.Fatalf("unexpected create input: %#v", service.createInput)
	}
	var response buildAdapterDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil || response.BuildAdapter.Version != 1 || response.BuildAdapter.Status != "active" {
		t.Fatalf("unexpected response %#v, %v", response, err)
	}
}

func TestCreateBuildAdapterReturnsTemplateLine(t *testing.T) {
	service := &fakeBuildAdapterService{createErr: &domain.BuildAdapterRenderError{Template: "Dockerfile", Line: 3, Message: "bad"}}
	rec := serveBuildAdapterRequest(t, Dependencies{BuildAdapterService: service}, http.MethodPost, "/api/v1/build-adapters", map[string]any{"name": "x"})
	var body buildAdapterErrorBody
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if rec.Code != http.StatusBadRequest || body.Line != 3 || body.Template != "Dockerfile" {
		t.Fatalf("unexpected response %d %#v", rec.Code, body)
	}
}

func TestBuildAdapterManageRequiresPermission(t *testing.T) {
	deps := Dependencies{BuildAdapterService: &fakeBuildAdapterService{}, AuthorizationService: fakeAuthorizationService{err: domain.ErrForbidden}}
	for _, item := range []struct{ method, target string }{
		{http.MethodPost, "/api/v1/build-adapters"},
		{http.MethodPatch, "/api/v1/build-adapters/adapter-1"},
		{http.MethodPost, "/api/v1/build-adapters/adapter-1/disable"},
		{http.MethodGet, "/api/v1/build-adapters"},
		{http.MethodPost, "/api/v1/build-adapters/render-preview"},
	} {
		if rec := serveBuildAdapterRequest(t, deps, item.method, item.target, map[string]any{}); rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s: expected 403, got %d", item.method, item.target, rec.Code)
		}
	}
}

func TestUpdateBuildAdapterKeepsOmittedFields(t *testing.T) {
	service := &fakeBuildAdapterService{}
	rec := serveBuildAdapterRequest(t, Dependencies{BuildAdapterService: service}, http.MethodPatch, "/api/v1/build-adapters/adapter-1", map[string]any{"name": "renamed"})
	if rec.Code != http.StatusOK || service.patch.Name == nil || *service.patch.Name != "renamed" || service.patch.Files != nil || service.patch.Parameters != nil || service.patch.DockerfileTemplate != nil {
		t.Fatalf("unexpected patch %d %#v", rec.Code, service.patch)
	}
}

func TestRenderPreviewBuildAdapter(t *testing.T) {
	service := &fakeBuildAdapterService{}
	rec := serveBuildAdapterRequest(t, Dependencies{BuildAdapterService: service}, http.MethodPost, "/api/v1/build-adapters/adapter-1/render-preview", map[string]any{"moduleId": "m1", "params": map[string]string{"entry": "a"}})
	var response buildAdapterRenderPreviewResponse
	_ = json.NewDecoder(rec.Body).Decode(&response)
	if rec.Code != http.StatusOK || !response.OK || response.Files["a.sh"] != "x" || service.previewInput.ModuleID != "m1" || service.previewInput.Params["entry"] != "a" {
		t.Fatalf("unexpected preview %d %#v", rec.Code, response)
	}

	service.previewErr = &domain.BuildAdapterRenderError{Template: "a.sh", Line: 2, Message: "missing"}
	rec = serveBuildAdapterRequest(t, Dependencies{BuildAdapterService: service}, http.MethodPost, "/api/v1/build-adapters/adapter-1/render-preview", map[string]any{})
	response = buildAdapterRenderPreviewResponse{}
	_ = json.NewDecoder(rec.Body).Decode(&response)
	if rec.Code != http.StatusOK || response.OK || response.Error == nil || response.Error.Line != 2 || response.Error.Template != "a.sh" {
		t.Fatalf("unexpected preview error %d %#v", rec.Code, response)
	}
	if service.previewInput.Params != nil {
		t.Fatalf("expected omitted params to stay nil, got %#v", service.previewInput.Params)
	}
}

func TestRenderDraftPreviewBuildAdapter(t *testing.T) {
	service := &fakeBuildAdapterService{}
	rec := serveBuildAdapterRequest(t, Dependencies{BuildAdapterService: service}, http.MethodPost, "/api/v1/build-adapters/render-preview", map[string]any{
		"dockerfileTemplate": "FROM node\n",
		"files":              map[string]string{"a.sh": "x"},
		"parameters":         []map[string]any{{"key": "entry", "required": true}},
		"moduleId":           "m1",
		"params":             map[string]string{"entry": "a"},
	})
	var response buildAdapterRenderPreviewResponse
	_ = json.NewDecoder(rec.Body).Decode(&response)
	if rec.Code != http.StatusOK || !response.OK || service.draftInput.DockerfileTemplate != "FROM node\n" || service.draftInput.Files["a.sh"] != "x" ||
		len(service.draftInput.Parameters) != 1 || !service.draftInput.Parameters[0].Required || service.previewInput.ModuleID != "m1" || service.previewInput.Params["entry"] != "a" {
		t.Fatalf("unexpected draft preview %d %#v %#v", rec.Code, response, service.draftInput)
	}

	service.previewErr = domain.ErrInvalidBuildAdapterInput
	rec = serveBuildAdapterRequest(t, Dependencies{BuildAdapterService: service}, http.MethodPost, "/api/v1/build-adapters/render-preview", map[string]any{})
	response = buildAdapterRenderPreviewResponse{}
	_ = json.NewDecoder(rec.Body).Decode(&response)
	if rec.Code != http.StatusOK || response.OK || response.Error == nil {
		t.Fatalf("expected ok=false for invalid draft, got %d %#v", rec.Code, response)
	}
}
