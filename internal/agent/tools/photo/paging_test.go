package photo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
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

func TestSearchPlacesAsksForTheCitiesNearAPoint(test *testing.T) {
	current := newRun(test, map[string]string{"SearchPhotoPlaces": `[{"placeName":"Harborview, Exampleland","latitude":10.5,"longitude":20.25,"distanceKilometers":3.14159}]`})
	result, err := current.call(map[string]any{"operation": "search-places", "query": "harbor", "near_latitude": 10.0, "near_longitude": 20.0})
	if err != nil || !strings.Contains(result.Content, `"place_name":"Harborview, Exampleland"`) || !strings.Contains(result.Content, `"distance_kilometers":3.1`) {
		test.Fatalf("%v %v", result, err)
	}
	asked := current.operations.asked("SearchPhotoPlaces")
	if len(asked) != 1 || asked[0]["query"] != "harbor" || asked[0]["nearLatitude"] != 10.0 || asked[0]["nearLongitude"] != 20.0 || asked[0]["limitCount"] != 10 {
		test.Fatalf("%v", asked)
	}
	for name, arguments := range map[string]map[string]any{
		"no words":     {"operation": "search-places"},
		"half a point": {"operation": "search-places", "query": "harbor", "near_latitude": 10.0},
		"blank words":  {"operation": "search-places", "query": "   "},
		"other half":   {"operation": "search-places", "query": "harbor", "near_longitude": 20.0},
	} {
		if _, err := current.call(arguments); err == nil {
			test.Fatalf("%s was accepted", name)
		}
	}
	if len(current.operations.asked("SearchPhotoPlaces")) != 1 {
		test.Fatal("a search that was refused was sent")
	}
}

func TestSetLocationUsesTheBestMatchOrAPositionAndNeverBoth(test *testing.T) {
	current := newRun(test, map[string]string{
		"SearchPhotoPlaces": `[{"placeName":"Harborview, Exampleland","latitude":10.5,"longitude":20.25}]`,
		"SetPhotoLocation":  `2`,
	})
	// The server names the photos after the larger city near the one matched.
	current.operations.photos = map[string]string{"photo-one": `{"photo":{"id":"photo-one","latitude":10.5,"longitude":20.25,"placeName":"Harbor City, Exampleland"},"photoFiles":[],"photoPreviews":[]}`}
	// The card says the city the words matched, not the words.
	encoded, _ := json.Marshal(map[string]any{"operation": "set-location", "photo_ids": []string{"photo-one", "photo-two"}, "place": "harbor"})
	if line := cardPreview(tools.WithRun(context.Background(), current), encoded); line != "Set where 2 photos were taken to Harborview, Exampleland (10.5000, 20.2500)" {
		test.Fatalf("the card says %q", line)
	}
	result, err := current.call(map[string]any{"operation": "set-location", "photo_ids": []string{"photo-one", "photo-two", "photo-three"}, "place": "harborview"})
	if err != nil || !strings.Contains(result.Content, "2 of 3 photos were set to Harbor City, Exampleland") {
		test.Fatalf("%v %v", result, err)
	}
	asked := current.operations.asked("SetPhotoLocation")
	if len(asked) != 1 || asked[0]["latitude"] != 10.5 || asked[0]["longitude"] != 20.25 || len(asked[0]["photoIds"].([]string)) != 3 {
		test.Fatalf("%v", asked)
	}
	if searched := current.operations.asked("SearchPhotoPlaces"); len(searched) != 2 || searched[1]["query"] != "harborview" || searched[1]["limitCount"] != 1 {
		test.Fatalf("%v", searched)
	}

	// A position is looked up for its name, then set.
	result, err = current.call(map[string]any{"operation": "set-location", "photo_id": "photo-one", "latitude": 10.5, "longitude": 20.25})
	if err != nil || !strings.Contains(result.Content, "were set to Harbor City, Exampleland") {
		test.Fatalf("%v %v", result, err)
	}
	if searched := current.operations.asked("SearchPhotoPlaces"); len(searched) != 3 || searched[2]["query"] != "10.5, 20.25" {
		test.Fatalf("%v", searched)
	}

	sent := len(current.operations.asked("SetPhotoLocation"))
	for name, arguments := range map[string]map[string]any{
		"no photos":       {"operation": "set-location", "place": "harborview"},
		"neither":         {"operation": "set-location", "photo_id": "photo-one"},
		"both":            {"operation": "set-location", "photo_id": "photo-one", "place": "harborview", "latitude": 10.5, "longitude": 20.25},
		"half a position": {"operation": "set-location", "photo_id": "photo-one", "latitude": 10.5},
		"too many photos": {"operation": "set-location", "photo_ids": make([]string, 101), "place": "harborview"},
	} {
		if _, err := current.call(arguments); err == nil {
			test.Fatalf("%s was accepted", name)
		}
	}
	if len(current.operations.asked("SetPhotoLocation")) != sent {
		test.Fatal("a location that was refused was sent")
	}

	// Words no city matches set nothing.
	none := newRun(test, map[string]string{"SearchPhotoPlaces": `[]`, "SetPhotoLocation": `1`})
	if _, err := none.call(map[string]any{"operation": "set-location", "photo_id": "photo-one", "place": "atlantis"}); err == nil || !strings.Contains(err.Error(), "no city matches") {
		test.Fatalf("%v", err)
	}
	if len(none.operations.asked("SetPhotoLocation")) != 0 {
		test.Fatal("a place that matched nothing was sent")
	}
}

func TestRemoveLocationNamesPhotosAndSendsThem(test *testing.T) {
	current := newRun(test, map[string]string{"RemovePhotoLocation": `1`})
	result, err := current.call(map[string]any{"operation": "remove-location", "photo_ids": []string{"photo-one", "photo-two"}})
	if err != nil || !strings.Contains(result.Content, "1 of 2 photos had their location removed") {
		test.Fatalf("%v %v", result, err)
	}
	if _, err := current.call(map[string]any{"operation": "remove-location"}); err == nil {
		test.Fatal("no photos were accepted")
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
