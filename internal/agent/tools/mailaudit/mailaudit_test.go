package mailaudit

import "testing"

func TestPageRowsSaysWhereTheNextPageStarts(test *testing.T) {
	rows := []any{1, 2, 3}
	kept, more := pageRows(rows, 2, 4)
	if len(kept) != 2 || more["has_more"] != true || more["next_offset"] != 6 {
		test.Fatalf("kept %v, more %v", kept, more)
	}
	kept, more = pageRows(rows[:2], 2, 4)
	if len(kept) != 2 || more != nil {
		test.Fatalf("a last page said there was more: %v", more)
	}
	if page := pageOf(20, 0); page["first"] != 21 || page["offset"] != nil {
		test.Fatalf("page = %v", page)
	}
}
