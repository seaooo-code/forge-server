package service

import (
	"context"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestArtifactAccessTokenServiceCreateHashesToken(t *testing.T) {
	repository := &fakeArtifactAccessTokenRepository{}
	service := NewArtifactAccessTokenService(repository)
	plainToken := "forge_art_1234567890123456789012345678901234567890123"

	created, err := service.CreateArtifactAccessToken(context.Background(), domain.ArtifactAccessTokenCreateInput{
		Name:            " Bundle 客户端 ",
		Token:           plainToken,
		CreatedByUserID: "ou-user-1",
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if repository.lastCreateInput.Name != "Bundle 客户端" {
		t.Fatalf("expected trimmed name, got %q", repository.lastCreateInput.Name)
	}
	if repository.lastCreateInput.TokenHash == plainToken || repository.lastCreateInput.TokenHash == "" {
		t.Fatalf("expected hashed token, got %q", repository.lastCreateInput.TokenHash)
	}
	if repository.lastCreateInput.TokenPreview != artifactAccessTokenPreview(plainToken) {
		t.Fatalf("unexpected token preview: %q", repository.lastCreateInput.TokenPreview)
	}
	if created.Token.TokenHash != repository.lastCreateInput.TokenHash {
		t.Fatalf("expected repository response")
	}
}

func TestArtifactAccessTokenServiceGenerateToken(t *testing.T) {
	service := NewArtifactAccessTokenService(&fakeArtifactAccessTokenRepository{})

	result, err := service.GenerateArtifactAccessToken(context.Background())
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if !validGeneratedArtifactAccessToken(result.PlainToken) {
		t.Fatalf("expected generated token format, got %q", result.PlainToken)
	}
}

func TestArtifactAccessTokenServiceRejectsMissingName(t *testing.T) {
	service := NewArtifactAccessTokenService(&fakeArtifactAccessTokenRepository{})

	_, err := service.CreateArtifactAccessToken(context.Background(), domain.ArtifactAccessTokenCreateInput{
		Token:           "forge_art_1234567890123456789012345678901234567890123",
		CreatedByUserID: "ou-user-1",
	})
	if err != domain.ErrInvalidArtifactAccessTokenInput {
		t.Fatalf("expected invalid input error, got %v", err)
	}
}

func TestArtifactAccessTokenServiceRejectsMissingGeneratedToken(t *testing.T) {
	service := NewArtifactAccessTokenService(&fakeArtifactAccessTokenRepository{})

	_, err := service.CreateArtifactAccessToken(context.Background(), domain.ArtifactAccessTokenCreateInput{
		Name:            "Bundle 客户端",
		CreatedByUserID: "ou-user-1",
	})
	if err != domain.ErrInvalidArtifactAccessTokenInput {
		t.Fatalf("expected invalid input error, got %v", err)
	}
}

func TestArtifactAccessTokenServiceValidateActiveToken(t *testing.T) {
	repository := &fakeArtifactAccessTokenRepository{
		findToken: domain.ArtifactAccessToken{
			ID:     "artifact-token-1",
			Status: domain.ArtifactAccessTokenStatusActive,
		},
	}
	service := NewArtifactAccessTokenService(repository)

	result, err := service.ValidateArtifactAccessToken(context.Background(), "client-token-123456")
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if !result.Matched || !result.Allowed {
		t.Fatalf("expected active token to be allowed, got %+v", result)
	}
	if repository.touchedID != "artifact-token-1" {
		t.Fatalf("expected last_used_at touch, got %q", repository.touchedID)
	}
}

func TestArtifactAccessTokenServiceValidateDisabledOrExpiredToken(t *testing.T) {
	expiredAt := time.Now().Add(-time.Minute)
	tests := []struct {
		name  string
		token domain.ArtifactAccessToken
	}{
		{
			name:  "disabled",
			token: domain.ArtifactAccessToken{ID: "artifact-token-1", Status: domain.ArtifactAccessTokenStatusDisabled},
		},
		{
			name:  "expired",
			token: domain.ArtifactAccessToken{ID: "artifact-token-2", Status: domain.ArtifactAccessTokenStatusActive, ExpiresAt: &expiredAt},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &fakeArtifactAccessTokenRepository{findToken: tt.token}
			service := NewArtifactAccessTokenService(repository)

			result, err := service.ValidateArtifactAccessToken(context.Background(), "client-token-123456")
			if err != nil {
				t.Fatalf("validate token: %v", err)
			}
			if !result.Matched || result.Allowed {
				t.Fatalf("expected token to match but be denied, got %+v", result)
			}
			if repository.touchedID != "" {
				t.Fatalf("expected denied token not to be touched, got %q", repository.touchedID)
			}
		})
	}
}

func TestArtifactAccessTokenServiceValidateUnknownToken(t *testing.T) {
	service := NewArtifactAccessTokenService(&fakeArtifactAccessTokenRepository{findErr: domain.ErrArtifactAccessTokenNotFound})

	result, err := service.ValidateArtifactAccessToken(context.Background(), "client-token-123456")
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if result.Matched || result.Allowed {
		t.Fatalf("expected unknown token to be ignored, got %+v", result)
	}
}

type fakeArtifactAccessTokenRepository struct {
	findErr         error
	findToken       domain.ArtifactAccessToken
	lastCreateInput domain.ArtifactAccessTokenStoredInput
	touchedID       string
}

func (r *fakeArtifactAccessTokenRepository) CreateArtifactAccessToken(_ context.Context, input domain.ArtifactAccessTokenStoredInput) (domain.ArtifactAccessToken, error) {
	r.lastCreateInput = input
	return domain.ArtifactAccessToken{
		ID:              "artifact-token-1",
		Name:            input.Name,
		TokenHash:       input.TokenHash,
		TokenPreview:    input.TokenPreview,
		Status:          input.Status,
		CreatedByUserID: input.CreatedByUserID,
	}, nil
}

func (r *fakeArtifactAccessTokenRepository) DeleteArtifactAccessToken(context.Context, string) error {
	return nil
}

func (r *fakeArtifactAccessTokenRepository) FindArtifactAccessTokenByHash(context.Context, string) (domain.ArtifactAccessToken, error) {
	if r.findErr != nil {
		return domain.ArtifactAccessToken{}, r.findErr
	}
	return r.findToken, nil
}

func (r *fakeArtifactAccessTokenRepository) ListArtifactAccessTokens(context.Context) ([]domain.ArtifactAccessToken, error) {
	return nil, nil
}

func (r *fakeArtifactAccessTokenRepository) TouchArtifactAccessToken(_ context.Context, id string) error {
	r.touchedID = id
	return nil
}

func (r *fakeArtifactAccessTokenRepository) UpdateArtifactAccessToken(context.Context, string, domain.ArtifactAccessTokenUpdateInput) (domain.ArtifactAccessToken, error) {
	return domain.ArtifactAccessToken{}, nil
}
