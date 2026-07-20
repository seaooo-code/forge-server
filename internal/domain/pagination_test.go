package domain

import "testing"

func TestPaginationLimitAndOffset(t *testing.T) {
	pagination := Pagination{Page: 3, PageSize: 20}
	if pagination.Limit() != 20 || pagination.Offset() != 40 {
		t.Fatalf("unexpected pagination window: limit=%d offset=%d", pagination.Limit(), pagination.Offset())
	}
	if !(Pagination{}).IsValid() || (Pagination{Page: 1}).IsValid() {
		t.Fatal("expected only unbounded or complete pagination values to be valid")
	}
}
