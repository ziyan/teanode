package apigraph

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/graphql-go/graphql"
	gqlparser "github.com/graphql-go/graphql/language/parser"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestPhotoAlbumOperationsMakeFillRenameAndDeleteAnAlbum(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("album-operations-owner", models.PermissionPhotosUse)
	first := world.storePhoto(ownerId, "first", "first.jpg", []byte("first original"), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewSmall: []byte("small one")})
	second := world.storePhoto(ownerId, "second", "second.jpg", []byte("second original"), nil)
	third := world.storePhoto(ownerId, "third", "third.jpg", []byte("third original"), nil)
	allowed := []models.Permission{models.PermissionPhotosUse}

	world.ask(ownerId, allowed, func(ctx context.Context) {
		if albums, err := world.graph.ListPhotoAlbums(ctx); err != nil || albums == nil || len(albums) != 0 {
			test.Fatalf("no albums should be an empty list: %v %v", albums, err)
		}
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: " Picnic ", PhotoIDs: []string{first.ID, second.ID}})
		if err != nil || album.AlbumTitle != "Picnic" || album.PhotoCount != 2 || album.UserID != ownerId {
			test.Fatalf("created %+v %v", album, err)
		}
		// The cover is the one photo that has a small preview, addressed the
		// way the photo's own preview is.
		wantCover := api.PhotoPreviewPath(first.ID, "small") + "?v=" + strconv.FormatInt(first.UpdatedAt.Unix(), 10)
		listed, err := world.graph.ListPhotoAlbums(ctx)
		if err != nil || len(listed) != 1 || listed[0].CoverPreviewURL != wantCover || listed[0].CoverPhotoID != nil {
			test.Fatalf("listed %+v %v (want the cover %s)", listed, err, wantCover)
		}
		if read, err := world.graph.ReadPhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: album.ID}); err != nil || read.ID != album.ID || read.CoverPreviewURL != wantCover {
			test.Fatalf("read %+v %v", read, err)
		}
		empty, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Empty"})
		if err != nil || empty.PhotoCount != 0 || empty.CoverPreviewURL != "" {
			test.Fatalf("empty %+v %v", empty, err)
		}

		// A title outside the limits is refused with the limit said.
		for _, title := range []string{"", "   ", strings.Repeat("x", 256)} {
			_, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: title})
			if err == nil || !strings.Contains(err.Error(), "1 to 255 characters") {
				test.Fatalf("the title %q: %v", title, err)
			}
			if _, err := world.graph.RenamePhotoAlbum(ctx, RenamePhotoAlbumArguments{PhotoAlbumID: album.ID, AlbumTitle: title}); err == nil || !strings.Contains(err.Error(), "1 to 255 characters") {
				test.Fatalf("renaming to %q: %v", title, err)
			}
		}
		renamed, err := world.graph.RenamePhotoAlbum(ctx, RenamePhotoAlbumArguments{PhotoAlbumID: album.ID, AlbumTitle: "Picnic at the lake"})
		if err != nil || renamed.AlbumTitle != "Picnic at the lake" {
			test.Fatalf("renamed %+v %v", renamed, err)
		}

		if added, err := world.graph.AddPhotosToAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: album.ID, PhotoIDs: []string{second.ID, third.ID}}); err != nil || added != 1 {
			test.Fatalf("added %d %v", added, err)
		}
		members, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: album.ID})
		if err != nil || len(members) != 3 {
			test.Fatalf("the album's photos %v %v", members, err)
		}
		if count, err := world.graph.CountPhotos(ctx, ListPhotosArguments{AlbumID: album.ID}); err != nil || count != 3 {
			test.Fatalf("counted %d %v", count, err)
		}
		// The photo says which albums it is in.
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: third.ID})
		if err != nil || len(view.Albums) != 1 || view.Albums[0].ID != album.ID || view.Albums[0].AlbumTitle != "Picnic at the lake" {
			test.Fatalf("read %+v %v", view, err)
		}
		if view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: first.ID}); err != nil || view.Albums == nil || len(view.Albums) != 1 {
			test.Fatalf("%+v %v", view, err)
		}

		covered, err := world.graph.SetPhotoAlbumCover(ctx, SetPhotoAlbumCoverArguments{PhotoAlbumID: album.ID, PhotoID: first.ID})
		if err != nil || covered.CoverPhotoID == nil || *covered.CoverPhotoID != first.ID {
			test.Fatalf("covered %+v %v", covered, err)
		}
		if _, err := world.graph.SetPhotoAlbumCover(ctx, SetPhotoAlbumCoverArguments{PhotoAlbumID: empty.ID, PhotoID: first.ID}); err == nil {
			test.Fatal("a photo outside the album was chosen")
		}
		if removed, err := world.graph.RemovePhotosFromAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: album.ID, PhotoIDs: []string{first.ID, third.ID}}); err != nil || removed != 2 {
			test.Fatalf("removed %d %v", removed, err)
		}
		if view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: first.ID}); err != nil || view.Albums == nil || len(view.Albums) != 0 {
			test.Fatalf("a photo in no album should say an empty list: %+v %v", view, err)
		}

		if deleted, err := world.graph.DeletePhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: album.ID}); err != nil || !deleted {
			test.Fatalf("deleted %v %v", deleted, err)
		}
		if _, err := world.graph.ReadPhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: album.ID}); err == nil {
			test.Fatal("a deleted album was read")
		}
		// The photos stay.
		if all, err := world.graph.ListPhotos(ctx, ListPhotosArguments{}); err != nil || len(all) != 3 {
			test.Fatalf("the library is %v %v", all, err)
		}
	})
}

func TestAnotherPersonsAlbumAndPhotosAreNotFound(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("album-visible-owner", models.PermissionPhotosUse)
	otherId := world.person("album-visible-other", models.PermissionPhotosUse)
	owned := world.storePhoto(ownerId, "owned", "owned.jpg", []byte("owned original"), nil)
	foreign := world.storePhoto(otherId, "foreign", "foreign.jpg", []byte("foreign original"), nil)
	allowed := []models.Permission{models.PermissionPhotosUse}
	var albumId string
	world.ask(ownerId, allowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Mine", PhotoIDs: []string{owned.ID}})
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
		if _, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Borrowed", PhotoIDs: []string{foreign.ID}}); err != api.ErrNotFound {
			test.Fatalf("an album of another's photo: %v", err)
		}
		if _, err := world.graph.AddPhotosToAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: albumId, PhotoIDs: []string{foreign.ID}}); err != api.ErrNotFound {
			test.Fatalf("another's photo added: %v", err)
		}
	})
	world.ask(otherId, allowed, func(ctx context.Context) {
		if albums, err := world.graph.ListPhotoAlbums(ctx); err != nil || len(albums) != 0 {
			test.Fatalf("the other sees %v %v", albums, err)
		}
		for name, err := range map[string]error{
			"read": func() error {
				_, err := world.graph.ReadPhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: albumId})
				return err
			}(),
			"rename": func() error {
				_, err := world.graph.RenamePhotoAlbum(ctx, RenamePhotoAlbumArguments{PhotoAlbumID: albumId, AlbumTitle: "Taken"})
				return err
			}(),
			"delete": func() error {
				_, err := world.graph.DeletePhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: albumId})
				return err
			}(),
			"add": func() error {
				_, err := world.graph.AddPhotosToAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: albumId, PhotoIDs: []string{foreign.ID}})
				return err
			}(),
			"remove": func() error {
				_, err := world.graph.RemovePhotosFromAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: albumId, PhotoIDs: []string{owned.ID}})
				return err
			}(),
			"cover": func() error {
				_, err := world.graph.SetPhotoAlbumCover(ctx, SetPhotoAlbumCoverArguments{PhotoAlbumID: albumId, PhotoID: owned.ID})
				return err
			}(),
			"list the photos": func() error {
				_, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId})
				return err
			}(),
		} {
			if err != api.ErrNotFound {
				test.Errorf("%s: %v", name, err)
			}
		}
	})
	// Nothing changed.
	world.ask(ownerId, allowed, func(ctx context.Context) {
		album, err := world.graph.ReadPhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: albumId})
		if err != nil || album.AlbumTitle != "Mine" || album.PhotoCount != 1 {
			test.Fatalf("%+v %v", album, err)
		}
	})
}

func TestPhotoAlbumOperationsRefuseWithoutThePermissionOrTheService(test *testing.T) {
	world := newPhotoWorld(test)
	userId := world.person("album-refused", models.PermissionCalendarUse)
	calls := map[string]func(ctx context.Context) error{
		"list": func(ctx context.Context) error { _, err := world.graph.ListPhotoAlbums(ctx); return err },
		"read": func(ctx context.Context) error {
			_, err := world.graph.ReadPhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: "x"})
			return err
		},
		"create": func(ctx context.Context) error {
			_, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "x"})
			return err
		},
		"rename": func(ctx context.Context) error {
			_, err := world.graph.RenamePhotoAlbum(ctx, RenamePhotoAlbumArguments{PhotoAlbumID: "x", AlbumTitle: "y"})
			return err
		},
		"delete": func(ctx context.Context) error {
			_, err := world.graph.DeletePhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: "x"})
			return err
		},
		"add": func(ctx context.Context) error {
			_, err := world.graph.AddPhotosToAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: "x", PhotoIDs: []string{"y"}})
			return err
		},
		"remove": func(ctx context.Context) error {
			_, err := world.graph.RemovePhotosFromAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: "x", PhotoIDs: []string{"y"}})
			return err
		},
		"cover": func(ctx context.Context) error {
			_, err := world.graph.SetPhotoAlbumCover(ctx, SetPhotoAlbumCoverArguments{PhotoAlbumID: "x", PhotoID: "y"})
			return err
		},
		"suggest": func(ctx context.Context) error {
			_, err := world.graph.SuggestPhotoAlbums(ctx, SuggestPhotoAlbumsArguments{})
			return err
		},
	}
	world.ask(userId, []models.Permission{models.PermissionCalendarUse}, func(ctx context.Context) {
		for name, call := range calls {
			if err := call(ctx); err == nil {
				test.Errorf("%s without photos:use: %v", name, err)
			}
		}
	})
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Services.Photos.IsEnabled = new(false)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	world.ask(userId, []models.Permission{models.PermissionPhotosUse}, func(ctx context.Context) {
		for name, call := range calls {
			var unavailable *models.ServiceUnavailableError
			if err := call(ctx); !errorsAs(err, &unavailable) {
				test.Errorf("%s with photos switched off: %v", name, err)
			}
		}
	})
}

func TestSuggestedPhotoAlbumsAreRunsOfPhotosInNoAlbum(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("album-suggestion-owner", models.PermissionPhotosUse)
	otherId := world.person("album-suggestion-other", models.PermissionPhotosUse)
	allowed := []models.Permission{models.PermissionPhotosUse}
	start := time.Date(2031, 7, 12, 9, 0, 0, 0, time.UTC)
	var tripIds []string
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		make := func(userId, label string, takenAt time.Time, placeName string) *models.Photo {
			created, _, err := transaction.CreatePhoto(&models.Photo{UserID: userId, ClientPhotoID: label, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceIPhone, TakenAt: &takenAt, PlaceName: placeName})
			if err != nil {
				test.Fatal(err)
			}
			return created
		}
		for index := 0; index < 14; index++ {
			tripIds = append(tripIds, make(ownerId, fmt.Sprintf("trip-%d", index), start.Add(time.Duration(index)*time.Hour), "Exampleton, Exampleland").ID)
		}
		// A weekend with too few photos, and somebody else's trip.
		for index := 0; index < 3; index++ {
			make(ownerId, fmt.Sprintf("snapshot-%d", index), start.AddDate(0, 0, 30).Add(time.Duration(index)*time.Hour), "")
		}
		for index := 0; index < 14; index++ {
			make(otherId, fmt.Sprintf("theirs-%d", index), start.Add(time.Duration(index)*time.Hour), "Sampleburg, Exampleland")
		}
	})
	world.ask(ownerId, allowed, func(ctx context.Context) {
		suggestions, err := world.graph.SuggestPhotoAlbums(ctx, SuggestPhotoAlbumsArguments{})
		if err != nil || len(suggestions) != 1 {
			test.Fatalf("%+v %v", suggestions, err)
		}
		suggestion := suggestions[0]
		if suggestion.AlbumTitle != "Exampleton, Exampleland" || suggestion.PhotoCount != 14 || len(suggestion.SamplePhotoIDs) != 6 ||
			len(suggestion.PlaceNames) != 1 || !suggestion.TakenAfter.Equal(start) || !suggestion.TakenBefore.Equal(start.Add(13*time.Hour+time.Second)) {
			test.Fatalf("%+v", suggestion)
		}
		// The dates it gives list exactly the run's photos.
		listed, err := world.graph.ListPhotos(ctx, ListPhotosArguments{TakenAfter: &suggestion.TakenAfter, TakenBefore: &suggestion.TakenBefore})
		if err != nil || len(listed) != 14 {
			test.Fatalf("%d photos in the run's dates: %v", len(listed), err)
		}
		// Once the trip has an album it is no longer a suggestion.
		if _, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: suggestion.AlbumTitle, PhotoIDs: tripIds}); err != nil {
			test.Fatal(err)
		}
		if again, err := world.graph.SuggestPhotoAlbums(ctx, SuggestPhotoAlbumsArguments{}); err != nil || again == nil || len(again) != 0 {
			test.Fatalf("%+v %v", again, err)
		}
	})
	// The dates narrow what is considered.
	world.ask(otherId, allowed, func(ctx context.Context) {
		before := start
		if narrowed, err := world.graph.SuggestPhotoAlbums(ctx, SuggestPhotoAlbumsArguments{TakenBefore: &before}); err != nil || len(narrowed) != 0 {
			test.Fatalf("%+v %v", narrowed, err)
		}
		if suggestions, err := world.graph.SuggestPhotoAlbums(ctx, SuggestPhotoAlbumsArguments{}); err != nil || len(suggestions) != 1 || suggestions[0].AlbumTitle != "Sampleburg, Exampleland" {
			test.Fatalf("%+v %v", suggestions, err)
		}
	})
}

// Every document the command line and the tool send for albums is valid
// against the schema the server builds, and so are the photo documents that
// gained the album filter and the albums of a photo.
func TestPhotoAlbumClientDocumentsMatchTheBuiltSchema(test *testing.T) {
	schema := buildSchemaForValidation(test)
	for name, source := range map[string]string{
		"ListPhotoAlbums":       client.DocumentListPhotoAlbums,
		"ReadPhotoAlbum":        client.DocumentReadPhotoAlbum,
		"SuggestPhotoAlbums":    client.DocumentSuggestPhotoAlbums,
		"CreatePhotoAlbum":      client.DocumentCreatePhotoAlbum,
		"RenamePhotoAlbum":      client.DocumentRenamePhotoAlbum,
		"DeletePhotoAlbum":      client.DocumentDeletePhotoAlbum,
		"AddPhotosToAlbum":      client.DocumentAddPhotosToAlbum,
		"RemovePhotosFromAlbum": client.DocumentRemovePhotosFromAlbum,
		"SetPhotoAlbumCover":    client.DocumentSetPhotoAlbumCover,
		"ListPhotos":            client.DocumentListPhotos,
		"CountPhotos":           client.DocumentCountPhotos,
		"ReadPhoto":             client.DocumentReadPhoto,
	} {
		document, err := gqlparser.Parse(gqlparser.ParseParams{Source: source})
		if err != nil {
			test.Fatalf("%s does not parse: %s", name, err)
		}
		if validated := graphql.ValidateDocument(&schema, document, nil); !validated.IsValid {
			test.Errorf("%s does not match the built schema: %v", name, validated.Errors)
		}
	}
}
