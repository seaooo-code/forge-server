package postgres

import (
	"testing"

	"github.com/google/uuid"
)

func TestBuildAndReleaseRecordIDsUseUUID(t *testing.T) {
	if _, err := uuid.Parse(newBuildRecordID()); err != nil {
		t.Fatalf("expected build record id to be UUID: %v", err)
	}
	if _, err := uuid.Parse(newReleaseID()); err != nil {
		t.Fatalf("expected release record id to be UUID: %v", err)
	}
}
