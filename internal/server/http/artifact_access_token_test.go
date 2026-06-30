package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestCreateArtifactAccessToken(t *testing.T) {
	service := &fakeArtifactAccessTokenService{
		createResult: domain.ArtifactAccessTokenCreateResult{
			Token: testHTTPArtifactAccessToken("artifact-token-1", domain.ArtifactAccessTokenStatusActive),
		},
	}
	e := newAuthorizedTestServer(Dependencies{
		ArtifactAccessTokenService: service,
		FeishuAuthService:          testArtifactAccessTokenAuthService(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifact-access-tokens", jsonBody(artifactAccessTokenCreateRequest{
		Name:  "Bundle 客户端",
		Token: "forge_art_1234567890123456789012345678901234567890123",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	if service.lastCreateInput.CreatedByUserID != "ou-user-1" {
		t.Fatalf("unexpected create input: %+v", service.lastCreateInput)
	}
	if service.lastCreateInput.Token != "forge_art_1234567890123456789012345678901234567890123" {
		t.Fatalf("expected generated token in create input, got %q", service.lastCreateInput.Token)
	}
	if strings.Contains(rec.Body.String(), "token_hash") || strings.Contains(rec.Body.String(), "plainToken") {
		t.Fatalf("response leaked token secret: %s", rec.Body.String())
	}

	var response artifactAccessTokenDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Token.TokenPreview != "clie...3456" || response.Token.CreatedBy != "海鸥" {
		t.Fatalf("unexpected response: %#v", response.Token)
	}
}

func TestGenerateArtifactAccessToken(t *testing.T) {
	service := &fakeArtifactAccessTokenService{
		generateResult: domain.ArtifactAccessTokenGenerateResult{PlainToken: "forge_art_generated_token"},
	}
	e := newAuthorizedTestServer(Dependencies{
		ArtifactAccessTokenService: service,
		FeishuAuthService:          testArtifactAccessTokenAuthService(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifact-access-tokens/generate", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var response artifactAccessTokenGenerateResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.PlainToken != "forge_art_generated_token" {
		t.Fatalf("expected generated token, got %q", response.PlainToken)
	}
}

func TestCreateArtifactAccessTokenRejectsInvalidRequest(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		ArtifactAccessTokenService: &fakeArtifactAccessTokenService{err: domain.ErrInvalidArtifactAccessTokenInput},
		FeishuAuthService:          testArtifactAccessTokenAuthService(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifact-access-tokens", jsonBody(artifactAccessTokenCreateRequest{
		Name: "Bundle 客户端",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestListArtifactAccessTokensDoesNotReturnTokenHash(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ArtifactAccessTokenService: &fakeArtifactAccessTokenService{
			tokens: []domain.ArtifactAccessToken{testHTTPArtifactAccessToken("artifact-token-1", domain.ArtifactAccessTokenStatusActive)},
		},
		FeishuAuthService: testArtifactAccessTokenAuthService(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifact-access-tokens", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hash-secret") || strings.Contains(rec.Body.String(), "tokenHash") {
		t.Fatalf("response leaked token hash: %s", rec.Body.String())
	}
}

func TestUpdateArtifactAccessTokenStatus(t *testing.T) {
	service := &fakeArtifactAccessTokenService{
		updatedToken: testHTTPArtifactAccessToken("artifact-token-1", domain.ArtifactAccessTokenStatusDisabled),
	}
	e := newAuthorizedTestServer(Dependencies{
		ArtifactAccessTokenService: service,
		FeishuAuthService:          testArtifactAccessTokenAuthService(),
	})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifact-access-tokens/artifact-token-1", jsonBody(artifactAccessTokenUpdateRequest{Status: "disabled"}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastUpdateID != "artifact-token-1" || service.lastUpdateInput.Status != domain.ArtifactAccessTokenStatusDisabled {
		t.Fatalf("unexpected update input: id=%q input=%+v", service.lastUpdateID, service.lastUpdateInput)
	}
}

func TestDeleteArtifactAccessToken(t *testing.T) {
	service := &fakeArtifactAccessTokenService{}
	e := newAuthorizedTestServer(Dependencies{
		ArtifactAccessTokenService: service,
		FeishuAuthService:          testArtifactAccessTokenAuthService(),
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifact-access-tokens/artifact-token-1", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNoContent, rec.Code, rec.Body.String())
	}
	if service.deletedID != "artifact-token-1" {
		t.Fatalf("unexpected deleted id: %q", service.deletedID)
	}
}

func testArtifactAccessTokenAuthService() *fakeFeishuAuthService {
	return &fakeFeishuAuthService{result: domain.FeishuAuthResult{
		SessionExpiresAt: time.Now().Add(time.Hour),
		SessionToken:     "session-token",
		User: domain.FeishuUser{
			OpenID: "ou-user-1",
			Name:   "海鸥",
		},
	}}
}

func addTestConsoleSession(req *http.Request) {
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
}

func testHTTPArtifactAccessToken(id string, status domain.ArtifactAccessTokenStatus) domain.ArtifactAccessToken {
	createdAt := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	return domain.ArtifactAccessToken{
		ID:              id,
		Name:            "Bundle 客户端",
		TokenHash:       "hash-secret",
		TokenPreview:    "clie...3456",
		Status:          status,
		CreatedByUserID: "ou-user-1",
		CreatedBy:       "海鸥",
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
	}
}

type fakeArtifactAccessTokenService struct {
	createResult    domain.ArtifactAccessTokenCreateResult
	deletedID       string
	err             error
	generateResult  domain.ArtifactAccessTokenGenerateResult
	lastCreateInput domain.ArtifactAccessTokenCreateInput
	lastUpdateID    string
	lastUpdateInput domain.ArtifactAccessTokenUpdateInput
	tokens          []domain.ArtifactAccessToken
	updatedToken    domain.ArtifactAccessToken
	validation      domain.ArtifactAccessTokenValidationResult
	validatedToken  string
	validationErr   error
}

func (s *fakeArtifactAccessTokenService) CreateArtifactAccessToken(_ context.Context, input domain.ArtifactAccessTokenCreateInput) (domain.ArtifactAccessTokenCreateResult, error) {
	s.lastCreateInput = input
	if s.err != nil {
		return domain.ArtifactAccessTokenCreateResult{}, s.err
	}
	return s.createResult, nil
}

func (s *fakeArtifactAccessTokenService) DeleteArtifactAccessToken(_ context.Context, id string) error {
	s.deletedID = id
	return s.err
}

func (s *fakeArtifactAccessTokenService) GenerateArtifactAccessToken(context.Context) (domain.ArtifactAccessTokenGenerateResult, error) {
	if s.err != nil {
		return domain.ArtifactAccessTokenGenerateResult{}, s.err
	}
	return s.generateResult, nil
}

func (s *fakeArtifactAccessTokenService) ListArtifactAccessTokens(context.Context) ([]domain.ArtifactAccessToken, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.tokens, nil
}

func (s *fakeArtifactAccessTokenService) UpdateArtifactAccessToken(_ context.Context, id string, input domain.ArtifactAccessTokenUpdateInput) (domain.ArtifactAccessToken, error) {
	s.lastUpdateID = id
	s.lastUpdateInput = input
	if s.err != nil {
		return domain.ArtifactAccessToken{}, s.err
	}
	return s.updatedToken, nil
}

func (s *fakeArtifactAccessTokenService) ValidateArtifactAccessToken(_ context.Context, token string) (domain.ArtifactAccessTokenValidationResult, error) {
	s.validatedToken = token
	if s.validationErr != nil {
		return domain.ArtifactAccessTokenValidationResult{}, s.validationErr
	}
	return s.validation, nil
}
