package domain

type Pagination struct {
	Page     int
	PageSize int
}

func (p Pagination) IsValid() bool {
	return (p.Page == 0 && p.PageSize == 0) || (p.Page > 0 && p.PageSize > 0)
}

func (p Pagination) Limit() int {
	if p.Page <= 0 || p.PageSize <= 0 {
		return 0
	}
	return p.PageSize
}

func (p Pagination) Offset() int {
	if p.Page <= 0 || p.PageSize <= 0 {
		return 0
	}
	return (p.Page - 1) * p.PageSize
}
