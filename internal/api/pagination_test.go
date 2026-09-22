package api

import (
	"math"
	"testing"
)

func TestPaginationAlwaysBoundsDatabasePages(test *testing.T) {
	for _, fixture := range []struct {
		name       string
		pagination *Pagination
		pageSize   int
	}{
		{name: "omitted pagination", pageSize: MaximumPageSize},
		{name: "omitted size", pagination: &Pagination{}, pageSize: MaximumPageSize},
		{name: "zero size", pagination: &Pagination{First: new(uint64(0))}, pageSize: MaximumPageSize},
		{name: "small page", pagination: &Pagination{First: new(uint64(12))}, pageSize: 12},
		{name: "oversized page", pagination: &Pagination{First: new(uint64(MaximumPageSize + 1))}, pageSize: MaximumPageSize},
		{name: "integer overflow", pagination: &Pagination{First: new(uint64(math.MaxUint64))}, pageSize: MaximumPageSize},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			if pageSize := fixture.pagination.Limit(); pageSize != fixture.pageSize {
				test.Fatalf("page size = %d, want %d", pageSize, fixture.pageSize)
			}
			if options := fixture.pagination.Options(); options.Limit != uint64(fixture.pageSize) {
				test.Fatalf("database limit = %d, want %d", options.Limit, fixture.pageSize)
			}
		})
	}
}

func TestPaginationKeepsPositionWhenCappingPage(test *testing.T) {
	pagination := &Pagination{First: new(uint64(0)), Offset: new(uint64(17)), After: new("synthetic-cursor")}
	options := pagination.Options()
	if options.Limit != MaximumPageSize || options.Offset != 17 || options.Cursor != "synthetic-cursor" {
		test.Fatalf("pagination lost its bound or position: %#v", options)
	}
}
