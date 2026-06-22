package domain

import (
	"errors"
	"time"
)

var (
	ErrArtifactAccessTokenNotFound      = errors.New("artifact access token not found")
	ErrInvalidArtifactAccessTokenInput  = errors.New("invalid artifact access token request")
	ErrArtifactAccessTokenAlreadyExists = errors.New("artifact access token already exists")
)

type ArtifactAccessTokenStatus string

const (
	ArtifactAccessTokenStatusActive   ArtifactAccessTokenStatus = "active"
	ArtifactAccessTokenStatusDisabled ArtifactAccessTokenStatus = "disabled"
)

type ArtifactAccessToken struct {
	ID              string
	Name            string
	TokenHash       string
	TokenPreview    string
	Status          ArtifactAccessTokenStatus
	CreatedByUserID string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastUsedAt      *time.Time
	ExpiresAt       *time.Time
}

type ArtifactAccessTokenCreateInput struct {
	Name            string
	Token           string
	CreatedByUserID string
	ExpiresAt       *time.Time
}

type ArtifactAccessTokenCreateResult struct {
	Token ArtifactAccessToken
}

type ArtifactAccessTokenGenerateResult struct {
	PlainToken string
}

type ArtifactAccessTokenUpdateInput struct {
	Status ArtifactAccessTokenStatus
}

type ArtifactAccessTokenStoredInput struct {
	Name            string
	TokenHash       string
	TokenPreview    string
	Status          ArtifactAccessTokenStatus
	CreatedByUserID string
	ExpiresAt       *time.Time
}

type ArtifactAccessTokenValidationResult struct {
	Matched bool
	Allowed bool
}

func IsValidArtifactAccessTokenStatus(status ArtifactAccessTokenStatus) bool {
	switch status {
	case ArtifactAccessTokenStatusActive, ArtifactAccessTokenStatusDisabled:
		return true
	default:
		return false
	}
}
