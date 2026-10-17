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

func TestAlbumShowListsTheAlbumsPhotos(test *testing.T) {
	server := newPhotoServer(test)
	_, table, err := server.run(test, "album", "show", "album-1", "--limit", "7", "--continue-after", "photo-9")
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
	_, output, err := server.run(test, "album", "show", "album-1", "--json")
	var shown struct {
		Album  models.PhotoAlbum `json:"album"`
		Photos []models.Photo    `json:"photos"`
	}
	if err != nil || json.Unmarshal([]byte(output), &shown) != nil || shown.Album.ID != "album-1" || len(shown.Photos) != 1 {
		test.Fatalf("%v: %s", err, output)
	}
	for _, arguments := range [][]string{{"album", "show"}, {"album", "show", "album-1", "--limit", "501"}} {
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
