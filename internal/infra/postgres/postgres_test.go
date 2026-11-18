package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestResolvePasswordTrimsOutput(t *testing.T) {
	password, err := resolvePassword(context.Background(), []string{"echo", "s3cret"})
	if err != nil {
		t.Fatalf("resolve password: %v", err)
	}
	if password != "s3cret" {
		t.Fatalf("expected trimmed password, got %q", password)
	}
}

func TestResolvePasswordRejectsEmptyOutput(t *testing.T) {
	if _, err := resolvePassword(context.Background(), []string{"true"}); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestResolvePasswordReportsFailure(t *testing.T) {
	_, err := resolvePassword(context.Background(), []string{"sh", "-c", "echo boom >&2; exit 3"})
	if err == nil {
		t.Fatal("expected error for failing command")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

func TestResolvePasswordReportsMissingCommand(t *testing.T) {
	if _, err := resolvePassword(context.Background(), []string{"forge-no-such-command"}); err == nil {
		t.Fatal("expected error for missing command")
	}
}
