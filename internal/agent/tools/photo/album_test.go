package photo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
)

const (
	albumRowJson      = `{"id":"album-one","albumTitle":"Picnic","photoCount":3,"firstTakenAt":"2025-07-12T09:00:00Z","lastTakenAt":"2025-07-14T16:00:00Z","createdAt":"2025-07-15T08:30:00Z"}`
	emptyAlbumRowJson = `{"id":"album-two","albumTitle":"Nothing yet","photoCount":0}`
)

func TestAlbumListAndReadGiveTitlesCountsAndDaysInThePersonsZone(test *testing.T) {
	current := newRun(test, map[string]string{
		"ListPhotoAlbums": `[` + albumRowJson + `,` + emptyAlbumRowJson + `]`,
		"ReadPhotoAlbum":  albumRowJson,
	})
	result, err := current.call(map[string]any{"operation": "album-list"})
	if err != nil {
		test.Fatal(err)
	}
	if !result.Untrusted {
		test.Fatal("an album's title is somebody's words")
	}
	var listed struct {
		Albums []struct {
			AlbumID      string `json:"album_id"`
			AlbumTitle   string `json:"album_title"`
			PhotoCount   int    `json:"photo_count"`
			FirstTakenOn string `json:"first_taken_on"`
			LastTakenOn  string `json:"last_taken_on"`
		} `json:"albums"`
	}
	if err := json.Unmarshal([]byte(result.Content), &listed); err != nil {
		test.Fatal(err)
	}
	if len(listed.Albums) != 2 || listed.Albums[0].AlbumID != "album-one" || listed.Albums[0].AlbumTitle != "Picnic" || listed.Albums[0].PhotoCount != 3 ||
		listed.Albums[0].FirstTakenOn != "2025-07-12" || listed.Albums[0].LastTakenOn != "2025-07-14" || listed.Albums[1].FirstTakenOn != "" {
		test.Fatalf("%+v", listed.Albums)
	}

	result, err = current.call(map[string]any{"operation": "album-read", "album_id": "album-one"})
	if err != nil || !result.Untrusted || !strings.Contains(result.Content, `"album_title":"Picnic"`) || !strings.Contains(result.Content, `"created_at":"2025-07-15 08:30"`) || !strings.Contains(result.Content, "list with album_id") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := current.operations.asked("ReadPhotoAlbum"); len(asked) != 1 || asked[0]["photoAlbumId"] != "album-one" {
		test.Fatalf("%v", asked)
	}
	if _, err := current.call(map[string]any{"operation": "album-read"}); err == nil {
		test.Fatal("an album was read without saying which")
	}
}

func TestAlbumCreateNamesTheTitleAndThePhotos(test *testing.T) {
	current := newRun(test, map[string]string{"CreatePhotoAlbum": `{"id":"album-new","albumTitle":"Picnic","photoCount":2}`})
	result, err := current.call(map[string]any{"operation": "album-create", "album_title": "  Picnic ", "photo_ids": []string{"photo-one", "photo-two", "photo-one"}})
	if err != nil || !strings.Contains(result.Content, `"album_id":"album-new"`) || !strings.Contains(result.Content, `"photo_count":2`) {
		test.Fatalf("%v %v", result, err)
	}
	asked := current.operations.asked("CreatePhotoAlbum")
	if len(asked) != 1 || asked[0]["albumTitle"] != "Picnic" || len(asked[0]["photoIds"].([]string)) != 2 {
		test.Fatalf("%v", asked)
	}
	// An empty album is allowed; one with no title is not.
	if _, err := current.call(map[string]any{"operation": "album-create", "album_title": "Empty"}); err != nil {
		test.Fatal(err)
	}
	if asked := current.operations.asked("CreatePhotoAlbum"); asked[1]["photoIds"] != nil {
		test.Fatalf("%v", asked[1])
	}
	if _, err := current.call(map[string]any{"operation": "album-create", "photo_ids": []string{"photo-one"}}); err == nil {
		test.Fatal("an album with no title was made")
	}
	tooMany := make([]string, 501)
	for index := range tooMany {
		tooMany[index] = "photo-" + string(rune('a'+index%26)) + string(rune('a'+index/26))
	}
	if _, err := current.call(map[string]any{"operation": "album-create", "album_title": "Everything", "photo_ids": tooMany}); err == nil {
		test.Fatal("501 photos were put in one album at once")
	}
}

func TestAlbumRenameAddRemoveAndDeleteUseTheSharedOperations(test *testing.T) {
	current := newRun(test, map[string]string{
		"RenamePhotoAlbum":      `{"id":"album-one","albumTitle":"Picnic at the lake"}`,
		"AddPhotosToAlbum":      `2`,
		"RemovePhotosFromAlbum": `1`,
		"DeletePhotoAlbum":      `true`,
	})
	operations := current.operations
	if result, err := current.call(map[string]any{"operation": "album-rename", "album_id": "album-one", "album_title": "Picnic at the lake"}); err != nil || !strings.Contains(result.Content, "album-one is now called Picnic at the lake") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("RenamePhotoAlbum"); len(asked) != 1 || asked[0]["photoAlbumId"] != "album-one" || asked[0]["albumTitle"] != "Picnic at the lake" {
		test.Fatalf("%v", asked)
	}
	if result, err := current.call(map[string]any{"operation": "album-add", "album_id": "album-one", "photo_ids": []string{"photo-one", "photo-two", "photo-three"}}); err != nil || !strings.Contains(result.Content, "2 of 3 photos added to the album") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("AddPhotosToAlbum"); len(asked) != 1 || asked[0]["photoAlbumId"] != "album-one" || len(asked[0]["photoIds"].([]string)) != 3 {
		test.Fatalf("%v", asked)
	}
	if result, err := current.call(map[string]any{"operation": "album-remove", "album_id": "album-one", "photo_ids": []string{"photo-one"}}); err != nil || !strings.Contains(result.Content, "1 of 1 photos taken out of the album") {
		test.Fatalf("%v %v", result, err)
	}
	if result, err := current.call(map[string]any{"operation": "album-delete", "album_id": "album-one"}); err != nil || !strings.Contains(result.Content, "its photos are still in the library") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("DeletePhotoAlbum"); len(asked) != 1 || asked[0]["photoAlbumId"] != "album-one" {
		test.Fatalf("%v", asked)
	}
	for _, arguments := range []map[string]any{
		{"operation": "album-rename", "album_title": "No album"}, {"operation": "album-rename", "album_id": "album-one"},
		{"operation": "album-add", "photo_ids": []string{"photo-one"}}, {"operation": "album-add", "album_id": "album-one"},
		{"operation": "album-remove", "album_id": "album-one"}, {"operation": "album-delete"},
	} {
		if _, err := current.call(arguments); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
	if len(operations.asked("RenamePhotoAlbum")) != 1 || len(operations.asked("DeletePhotoAlbum")) != 1 {
		test.Fatal("something was sent for a call that was refused")
	}
}

func TestAlbumSuggestGivesDatesThatListAcceptsAndSaysNothingIsMade(test *testing.T) {
	current := newRun(test, map[string]string{"SuggestPhotoAlbums": `[{"takenAfter":"2031-07-12T09:00:00Z","takenBefore":"2031-07-14T16:00:01Z","placeNames":["Exampleton, Exampleland"],"albumTitle":"Exampleton, Exampleland","photoCount":40,"samplePhotoIds":["photo-one","photo-two"]}]`})
	result, err := current.call(map[string]any{"operation": "album-suggest", "taken_after": "2031-01-01", "taken_before": "2032-01-01"})
	if err != nil {
		test.Fatal(err)
	}
	var answer struct {
		Suggestions []struct {
			AlbumTitle     string   `json:"album_title"`
			PhotoCount     int      `json:"photo_count"`
			PlaceNames     []string `json:"place_names"`
			TakenAfter     string   `json:"taken_after"`
			TakenBefore    string   `json:"taken_before"`
			SamplePhotoIDs []string `json:"sample_photo_ids"`
		} `json:"suggestions"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		test.Fatal(err)
	}
	if len(answer.Suggestions) != 1 || answer.Suggestions[0].PhotoCount != 40 || answer.Suggestions[0].TakenAfter != "2031-07-12T09:00:00Z" ||
		answer.Suggestions[0].TakenBefore != "2031-07-14T16:00:01Z" || len(answer.Suggestions[0].SamplePhotoIDs) != 2 || !strings.Contains(answer.Note, "nothing is created yet") {
		test.Fatalf("%s", result.Content)
	}
	asked := current.operations.asked("SuggestPhotoAlbums")
	if len(asked) != 1 || asked[0]["takenAfter"] != "2031-01-01T00:00:00Z" || asked[0]["takenBefore"] != "2032-01-01T00:00:00Z" {
		test.Fatalf("%v", asked)
	}
	// The dates it gives are accepted by list as they stand.
	if _, err := current.call(map[string]any{"operation": "list", "taken_after": answer.Suggestions[0].TakenAfter, "taken_before": answer.Suggestions[0].TakenBefore}); err != nil {
		test.Fatal(err)
	}
	if asked := current.operations.asked("ListPhotos"); len(asked) != 1 || asked[0]["takenAfter"] != "2031-07-12T09:00:00Z" || asked[0]["takenBefore"] != "2031-07-14T16:00:01Z" {
		test.Fatalf("%v", asked)
	}
	current.operations.answers["SuggestPhotoAlbums"] = `[]`
	if result, err := current.call(map[string]any{"operation": "album-suggest"}); err != nil || !strings.Contains(result.Content, `"suggestions":[]`) || !strings.Contains(result.Content, "a run of at least 12 photos") {
		test.Fatalf("%v %v", result, err)
	}
	if _, err := current.call(map[string]any{"operation": "album-suggest", "taken_after": "last blue moon"}); err == nil {
		test.Fatal("a date that is not one was accepted")
	}
}

func TestListTakesAnAlbumAndReadSaysWhichAlbumsAPhotoIsIn(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotos": `[` + pictureRow + `]`})
	if _, err := current.call(map[string]any{"operation": "list", "album_id": " album-one "}); err != nil {
		test.Fatal(err)
	}
	if asked := current.operations.asked("ListPhotos"); len(asked) != 1 || asked[0]["albumId"] != "album-one" {
		test.Fatalf("%v", asked)
	}
	current.operations = &fakeOperations{photos: map[string]string{
		"photo-one": `{"photo":` + pictureRow + `,"photoFiles":[],"photoPreviews":[],"albums":[{"id":"album-one","albumTitle":"Picnic"}]}`,
		"photo-two": `{"photo":` + videoRow + `,"photoFiles":[],"photoPreviews":[]}`,
	}}
	result, err := current.call(map[string]any{"operation": "read", "photo_id": "photo-one"})
	if err != nil || !strings.Contains(result.Content, `"albums":[{"album_id":"album-one","album_title":"Picnic"}]`) {
		test.Fatalf("%v %v", result, err)
	}
	if result, err := current.call(map[string]any{"operation": "read", "photo_id": "photo-two"}); err != nil || !strings.Contains(result.Content, `"albums":[]`) {
		test.Fatalf("a photo in no album should say so: %v %v", result, err)
	}
}

func TestTheCardForDeletingAnAlbumNamesIt(test *testing.T) {
	photoTool := tools.Build().Get("photo")
	if photoTool == nil {
		test.Fatal("the photo tool is not registered")
	}
	current := newRun(test, map[string]string{"ReadPhotoAlbum": albumRowJson})
	arguments, _ := json.Marshal(map[string]any{"operation": "album-delete", "album_id": "album-one"})
	line := photoTool.PreviewLine(tools.WithRun(test.Context(), current), arguments)
	if line != `Delete "Picnic"; its photos stay in the library` {
		test.Fatalf("the card says %q", line)
	}
	// Without a run to look it up in, or for another operation, it still says
	// something a person can read.
	if line := photoTool.PreviewLine(test.Context(), arguments); line != "Delete an album; its photos stay in the library" {
		test.Fatalf("the card says %q", line)
	}
	other, _ := json.Marshal(map[string]any{"operation": "album-add", "album_id": "album-one", "photo_ids": []string{"a", "b"}})
	if line := photoTool.PreviewLine(tools.WithRun(test.Context(), current), other); line != "Put 2 photos in an album" {
		test.Fatalf("the card says %q", line)
	}
	create, _ := json.Marshal(map[string]any{"operation": "album-create", "album_title": "Picnic", "photo_ids": []string{"a"}})
	if line := photoTool.PreviewLine(test.Context(), create); line != `Make an album called "Picnic" with 1 photo in it` {
		test.Fatalf("the card says %q", line)
	}
}

func TestAnAlbumCoverIsChosenFromItsPhotosWithTheSharedOperation(test *testing.T) {
	current := newRun(test, map[string]string{"SetPhotoAlbumCover": `{"id":"album-one","albumTitle":"Picnic at the lake"}`})
	if result, err := current.call(map[string]any{"operation": "album-cover", "album_id": " album-one ", "photo_id": "photo-two"}); err != nil || !strings.Contains(result.Content, "album-one now shows photo photo-two as its cover") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := current.operations.asked("SetPhotoAlbumCover"); len(asked) != 1 || asked[0]["photoAlbumId"] != "album-one" || asked[0]["photoId"] != "photo-two" {
		test.Fatalf("%v", asked)
	}
	for _, arguments := range []map[string]any{
		{"operation": "album-cover", "photo_id": "photo-two"}, {"operation": "album-cover", "album_id": "album-one"},
	} {
		if _, err := current.call(arguments); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
	if len(current.operations.asked("SetPhotoAlbumCover")) != 1 {
		test.Fatal("a refused call was sent")
	}
}
