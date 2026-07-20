package httpserver

import (
	"strconv"
	"strings"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

type paginationResponse struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
}

func requestPagination(c echo.Context) (domain.Pagination, bool) {
	page, ok := positiveQueryInt(c.QueryParam("page"), defaultPage)
	if !ok {
		return domain.Pagination{}, false
	}
	pageSize, ok := positiveQueryInt(c.QueryParam("pageSize"), defaultPageSize)
	if !ok || pageSize > maxPageSize {
		return domain.Pagination{}, false
	}
	return domain.Pagination{Page: page, PageSize: pageSize}, true
}

func positiveQueryInt(raw string, fallback int) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil && value > 0
}

func newPaginationResponse(pagination domain.Pagination, total int) paginationResponse {
	return paginationResponse{Page: pagination.Page, PageSize: pagination.PageSize, Total: total}
}
