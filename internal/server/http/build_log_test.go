package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"forge-server/internal/domain"
)

type fakeBuildLogService struct {
	page         domain.BuildLogPage
	err          error
	lastBuildID  string
	lastAfterSeq int
}

func (s *fakeBuildLogService) ListBuildLogs(_ context.Context, buildID string, afterSeq int) (domain.BuildLogPage, error) {
	s.lastBuildID = buildID
	s.lastAfterSeq = afterSeq
	return s.page, s.err
}

func TestListBuildLogs(t *testing.T) {
	service := &fakeBuildLogService{page: domain.BuildLogPage{
		Chunks:   []domain.BuildLogChunk{{Seq: 3, Content: "step\n"}},
		Finished: true,
	}}
	e := newAuthorizedTestServer(Dependencies{BuildLogService: service})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/logs?afterSeq=2", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response buildLogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !response.Finished || len(response.Chunks) != 1 || response.Chunks[0].Seq != 3 || response.Chunks[0].Content != "step\n" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if service.lastBuildID != "build-1" || service.lastAfterSeq != 2 {
		t.Fatalf("unexpected service call: %q %d", service.lastBuildID, service.lastAfterSeq)
	}
}

func TestListBuildLogsDefaultsAfterSeqAndValidates(t *testing.T) {
	service := &fakeBuildLogService{}
	e := newAuthorizedTestServer(Dependencies{BuildLogService: service})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/logs", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || service.lastAfterSeq != -1 || rec.Body.String() != "{\"chunks\":[],\"finished\":false}\n" {
		t.Fatalf("unexpected default response: %d %q after=%d", rec.Code, rec.Body.String(), service.lastAfterSeq)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/logs?afterSeq=abc", nil)
	addTestConsoleSession(req)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	service.err = domain.ErrBuildRecordNotFound
	req = httptest.NewRequest(http.MethodGet, "/api/v1/builds/missing/logs", nil)
	addTestConsoleSession(req)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestListBuildLogsRequiresSession(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{BuildLogService: &fakeBuildLogService{}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/logs", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
