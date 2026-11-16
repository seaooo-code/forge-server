package s3

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

func TestTesterReportsReadableReasons(t *testing.T) {
	status := http.StatusForbidden
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	defer server.Close()

	input := domain.StorageConfigurationInput{
		Region:          "us-east-1",
		Bucket:          "demo",
		Endpoint:        server.URL,
		AccessKeyID:     "AKTEST",
		SecretAccessKey: "secret-value",
	}

	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusForbidden, "HTTP 403"},
		{http.StatusNotFound, "HTTP 404"},
		{http.StatusBadRequest, "HTTP 400"},
	} {
		status = tc.status
		err := Tester{}.TestStorageConfiguration(context.Background(), input)
		var testErr *domain.StorageConfigurationTestError
		if !errors.As(err, &testErr) || !strings.Contains(testErr.Detail, tc.want) {
			t.Fatalf("status %d: expected detail containing %q, got %v", tc.status, tc.want, err)
		}
		if strings.Contains(testErr.Detail, "secret-value") || !errors.Is(err, domain.ErrStorageConfigurationTestFailed) {
			t.Fatalf("status %d: detail must not leak secrets and must wrap the sentinel, got %v", tc.status, err)
		}
	}

	input.Endpoint = "s3.example.com"
	err := Tester{}.TestStorageConfiguration(context.Background(), input)
	var testErr *domain.StorageConfigurationTestError
	if !errors.As(err, &testErr) || !strings.Contains(testErr.Detail, "带协议") {
		t.Fatalf("expected endpoint hint, got %v", err)
	}
}
