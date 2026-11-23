package postgres

import (
	"testing"

	"github.com/google/uuid"
)

func TestBuildReleaseAndRouteIDsUseUUID(t *testing.T) {
	if _, err := uuid.Parse(newBuildRecordID()); err != nil {
		t.Fatalf("expected build record id to be UUID: %v", err)
	}
	if _, err := uuid.Parse(newReleaseID()); err != nil {
		t.Fatalf("expected release record id to be UUID: %v", err)
	}
	// 路由 ID 曾按秒生成，同一秒内的两次发布会主键冲突。
	if first, second := newReleaseRouteID(), newReleaseRouteID(); first == second {
		t.Fatalf("expected unique release route ids, got %q twice", first)
	} else if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("expected release route id to be UUID: %v", err)
	}
}
