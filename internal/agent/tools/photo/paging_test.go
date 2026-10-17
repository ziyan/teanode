package photo

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// rowsOf is count JSON rows made by build, joined as a list.
func rowsOf(count int, build func(index int) string) string {
	rows := []string{}
	for index := range count {
		rows = append(rows, build(index))
	}
	return "[" + strings.Join(rows, ",") + "]"
}

func photoRows(count int) string {
	return rowsOf(count, func(index int) string {
		return fmt.Sprintf(`{"id":"photo-%d","photoKind":"picture","photoProcessingState":"ready","largePreviewUrl":"/x"}`, index)
	})
}

type pagedAnswer struct {
	NextOffset         *int   `json:"next_offset"`
	More               string `json:"more"`
	TotalCount         int    `json:"total_count"`
	MatchingPhotoCount int    `json:"matching_photo_count"`
	Photos             []any  `json:"photos"`
	Albums             []any  `json:"albums"`
	People             []any  `json:"people"`
	Shares             []any  `json:"shares"`
}

func (self *fakeRun) page(test *testing.T, arguments map[string]any) *pagedAnswer {
	test.Helper()
	result, err := self.call(arguments)
	if err != nil {
		test.Fatalf("%v: %v", arguments, err)
	}
	var answer pagedAnswer
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		test.Fatal(err)
	}
	return &answer
}

func nextOffsetOf(answer *pagedAnswer) int {
	if answer.NextOffset == nil {
		return -1
	}
	return *answer.NextOffset
}

// A list of photos is read a page at a time: offset says where the page starts,
// and a page with more behind it says where the next one does.
func TestListPagesWithOffsetAndNextOffset(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotos": photoRows(3), "CountPhotos": "8"})
	first := current.page(test, map[string]any{"operation": "list", "page_size": 3})
	if len(first.Photos) != 3 || nextOffsetOf(first) != 3 || first.MatchingPhotoCount != 8 || !strings.Contains(first.More, "offset 3") {
		test.Fatalf("%+v", first)
	}
	if asked := current.operations.asked("ListPhotos"); asked[0]["offset"] != nil {
		test.Fatalf("the first page skipped %v", asked[0]["offset"])
	}
	second := current.page(test, map[string]any{"operation": "list", "page_size": 3, "offset": 3})
	if nextOffsetOf(second) != 6 {
		test.Fatalf("%+v", second)
	}
	if asked := current.operations.asked("ListPhotos"); asked[1]["offset"] != 3 {
		test.Fatalf("the second page asked %v", asked[1])
	}
	// The count is of the whole listing, so it is asked without the page.
	if counted := current.operations.asked("CountPhotos"); len(counted) != 2 || counted[1]["offset"] != nil || counted[1]["pageSize"] != nil {
		test.Fatalf("counted %v", counted)
	}
	// The last page ends it, whether the count says so or the page is short.
	last := current.page(test, map[string]any{"operation": "list", "page_size": 3, "offset": 5})
	if last.NextOffset != nil || last.More != "" {
		test.Fatalf("the last page has a next: %+v", last)
	}
	short := current.page(test, map[string]any{"operation": "list", "page_size": 5})
	if short.NextOffset != nil || short.MatchingPhotoCount != 0 {
		test.Fatalf("a short page has a next, or was counted: %+v", short)
	}
	for _, arguments := range []map[string]any{{"operation": "list", "offset": -1}, {"operation": "list", "offset": 100001}} {
		if _, err := current.call(arguments); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
}

// Past the offset the server reads to, the answer says to narrow the question
// and does not name an offset that would be refused.
func TestListSaysToNarrowPastTheOffsetTheServerReadsTo(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotos": photoRows(3), "CountPhotos": "500000"})
	answer := current.page(test, map[string]any{"operation": "list", "page_size": 3, "offset": 99999})
	if answer.NextOffset != nil || !strings.Contains(answer.More, "narrow") {
		test.Fatalf("%+v", answer)
	}
}

func TestAlbumListPeopleAndSharesPageWithOffsetAndNextOffset(test *testing.T) {
	albums := rowsOf(5, func(index int) string {
		return fmt.Sprintf(`{"id":"album-%d","albumTitle":"Album %d","photoCount":%d}`, index, index, index)
	})
	groups := rowsOf(5, func(index int) string { return fmt.Sprintf(`{"id":"group-%d","faceCount":4,"photoCount":3}`, index) })
	shares := rowsOf(5, func(index int) string {
		return fmt.Sprintf(`{"id":"share-%d","shareLevel":"view","granteeUsername":"person-%d"}`, index, index)
	})
	current := newRun(test, map[string]string{"ListPhotoAlbums": albums, "ListPhotoFaceGroups": groups, "ListPhotoAlbumShares": shares})
	for _, scenario := range []struct {
		operation string
		extra     map[string]any
		rows      func(*pagedAnswer) []any
	}{
		{"album-list", nil, func(answer *pagedAnswer) []any { return answer.Albums }},
		{"people", nil, func(answer *pagedAnswer) []any { return answer.People }},
		{"album-shares", map[string]any{"album_id": "album-own"}, func(answer *pagedAnswer) []any { return answer.Shares }},
	} {
		ask := func(offset, pageSize int) *pagedAnswer {
			arguments := map[string]any{"operation": scenario.operation, "page_size": pageSize, "offset": offset}
			for name, value := range scenario.extra {
				arguments[name] = value
			}
			return current.page(test, arguments)
		}
		first := ask(0, 2)
		if len(scenario.rows(first)) != 2 || first.TotalCount != 5 || nextOffsetOf(first) != 2 || !strings.Contains(first.More, "offset 2") {
			test.Fatalf("%s first page: %+v", scenario.operation, first)
		}
		last := ask(4, 2)
		if len(scenario.rows(last)) != 1 || last.TotalCount != 5 || last.NextOffset != nil {
			test.Fatalf("%s last page: %+v", scenario.operation, last)
		}
		past := ask(9, 2)
		if len(scenario.rows(past)) != 0 || past.NextOffset != nil {
			test.Fatalf("%s past the end: %+v", scenario.operation, past)
		}
		// Everything fits in the default page, and nothing is cut off.
		all := current.page(test, mergeArguments(map[string]any{"operation": scenario.operation}, scenario.extra))
		if len(scenario.rows(all)) != 5 || all.NextOffset != nil {
			test.Fatalf("%s default page: %+v", scenario.operation, all)
		}
		if _, err := current.call(mergeArguments(map[string]any{"operation": scenario.operation, "page_size": 101}, scenario.extra)); err == nil {
			test.Fatalf("%s accepted a page of 101", scenario.operation)
		}
		if _, err := current.call(mergeArguments(map[string]any{"operation": scenario.operation, "offset": -2}, scenario.extra)); err == nil {
			test.Fatalf("%s accepted a negative offset", scenario.operation)
		}
	}
}

func mergeArguments(base, extra map[string]any) map[string]any {
	for name, value := range extra {
		base[name] = value
	}
	return base
}

// An album is read with its photos, a page at a time like any listing.
func TestAlbumReadPagesTheAlbumsPhotos(test *testing.T) {
	current := newRun(test, map[string]string{"ReadPhotoAlbum": albumRowJson, "ListPhotos": photoRows(2), "CountPhotos": "7"})
	first := current.page(test, map[string]any{"operation": "album-read", "album_id": "album-one", "page_size": 2})
	if len(first.Photos) != 2 || nextOffsetOf(first) != 2 || first.MatchingPhotoCount != 7 {
		test.Fatalf("%+v", first)
	}
	if asked := current.operations.asked("ListPhotos"); len(asked) != 1 || asked[0]["albumId"] != "album-one" || asked[0]["pageSize"] != 2 {
		test.Fatalf("the album's photos were asked as %v", asked)
	}
	last := current.page(test, map[string]any{"operation": "album-read", "album_id": "album-one", "page_size": 2, "offset": 5})
	if last.NextOffset != nil {
		test.Fatalf("%+v", last)
	}
	if asked := current.operations.asked("ListPhotos"); asked[1]["offset"] != 5 {
		test.Fatalf("%v", asked[1])
	}
}

// A person's photos are listed like any others, from an offset.
func TestPersonPhotosPageWithOffset(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotos": photoRows(3), "CountPhotos": "9"})
	answer := current.page(test, map[string]any{"operation": "person-photos", "face_group_id": "group-one", "page_size": 3, "offset": 3})
	if len(answer.Photos) != 3 || nextOffsetOf(answer) != 6 {
		test.Fatalf("%+v", answer)
	}
	if asked := current.operations.asked("ListPhotos"); asked[0]["faceGroupId"] != "group-one" || asked[0]["offset"] != 3 {
		test.Fatalf("%v", asked)
	}
}

func TestSetDateSendsTheTimeWithItsOffsetAndRefusesOneWithout(test *testing.T) {
	current := newRun(test, map[string]string{"UpdatePhoto": `{"id":"photo-one","photoKind":"picture","takenAt":"2025-07-12T05:30:00Z","takenAtOffsetMinutes":540}`})
	result, err := current.call(map[string]any{"operation": "set-date", "photo_id": "photo-one", "taken_at": "2025-07-12T14:30:00+09:00"})
	if err != nil || !strings.Contains(result.Content, "photo-one was taken 2025-07-12 14:30 (UTC+09:00)") {
		test.Fatalf("%v %v", result, err)
	}
	asked := current.operations.asked("UpdatePhoto")
	if len(asked) != 1 || asked[0]["photoId"] != "photo-one" || asked[0]["takenAt"] != "2025-07-12T14:30:00+09:00" || asked[0]["isFavorite"] != nil {
		test.Fatalf("%v", asked)
	}
	for name, arguments := range map[string]map[string]any{
		"no offset":  {"operation": "set-date", "photo_id": "photo-one", "taken_at": "2025-07-12T14:30:00"},
		"a day":      {"operation": "set-date", "photo_id": "photo-one", "taken_at": "2025-07-12"},
		"no time":    {"operation": "set-date", "photo_id": "photo-one"},
		"no photo":   {"operation": "set-date", "taken_at": "2025-07-12T14:30:00+09:00"},
		"not a time": {"operation": "set-date", "photo_id": "photo-one", "taken_at": "last Tuesday"},
	} {
		if _, err := current.call(arguments); err == nil {
			test.Fatalf("%s was accepted", name)
		}
	}
	if len(current.operations.asked("UpdatePhoto")) != 1 {
		test.Fatal("a date that was refused was sent")
	}
}
