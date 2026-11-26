package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestProtectedAPIRouteRejectsMissingSession(t *testing.T) {
	e := New(config.Config{}, Dependencies{FeishuAuthService: &fakeFeishuAuthService{}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/enums", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestProtectedAPIRouteAcceptsSession(t *testing.T) {
	e := New(config.Config{}, Dependencies{FeishuAuthService: &fakeFeishuAuthService{result: domain.ConsoleSession{
		ExpiresAt:    time.Now().Add(time.Hour),
		SessionToken: "session-token",
		User:         domain.User{ID: "user-1"},
	}}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/enums", nil)
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestProtectedAPIRouteTreatsAuthorizationFailureAsUnauthorized(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		FeishuAuthService: &fakeFeishuAuthService{sessionErr: domain.ErrFeishuAuthorization},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/enums", nil)
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
	assertClearedSessionCookie(t, rec)
}
