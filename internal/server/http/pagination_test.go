package httpserver

import (
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestRequestPagination(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		page     int
		pageSize int
		ok       bool
	}{
		{name: "defaults", page: 1, pageSize: 20, ok: true},
		{name: "explicit", query: "?page=3&pageSize=50", page: 3, pageSize: 50, ok: true},
		{name: "invalid page", query: "?page=0", ok: false},
		{name: "invalid page size", query: "?pageSize=101", ok: false},
		{name: "non numeric", query: "?page=next", ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := echo.New()
			request := httptest.NewRequest("GET", "/items"+test.query, nil)
			pagination, ok := requestPagination(e.NewContext(request, httptest.NewRecorder()))
			if ok != test.ok {
				t.Fatalf("expected ok=%v, got %v", test.ok, ok)
			}
			if ok && (pagination.Page != test.page || pagination.PageSize != test.pageSize) {
				t.Fatalf("unexpected pagination: %+v", pagination)
			}
		})
	}
}
