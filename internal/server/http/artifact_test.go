package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestDownloadArtifactFile(t *testing.T) {
	service := &fakeArtifactService{
		object: domain.ArtifactObject{
			Body:          io.NopCloser(strings.NewReader("image-body")),
			ContentType:   "image/png",
			ContentLength: 10,
			ETag:          `"etag"`,
			Name:          "icon.png",
		},
	}
	e := New(config.Config{}, Dependencies{ArtifactService: service})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/applications/seaooo/modules/module-profile/environments/qa/builds/018f4d7a-0a6c-789b-9c12-36a4dfd9c001/file?path=assets/icon.png", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "image-body" {
		t.Fatalf("unexpected response body: %q", rec.Body.String())
	}
	if rec.Header().Get(echoHeaderContentType) != "image/png" {
		t.Fatalf("unexpected content type: %q", rec.Header().Get(echoHeaderContentType))
	}
	if rec.Header().Get("Etag") != `"etag"` {
		t.Fatalf("unexpected etag: %q", rec.Header().Get("Etag"))
	}
	if rec.Header().Get("Content-Disposition") != `attachment; filename=icon.png` {
		t.Fatalf("unexpected content disposition: %q", rec.Header().Get("Content-Disposition"))
	}
	if service.lastDownloadInput.ApplicationSlug != "seaooo" ||
		service.lastDownloadInput.ModuleID != "module-profile" ||
		service.lastDownloadInput.Environment != "qa" ||
		service.lastDownloadInput.Path != "assets/icon.png" {
		t.Fatalf("unexpected download input: %+v", service.lastDownloadInput)
	}
}

func TestDownloadArtifactFileRejectsInvalidRequest(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ArtifactService: &fakeArtifactService{err: domain.ErrInvalidArtifactInput},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/applications/seaooo/modules/module-profile/environments/qa/builds/018f4d7a-0a6c-789b-9c12-36a4dfd9c001/file?path=../secret", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestDownloadArtifactFileNotFound(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ArtifactService: &fakeArtifactService{err: domain.ErrArtifactNotFound},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/applications/seaooo/modules/module-profile/environments/qa/builds/018f4d7a-0a6c-789b-9c12-36a4dfd9c001/file?path=assets/icon.png", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

func TestGetArtifactManifest(t *testing.T) {
	service := &fakeArtifactService{
		manifest: domain.ArtifactApplicationManifest{
			Application: domain.ArtifactApplication{ID: "app-seaooo", Name: "seaooo", Slug: "seaooo"},
			Environment: domain.ArtifactEnvironment{ID: "qa", Name: "QA"},
			Identifier:  "staff-1",
			Modules: []domain.ArtifactModuleManifest{
				{
					ID:            "module-profile",
					Name:          "profile",
					Kind:          domain.ModuleKindFeature,
					ReleaseID:     "018f4d7a-0a6c-789b-9c12-36a4dfd9d001",
					ReleaseType:   domain.ReleaseTypeTargeted,
					Identifier:    "staff-1",
					BuildRecordID: "018f4d7a-0a6c-789b-9c12-36a4dfd9c001",
					ArtifactKey:   "rn-bundles/seaooo/profile/QA/build-1",
					ManifestKey:   "rn-bundles/seaooo/profile/QA/build-1/manifest.json",
					Files: []domain.ArtifactManifestResolvedFile{
						{
							Path:        "assets/icon.png",
							URL:         "https://cdn.example.com/assets/icon.png",
							ContentType: "image/png",
							SizeBytes:   12,
							SHA256:      "hash",
						},
					},
				},
			},
			MissingModules: []domain.ArtifactMissingModule{
				{ID: "module-home", Name: "home", Kind: domain.ModuleKindFeature, Reason: "release not found"},
			},
		},
	}
	e := New(config.Config{}, Dependencies{ArtifactService: service})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/applications/seaooo/environments/qa/manifest?identifier=staff-1", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response artifactManifestResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Application.Slug != "seaooo" ||
		response.Environment.ID != "qa" ||
		response.Identifier != "staff-1" ||
		len(response.Modules) != 1 ||
		len(response.MissingModules) != 1 ||
		response.Modules[0].Files[0].URL != "https://cdn.example.com/assets/icon.png" {
		t.Fatalf("unexpected manifest response: %#v", response)
	}
	if service.lastManifestInput.Identifier != "staff-1" {
		t.Fatalf("expected identifier to be passed to service, got %+v", service.lastManifestInput)
	}
}

type fakeArtifactService struct {
	object            domain.ArtifactObject
	manifest          domain.ArtifactApplicationManifest
	err               error
	lastDownloadInput domain.ArtifactDownloadInput
	lastManifestInput domain.ArtifactManifestInput
}

func (s *fakeArtifactService) DownloadArtifactFile(_ context.Context, input domain.ArtifactDownloadInput) (domain.ArtifactObject, error) {
	s.lastDownloadInput = input
	if s.err != nil {
		return domain.ArtifactObject{}, s.err
	}
	return s.object, nil
}

func (s *fakeArtifactService) GetApplicationManifest(_ context.Context, input domain.ArtifactManifestInput) (domain.ArtifactApplicationManifest, error) {
	s.lastManifestInput = input
	if s.err != nil {
		return domain.ArtifactApplicationManifest{}, s.err
	}
	return s.manifest, nil
}

const echoHeaderContentType = "Content-Type"
