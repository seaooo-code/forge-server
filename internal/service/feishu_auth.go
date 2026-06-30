package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"forge-server/internal/domain"
)

const feishuSessionRefreshWindow = 5 * time.Minute

type FeishuOAuthClient interface {
	AuthorizationURL(state string) (string, error)
	ExchangeCode(ctx context.Context, code string) (domain.FeishuToken, error)
	GetUserInfo(ctx context.Context, accessToken string) (domain.FeishuUser, error)
	RefreshAccessToken(ctx context.Context, refreshToken string) (domain.FeishuToken, error)
}

type FeishuUserRepository interface {
	BootstrapFirstSuperAdmin(ctx context.Context, userID string) error
	CreateFeishuAuthSession(ctx context.Context, session domain.FeishuAuthSession) error
	DeleteFeishuAuthSession(ctx context.Context, sessionTokenHash string) error
	FindFeishuAuthSession(ctx context.Context, sessionTokenHash string) (domain.FeishuAuthSession, error)
	ListGlobalRoles(ctx context.Context, userID string) ([]domain.GlobalRole, error)
	UpdateFeishuAuthSessionToken(ctx context.Context, session domain.FeishuAuthSession) error
	UpsertFeishuUser(ctx context.Context, user domain.FeishuUser) (domain.FeishuUser, error)
	UpsertUser(ctx context.Context, user domain.UserInput) error
}

type FeishuAuthService struct {
	client     FeishuOAuthClient
	repository FeishuUserRepository
}

func NewFeishuAuthService(client FeishuOAuthClient, repository FeishuUserRepository) *FeishuAuthService {
	return &FeishuAuthService{
		client:     client,
		repository: repository,
	}
}

func (s *FeishuAuthService) AuthorizationURL(state string) (string, error) {
	return s.client.AuthorizationURL(state)
}

func (s *FeishuAuthService) CompleteAuthorization(ctx context.Context, code string) (domain.FeishuAuthResult, error) {
	token, err := s.client.ExchangeCode(ctx, code)
	if err != nil {
		return domain.FeishuAuthResult{}, err
	}

	user, err := s.client.GetUserInfo(ctx, token.AccessToken)
	if err != nil {
		return domain.FeishuAuthResult{}, err
	}

	savedUser, err := s.repository.UpsertFeishuUser(ctx, user)
	if err != nil {
		return domain.FeishuAuthResult{}, err
	}
	userID, err := s.syncConsoleUser(ctx, savedUser)
	if err != nil {
		return domain.FeishuAuthResult{}, err
	}

	sessionToken, err := randomSessionToken()
	if err != nil {
		return domain.FeishuAuthResult{}, err
	}

	now := time.Now()
	session := newAuthSession(sessionTokenHash(sessionToken), savedUser.UnionID, token, now)
	if err := s.repository.CreateFeishuAuthSession(ctx, session); err != nil {
		return domain.FeishuAuthResult{}, err
	}

	roles, err := s.repository.ListGlobalRoles(ctx, userID)
	if err != nil {
		return domain.FeishuAuthResult{}, err
	}
	token.ExpiresIn = secondsUntil(session.AccessTokenExpiresAt, now)
	return domain.FeishuAuthResult{
		Capabilities:     domain.GlobalCapabilitiesForRoles(roles),
		Roles:            roles,
		SessionExpiresAt: sessionExpiresAt(session),
		SessionToken:     sessionToken,
		Token:            token,
		User:             savedUser,
	}, nil
}

func (s *FeishuAuthService) GetSession(ctx context.Context, sessionToken string) (domain.FeishuAuthResult, error) {
	session, err := s.repository.FindFeishuAuthSession(ctx, sessionTokenHash(sessionToken))
	if err != nil {
		return domain.FeishuAuthResult{}, err
	}

	now := time.Now()
	sessionExpired := !now.Before(session.AccessTokenExpiresAt)
	if session.RefreshToken != "" {
		sessionExpired = refreshTokenExpired(session, now)
	}
	if sessionExpired {
		_ = s.repository.DeleteFeishuAuthSession(ctx, session.SessionTokenHash)
		return domain.FeishuAuthResult{}, domain.ErrFeishuSessionExpired
	}

	if !session.AccessTokenExpiresAt.After(now.Add(feishuSessionRefreshWindow)) {
		refreshedSession, err := s.refreshSession(ctx, session, now)
		if err != nil {
			if now.Before(session.AccessTokenExpiresAt) {
				return s.authResultWithRoles(ctx, session, sessionToken, now)
			}
			return domain.FeishuAuthResult{}, err
		}
		session = refreshedSession
	}
	if _, err := s.syncConsoleUser(ctx, session.User); err != nil {
		return domain.FeishuAuthResult{}, err
	}

	return s.authResultWithRoles(ctx, session, sessionToken, now)
}

func (s *FeishuAuthService) Logout(ctx context.Context, sessionToken string) error {
	if sessionToken == "" {
		return nil
	}
	return s.repository.DeleteFeishuAuthSession(ctx, sessionTokenHash(sessionToken))
}

func (s *FeishuAuthService) syncConsoleUser(ctx context.Context, user domain.FeishuUser) (string, error) {
	id := domain.ConsoleUserIDFromFeishu(user)
	if id == "" {
		return "", domain.ErrFeishuAuthorization
	}
	if err := s.repository.UpsertUser(ctx, domain.UserInput{
		ID:            id,
		Name:          firstNonEmpty(user.Name, user.EnName, id),
		Email:         firstNonEmpty(user.EnterpriseEmail, user.Email),
		AvatarURL:     firstNonEmpty(user.AvatarURL, user.AvatarMiddle, user.AvatarThumb, user.AvatarBig),
		DepartmentIDs: []string{},
		FeishuUnionID: user.UnionID,
		FeishuOpenID:  user.OpenID,
		FeishuUserID:  user.UserID,
	}); err != nil {
		return "", err
	}
	if err := s.repository.BootstrapFirstSuperAdmin(ctx, id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *FeishuAuthService) refreshSession(ctx context.Context, session domain.FeishuAuthSession, now time.Time) (domain.FeishuAuthSession, error) {
	if session.RefreshToken == "" || refreshTokenExpired(session, now) {
		_ = s.repository.DeleteFeishuAuthSession(ctx, session.SessionTokenHash)
		return domain.FeishuAuthSession{}, domain.ErrFeishuSessionExpired
	}

	token, err := s.client.RefreshAccessToken(ctx, session.RefreshToken)
	if err != nil {
		return domain.FeishuAuthSession{}, err
	}

	session.AccessToken = token.AccessToken
	session.AccessTokenExpiresAt = tokenExpiresAt(token.ExpiresIn, now)
	if token.RefreshToken != "" {
		session.RefreshToken = token.RefreshToken
	}
	if token.RefreshTokenExpiresIn > 0 {
		session.RefreshTokenExpiresAt = tokenExpiresAt(token.RefreshTokenExpiresIn, now)
	}
	session.TokenType = token.TokenType
	session.Scope = token.Scope

	if err := s.repository.UpdateFeishuAuthSessionToken(ctx, session); err != nil {
		return domain.FeishuAuthSession{}, err
	}

	return session, nil
}

func newAuthSession(sessionTokenHash string, unionID string, token domain.FeishuToken, now time.Time) domain.FeishuAuthSession {
	session := domain.FeishuAuthSession{
		SessionTokenHash:     sessionTokenHash,
		UnionID:              unionID,
		AccessToken:          token.AccessToken,
		AccessTokenExpiresAt: tokenExpiresAt(token.ExpiresIn, now),
		RefreshToken:         token.RefreshToken,
		TokenType:            token.TokenType,
		Scope:                token.Scope,
	}
	if token.RefreshTokenExpiresIn > 0 {
		session.RefreshTokenExpiresAt = tokenExpiresAt(token.RefreshTokenExpiresIn, now)
	}
	return session
}

func authResultFromSession(session domain.FeishuAuthSession, sessionToken string, now time.Time) domain.FeishuAuthResult {
	return domain.FeishuAuthResult{
		SessionExpiresAt: sessionExpiresAt(session),
		SessionToken:     sessionToken,
		Token: domain.FeishuToken{
			AccessToken:           session.AccessToken,
			ExpiresIn:             secondsUntil(session.AccessTokenExpiresAt, now),
			RefreshToken:          session.RefreshToken,
			RefreshTokenExpiresIn: secondsUntil(session.RefreshTokenExpiresAt, now),
			TokenType:             session.TokenType,
			Scope:                 session.Scope,
		},
		User: session.User,
	}
}

func (s *FeishuAuthService) authResultWithRoles(ctx context.Context, session domain.FeishuAuthSession, sessionToken string, now time.Time) (domain.FeishuAuthResult, error) {
	result := authResultFromSession(session, sessionToken, now)
	userID := domain.ConsoleUserIDFromFeishu(session.User)
	roles, err := s.repository.ListGlobalRoles(ctx, userID)
	if err != nil {
		return domain.FeishuAuthResult{}, err
	}
	result.Roles = roles
	result.Capabilities = domain.GlobalCapabilitiesForRoles(roles)
	return result, nil
}

func refreshTokenExpired(session domain.FeishuAuthSession, now time.Time) bool {
	return session.RefreshTokenExpiresAt.IsZero() || !now.Before(session.RefreshTokenExpiresAt)
}

func sessionExpiresAt(session domain.FeishuAuthSession) time.Time {
	if session.RefreshToken != "" && !session.RefreshTokenExpiresAt.IsZero() {
		return session.RefreshTokenExpiresAt
	}
	return session.AccessTokenExpiresAt
}

func tokenExpiresAt(expiresIn int, now time.Time) time.Time {
	if expiresIn <= 0 {
		return now
	}
	return now.Add(time.Duration(expiresIn) * time.Second)
}

func secondsUntil(value time.Time, now time.Time) int {
	if value.IsZero() {
		return 0
	}
	seconds := int(time.Until(value).Seconds())
	if !now.IsZero() {
		seconds = int(value.Sub(now).Seconds())
	}
	if seconds < 0 {
		return 0
	}
	return seconds
}

func randomSessionToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func sessionTokenHash(sessionToken string) string {
	sum := sha256.Sum256([]byte(sessionToken))
	return hex.EncodeToString(sum[:])
}
