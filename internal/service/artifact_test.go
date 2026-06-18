package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"forge-server/internal/domain"
)

const (
	testArtifactBuildID       = "018f4d7a-0a6c-789b-9c12-36a4dfd9c001"
	testArtifactSecondBuildID = "018f4d7a-0a6c-789b-9c12-36a4dfd9c002"
)

func TestArtifactServiceDownloadsManifestFile(t *testing.T) {
	record := testArtifactBuildRecord(testArtifactBuildID, "module-profile")
	repository := &fakeArtifactRepository{downloadRecord: record}
	manifestReader := &fakeServiceArtifactManifestReader{
		manifests: map[string]domain.ArtifactManifest{
			record.ID: {
				Files: []domain.ArtifactManifestFile{
					{
						Path:        "assets/icon.png",
						ObjectKey:   "rn-bundles/seaooo/profile/QA/" + record.ID + "/assets/icon.png",
						ContentType: "image/png",
						SizeBytes:   12,
						SHA256:      "hash",
					},
				},
			},
		},
	}
	objectReader := &fakeArtifactObjectReader{
		object: domain.ArtifactObject{
			Body:          io.NopCloser(strings.NewReader("image-body")),
			ContentLength: 12,
			ETag:          `"etag"`,
		},
	}
	service := NewArtifactService(repository, &fakeArtifactBuildRepository{}, manifestReader, objectReader)

	object, err := service.DownloadArtifactFile(context.Background(), domain.ArtifactDownloadInput{
		ApplicationSlug: "seaooo",
		ModuleID:        "module-profile",
		Environment:     "qa",
		BuildRecordID:   record.ID,
		Path:            "assets/icon.png",
	})
	if err != nil {
		t.Fatalf("download artifact file: %v", err)
	}
	defer object.Body.Close()

	if object.Path != "assets/icon.png" || object.Name != "icon.png" || object.ContentType != "image/png" {
		t.Fatalf("unexpected artifact object metadata: %+v", object)
	}
	if objectReader.lastObjectKey != "rn-bundles/seaooo/profile/QA/"+record.ID+"/assets/icon.png" {
		t.Fatalf("unexpected object key: %q", objectReader.lastObjectKey)
	}
}

func TestArtifactServiceRejectsInvalidDownloadPath(t *testing.T) {
	service := NewArtifactService(&fakeArtifactRepository{}, &fakeArtifactBuildRepository{}, &fakeServiceArtifactManifestReader{}, &fakeArtifactObjectReader{})

	_, err := service.DownloadArtifactFile(context.Background(), domain.ArtifactDownloadInput{
		ApplicationSlug: "seaooo",
		ModuleID:        "module-profile",
		Environment:     "qa",
		BuildRecordID:   testArtifactBuildID,
		Path:            "../secret",
	})
	if !errors.Is(err, domain.ErrInvalidArtifactInput) {
		t.Fatalf("expected invalid artifact input, got %v", err)
	}
}

func TestArtifactServiceManifestResolvesURLsAndMissingModules(t *testing.T) {
	firstRecord := testArtifactBuildRecord(testArtifactBuildID, "module-profile")
	firstRecord.StoragePublicBaseURL = "https://cdn.example.com/assets/"
	secondRecord := testArtifactBuildRecord(testArtifactSecondBuildID, "module-home")
	secondRecord.StoragePublicBaseURL = ""
	repository := &fakeArtifactRepository{
		manifestSource: domain.ArtifactManifestSource{
			Application: domain.ArtifactApplication{ID: "app-seaooo", Name: "seaooo", Slug: "seaooo"},
			Environment: domain.ArtifactEnvironment{ID: "qa", Name: "QA"},
			Modules: []domain.ArtifactModuleReleaseSource{
				{
					ID:            "module-profile",
					Name:          "profile",
					Kind:          domain.ModuleKindFeature,
					ReleaseID:     "018f4d7a-0a6c-789b-9c12-36a4dfd9d001",
					ReleaseType:   domain.ReleaseTypeTargeted,
					Identifier:    "staff-1",
					BuildRecordID: firstRecord.ID,
				},
				{
					ID:            "module-home",
					Name:          "home",
					Kind:          domain.ModuleKindFeature,
					ReleaseID:     "018f4d7a-0a6c-789b-9c12-36a4dfd9d002",
					ReleaseType:   domain.ReleaseTypeFull,
					BuildRecordID: secondRecord.ID,
				},
				{
					ID:   "module-settings",
					Name: "settings",
					Kind: domain.ModuleKindFeature,
				},
			},
		},
	}
	buildRepository := &fakeArtifactBuildRepository{
		records: map[string]domain.BuildRecord{
			firstRecord.ID:  firstRecord,
			secondRecord.ID: secondRecord,
		},
	}
	manifestReader := &fakeServiceArtifactManifestReader{
		manifests: map[string]domain.ArtifactManifest{
			firstRecord.ID: {
				Files: []domain.ArtifactManifestFile{
					{
						Path:        "assets/icon.png",
						ObjectKey:   "rn-bundles/seaooo/profile/QA/" + firstRecord.ID + "/assets/icon.png",
						ContentType: "image/png",
						SizeBytes:   12,
						SHA256:      "hash-1",
					},
				},
			},
			secondRecord.ID: {
				Files: []domain.ArtifactManifestFile{
					{
						Path:        "index.bundle",
						ObjectKey:   "rn-bundles/seaooo/home/QA/" + secondRecord.ID + "/index.bundle",
						ContentType: "application/javascript",
						SizeBytes:   24,
						SHA256:      "hash-2",
					},
				},
			},
		},
	}
	service := NewArtifactService(repository, buildRepository, manifestReader, &fakeArtifactObjectReader{})

	manifest, err := service.GetApplicationManifest(context.Background(), domain.ArtifactManifestInput{
		ApplicationSlug: "seaooo",
		Environment:     "qa",
		Identifier:      "staff-1",
	})
	if err != nil {
		t.Fatalf("get application manifest: %v", err)
	}

	if manifest.Identifier != "staff-1" || len(manifest.Modules) != 2 || len(manifest.MissingModules) != 1 {
		t.Fatalf("unexpected manifest shape: %+v", manifest)
	}
	if got := manifest.Modules[0].Files[0].URL; got != "https://cdn.example.com/assets/rn-bundles/seaooo/profile/QA/"+firstRecord.ID+"/assets/icon.png" {
		t.Fatalf("unexpected public file url: %q", got)
	}
	wantDownloadURL := "/api/v1/artifacts/applications/seaooo/modules/module-home/environments/qa/builds/" + secondRecord.ID + "/file?path=index.bundle"
	if got := manifest.Modules[1].Files[0].URL; got != wantDownloadURL {
		t.Fatalf("unexpected fallback file url: %q", got)
	}
	if manifest.MissingModules[0].ID != "module-settings" || manifest.MissingModules[0].Reason != "release not found" {
		t.Fatalf("unexpected missing module: %+v", manifest.MissingModules[0])
	}
}

type fakeArtifactRepository struct {
	downloadRecord domain.BuildRecord
	manifestSource domain.ArtifactManifestSource
	err            error
}

func (r *fakeArtifactRepository) GetArtifactBuildRecord(context.Context, domain.ArtifactDownloadInput) (domain.BuildRecord, error) {
	if r.err != nil {
		return domain.BuildRecord{}, r.err
	}
	return r.downloadRecord, nil
}

func (r *fakeArtifactRepository) GetArtifactManifestSource(context.Context, domain.ArtifactManifestInput) (domain.ArtifactManifestSource, error) {
	if r.err != nil {
		return domain.ArtifactManifestSource{}, r.err
	}
	return r.manifestSource, nil
}

type fakeArtifactBuildRepository struct {
	records map[string]domain.BuildRecord
	err     error
}

func (r *fakeArtifactBuildRepository) GetBuildRecord(_ context.Context, id string) (domain.BuildRecord, error) {
	if r.err != nil {
		return domain.BuildRecord{}, r.err
	}
	record, ok := r.records[id]
	if !ok {
		return domain.BuildRecord{}, domain.ErrBuildRecordNotFound
	}
	return record, nil
}

type fakeServiceArtifactManifestReader struct {
	manifests map[string]domain.ArtifactManifest
	err       error
}

func (r *fakeServiceArtifactManifestReader) ReadArtifactManifest(_ context.Context, record domain.BuildRecord) (domain.ArtifactManifest, error) {
	if r.err != nil {
		return domain.ArtifactManifest{}, r.err
	}
	manifest, ok := r.manifests[record.ID]
	if !ok {
		return domain.ArtifactManifest{}, errors.New("missing manifest")
	}
	return manifest, nil
}

type fakeArtifactObjectReader struct {
	object        domain.ArtifactObject
	err           error
	lastObjectKey string
}

func (r *fakeArtifactObjectReader) GetArtifactObject(_ context.Context, _ domain.BuildRecord, objectKey string) (domain.ArtifactObject, error) {
	r.lastObjectKey = objectKey
	if r.err != nil {
		return domain.ArtifactObject{}, r.err
	}
	return r.object, nil
}

func testArtifactBuildRecord(id string, moduleID string) domain.BuildRecord {
	moduleName := strings.TrimPrefix(moduleID, "module-")
	return domain.BuildRecord{
		ID:                       id,
		ApplicationName:          "seaooo",
		ModuleID:                 moduleID,
		ModuleName:               moduleName,
		Environment:              "qa",
		EnvironmentName:          "QA",
		StorageConfigurationID:   "storage-qa",
		StorageConfigurationName: "QA 存储",
		StorageBucket:            "forge-assets",
		StorageEndpoint:          "https://s3.example.com",
		StorageRegion:            "cn-beijing",
		StoragePublicBaseURL:     "https://cdn.example.com/assets",
		StorageAccessKeyID:       "access-key",
		StorageSecretAccessKey:   "secret-key",
		ArtifactKey:              "rn-bundles/seaooo/" + moduleName + "/QA/" + id,
		ManifestKey:              "rn-bundles/seaooo/" + moduleName + "/QA/" + id + "/manifest.json",
		Status:                   domain.BuildStatusSucceeded,
		CreatedAt:                time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC),
	}
}
