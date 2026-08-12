package contracts

import "gorm.io/gorm"

// Pagination limits how much of a result set a query returns.
//
// Every list endpoint previously returned every row. That is fine for the
// hundred tasks a demo has and untenable for the hundred thousand a real
// deployment accumulates: the database serialises them all, the API holds them
// all in memory, and the browser downloads and renders them all. Virtualising
// the table would hide the symptom in the UI while the first three costs
// remain.
//
// Offset paging is used rather than a cursor because these lists are sorted by
// creation time, are browsed rather than streamed, and need a total so the
// caller can say "1–50 of 1,234". The tradeoff is the usual one: a row
// inserted while someone pages can shift the window. For an operational list
// that is acceptable; a cursor would be the answer if these were exported or
// consumed by a worker.
type Pagination struct {
	// Page is 1-based. Values below 1 are treated as 1.
	Page int
	// PageSize caps at MaxPageSize; 0 means DefaultPageSize.
	PageSize int
}

const (
	// DefaultPageSize is what a caller gets when it asks for nothing. Chosen to
	// fill a screen twice over without being worth virtualising.
	DefaultPageSize = 50

	// MaxPageSize bounds what a caller may request. Without it, `?pageSize=1000000`
	// reinstates exactly the problem paging exists to solve — and does so on
	// demand, from outside.
	MaxPageSize = 200
)

// Normalize clamps the values into their supported range. Always call it
// before using a Pagination that came from a request.
func (p Pagination) Normalize() Pagination {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize <= 0 {
		p.PageSize = DefaultPageSize
	}
	if p.PageSize > MaxPageSize {
		p.PageSize = MaxPageSize
	}
	return p
}

// Offset is the number of rows to skip for this page.
func (p Pagination) Offset() int {
	n := p.Normalize()
	return (n.Page - 1) * n.PageSize
}

// Apply adds LIMIT and OFFSET to a query.
func (p Pagination) Apply(db *gorm.DB) *gorm.DB {
	n := p.Normalize()
	return db.Limit(n.PageSize).Offset(n.Offset())
}

// Page is one slice of a larger result set, with enough context for a caller
// to render "showing 51–100 of 1,234" and decide whether to offer a next page.
type Page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// NewPage builds a Page, normalising the pagination it reports so the caller
// sees the values actually used rather than the ones it asked for.
func NewPage[T any](items []T, total int64, p Pagination) Page[T] {
	n := p.Normalize()
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, Total: total, Page: n.Page, PageSize: n.PageSize}
}

// HasMore reports whether another page exists after this one.
func (p Page[T]) HasMore() bool {
	return int64(p.Page)*int64(p.PageSize) < p.Total
}

// TotalPages is the number of pages the result set spans, at least 1.
func (p Page[T]) TotalPages() int {
	if p.PageSize <= 0 || p.Total <= 0 {
		return 1
	}
	pages := int((p.Total + int64(p.PageSize) - 1) / int64(p.PageSize))
	if pages < 1 {
		return 1
	}
	return pages
}
