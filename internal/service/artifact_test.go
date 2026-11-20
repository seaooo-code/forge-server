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

func TestArtifactServiceDownloadsPlatformRelativePath(t *testing.T) {
	record := testArtifactBuildRecord(testArtifactBuildID, "module-profile")
	objectKey := "rn-bundles/seaooo/profile/QA/" + record.ID + "/android/index.bundle"
	manifestReader := &fakeServiceArtifactManifestReader{
		manifests: map[string]domain.ArtifactManifest{
			record.ID: {Files: []domain.ArtifactManifestFile{
				{Path: "android/index.bundle", ObjectKey: objectKey, ContentType: "application/javascript", SizeBytes: 3},
			}},
		},
	}
	objectReader := &fakeArtifactObjectReader{object: domain.ArtifactObject{Body: io.NopCloser(strings.NewReader("abc"))}}
	service := NewArtifactService(&fakeArtifactRepository{downloadRecord: record}, &fakeArtifactBuildRepository{}, manifestReader, objectReader)

	input := domain.ArtifactDownloadInput{
		ApplicationSlug: "seaooo",
		ModuleID:        "module-profile",
		Environment:     "qa",
		BuildRecordID:   record.ID,
		Platform:        domain.PlatformAndroid,
		Path:            "index.bundle",
	}
	object, err := service.DownloadArtifactFile(context.Background(), input)
	if err != nil {
		t.Fatalf("download platform artifact file: %v", err)
	}
	defer object.Body.Close()
	if objectReader.lastObjectKey != objectKey || object.Path != "index.bundle" || object.Name != "index.bundle" {
		t.Fatalf("unexpected platform download: key=%q object=%+v", objectReader.lastObjectKey, object)
	}

	input.Platform = "windows"
	if _, err := service.DownloadArtifactFile(context.Background(), input); !errors.Is(err, domain.ErrInvalidArtifactInput) {
		t.Fatalf("expected invalid platform to be rejected, got %v", err)
	}
}

func TestArtifactServiceManifestRequiresPlatformAndRuntimeVersion(t *testing.T) {
	service := NewArtifactService(&fakeArtifactRepository{}, &fakeArtifactBuildRepository{}, &fakeServiceArtifactManifestReader{}, &fakeArtifactObjectReader{})
	for _, input := range []domain.ArtifactManifestInput{
		{ApplicationSlug: "seaooo", Environment: "qa", RuntimeVersion: "1.0.0"},
		{ApplicationSlug: "seaooo", Environment: "qa", Platform: domain.PlatformAndroid},
		{ApplicationSlug: "seaooo", Environment: "qa", Platform: "web", RuntimeVersion: "1.0.0"},
		{ApplicationSlug: "seaooo", Environment: "qa", Platform: domain.PlatformAndroid, RuntimeVersion: "1.0.0/beta"},
	} {
		if _, err := service.GetApplicationManifest(context.Background(), input); !errors.Is(err, domain.ErrInvalidArtifactInput) {
			t.Fatalf("expected invalid artifact input for %+v, got %v", input, err)
		}
	}
}

func TestArtifactServiceManifestResolvesURLsAndMissingModules(t *testing.T) {
	firstRecord := testArtifactBuildRecord(testArtifactBuildID, "module-profile")
	firstRecord.StoragePublicBaseURL = "https://cdn.example.com/assets/"
	firstRecord.Branch = "feature/login"
	firstRecord.CommitSHA = "abc123"
	secondRecord := testArtifactBuildRecord(testArtifactSecondBuildID, "module-home")
	secondRecord.StoragePublicBaseURL = ""
	iosOnlyRecord := testArtifactBuildRecord("018f4d7a-0a6c-789b-9c12-36a4dfd9c003", "module-ios")
	iosOnlyRecord.Platforms = []string{domain.PlatformIOS}
	repository := &fakeArtifactRepository{
		manifestSource: domain.ArtifactManifestSource{
			Application: domain.ArtifactApplication{ID: "app-seaooo", Name: "seaooo", Slug: "seaooo"},
			Environment: domain.ArtifactEnvironment{ID: "qa", Name: "QA"},
			Modules: []domain.ArtifactModuleReleaseSource{
				{
					ID:              "module-profile",
					Name:            "profile",
					Kind:            domain.ModuleKindFeature,
					TestPlanID:      "018f4d7a-0a6c-789b-9c12-36a4dfd9e001",
					TestPlanName:    "登录改版",
					TestPlanMatches: 1,
					BuildRecordID:   firstRecord.ID,
					HasRuntimeRoute: true,
				},
				{
					ID:              "module-home",
					Name:            "home",
					Kind:            domain.ModuleKindFeature,
					ReleaseID:       "018f4d7a-0a6c-789b-9c12-36a4dfd9d002",
					ReleaseType:     domain.ReleaseTypeFull,
					BuildRecordID:   secondRecord.ID,
					HasRuntimeRoute: true,
				},
				{ID: "module-settings", Name: "settings", Kind: domain.ModuleKindFeature},
				{ID: "module-gray-only", Name: "gray-only", Kind: domain.ModuleKindFeature, HasRuntimeRoute: true},
				{
					ID:              "module-ios",
					Name:            "ios",
					Kind:            domain.ModuleKindFeature,
					ReleaseID:       "018f4d7a-0a6c-789b-9c12-36a4dfd9d003",
					ReleaseType:     domain.ReleaseTypeFull,
					BuildRecordID:   iosOnlyRecord.ID,
					HasRuntimeRoute: true,
				},
			},
		},
	}
	buildRepository := &fakeArtifactBuildRepository{
		records: map[string]domain.BuildRecord{
			firstRecord.ID:   firstRecord,
			secondRecord.ID:  secondRecord,
			iosOnlyRecord.ID: iosOnlyRecord,
		},
	}
	firstPrefix := "rn-bundles/seaooo/profile/QA/" + firstRecord.ID
	secondPrefix := "rn-bundles/seaooo/home/QA/" + secondRecord.ID
	manifestReader := &fakeServiceArtifactManifestReader{
		manifests: map[string]domain.ArtifactManifest{
			firstRecord.ID: {
				Files: []domain.ArtifactManifestFile{
					{Path: "forge.json", ObjectKey: firstPrefix + "/forge.json", SizeBytes: 2},
					{Path: "android/assets/icon.png", ObjectKey: firstPrefix + "/android/assets/icon.png", ContentType: "image/png", SizeBytes: 12, SHA256: "hash-1", Digests: map[string]string{"sha256": "hash-1", "md5": "md5-1"}},
					{Path: "ios/assets/icon.png", ObjectKey: firstPrefix + "/ios/assets/icon.png", ContentType: "image/png", SizeBytes: 12, SHA256: "hash-ios"},
				},
			},
			secondRecord.ID: {
				Files: []domain.ArtifactManifestFile{
					{Path: "android/index.bundle", ObjectKey: secondPrefix + "/android/index.bundle", ContentType: "application/javascript", SizeBytes: 24, SHA256: "hash-2"},
				},
			},
		},
	}
	service := NewArtifactService(repository, buildRepository, manifestReader, &fakeArtifactObjectReader{})

	manifest, err := service.GetApplicationManifest(context.Background(), domain.ArtifactManifestInput{
		ApplicationSlug: "seaooo",
		Environment:     "qa",
		Platform:        " android ",
		RuntimeVersion:  "1.0.0",
		UserID:          " 1001 ",
	})
	if err != nil {
		t.Fatalf("get application manifest: %v", err)
	}

	if manifest.UserID != "1001" || manifest.Platform != domain.PlatformAndroid || manifest.RuntimeVersion != "1.0.0" ||
		len(manifest.Modules) != 2 || len(manifest.MissingModules) != 3 {
		t.Fatalf("unexpected manifest shape: %+v", manifest)
	}
	profile := manifest.Modules[0]
	if profile.Branch != "feature/login" || profile.CommitSHA != "abc123" {
		t.Fatalf("expected build branch and commit on module, got %+v", profile)
	}
	if profile.Source != domain.ArtifactSourceTestPlan || profile.TestPlanName != "登录改版" || profile.ReleaseID != "" {
		t.Fatalf("expected module from test plan without release fields, got %+v", profile)
	}
	if home := manifest.Modules[1]; home.Source != domain.ArtifactSourceFull || home.TestPlanID != "" {
		t.Fatalf("expected full release module, got %+v", home)
	}
	if len(profile.Files) != 1 || profile.Files[0].Path != "assets/icon.png" || profile.Files[0].SHA256 != "hash-1" {
		t.Fatalf("expected only android files with platform prefix removed, got %+v", profile.Files)
	}
	if digests := profile.Files[0].Digests; len(digests) != 2 || digests["sha256"] != "hash-1" || digests["md5"] != "md5-1" {
		t.Fatalf("expected manifest digests to be passed through, got %+v", digests)
	}
	// 旧清单没有 digests 字段时用 sha256 补齐。
	if digests := manifest.Modules[1].Files[0].Digests; len(digests) != 1 || digests["sha256"] != "hash-2" {
		t.Fatalf("expected legacy manifest digests to fall back to sha256, got %+v", digests)
	}
	if got := profile.Files[0].URL; got != "https://cdn.example.com/assets/"+firstPrefix+"/android/assets/icon.png" {
		t.Fatalf("unexpected public file url: %q", got)
	}
	wantDownloadURL := "/api/v1/artifacts/applications/seaooo/modules/module-home/environments/qa/builds/" + secondRecord.ID + "/file?path=index.bundle&platform=android"
	if got := manifest.Modules[1].Files[0].URL; got != wantDownloadURL {
		t.Fatalf("unexpected fallback file url: %q", got)
	}

	reasons := map[string]string{}
	for _, missing := range manifest.MissingModules {
		reasons[missing.ID] = missing.Reason
	}
	if reasons["module-settings"] != domain.ArtifactMissingReasonNoReleaseForRuntime ||
		reasons["module-gray-only"] != domain.ArtifactMissingReasonReleaseNotFound ||
		reasons["module-ios"] != domain.ArtifactMissingReasonPlatformUnavailable {
		t.Fatalf("unexpected missing module reasons: %+v", reasons)
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
		RuntimeVersion:           "1.0.0",
		Platforms:                []string{domain.PlatformAndroid, domain.PlatformIOS},
		Status:                   domain.BuildStatusSucceeded,
		CreatedAt:                time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC),
	}
}
