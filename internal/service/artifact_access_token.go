package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"forge-server/internal/domain"
)

type ArtifactAccessTokenRepository interface {
	CreateArtifactAccessToken(ctx context.Context, input domain.ArtifactAccessTokenStoredInput) (domain.ArtifactAccessToken, error)
	DeleteArtifactAccessToken(ctx context.Context, id string) error
	FindArtifactAccessTokenByHash(ctx context.Context, tokenHash string) (domain.ArtifactAccessToken, error)
	ListArtifactAccessTokens(ctx context.Context) ([]domain.ArtifactAccessToken, error)
	TouchArtifactAccessToken(ctx context.Context, id string) error
	UpdateArtifactAccessToken(ctx context.Context, id string, input domain.ArtifactAccessTokenUpdateInput) (domain.ArtifactAccessToken, error)
}

type ArtifactAccessTokenService struct {
	repository ArtifactAccessTokenRepository
}

func NewArtifactAccessTokenService(repository ArtifactAccessTokenRepository) *ArtifactAccessTokenService {
	return &ArtifactAccessTokenService{repository: repository}
}

func (s *ArtifactAccessTokenService) ListArtifactAccessTokens(ctx context.Context) ([]domain.ArtifactAccessToken, error) {
	return s.repository.ListArtifactAccessTokens(ctx)
}

func (s *ArtifactAccessTokenService) GenerateArtifactAccessToken(_ context.Context) (domain.ArtifactAccessTokenGenerateResult, error) {
	plainToken, err := generateArtifactAccessToken()
	if err != nil {
		return domain.ArtifactAccessTokenGenerateResult{}, err
	}
	return domain.ArtifactAccessTokenGenerateResult{PlainToken: plainToken}, nil
}

func (s *ArtifactAccessTokenService) CreateArtifactAccessToken(ctx context.Context, input domain.ArtifactAccessTokenCreateInput) (domain.ArtifactAccessTokenCreateResult, error) {
	name := strings.TrimSpace(input.Name)
	plainToken := strings.TrimSpace(input.Token)
	createdByUserID := strings.TrimSpace(input.CreatedByUserID)
	if name == "" || !validGeneratedArtifactAccessToken(plainToken) || createdByUserID == "" {
		return domain.ArtifactAccessTokenCreateResult{}, domain.ErrInvalidArtifactAccessTokenInput
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		return domain.ArtifactAccessTokenCreateResult{}, domain.ErrInvalidArtifactAccessTokenInput
	}

	record, err := s.repository.CreateArtifactAccessToken(ctx, domain.ArtifactAccessTokenStoredInput{
		Name:            name,
		TokenHash:       artifactAccessTokenHash(plainToken),
		TokenPreview:    artifactAccessTokenPreview(plainToken),
		Status:          domain.ArtifactAccessTokenStatusActive,
		CreatedByUserID: createdByUserID,
		ExpiresAt:       input.ExpiresAt,
	})
	if err != nil {
		return domain.ArtifactAccessTokenCreateResult{}, err
	}
	return domain.ArtifactAccessTokenCreateResult{Token: record}, nil
}

func (s *ArtifactAccessTokenService) UpdateArtifactAccessToken(ctx context.Context, id string, input domain.ArtifactAccessTokenUpdateInput) (domain.ArtifactAccessToken, error) {
	id = strings.TrimSpace(id)
	if id == "" || !domain.IsValidArtifactAccessTokenStatus(input.Status) {
		return domain.ArtifactAccessToken{}, domain.ErrInvalidArtifactAccessTokenInput
	}
	return s.repository.UpdateArtifactAccessToken(ctx, id, input)
}

func (s *ArtifactAccessTokenService) DeleteArtifactAccessToken(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.ErrArtifactAccessTokenNotFound
	}
	return s.repository.DeleteArtifactAccessToken(ctx, id)
}

func (s *ArtifactAccessTokenService) ValidateArtifactAccessToken(ctx context.Context, token string) (domain.ArtifactAccessTokenValidationResult, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return domain.ArtifactAccessTokenValidationResult{}, nil
	}

	record, err := s.repository.FindArtifactAccessTokenByHash(ctx, artifactAccessTokenHash(token))
	if err != nil {
		if errors.Is(err, domain.ErrArtifactAccessTokenNotFound) {
			return domain.ArtifactAccessTokenValidationResult{}, nil
		}
		return domain.ArtifactAccessTokenValidationResult{}, err
	}

	result := domain.ArtifactAccessTokenValidationResult{Matched: true}
	if record.Status != domain.ArtifactAccessTokenStatusActive {
		return result, nil
	}
	if record.ExpiresAt != nil && !record.ExpiresAt.After(time.Now()) {
		return result, nil
	}
	if err := s.repository.TouchArtifactAccessToken(ctx, record.ID); err != nil {
		return domain.ArtifactAccessTokenValidationResult{}, err
	}
	result.Allowed = true
	return result, nil
}

func generateArtifactAccessToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "forge_art_" + base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func validGeneratedArtifactAccessToken(token string) bool {
	return strings.HasPrefix(token, "forge_art_") && len(token) == len("forge_art_")+43
}

func artifactAccessTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func artifactAccessTokenPreview(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "..." + token[len(token)-4:]
}
