package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

func TestAlbumListPrintsATableOrJSON(test *testing.T) {
	server := newPhotoServer(test)
	_, table, err := server.run(test, "album", "list")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"album-1", "Picnic", "3", "album-2", "Empty one"} {
		if !strings.Contains(table, want) {
			test.Fatalf("the table lacks %q:\n%s", want, table)
		}
	}
	_, output, err := server.run(test, "album", "list", "--json")
	var albums []models.PhotoAlbum
	if err != nil || json.Unmarshal([]byte(output), &albums) != nil || len(albums) != 2 || albums[0].AlbumTitle != "Picnic" || albums[0].PhotoCount != 3 {
		test.Fatalf("%v: %s", err, output)
	}
}

func TestAlbumReadListsTheAlbumsPhotos(test *testing.T) {
	server := newPhotoServer(test)
	_, table, err := server.run(test, "album", "read", "album-1", "--limit", "7", "--continue-after", "photo-9")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(table, "Picnic") || !strings.Contains(table, "photo-1") || !strings.Contains(table, "IMG_1.JPG") {
		test.Fatalf("the album and its photos:\n%s", table)
	}
	// The photos are the listing of the album: the same operation, asked for
	// by the album's id.
	variables := server.variables[len(server.variables)-1]
	if variables["albumId"] != "album-1" || variables["pageSize"] != float64(7) || variables["beforePhotoId"] != "photo-9" {
		test.Fatalf("%v", variables)
	}
	_, output, err := server.run(test, "album", "read", "album-1", "--json")
	var shown struct {
		Album  models.PhotoAlbum `json:"album"`
		Photos []models.Photo    `json:"photos"`
	}
	if err != nil || json.Unmarshal([]byte(output), &shown) != nil || shown.Album.ID != "album-1" || len(shown.Photos) != 1 {
		test.Fatalf("%v: %s", err, output)
	}
	for _, arguments := range [][]string{{"album", "read"}, {"album", "read", "album-1", "--limit", "501"}} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
	// The same listing is reached from photo list.
	if _, _, err := server.run(test, "list", "--album", "album-1"); err != nil {
		test.Fatal(err)
	}
	if variables := server.variables[len(server.variables)-1]; variables["albumId"] != "album-1" {
		test.Fatalf("%v", variables)
	}
}

func TestAlbumCreateRenameAddRemoveAndCoverSendWhatTheyAreGiven(test *testing.T) {
	server := newPhotoServer(test)
	output, _, err := server.run(test, "album", "create", "Weekend away", "photo-1", "photo-2")
	if err != nil || !strings.Contains(output, "created Weekend away (album-new)") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.variables[len(server.variables)-1]; variables["albumTitle"] != "Weekend away" || !reflect.DeepEqual(variables["photoIds"], []any{"photo-1", "photo-2"}) {
		test.Fatalf("%v", variables)
	}
	// An empty album sends no list at all.
	if _, _, err := server.run(test, "album", "create", "Nothing yet"); err != nil {
		test.Fatal(err)
	}
	if variables := server.variables[len(server.variables)-1]; variables["photoIds"] != nil {
		test.Fatalf("%v", variables)
	}
	if output, _, err := server.run(test, "album", "rename", "album-1", "Picnic at the lake"); err != nil || !strings.Contains(output, "renamed to Picnic at the lake (album-1)") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.variables[len(server.variables)-1]; variables["photoAlbumId"] != "album-1" || variables["albumTitle"] != "Picnic at the lake" {
		test.Fatalf("%v", variables)
	}
	if output, _, err := server.run(test, "album", "add", "album-1", "photo-1", "photo-2"); err != nil || !strings.Contains(output, "added 1 of 2 photos") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.variables[len(server.variables)-1]; variables["photoAlbumId"] != "album-1" || !reflect.DeepEqual(variables["photoIds"], []any{"photo-1", "photo-2"}) {
		test.Fatalf("%v", variables)
	}
	_, output, err = server.run(test, "album", "remove", "album-1", "photo-1", "--json")
	var removed map[string]int
	if err != nil || json.Unmarshal([]byte(output), &removed) != nil || removed["removedCount"] != 2 {
		test.Fatalf("%v: %s", err, output)
	}
	if output, _, err := server.run(test, "album", "cover", "album-1", "photo-1"); err != nil || !strings.Contains(output, "Picnic (album-1) is shown by photo-1") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.variables[len(server.variables)-1]; variables["photoAlbumId"] != "album-1" || variables["photoId"] != "photo-1" {
		test.Fatalf("%v", variables)
	}

	before := len(server.documents)
	for _, arguments := range [][]string{
		{"album", "create"}, {"album", "rename", "album-1"}, {"album", "rename", "album-1", "a", "b"},
		{"album", "add", "album-1"}, {"album", "remove"}, {"album", "cover", "album-1"}, {"album", "delete"},
	} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
	tooMany := []string{"album", "add", "album-1"}
	for index := 0; index < 501; index++ {
		tooMany = append(tooMany, "photo")
	}
	if _, _, err := server.run(test, tooMany...); err == nil {
		test.Fatal("501 photos were accepted")
	}
	if len(server.documents) != before {
		test.Fatal("something was sent for a command that was refused")
	}
}

func TestAlbumDeleteAsksFirstAndSaysThePhotosStay(test *testing.T) {
	server := newPhotoServer(test)
	before := len(server.documents)
	if _, _, err := server.run(test, "album", "delete", "album-1"); err == nil || !strings.Contains(err.Error(), "--force") || !strings.Contains(err.Error(), "photos stay") {
		test.Fatalf("%v", err)
	}
	if len(server.documents) != before {
		test.Fatal("something was sent without confirmation")
	}
	output, _, err := server.run(test, "album", "delete", "album-1", "--force")
	if err != nil || !strings.Contains(output, "deleted album album-1") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.variables[len(server.variables)-1]; variables["photoAlbumId"] != "album-1" {
		test.Fatalf("%v", variables)
	}
}

func TestAlbumSuggestPrintsDatesPlacesTitleAndCount(test *testing.T) {
	server := newPhotoServer(test)
	_, table, err := server.run(test, "album", "suggest", "--after", "2031-01-01", "--before", "2032-01-01")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"Exampleton, Exampleland; Sampleburg, Exampleland", "Exampleton and Sampleburg, Exampleland", "40"} {
		if !strings.Contains(table, want) {
			test.Fatalf("the table lacks %q:\n%s", want, table)
		}
	}
	variables := server.variables[len(server.variables)-1]
	if variables["takenAfter"] == nil || variables["takenBefore"] == nil {
		test.Fatalf("%v", variables)
	}
	_, output, err := server.run(test, "album", "suggest", "--json")
	var suggestions []models.PhotoAlbumSuggestion
	if err != nil || json.Unmarshal([]byte(output), &suggestions) != nil || len(suggestions) != 1 || suggestions[0].PhotoCount != 40 || len(suggestions[0].PlaceNames) != 2 {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.variables[len(server.variables)-1]; variables["takenAfter"] != nil {
		test.Fatalf("dates nobody gave were sent: %v", variables)
	}
	if _, _, err := server.run(test, "album", "suggest", "--after", "yesterday"); err == nil {
		test.Fatal("a date that is not one was accepted")
	}
}

func TestAlbumListSaysWhoseEachAlbumIsAndWhatYouMayDoWithIt(test *testing.T) {
	server := newPhotoServer(test)
	server.albumsWithOthers = true
	_, table, err := server.run(test, "album", "list")
	if err != nil {
		test.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if len(lines) != 4 {
		test.Fatalf("a table of three albums:\n%s", table)
	}
	for index, want := range [][]string{{"owner", "level"}, {"album-1", "you", "owner"}, {"album-3", "Ben Example", "view"}, {"album-4", "circle Home", "contribute"}} {
		fields := strings.Join(strings.Fields(lines[index]), " ")
		for _, text := range want {
			if !strings.Contains(fields, text) {
				test.Errorf("line %d lacks %q:\n%s", index, text, table)
			}
		}
	}
}

func TestAlbumShareSharesWithAPersonOrACircleAtALevel(test *testing.T) {
	server := newPhotoServer(test)
	output, _, err := server.run(test, "album", "share", "album-1", "--person", "Ben", "--contribute", "--with-location", "--with-face-names")
	if err != nil || !strings.Contains(output, "shared with ben at contribute, with where the photos were taken, with the names of the faces (share share-person)") {
		test.Fatalf("%v: %s", err, output)
	}
	variables := server.variables[len(server.variables)-1]
	if variables["photoAlbumId"] != "album-1" || variables["granteeUserId"] != "user-ben" || variables["granteeCircleId"] != nil || variables["shareLevel"] != "contribute" || variables["isLocationIncluded"] != true || variables["isFaceNamesIncluded"] != true {
		test.Fatalf("%v", variables)
	}
	// A circle by its name or its identifier, to view, without places.
	output, _, err = server.run(test, "album", "share", "album-1", "--circle", "home")
	if err != nil || !strings.Contains(output, "shared with everyone in Home at view, without where the photos were taken, without the names of the faces") {
		test.Fatalf("%v: %s", err, output)
	}
	variables = server.variables[len(server.variables)-1]
	if variables["granteeCircleId"] != "circle-home" || variables["granteeUserId"] != nil || variables["shareLevel"] != "view" || variables["isLocationIncluded"] != false || variables["isFaceNamesIncluded"] != false {
		test.Fatalf("%v", variables)
	}
	if _, output, err := server.run(test, "album", "share", "album-1", "--circle", "circle-home", "--json"); err != nil || !strings.Contains(output, `"id": "share-circle"`) && !strings.Contains(output, `"id":"share-circle"`) {
		test.Fatalf("%v: %s", err, output)
	}

	before := len(server.documents)
	for name, arguments := range map[string][]string{
		"no album":         {"album", "share", "--person", "ben"},
		"neither":          {"album", "share", "album-1"},
		"both":             {"album", "share", "album-1", "--person", "ben", "--circle", "home"},
		"somebody unknown": {"album", "share", "album-1", "--person", "nobody"},
		"a circle unknown": {"album", "share", "album-1", "--circle", "Nowhere"},
	} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Errorf("sharing with %s was accepted", name)
		}
	}
	for _, document := range server.documents[before:] {
		if strings.Contains(document, "SharePhotoAlbum(") {
			test.Fatal("a share that was refused was sent")
		}
	}
}

// share-update changes only what its flags name. The server writes the level
// and the places together, so the share is read first and what the flags leave
// out is sent as the share has it. In the test server share-person is view
// without places or face names, and share-circle is contribute with both.
func TestAlbumShareUpdateChangesOnlyWhatTheFlagsName(test *testing.T) {
	server := newPhotoServer(test)
	updates := func() []map[string]any {
		var found []map[string]any
		for index, document := range server.documents {
			if strings.Contains(document, "UpdatePhotoAlbumShare(") {
				found = append(found, server.variables[index])
			}
		}
		return found
	}
	// Places on, and the level the share has stays.
	output, _, err := server.run(test, "album", "share-update", "album-1", "share-person", "--with-location")
	if err != nil || !strings.Contains(output, "shared with ben at view, with where the photos were taken, without the names of the faces (share share-person)") {
		test.Fatalf("%v: %s", err, output)
	}
	if found := updates(); len(found) != 1 || found[0]["photoAlbumShareId"] != "share-person" || found[0]["shareLevel"] != "view" || found[0]["isLocationIncluded"] != true {
		test.Fatalf("%v", found)
	}
	// Stopping a circle adding photos keeps the places it sees.
	if _, _, err := server.run(test, "album", "share-update", "album-1", "share-circle", "--level", "view"); err != nil {
		test.Fatal(err)
	}
	if found := updates(); len(found) != 2 || found[1]["photoAlbumShareId"] != "share-circle" || found[1]["shareLevel"] != "view" || found[1]["isLocationIncluded"] != true {
		test.Fatalf("narrowing the level took the places away: %v", found)
	}
	// Places off keeps the level.
	if _, _, err := server.run(test, "album", "share-update", "album-1", "share-circle", "--without-location"); err != nil {
		test.Fatal(err)
	}
	if found := updates(); len(found) != 3 || found[2]["shareLevel"] != "contribute" || found[2]["isLocationIncluded"] != false {
		test.Fatalf("%v", found)
	}
	// Face names alone: the level and the places stay.
	if _, _, err := server.run(test, "album", "share-update", "album-1", "share-person", "--with-face-names"); err != nil {
		test.Fatal(err)
	}
	if found := updates(); len(found) != 4 || found[3]["shareLevel"] != "view" || found[3]["isLocationIncluded"] != false || found[3]["isFaceNamesIncluded"] != true {
		test.Fatalf("%v", found)
	}
	// Taking the places away keeps the face names, and the other way round.
	if _, _, err := server.run(test, "album", "share-update", "album-1", "share-circle", "--without-face-names"); err != nil {
		test.Fatal(err)
	}
	if found := updates(); len(found) != 5 || found[4]["shareLevel"] != "contribute" || found[4]["isLocationIncluded"] != true || found[4]["isFaceNamesIncluded"] != false {
		test.Fatalf("%v", found)
	}
	// Both named at once.
	if _, _, err := server.run(test, "album", "share-update", "album-1", "share-person", "--level", "Contribute", "--with-location"); err != nil {
		test.Fatal(err)
	}
	if found := updates(); len(found) != 6 || found[5]["shareLevel"] != "contribute" || found[5]["isLocationIncluded"] != true {
		test.Fatalf("%v", found)
	}
	// What the share has already is not sent.
	output, _, err = server.run(test, "album", "share-update", "album-1", "share-circle", "--level", "contribute", "--with-location", "--with-face-names")
	if err != nil || !strings.Contains(output, "nothing changed") || len(updates()) != 6 {
		test.Fatalf("%v: %s", err, output)
	}
	before := len(server.documents)
	for name, arguments := range map[string][]string{
		"no arguments":            {"album", "share-update"},
		"no share":                {"album", "share-update", "album-1"},
		"too many":                {"album", "share-update", "album-1", "share-person", "extra", "--level", "view"},
		"nothing named":           {"album", "share-update", "album-1", "share-person"},
		"a level not one":         {"album", "share-update", "album-1", "share-person", "--level", "owner"},
		"places both ways":        {"album", "share-update", "album-1", "share-person", "--with-location", "--without-location"},
		"face names both ways":    {"album", "share-update", "album-1", "share-person", "--with-face-names", "--without-face-names"},
		"a share the album lacks": {"album", "share-update", "album-1", "share-elsewhere", "--level", "view"},
	} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
	for _, document := range server.documents[before:] {
		if strings.Contains(document, "UpdatePhotoAlbumShare(") {
			test.Fatal("a share-update that was refused was sent")
		}
	}
}

func TestAlbumSharesListsAndUnshareTakesOneBack(test *testing.T) {
	server := newPhotoServer(test)
	_, table, err := server.run(test, "album", "shares", "album-1")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"ben", "view", "no", "share-person", "everyone in Home", "contribute", "yes", "share-circle"} {
		if !strings.Contains(table, want) {
			test.Fatalf("the table lacks %q:\n%s", want, table)
		}
	}
	if variables := server.variables[len(server.variables)-1]; variables["photoAlbumId"] != "album-1" {
		test.Fatalf("%v", variables)
	}
	output, _, err := server.run(test, "album", "unshare", "share-person")
	if err != nil || !strings.Contains(output, "no longer shared") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.variables[len(server.variables)-1]; variables["photoAlbumShareId"] != "share-person" {
		test.Fatalf("%v", variables)
	}
	for _, arguments := range [][]string{{"album", "shares"}, {"album", "unshare"}, {"album", "unshare", "a", "b"}} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
}

func TestAlbumCreateForACircleAndSaveASharedPhoto(test *testing.T) {
	server := newPhotoServer(test)
	if output, _, err := server.run(test, "album", "create", "Ours", "--circle", "Home", "photo-1"); err != nil || !strings.Contains(output, "created Ours (album-new)") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.variables[len(server.variables)-1]; variables["circleId"] != "circle-home" || variables["albumTitle"] != "Ours" {
		test.Fatalf("%v", variables)
	}
	if _, _, err := server.run(test, "album", "create", "Ours", "--circle", "Nowhere"); err == nil {
		test.Fatal("an album was made for a circle the person is not in")
	}
	// An album with no circle sends none.
	if _, _, err := server.run(test, "album", "create", "Mine"); err != nil {
		test.Fatal(err)
	}
	if variables := server.variables[len(server.variables)-1]; variables["circleId"] != nil {
		test.Fatalf("%v", variables)
	}

	output, _, err := server.run(test, "save", "photo-shared")
	if err != nil || !strings.Contains(output, "saved to your library as photo-mine") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.variables[len(server.variables)-1]; variables["photoId"] != "photo-shared" {
		test.Fatalf("%v", variables)
	}
	for _, arguments := range [][]string{{"save"}, {"save", "a", "b"}} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
}
