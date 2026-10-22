package photo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
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
	if err != nil || !result.Untrusted || !strings.Contains(result.Content, `"album_title":"Picnic"`) || !strings.Contains(result.Content, `"created_at":"2025-07-15 08:30"`) || !strings.Contains(result.Content, `"photos":[]`) {
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

const (
	ownAlbumRowJson      = `{"id":"album-own","ownerUserId":"user-alice","albumTitle":"Mine","photoCount":1,"isOwnedByCaller":true,"canCallerManage":true,"canCallerContribute":true}`
	sharedAlbumRowJson   = `{"id":"album-shared","ownerUserId":"user-ben","albumTitle":"Hers","photoCount":4,"ownerName":"Ben Example","shareLevel":"view"}`
	circleAlbumRowJson   = `{"id":"album-circle","circleId":"circle-home","albumTitle":"Ours","photoCount":9,"circleName":"Home","shareLevel":"contribute"}`
	locationAlbumRowJson = `{"id":"album-places","ownerUserId":"user-ben","albumTitle":"With places","photoCount":2,"ownerName":"Ben Example","shareLevel":"view","isLocationIncluded":true}`
)

func TestAlbumListMarksTheAlbumsThatAreNotThePersonsOwn(test *testing.T) {
	current := newRun(test, map[string]string{
		"ListPhotoAlbums": `[` + ownAlbumRowJson + `,` + sharedAlbumRowJson + `,` + circleAlbumRowJson + `,` + locationAlbumRowJson + `]`,
	})
	result, err := current.call(map[string]any{"operation": "album-list"})
	if err != nil || !result.Untrusted {
		test.Fatalf("%v %v", result, err)
	}
	var listed struct {
		Albums []map[string]any `json:"albums"`
		Note   string           `json:"note"`
	}
	if err := json.Unmarshal([]byte(result.Content), &listed); err != nil || len(listed.Albums) != 4 {
		test.Fatalf("%v %v", result.Content, err)
	}
	albums := map[string]map[string]any{}
	for _, album := range listed.Albums {
		albums[album["album_id"].(string)] = album
	}
	if own := albums["album-own"]; own["level"] != "owner" || own["owner_name"] != nil || own["circle_name"] != nil {
		test.Fatalf("the person's own: %v", own)
	}
	if shared := albums["album-shared"]; shared["level"] != "view" || shared["owner_name"] != "Ben Example" || shared["shows_where_photos_were_taken"] != nil {
		test.Fatalf("shared with the person: %v", shared)
	}
	if circle := albums["album-circle"]; circle["level"] != "contribute" || circle["circle_name"] != "Home" || circle["owner_name"] != nil {
		test.Fatalf("a circle's: %v", circle)
	}
	if places := albums["album-places"]; places["shows_where_photos_were_taken"] != true {
		test.Fatalf("shared with places: %v", places)
	}
	if !strings.Contains(listed.Note, "shared with the person too") {
		test.Fatalf("%s", listed.Note)
	}
}

func TestAlbumShareTakesOneWithAndSharesAtALevel(test *testing.T) {
	current := newRun(test, map[string]string{
		"ListCirclePeople": `[{"userId":"user-ben","username":"ben"},{"userId":"user-cora","username":"cora"}]`,
		"ListCircles":      `[{"id":"circle-home","name":"Home","members":[{"userId":"user-ben"},{"userId":"user-cora"},{"userId":"user-alice"}]}]`,
		"SharePhotoAlbum":  `{"id":"share-one","photoAlbumId":"album-own","shareLevel":"contribute","isLocationIncluded":true}`,
	})
	result, err := current.call(map[string]any{"operation": "album-share", "album_id": "album-own", "with": "Ben", "share_level": "contribute", "is_location_included": true})
	if err != nil || !strings.Contains(result.Content, `"share_id":"share-one"`) || !strings.Contains(result.Content, `"shared_with":"ben"`) {
		test.Fatalf("%v %v", result, err)
	}
	asked := current.operations.asked("SharePhotoAlbum")
	if len(asked) != 1 || asked[0]["photoAlbumId"] != "album-own" || asked[0]["granteeUserId"] != "user-ben" || asked[0]["granteeCircleId"] != nil ||
		asked[0]["shareLevel"] != "contribute" || asked[0]["isLocationIncluded"] != true {
		test.Fatalf("%v", asked)
	}
	// A circle by its name or its id, and the level when none is said.
	for _, with := range []string{"home", "circle-home"} {
		if result, err := current.call(map[string]any{"operation": "album-share", "album_id": "album-own", "with": with}); err != nil || !strings.Contains(result.Content, `the circle \"Home\" (3 people)`) {
			test.Fatalf("%v %v", result, err)
		}
	}
	asked = current.operations.asked("SharePhotoAlbum")
	if len(asked) != 3 || asked[1]["granteeCircleId"] != "circle-home" || asked[1]["granteeUserId"] != nil || asked[1]["shareLevel"] != "view" || asked[1]["isLocationIncluded"] != false || asked[2]["granteeCircleId"] != "circle-home" {
		test.Fatalf("%v", asked)
	}
	for name, arguments := range map[string]map[string]any{
		"no album":         {"operation": "album-share", "with": "ben"},
		"nobody":           {"operation": "album-share", "album_id": "album-own"},
		"the old person":   {"operation": "album-share", "album_id": "album-own", "person": "ben"},
		"the old circle":   {"operation": "album-share", "album_id": "album-own", "circle": "Home"},
		"somebody unknown": {"operation": "album-share", "album_id": "album-own", "with": "nobody"},
		"a circle unknown": {"operation": "album-share", "album_id": "album-own", "with": "Nowhere"},
		"a level not one":  {"operation": "album-share", "album_id": "album-own", "with": "ben", "share_level": "owner"},
	} {
		if _, err := current.call(arguments); err == nil {
			test.Errorf("sharing with %s was not refused", name)
		}
	}
	if asked := current.operations.asked("SharePhotoAlbum"); len(asked) != 3 {
		test.Fatalf("a share that was refused reached the server: %v", asked)
	}
}

// Two circles can have the same name. Sharing with that name, or making an
// album for it, is refused with the ids to choose from instead of taking the
// first, and the id of either is accepted.
func TestACircleNameTwoCirclesHaveIsRefusedByAlbumShareAndAlbumCreate(test *testing.T) {
	current := newRun(test, map[string]string{
		"ListCirclePeople": `[{"userId":"user-ben","username":"ben"}]`,
		"ListCircles":      `[{"id":"circle-one","name":"Family","members":[{"userId":"user-ben"}]},{"id":"circle-two","name":"family","members":[{"userId":"user-ben"},{"userId":"user-cora"}]}]`,
		"SharePhotoAlbum":  `{"id":"share-one","photoAlbumId":"album-own","shareLevel":"view","isLocationIncluded":false}`,
		"CreatePhotoAlbum": `{"id":"album-new","albumTitle":"Ours","photoCount":0}`,
	})
	for _, arguments := range []map[string]any{
		{"operation": "album-share", "album_id": "album-own", "with": "Family"},
		{"operation": "album-create", "album_title": "Ours", "circle": "Family"},
	} {
		if _, err := current.call(arguments); err == nil || !strings.Contains(err.Error(), "more than one circle") || !strings.Contains(err.Error(), "circle-one") || !strings.Contains(err.Error(), "circle-two") {
			test.Errorf("%v was not refused with the ids: %v", arguments, err)
		}
	}
	if len(current.operations.asked("SharePhotoAlbum")) != 0 || len(current.operations.asked("CreatePhotoAlbum")) != 0 {
		test.Fatal("an ambiguous circle name reached the server")
	}
	if _, err := current.call(map[string]any{"operation": "album-share", "album_id": "album-own", "with": "circle-two"}); err != nil {
		test.Fatal(err)
	}
	if _, err := current.call(map[string]any{"operation": "album-create", "album_title": "Ours", "circle": "circle-two"}); err != nil {
		test.Fatal(err)
	}
	if shared, created := current.operations.asked("SharePhotoAlbum"), current.operations.asked("CreatePhotoAlbum"); len(shared) != 1 || shared[0]["granteeCircleId"] != "circle-two" || len(created) != 1 || created[0]["circleId"] != "circle-two" {
		test.Fatalf("%v %v", shared, created)
	}
}

// The card for sharing names who it is for the way the calendar's does: the
// username, or the circle in quotes with how many people it is.
func TestTheCardForSharingAnAlbumNamesWhoItIsFor(test *testing.T) {
	current := newRun(test, map[string]string{
		"ReadPhotoAlbum":   albumRowJson,
		"ListCirclePeople": `[{"userId":"user-ben","username":"ben"}]`,
		"ListCircles":      `[{"id":"circle-home","name":"Home","members":[{"userId":"user-ben"},{"userId":"user-alice"}]}]`,
	})
	photoTool := tools.Build().Get("photo")
	for with, want := range map[string]string{
		"ben":  `Share "Picnic" with ben, to view`,
		"Home": `Share "Picnic" with the circle "Home" (2 people), to view`,
	} {
		encoded, _ := json.Marshal(map[string]any{"operation": "album-share", "album_id": "album-own", "with": with})
		if line := photoTool.PreviewLine(tools.WithRun(test.Context(), current), encoded); line != want {
			test.Errorf("the card for %q says %q, want %q", with, line, want)
		}
		// Without a run to look them up in, the card quotes what the call names.
		if line := photoTool.PreviewLine(test.Context(), encoded); line != `Share an album with "`+with+`", to view` {
			test.Errorf("the plain card for %q says %q", with, line)
		}
	}
}

// The two shares the update tests change: a person who may only view and sees
// where the photos were taken, and a circle that may add photos and sees
// where they were taken too.
const (
	updateShareRows = `[` +
		`{"id":"share-one","photoAlbumId":"album-own","granteeUserId":"user-ben","granteeUsername":"ben","granteeName":"Ben Example","shareLevel":"view","isLocationIncluded":true},` +
		`{"id":"share-two","photoAlbumId":"album-own","granteeCircleId":"circle-home","granteeCircleName":"Home","shareLevel":"contribute","isLocationIncluded":true},` +
		`{"id":"share-three","photoAlbumId":"album-own","granteeUserId":"user-cai","granteeUsername":"cai","granteeName":"Cai Example","shareLevel":"view","isLocationIncluded":false}]`
	updatedShareRow = `{"id":"share-one","photoAlbumId":"album-own","granteeUserId":"user-ben","granteeUsername":"ben","granteeName":"Ben Example","shareLevel":"contribute","isLocationIncluded":true}`
)

// album-share-update changes only the settings the call names. The server
// writes the level and the places together, so the one left out is sent as the
// share has it: stopping somebody adding photos must not take the places away.
func TestAlbumShareUpdateChangesOnlyWhatIsNamed(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotoAlbumShares": updateShareRows, "UpdatePhotoAlbumShare": updatedShareRow})
	result, err := current.call(map[string]any{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-one", "share_level": "Contribute"})
	if err != nil || !result.Untrusted {
		test.Fatalf("%v %v", result, err)
	}
	for _, want := range []string{`"share_id":"share-one"`, `"share_level":"contribute"`, `"is_location_included":true`, `"person":"ben"`} {
		if !strings.Contains(result.Content, want) {
			test.Fatalf("%s lacks %s", result.Content, want)
		}
	}
	// The places were left out and are on in the share, so they stay on.
	asked := current.operations.asked("UpdatePhotoAlbumShare")
	if len(asked) != 1 || asked[0]["photoAlbumShareId"] != "share-one" || asked[0]["shareLevel"] != "contribute" || asked[0]["isLocationIncluded"] != true {
		test.Fatalf("an omitted is_location_included was not kept: %v", asked)
	}
	// Stopping a circle adding photos keeps the places it sees.
	if _, err := current.call(map[string]any{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-two", "share_level": "view"}); err != nil {
		test.Fatal(err)
	}
	asked = current.operations.asked("UpdatePhotoAlbumShare")
	if len(asked) != 2 || asked[1]["photoAlbumShareId"] != "share-two" || asked[1]["shareLevel"] != "view" || asked[1]["isLocationIncluded"] != true {
		test.Fatalf("narrowing the level took the places away: %v", asked)
	}
	// Taking the places away keeps the level the share has.
	if _, err := current.call(map[string]any{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-two", "is_location_included": false}); err != nil {
		test.Fatal(err)
	}
	asked = current.operations.asked("UpdatePhotoAlbumShare")
	if len(asked) != 3 || asked[2]["shareLevel"] != "contribute" || asked[2]["isLocationIncluded"] != false {
		test.Fatalf("an omitted share_level was not kept: %v", asked)
	}
	// Turning the places on, on a share that has them off, keeps its level.
	if _, err := current.call(map[string]any{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-three", "is_location_included": true}); err != nil {
		test.Fatal(err)
	}
	asked = current.operations.asked("UpdatePhotoAlbumShare")
	if len(asked) != 4 || asked[3]["shareLevel"] != "view" || asked[3]["isLocationIncluded"] != true {
		test.Fatalf("%v", asked)
	}
}

// A call that names what the share already has changes nothing, and says so
// without asking the server to write.
func TestAlbumShareUpdateThatChangesNothingMakesNoCall(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotoAlbumShares": updateShareRows, "UpdatePhotoAlbumShare": updatedShareRow})
	for _, arguments := range []map[string]any{
		{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-two", "share_level": "contribute"},
		{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-two", "is_location_included": true},
		{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-three", "share_level": "view", "is_location_included": false},
	} {
		result, err := current.call(arguments)
		if err != nil || !strings.Contains(result.Content, "nothing changed") {
			test.Fatalf("%v: %v %v", arguments, result, err)
		}
	}
	if asked := current.operations.asked("UpdatePhotoAlbumShare"); len(asked) != 0 {
		test.Fatalf("a call that changes nothing reached the server: %v", asked)
	}
}

func TestAlbumShareUpdateRefusesWhatItCannotDo(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotoAlbumShares": updateShareRows, "UpdatePhotoAlbumShare": updatedShareRow})
	for name, arguments := range map[string]struct {
		arguments map[string]any
		message   string
	}{
		"no album":                {map[string]any{"operation": "album-share-update", "share_id": "share-one", "share_level": "view"}, "album_id"},
		"no share":                {map[string]any{"operation": "album-share-update", "album_id": "album-own", "share_level": "view"}, "share_id"},
		"nothing to change":       {map[string]any{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-one"}, "share_level"},
		"a level not one":         {map[string]any{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-one", "share_level": "owner"}, "view or contribute"},
		"a share the album lacks": {map[string]any{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-elsewhere", "share_level": "view"}, "album-shares"},
	} {
		if _, err := current.call(arguments.arguments); err == nil || !strings.Contains(err.Error(), arguments.message) {
			test.Errorf("changing a share with %s: %v, want a message with %q", name, err, arguments.message)
		}
	}
	if asked := current.operations.asked("UpdatePhotoAlbumShare"); len(asked) != 0 {
		test.Fatalf("a change that was refused reached the server: %v", asked)
	}
}

// The person is asked when the call could give somebody more (contribute, or
// places on), judged from the arguments alone, and not when it only names view
// or places off. The card reads the share and the album, and says whose share
// of which album becomes what and what stays.
func TestAlbumShareUpdateAsksWhenItCouldWidenAndTheCardNamesWhatChanges(test *testing.T) {
	current := newRun(test, map[string]string{"ReadPhotoAlbum": albumRowJson, "ListPhotoAlbumShares": updateShareRows})
	photoTool := tools.Build().Get("photo")
	for _, scenario := range []struct {
		arguments map[string]any
		risk      tools.Risk
		line      string
		plainLine string
	}{
		{
			map[string]any{"share_level": "contribute"}, tools.RiskGranting,
			`Change the share of "Picnic" with "Ben Example" to view and add photos, keeping where the photos were taken`, "Change an album share to view and add photos",
		},
		{
			map[string]any{"share_id": "share-three", "is_location_included": true}, tools.RiskGranting,
			`Change the share of "Picnic" with "Cai Example" to show where the photos were taken`, "Change an album share to show where the photos were taken",
		},
		{
			map[string]any{"share_id": "share-two", "share_level": "view"}, tools.RiskWrite,
			`Change the share of "Picnic" with everyone in "Home" to view only, keeping where the photos were taken`, "Change an album share to view only",
		},
		{
			map[string]any{"share_level": "view", "is_location_included": false}, tools.RiskWrite,
			`Change the share of "Picnic" with "Ben Example" to hide where the photos were taken`, "Change an album share to view only, and hide where the photos were taken",
		},
		{
			map[string]any{"share_id": "share-two", "is_location_included": false}, tools.RiskWrite,
			`Change the share of "Picnic" with everyone in "Home" to hide where the photos were taken`, "Change an album share to hide where the photos were taken",
		},
		{
			// Asked about although the share has it already: riskOf cannot see
			// the share. The card says that nothing changes.
			map[string]any{"share_id": "share-two", "share_level": "contribute"}, tools.RiskGranting,
			`Change the share of "Picnic" with everyone in "Home": it is already like that, so nothing changes`, "Change an album share to view and add photos",
		},
	} {
		arguments := map[string]any{"operation": "album-share-update", "album_id": "album-own", "share_id": "share-one"}
		for key, value := range scenario.arguments {
			arguments[key] = value
		}
		encoded, _ := json.Marshal(arguments)
		if risk := riskOf(encoded); risk != scenario.risk {
			test.Errorf("%v is %s, want %s", scenario.arguments, risk, scenario.risk)
		}
		if line := photoTool.PreviewLine(tools.WithRun(test.Context(), current), encoded); line != scenario.line {
			test.Errorf("%v: the card says %q, want %q", scenario.arguments, line, scenario.line)
		}
		// Without a run to look the share up in, the card says what the call names.
		if line := photoTool.PreviewLine(test.Context(), encoded); line != scenario.plainLine {
			test.Errorf("%v: the plain card says %q, want %q", scenario.arguments, line, scenario.plainLine)
		}
	}
}

func TestAlbumSharesAndUnshareUseTheSharedOperations(test *testing.T) {
	current := newRun(test, map[string]string{
		"ListPhotoAlbumShares":  `[{"id":"share-one","granteeUserId":"user-ben","granteeUsername":"ben","granteeName":"Ben Example","shareLevel":"view"},{"id":"share-two","granteeCircleId":"circle-home","granteeCircleName":"Home","shareLevel":"contribute","isLocationIncluded":true}]`,
		"RemovePhotoAlbumShare": `true`,
	})
	result, err := current.call(map[string]any{"operation": "album-shares", "album_id": "album-own"})
	if err != nil || !result.Untrusted {
		test.Fatalf("%v %v", result, err)
	}
	for _, want := range []string{`"share_id":"share-one"`, `"person":"ben"`, `"circle_name":"Home"`, `"is_location_included":true`} {
		if !strings.Contains(result.Content, want) {
			test.Fatalf("%s lacks %s", result.Content, want)
		}
	}
	if result, err := current.call(map[string]any{"operation": "album-unshare", "share_id": "share-one"}); err != nil || !strings.Contains(result.Content, "stopped sharing") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := current.operations.asked("RemovePhotoAlbumShare"); len(asked) != 1 || asked[0]["photoAlbumShareId"] != "share-one" {
		test.Fatalf("%v", asked)
	}
	for _, arguments := range []map[string]any{{"operation": "album-shares"}, {"operation": "album-unshare"}} {
		if _, err := current.call(arguments); err == nil {
			test.Errorf("%v needed an identifier", arguments)
		}
	}
}

// Sharing changes who may see the person's photos: the person is asked unless
// they allowed it. Taking a share back only removes access, so it is a write,
// as calendar_unshare is. Saving a photo into the library is an ordinary
// write, and reading is a read.
func TestSharingAnAlbumIsAGrantAndSavingASharedPhotoIsAWrite(test *testing.T) {
	for operation, want := range map[string]tools.Risk{
		"album-share": tools.RiskGranting, "album-unshare": tools.RiskWrite, "album-shares": tools.RiskRead,
		"save-shared": tools.RiskWrite, "album-list": tools.RiskRead, "album-create": tools.RiskWrite,
	} {
		encoded, _ := json.Marshal(map[string]any{"operation": operation})
		if risk := riskOf(encoded); risk != want {
			test.Errorf("%s is %s, want %s", operation, risk, want)
		}
	}
	// And the tool tells the model what it may and may not do with what was shared.
	photoTool := tools.Build().Get("photo")
	if photoTool == nil {
		test.Fatal("no photo tool")
	}
	for _, want := range []string{"never copy a photo into the person's library unless they ask", "save-shared", "album-share", "always ask the person first"} {
		if !strings.Contains(photoTool.Description, want) && !strings.Contains(strings.ToLower(photoTool.Description), strings.ToLower(want)) {
			test.Errorf("the description does not say %q", want)
		}
	}
}

func TestSaveSharedKeepsEachPhotoAndSaysWhichCould(test *testing.T) {
	current := newRun(test, map[string]string{"SavePhotoToLibrary": `{"id":"photo-mine","photoKind":"picture"}`})
	result, err := current.call(map[string]any{"operation": "save-shared", "photo_ids": []string{"photo-one", "photo-two", "photo-one"}})
	if err != nil || !strings.Contains(result.Content, "photo-one: kept as photo-mine") || result.Note != "kept 2 of 2 photos in the library" {
		test.Fatalf("%v %v", result, err)
	}
	if asked := current.operations.asked("SavePhotoToLibrary"); len(asked) != 2 || asked[0]["photoId"] != "photo-one" || asked[1]["photoId"] != "photo-two" {
		test.Fatalf("%v", asked)
	}
	current.operations.refusals = map[string]string{"SavePhotoToLibrary": "not found"}
	result, err = current.call(map[string]any{"operation": "save-shared", "photo_ids": []string{"photo-else"}})
	if err != nil || !strings.Contains(result.Content, "photo-else: not found") || result.Note != "kept 0 of 1 photos in the library" {
		test.Fatalf("%v %v", result, err)
	}
	if _, err := current.call(map[string]any{"operation": "save-shared"}); err == nil {
		test.Fatal("nothing was named")
	}
	tooMany := make([]string, 26)
	for index := range tooMany {
		tooMany[index] = "photo-" + string(rune('a'+index))
	}
	if _, err := current.call(map[string]any{"operation": "save-shared", "photo_ids": tooMany}); err == nil {
		test.Fatal("26 photos were saved at once")
	}
}

func TestAPhotoOfSomebodyElsesIsListedWithItsOwnerAndNotDescribedByLook(test *testing.T) {
	sharedRow := `{"id":"photo-shared","photoKind":"picture","takenAt":"2025-03-04T10:30:00Z","takenAtOffsetMinutes":0,"originalFilename":"IMG_0009.JPG","photoProcessingState":"ready","largePreviewUrl":"/x","ownerName":"Ben Example"}`
	current := newRun(test, map[string]string{"ListPhotos": `[` + sharedRow + `]`})
	result, err := current.call(map[string]any{"operation": "list", "album_id": "album-shared"})
	if err != nil || !strings.Contains(result.Content, `"owner":"Ben Example"`) || strings.Contains(result.Content, "place_name") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := current.operations.asked("ListPhotos"); len(asked) != 1 || asked[0]["albumId"] != "album-shared" {
		test.Fatalf("%v", asked)
	}
	current.operations.photos = map[string]string{"photo-shared": `{"photo":` + sharedRow + `,"photoFiles":[],"photoPreviews":[{"photoPreviewSize":"large"}]}`}
	if err := current.store.PutFile(test.Context(), models.PhotoPreviewFileID("photo-shared", models.PhotoPreviewLarge), []byte{0xFF, 0xD8, 0xFF, 0xD9}); err != nil {
		test.Fatal(err)
	}
	result, err = current.call(map[string]any{"operation": "look", "photo_ids": []string{"photo-shared"}})
	if err != nil || len(result.Images) != 1 {
		test.Fatalf("%v %v", result, err)
	}
	// The description belongs to the owner's library: it is not asked for.
	if asked := current.operations.asked("DescribePhoto"); len(asked) != 0 || strings.Contains(result.Content, "Asked for a description") {
		test.Fatalf("a description of somebody else's photo was asked for: %v\n%s", asked, result.Content)
	}
	result, err = current.call(map[string]any{"operation": "read", "photo_id": "photo-shared"})
	if err != nil || !strings.Contains(result.Content, `"owner":"Ben Example"`) || !strings.Contains(result.Content, "shared with the person") {
		test.Fatalf("%v %v", result, err)
	}
}

func TestAlbumCreateWithACircleMakesTheCirclesAlbum(test *testing.T) {
	current := newRun(test, map[string]string{
		"ListCircles":      `[{"id":"circle-home","name":"Home"}]`,
		"CreatePhotoAlbum": `{"id":"album-new","albumTitle":"Ours","photoCount":0}`,
	})
	if _, err := current.call(map[string]any{"operation": "album-create", "album_title": "Ours", "circle": "Home"}); err != nil {
		test.Fatal(err)
	}
	if asked := current.operations.asked("CreatePhotoAlbum"); len(asked) != 1 || asked[0]["circleId"] != "circle-home" {
		test.Fatalf("%v", asked)
	}
	if _, err := current.call(map[string]any{"operation": "album-create", "album_title": "Ours", "circle": "Nowhere"}); err == nil {
		test.Fatal("an album was made for a circle the person is not in")
	}
}

// chattingRun is a run whose words are posted to the people of a chat.
type chattingRun struct{ *fakeRun }

func (self chattingRun) IsAnsweringInChat() bool { return true }

func (self chattingRun) call(arguments map[string]any) (*tools.Result, error) {
	encoded, _ := json.Marshal(arguments)
	return run(tools.WithRun(context.Background(), self), &tools.Call{ID: "fixture-call", Arguments: encoded})
}

// In a chat only the person's own photos and albums are read: what another
// person shared with the person is not posted to everybody in the room.
func TestPhotosAndAlbumsOfOthersAreNotReadIntoAChat(test *testing.T) {
	ownRow := `{"id":"photo-own","photoKind":"picture","takenAt":"2025-03-04T10:30:00Z","takenAtOffsetMinutes":0,"photoProcessingState":"ready","largePreviewUrl":"/x","isOwnedByCaller":true}`
	sharedRow := `{"id":"photo-shared","photoKind":"picture","takenAt":"2025-03-04T10:30:00Z","takenAtOffsetMinutes":0,"photoProcessingState":"ready","largePreviewUrl":"/x","ownerName":"Ben Example"}`
	current := newRun(test, map[string]string{
		"ListPhotoAlbums": `[` + ownAlbumRowJson + `,` + sharedAlbumRowJson + `,` + circleAlbumRowJson + `]`,
		"ListPhotos":      `[` + sharedRow + `]`,
	})
	current.operations.photos = map[string]string{
		"photo-own":    `{"photo":` + ownRow + `,"photoFiles":[],"photoPreviews":[{"photoPreviewSize":"large"}]}`,
		"photo-shared": `{"photo":` + sharedRow + `,"photoFiles":[],"photoPreviews":[{"photoPreviewSize":"large"}]}`,
	}
	for _, photoId := range []string{"photo-own", "photo-shared"} {
		if err := current.store.PutFile(test.Context(), models.PhotoPreviewFileID(photoId, models.PhotoPreviewLarge), []byte{0xFF, 0xD8, 0xFF, 0xD9}); err != nil {
			test.Fatal(err)
		}
	}
	chat := chattingRun{current}

	// The albums listed are the person's own.
	result, err := chat.call(map[string]any{"operation": "album-list"})
	if err != nil || strings.Contains(result.Content, "album-shared") || strings.Contains(result.Content, "album-circle") || !strings.Contains(result.Content, "album-own") {
		test.Fatalf("only the person's own albums are listed in a chat: %v %v", result, err)
	}

	// Somebody else's album, and its photos, are refused; so is one photo of
	// theirs, however it is asked for.
	current.operations.answers["ReadPhotoAlbum"] = sharedAlbumRowJson
	for _, arguments := range []map[string]any{
		{"operation": "album-read", "album_id": "album-shared"},
		{"operation": "list", "album_id": "album-shared"},
		{"operation": "read", "photo_id": "photo-shared"},
	} {
		if _, err := chat.call(arguments); err == nil || !strings.Contains(err.Error(), "not repeated into a chat") {
			test.Errorf("%v in a chat: %v", arguments, err)
		}
	}
	for _, operation := range []string{"look", "show"} {
		result, err := chat.call(map[string]any{"operation": operation, "photo_ids": []string{"photo-shared"}})
		if err != nil || !strings.Contains(result.Content, "not repeated into a chat") || len(result.Images) != 0 || strings.Contains(result.Content, "attachment_id") {
			test.Errorf("%s of somebody else's photo in a chat: %v %v", operation, result, err)
		}
	}

	// The person's own are read as ever.
	current.operations.answers["ReadPhotoAlbum"] = ownAlbumRowJson
	current.operations.answers["ListPhotos"] = `[` + ownRow + `]`
	for _, arguments := range []map[string]any{
		{"operation": "album-read", "album_id": "album-own"},
		{"operation": "list"},
		{"operation": "read", "photo_id": "photo-own"},
	} {
		if _, err := chat.call(arguments); err != nil {
			test.Errorf("%v of the person's own in a chat: %v", arguments, err)
		}
	}
	if result, err := chat.call(map[string]any{"operation": "look", "photo_ids": []string{"photo-own"}}); err != nil || len(result.Images) != 1 {
		test.Errorf("the person's own photo is looked at in a chat: %v %v", result, err)
	}
}
