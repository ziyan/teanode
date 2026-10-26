package apigraph

import (
	"context"
	"slices"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// albumTitles is the titles of the albums, in the order listed.
func albumTitles(albums []*models.PhotoAlbum) []string {
	titles := make([]string, 0, len(albums))
	for _, album := range albums {
		titles = append(titles, album.AlbumTitle)
	}
	return titles
}

// A circle's albums are its own and those shared with the whole circle, for
// every member, the owner of a shared one among them; an album shared with one
// member or with another circle is not one of them, and nobody outside the
// circle reads the list.
func TestACirclesAlbumsAreItsOwnAndThoseSharedWithIt(test *testing.T) {
	world := newPhotoWorld(test)
	adaId := world.person("circle-ada", models.PermissionPhotosUse)
	benId := world.person("circle-ben", models.PermissionPhotosUse)
	daveId := world.person("circle-dave", models.PermissionPhotosUse)
	erinId := world.person("circle-erin", models.PermissionPhotosUse)
	home := world.circleOf("Home", benId, adaId)
	friends := world.circleOf("Friends", adaId, erinId)
	world.circleOf("Club", daveId)

	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		for _, shared := range []struct {
			albumTitle string
			share      SharePhotoAlbumArguments
		}{
			{"Trip", SharePhotoAlbumArguments{GranteeCircleID: home.ID, ShareLevel: "view"}},
			{"Hike", SharePhotoAlbumArguments{GranteeUserID: benId, ShareLevel: "view"}},
			{"Lake", SharePhotoAlbumArguments{GranteeCircleID: friends.ID, ShareLevel: "contribute"}},
		} {
			album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: shared.albumTitle})
			if err != nil {
				test.Fatal(err)
			}
			shared.share.PhotoAlbumID = album.ID
			if _, err := world.graph.SharePhotoAlbum(ctx, shared.share); err != nil {
				test.Fatal(err)
			}
		}
		if _, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Notes"}); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(benId, photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Garden", CircleID: home.ID}); err != nil {
			test.Fatal(err)
		}
	})

	for _, reader := range []struct {
		userId string
		want   []string
	}{{adaId, []string{"Garden", "Trip"}}, {benId, []string{"Garden", "Trip"}}} {
		world.ask(reader.userId, photosAllowed, func(ctx context.Context) {
			albums, err := world.graph.ListPhotoAlbums(ctx, ListPhotoAlbumsArguments{CircleID: home.ID})
			if titles := albumTitles(albums); err != nil || !slices.Equal(titles, reader.want) {
				test.Fatalf("%s lists %v %v, want %v", reader.userId, titles, err, reader.want)
			}
		})
	}
	world.ask(erinId, photosAllowed, func(ctx context.Context) {
		if albums, err := world.graph.ListPhotoAlbums(ctx, ListPhotoAlbumsArguments{CircleID: friends.ID}); err != nil || !slices.Equal(albumTitles(albums), []string{"Lake"}) {
			test.Fatalf("%v %v", albumTitles(albums), err)
		}
	})
	for _, outsider := range []string{daveId, erinId} {
		world.ask(outsider, photosAllowed, func(ctx context.Context) {
			if albums, err := world.graph.ListPhotoAlbums(ctx, ListPhotoAlbumsArguments{CircleID: home.ID}); !isNotFoundError(err) {
				test.Fatalf("somebody outside the circle read %v %v", albumTitles(albums), err)
			}
		})
	}

	// Once ada has left, what she shared with the circle is no longer its.
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if _, err := transaction.RemoveCircleMember(home.ID, adaId); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(benId, photosAllowed, func(ctx context.Context) {
		if albums, err := world.graph.ListPhotoAlbums(ctx, ListPhotoAlbumsArguments{CircleID: home.ID}); err != nil || !slices.Equal(albumTitles(albums), []string{"Garden"}) {
			test.Fatalf("%v %v", albumTitles(albums), err)
		}
	})
}
