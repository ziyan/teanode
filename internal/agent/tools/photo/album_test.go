package photo

import (
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

func TestAlbumShareAsksTheNameOfThePersonOrCircleAndSharesAtALevel(test *testing.T) {
	current := newRun(test, map[string]string{
		"ListCirclePeople": `[{"userId":"user-ben","username":"ben"},{"userId":"user-cora","username":"cora"}]`,
		"ListCircles":      `[{"id":"circle-home","name":"Home"}]`,
		"SharePhotoAlbum":  `{"id":"share-one","photoAlbumId":"album-own","shareLevel":"contribute","isLocationIncluded":true}`,
	})
	result, err := current.call(map[string]any{"operation": "album-share", "album_id": "album-own", "person": "Ben", "share_level": "contribute", "is_location_included": true})
	if err != nil || !strings.Contains(result.Content, `"share_id":"share-one"`) || !strings.Contains(result.Content, `"shared_with":"ben"`) {
		test.Fatalf("%v %v", result, err)
	}
	asked := current.operations.asked("SharePhotoAlbum")
	if len(asked) != 1 || asked[0]["photoAlbumId"] != "album-own" || asked[0]["granteeUserId"] != "user-ben" || asked[0]["granteeCircleId"] != nil ||
		asked[0]["shareLevel"] != "contribute" || asked[0]["isLocationIncluded"] != true {
		test.Fatalf("%v", asked)
	}
	// A circle by its name, and the level when none is said.
	if result, err := current.call(map[string]any{"operation": "album-share", "album_id": "album-own", "circle": "home"}); err != nil || !strings.Contains(result.Content, "everyone in Home") {
		test.Fatalf("%v %v", result, err)
	}
	asked = current.operations.asked("SharePhotoAlbum")
	if len(asked) != 2 || asked[1]["granteeCircleId"] != "circle-home" || asked[1]["granteeUserId"] != nil || asked[1]["shareLevel"] != "view" || asked[1]["isLocationIncluded"] != false {
		test.Fatalf("%v", asked)
	}
	for name, arguments := range map[string]map[string]any{
		"no album":           {"operation": "album-share", "person": "ben"},
		"neither":            {"operation": "album-share", "album_id": "album-own"},
		"both":               {"operation": "album-share", "album_id": "album-own", "person": "ben", "circle": "Home"},
		"a person not known": {"operation": "album-share", "album_id": "album-own", "person": "nobody"},
		"a circle not known": {"operation": "album-share", "album_id": "album-own", "circle": "Nowhere"},
		"a level not one":    {"operation": "album-share", "album_id": "album-own", "person": "ben", "share_level": "owner"},
	} {
		if _, err := current.call(arguments); err == nil {
			test.Errorf("sharing with %s was not refused", name)
		}
	}
	if asked := current.operations.asked("SharePhotoAlbum"); len(asked) != 2 {
		test.Fatalf("a share that was refused reached the server: %v", asked)
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

// Sharing, and taking a share back, change who may see the person's photos:
// the person is asked unless they allowed it. Saving a photo into the library
// is an ordinary write, and reading is a read.
func TestSharingAnAlbumIsAGrantAndSavingASharedPhotoIsAWrite(test *testing.T) {
	for operation, want := range map[string]tools.Risk{
		"album-share": tools.RiskGranting, "album-unshare": tools.RiskGranting, "album-shares": tools.RiskRead,
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
