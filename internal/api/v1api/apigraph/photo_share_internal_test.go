package apigraph

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/chat"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// A circle "home" of the people named, the first of whom is its admin.
func (self *photoWorld) circleOf(name string, userIds ...string) *models.Circle {
	self.test.Helper()
	var made *models.Circle
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		if made, err = transaction.CreateCircle(&models.Circle{Name: name, CircleKind: models.CircleKindFamily}, userIds[0]); err != nil {
			self.test.Fatal(err)
		}
		for _, userId := range userIds[1:] {
			if _, err := transaction.AddCircleMember(made.ID, userId, models.MemberRoleMember, userIds[0]); err != nil {
				self.test.Fatal(err)
			}
		}
	})
	return made
}

// givePlace says where a photo was taken, as reading its file would.
func (self *photoWorld) givePlace(photoId, placeName string) {
	self.test.Helper()
	latitude, longitude := 10.5, 20.5
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		if err := transaction.UpdatePhotoMetadata(photoId, &db.PhotoMetadata{Latitude: &latitude, Longitude: &longitude, PlaceName: placeName}); err != nil {
			self.test.Fatal(err)
		}
	})
}

func isNotFoundError(err error) bool {
	return errors.Is(err, api.ErrNotFound) || errors.Is(err, db.ErrNotFound)
}

var photosAllowed = []models.Permission{models.PermissionPhotosUse}

// A reader of a shared album sees its photos without where they were taken,
// and is served the preview in place of the original, until the share includes
// places. Everybody the album was not given to finds nothing, on every route.
func TestASharedAlbumIsReadWithoutPlacesUntilTheShareIncludesThem(test *testing.T) {
	world := newPhotoWorld(test)
	adaId := world.person("share-ada", models.PermissionPhotosUse)
	benId := world.person("share-ben", models.PermissionPhotosUse)
	daveId := world.person("share-dave", models.PermissionPhotosUse)
	world.circleOf("Home", adaId, benId)
	original, large, small := []byte("the original, with its place in it"), []byte("the large preview"), []byte("the small preview")
	photo := world.storePhoto(adaId, "harbor", "IMG_0001.HEIC", original, map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: large, models.PhotoPreviewSmall: small})
	world.givePlace(photo.ID, "Harbor Quay")
	var albumId, shareId string
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Trip", PhotoIDs: []string{photo.ID}})
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
		share, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: albumId, GranteeUserID: benId, ShareLevel: "view"})
		if err != nil || share.ShareLevel != "view" || share.IsLocationIncluded || share.GranteeUsername != "share-ben" || share.GranteeUserID == nil || *share.GranteeUserID != benId {
			test.Fatalf("%+v %v", share, err)
		}
		shareId = share.ID
		// The owner reads it all.
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view.Photo.PlaceName != "Harbor Quay" || view.Photo.Latitude == nil || !view.Photo.IsOwnedByCaller || len(view.PhotoFiles) != 1 || view.PhotoFiles[0].BlobSHA256 == "" {
			test.Fatalf("%+v %v", view, err)
		}
	})

	world.ask(benId, photosAllowed, func(ctx context.Context) {
		albums, err := world.graph.ListPhotoAlbums(ctx)
		if err != nil || len(albums) != 1 || albums[0].ID != albumId || albums[0].IsOwnedByCaller || albums[0].OwnerName != "share-ada" ||
			albums[0].ShareLevel != models.PhotoAlbumShareLevelView || albums[0].CanCallerContribute || albums[0].CanCallerManage {
			test.Fatalf("%+v %v", albums, err)
		}
		if albums[0].OwnerAvatarColorIndex != 0 && albums[0].OwnerAvatarURL != nil {
			test.Fatalf("a person with no picture has an address: %+v", albums[0])
		}
		if wantCover := api.PhotoPreviewPath(photo.ID, "small") + "?v=" + strconv.FormatInt(photo.UpdatedAt.Unix(), 10); albums[0].CoverPreviewURL != wantCover {
			test.Fatalf("the cover is %q, want %q", albums[0].CoverPreviewURL, wantCover)
		}
		listed, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId})
		if err != nil || len(listed) != 1 {
			test.Fatalf("%v %v", listed, err)
		}
		shown := listed[0]
		if shown.Latitude != nil || shown.Longitude != nil || shown.PlaceName != "" || shown.IsOwnedByCaller || shown.OwnerName != "share-ada" || shown.ClientPhotoID != "" ||
			shown.LargePreviewURL == "" || len(shown.PhotoFileRoles) != 1 || shown.PhotoFileRoles[0] != models.PhotoFileRoleOriginal {
			test.Fatalf("a shared photo comes as the reader may have it: %+v", shown)
		}
		// Searching for the place finds nothing.
		if found, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId, SearchText: "harbor"}); err != nil || len(found) != 0 {
			test.Fatalf("a place was found by its words: %v %v", found, err)
		}
		// The library is the reader's own, which holds nothing.
		if own, err := world.graph.ListPhotos(ctx, ListPhotosArguments{}); err != nil || len(own) != 0 {
			test.Fatalf("another person's photo is in the library: %v %v", own, err)
		}
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view.Photo.Latitude != nil || view.Photo.PlaceName != "" || view.Photo.OwnerName != "share-ada" {
			test.Fatalf("%+v %v", view, err)
		}
		// What the reader may download is one file, the large preview, named
		// as a picture and not as the owner named it, and with none of the
		// original's digest.
		if len(view.PhotoFiles) != 1 || view.PhotoFiles[0].ContentType != "image/jpeg" || view.PhotoFiles[0].FileSizeBytes != int64(len(large)) ||
			view.PhotoFiles[0].OriginalFilename != "photo-"+photo.ID+".jpg" || view.PhotoFiles[0].BlobSHA256 != "" || view.PhotoFiles[0].FileURL != api.PhotoFilePath(photo.ID, "original") {
			test.Fatalf("%+v", view.PhotoFiles)
		}
		if len(view.Albums) != 1 || view.Albums[0].ID != albumId || view.Faces == nil {
			test.Fatalf("%+v", view)
		}
		// An address a player can fetch is for the large preview, not the file.
		fileRole := models.PhotoFileRoleOriginal
		address, err := world.graph.PhotoPlaybackAddress(ctx, PhotoPlaybackAddressArguments{PhotoID: photo.ID, PhotoFileRole: &fileRole})
		if err != nil || !strings.HasPrefix(address, api.PhotoPlaybackPreviewPath(photo.ID, "large")+"?owner="+adaId) {
			test.Fatalf("%q %v", address, err)
		}
		response := world.do(http.MethodGet, address, nil, nil)
		if body := readBody(test, response); response.StatusCode != http.StatusOK || body != string(large) {
			test.Fatalf("the address served %d %q", response.StatusCode, body)
		}
		liveVideo := models.PhotoFileRoleLiveVideo
		if _, err := world.graph.PhotoPlaybackAddress(ctx, PhotoPlaybackAddressArguments{PhotoID: photo.ID, PhotoFileRole: &liveVideo}); !isNotFoundError(err) {
			test.Fatalf("an address for a file that has no preview to stand for it: %v", err)
		}
	})
	// The routes.
	response := world.do(http.MethodGet, api.PhotoPreviewPath(photo.ID, "small"), asSession("share-ben"), nil)
	if body := readBody(test, response); response.StatusCode != http.StatusOK || body != string(small) {
		test.Fatalf("a preview for a reader: %d %q", response.StatusCode, body)
	}
	response = world.do(http.MethodGet, api.PhotoFilePath(photo.ID, "original"), asSession("share-ben"), nil)
	body := readBody(test, response)
	if response.StatusCode != http.StatusOK || body != string(large) || !strings.Contains(response.Header.Get("Content-Disposition"), "photo-"+photo.ID+".jpg") || strings.Contains(response.Header.Get("Content-Disposition"), "IMG_0001") || response.Header.Get("Content-Type") != "image/jpeg" {
		test.Fatalf("the original for a reader who may not have it: %d %q %v", response.StatusCode, body, response.Header)
	}
	if status := world.status(http.MethodGet, api.PhotoFilePath(photo.ID, "liveVideo"), asSession("share-ben"), nil); status != http.StatusNotFound {
		test.Fatalf("a Live Photo's video for a reader who may not have it got %d", status)
	}
	response = world.do(http.MethodGet, api.PhotoFilePath(photo.ID, "original"), asSession("share-ada"), nil)
	if body := readBody(test, response); response.StatusCode != http.StatusOK || body != string(original) {
		test.Fatalf("the original for its owner: %d %q", response.StatusCode, body)
	}
	for _, path := range []string{api.PhotoPreviewPath(photo.ID, "small"), api.PhotoFilePath(photo.ID, "original")} {
		if status := world.status(http.MethodGet, path, asSession("share-dave"), nil); status != http.StatusNotFound {
			test.Fatalf("%s for somebody the album was not given to got %d", path, status)
		}
	}
	world.ask(daveId, photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID}); !isNotFoundError(err) {
			test.Fatalf("%v", err)
		}
		if _, err := world.graph.ReadPhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: albumId}); !isNotFoundError(err) {
			test.Fatalf("%v", err)
		}
		if _, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId}); !isNotFoundError(err) {
			test.Fatalf("%v", err)
		}
		if albums, err := world.graph.ListPhotoAlbums(ctx); err != nil || len(albums) != 0 {
			test.Fatalf("%v %v", albums, err)
		}
	})

	// The owner lets the reader see where the photos were taken, and the
	// reader has them: the place, and the original.
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		updated, err := world.graph.UpdatePhotoAlbumShare(ctx, UpdatePhotoAlbumShareArguments{PhotoAlbumShareID: shareId, ShareLevel: "view", IsLocationIncluded: true})
		if err != nil || !updated.IsLocationIncluded {
			test.Fatalf("%+v %v", updated, err)
		}
	})
	world.ask(benId, photosAllowed, func(ctx context.Context) {
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view.Photo.PlaceName != "Harbor Quay" || view.Photo.Latitude == nil || len(view.PhotoFiles) != 1 || view.PhotoFiles[0].OriginalFilename != "IMG_0001.HEIC" {
			test.Fatalf("%+v %v", view, err)
		}
		if found, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId, SearchText: "harbor"}); err != nil || len(found) != 1 {
			test.Fatalf("%v %v", found, err)
		}
	})
	response = world.do(http.MethodGet, api.PhotoFilePath(photo.ID, "original"), asSession("share-ben"), nil)
	if body := readBody(test, response); response.StatusCode != http.StatusOK || body != string(original) {
		test.Fatalf("the original for a reader who may have it: %d %q", response.StatusCode, body)
	}

	// Taking the share back ends all of it.
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		if removed, err := world.graph.RemovePhotoAlbumShare(ctx, PhotoAlbumShareArguments{PhotoAlbumShareID: shareId}); err != nil || !removed {
			test.Fatalf("%v %v", removed, err)
		}
	})
	if status := world.status(http.MethodGet, api.PhotoPreviewPath(photo.ID, "small"), asSession("share-ben"), nil); status != http.StatusNotFound {
		test.Fatalf("a preview after the share was taken back got %d", status)
	}
}

// Who may do what to an album, for every operation, and for the albums of two
// kinds: a person's, shared directly at two levels, and a circle's.
func TestWhoMayDoWhatToAPersonsAlbumAndACirclesAlbum(test *testing.T) {
	world := newPhotoWorld(test)
	ids := map[string]string{}
	for _, name := range []string{"owner", "viewer", "contributor", "member", "admin", "stranger", "guest"} {
		ids[name] = world.person("matrix-"+name, models.PermissionPhotosUse)
	}
	// The admin, the owner, the viewer, the contributor, a member and a guest
	// the owner shares with in the tests share the circle; the stranger
	// shares none.
	circle := world.circleOf("Home", ids["admin"], ids["owner"], ids["viewer"], ids["contributor"], ids["member"], ids["guest"])
	photoIds := map[string]string{}
	for name, id := range ids {
		photoIds[name] = world.storePhoto(id, "photo-of-"+name, name+".jpg", []byte("original of "+name), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: []byte("large of " + name)}).ID
	}
	// The photo both albums start with, which nobody takes out: the owner's
	// own photo is the one the owner adds and removes in the table below.
	seedId := world.storePhoto(ids["owner"], "seed", "seed.jpg", []byte("original of the seed"), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: []byte("large of the seed")}).ID

	var personAlbumId, circleAlbumId string
	var contributorShareId string
	world.ask(ids["owner"], photosAllowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Owner's", PhotoIDs: []string{seedId}})
		if err != nil {
			test.Fatal(err)
		}
		personAlbumId = album.ID
		if _, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: personAlbumId, GranteeUserID: ids["viewer"], ShareLevel: "view"}); err != nil {
			test.Fatal(err)
		}
		share, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: personAlbumId, GranteeUserID: ids["contributor"], ShareLevel: "contribute"})
		if err != nil {
			test.Fatal(err)
		}
		contributorShareId = share.ID
		// A circle's album, made by the owner with a circle he is in; a
		// person who is not in the circle cannot make one.
		circleAlbum, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Home's", CircleID: circle.ID, PhotoIDs: []string{seedId}})
		if err != nil || circleAlbum.CircleID == nil || *circleAlbum.CircleID != circle.ID || circleAlbum.OwnerUserID != nil {
			test.Fatalf("%+v %v", circleAlbum, err)
		}
		circleAlbumId = circleAlbum.ID
	})
	world.ask(ids["stranger"], photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Not mine", CircleID: circle.ID}); !isNotFoundError(err) {
			test.Fatalf("somebody not in the circle made its album: %v", err)
		}
	})

	type outcome struct{ read, list, rename, add, remove, cover, share, shares bool }
	// What each person may do to the owner's album: read it, list its
	// photos, rename it, add their own photo, remove one, choose its cover,
	// share it, list its shares and delete it.
	personAlbum := map[string]outcome{
		"owner":       {read: true, list: true, rename: true, add: true, remove: true, cover: true, share: true, shares: true},
		"viewer":      {read: true, list: true},
		"contributor": {read: true, list: true, add: true, remove: true},
		"member":      {},
		"admin":       {},
		"stranger":    {},
	}
	// And to the circle's album, made by the owner: every member reads and
	// adds, the admin manages, nobody shares it.
	circleAlbum := map[string]outcome{
		"owner":       {read: true, list: true, add: true, remove: true},
		"viewer":      {read: true, list: true, add: true, remove: true},
		"contributor": {read: true, list: true, add: true, remove: true},
		"member":      {read: true, list: true, add: true, remove: true},
		"admin":       {read: true, list: true, rename: true, add: true, remove: true, cover: true, shares: true},
		"stranger":    {},
	}
	check := func(albumName, albumId string, expected map[string]outcome) {
		for _, name := range []string{"owner", "viewer", "contributor", "member", "admin", "stranger"} {
			want := expected[name]
			prefix := albumName + " as " + name + ": "
			world.ask(ids[name], photosAllowed, func(ctx context.Context) {
				expect := func(operation string, isAllowed bool, err error) {
					test.Helper()
					switch {
					case isAllowed && err != nil:
						test.Errorf("%s%s was refused: %v", prefix, operation, err)
					case !isAllowed && !isNotFoundError(err):
						test.Errorf("%s%s was not refused as not found: %v", prefix, operation, err)
					}
				}
				_, err := world.graph.ReadPhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: albumId})
				expect("read", want.read, err)
				_, err = world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId})
				expect("list the photos", want.list, err)
				_, err = world.graph.CountPhotos(ctx, ListPhotosArguments{AlbumID: albumId})
				expect("count the photos", want.list, err)
				if want.read {
					view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: seedId})
					expect("read the album's photo", true, err)
					if err == nil && name != "owner" && (view.Photo.PlaceName != "" || view.Photo.Latitude != nil) {
						test.Errorf("%sread the owner's photo with its place: %+v", prefix, view.Photo)
					}
				}
				_, err = world.graph.RenamePhotoAlbum(ctx, RenamePhotoAlbumArguments{PhotoAlbumID: albumId, AlbumTitle: "Renamed by " + name})
				expect("rename", want.rename, err)
				_, err = world.graph.SetPhotoAlbumCover(ctx, SetPhotoAlbumCoverArguments{PhotoAlbumID: albumId, PhotoID: seedId})
				expect("choose the cover", want.cover, err)
				_, err = world.graph.ListPhotoAlbumShares(ctx, PhotoAlbumArguments{PhotoAlbumID: albumId})
				expect("list the shares", want.shares, err)
				// Adding and removing the person's own photo.
				_, err = world.graph.AddPhotosToAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: albumId, PhotoIDs: []string{photoIds[name]}})
				expect("add their photo", want.add, err)
				_, err = world.graph.RemovePhotosFromAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: albumId, PhotoIDs: []string{photoIds[name]}})
				expect("remove their photo", want.remove, err)
				// Somebody else's photo goes into no album.
				otherPhotoId := photoIds["stranger"]
				if name == "stranger" {
					otherPhotoId = photoIds["owner"]
				}
				_, err = world.graph.AddPhotosToAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: albumId, PhotoIDs: []string{otherPhotoId}})
				expect("add somebody else's photo", false, err)
				_, err = world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: albumId, GranteeUserID: ids["guest"], ShareLevel: "view"})
				if albumName == "the circle's album" && name == "admin" {
					// The admin manages the circle's album, which is not shared
					// further, and says why instead of "not found".
					if err == nil || !strings.Contains(err.Error(), "belongs to every member") {
						test.Errorf("%sshared a circle's album: %v", prefix, err)
					}
				} else {
					expect("share", want.share, err)
				}
			})
		}
	}
	world.ask(ids["stranger"], photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: seedId}); !isNotFoundError(err) {
			test.Fatalf("a photo in albums the stranger reaches none of: %v", err)
		}
	})
	check("the owner's album", personAlbumId, personAlbum)
	check("the circle's album", circleAlbumId, circleAlbum)

	// The owner takes out a photo a contributor added; the contributor cannot
	// take out the owner's.
	world.ask(ids["contributor"], photosAllowed, func(ctx context.Context) {
		if added, err := world.graph.AddPhotosToAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: personAlbumId, PhotoIDs: []string{photoIds["contributor"]}}); err != nil || added != 1 {
			test.Fatalf("%d %v", added, err)
		}
		if removed, err := world.graph.RemovePhotosFromAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: personAlbumId, PhotoIDs: []string{seedId}}); err != nil || removed != 0 {
			test.Fatalf("a contributor took out the owner's photo: %d %v", removed, err)
		}
	})
	world.ask(ids["owner"], photosAllowed, func(ctx context.Context) {
		// A shared album's photos are listed with their owners.
		listed, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: personAlbumId})
		if err != nil || len(listed) != 2 {
			test.Fatalf("%v %v", listed, err)
		}
		for _, shown := range listed {
			isContributors := shown.ID == photoIds["contributor"]
			if shown.IsOwnedByCaller == isContributors || (isContributors && shown.OwnerName != "matrix-contributor") {
				test.Fatalf("%+v", shown)
			}
		}
		if removed, err := world.graph.RemovePhotosFromAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: personAlbumId, PhotoIDs: []string{photoIds["contributor"]}}); err != nil || removed != 1 {
			test.Fatalf("the owner took out a contributor's photo: %d %v", removed, err)
		}
		// Changing and removing shares by their identifier is the owner's.
		if _, err := world.graph.UpdatePhotoAlbumShare(ctx, UpdatePhotoAlbumShareArguments{PhotoAlbumShareID: contributorShareId, ShareLevel: "view"}); err != nil {
			test.Fatal(err)
		}
	})
	for _, name := range []string{"contributor", "viewer", "stranger"} {
		world.ask(ids[name], photosAllowed, func(ctx context.Context) {
			if _, err := world.graph.UpdatePhotoAlbumShare(ctx, UpdatePhotoAlbumShareArguments{PhotoAlbumShareID: contributorShareId, ShareLevel: "contribute"}); !isNotFoundError(err) {
				test.Errorf("%s changed a share: %v", name, err)
			}
			if _, err := world.graph.RemovePhotoAlbumShare(ctx, PhotoAlbumShareArguments{PhotoAlbumShareID: contributorShareId}); !isNotFoundError(err) {
				test.Errorf("%s removed a share: %v", name, err)
			}
		})
	}
	// Renaming and deleting, last: the admin of the circle's album, never a
	// member, and the owner of the person's.
	world.ask(ids["member"], photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.DeletePhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: circleAlbumId}); !isNotFoundError(err) {
			test.Fatalf("%v", err)
		}
	})
	world.ask(ids["admin"], photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.DeletePhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: personAlbumId}); !isNotFoundError(err) {
			test.Fatalf("an admin of a circle deleted a person's album: %v", err)
		}
		if deleted, err := world.graph.DeletePhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: circleAlbumId}); err != nil || !deleted {
			test.Fatalf("%v %v", deleted, err)
		}
	})
	world.ask(ids["owner"], photosAllowed, func(ctx context.Context) {
		if deleted, err := world.graph.DeletePhotoAlbum(ctx, PhotoAlbumArguments{PhotoAlbumID: personAlbumId}); err != nil || !deleted {
			test.Fatalf("%v %v", deleted, err)
		}
	})
}

// The faces in a shared photo are the owner's, with the names the owner gave,
// and a picture of a face is served to whoever reads the photo.
func TestTheFacesOfASharedPhotoAreTheOwnersAndTheirPicturesAreServed(test *testing.T) {
	world := newPhotoWorld(test)
	adaId := world.person("faces-ada", models.PermissionPhotosUse)
	benId := world.person("faces-ben", models.PermissionPhotosUse)
	world.person("faces-dave", models.PermissionPhotosUse)
	world.circleOf("Home", adaId, benId)
	var jpegBuffer bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for column := 0; column < 100; column++ {
		for row := 0; row < 100; row++ {
			picture.Set(column, row, color.RGBA{R: uint8(column), G: uint8(row), B: 128, A: 255})
		}
	}
	if err := jpeg.Encode(&jpegBuffer, picture, nil); err != nil {
		test.Fatal(err)
	}
	photo := world.storePhoto(adaId, "group", "group.jpg", []byte("original"), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: jpegBuffer.Bytes()})
	var faceId string
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		stored, err := transaction.StorePhotoFaces(adaId, photo.ID, []*models.PhotoFace{{
			BoxLeftFraction: 0.1, BoxTopFraction: 0.2, BoxWidthFraction: 0.3, BoxHeightFraction: 0.3, DetectionScore: 0.9, FaceVector: unitVector(),
		}})
		if err != nil || len(stored) != 1 {
			test.Fatalf("%v %v", stored, err)
		}
		faceId = stored[0].ID
	})
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Group", PhotoIDs: []string{photo.ID}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: album.ID, GranteeUserID: benId, ShareLevel: "view"}); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(benId, photosAllowed, func(ctx context.Context) {
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || len(view.Faces) != 1 || view.Faces[0].ID != faceId || view.Faces[0].FacePictureURL != api.PhotoFacePicturePath(faceId) {
			test.Fatalf("%+v %v", view, err)
		}
	})
	// The picture is cut from the large preview for the owner and for a reader
	// of the photo, and found by nobody else.
	for username, want := range map[string]int{"faces-ada": http.StatusOK, "faces-ben": http.StatusOK, "faces-dave": http.StatusNotFound} {
		response := world.do(http.MethodGet, api.PhotoFacePicturePath(faceId), asSession(username), nil)
		_ = response.Body.Close()
		if response.StatusCode != want {
			test.Fatalf("%s got %d for a face picture, want %d", username, response.StatusCode, want)
		}
	}
}

// unitVector is a face vector of unit length, as the reader gives.
func unitVector() []float32 {
	vector := make([]float32, 128)
	vector[0] = 1
	return vector
}

// Saving a photo of a shared album makes a photo of the reader's own over the
// same blob when the share includes places, so that nothing new is stored, and
// the preview the reader could see when it does not.
func TestSavingASharedPhotoToTheLibrary(test *testing.T) {
	world := newPhotoWorld(test)
	adaId := world.person("save-ada", models.PermissionPhotosUse)
	benId := world.person("save-ben", models.PermissionPhotosUse)
	daveId := world.person("save-dave", models.PermissionPhotosUse)
	world.circleOf("Home", adaId, benId)
	original, large := []byte("the original, with its place in it"), []byte("the large preview without one")
	photo := world.storePhoto(adaId, "harbor", "IMG_0002.HEIC", original, map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: large, models.PhotoPreviewSmall: []byte("small")})
	world.givePlace(photo.ID, "Harbor Quay")
	var originalDigest string
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		files, err := transaction.ListPhotoFiles(adaId, []string{photo.ID})
		if err != nil || len(files) != 1 {
			test.Fatalf("%v %v", files, err)
		}
		originalDigest = files[0].BlobSHA256
	})
	var shareId string
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Trip", PhotoIDs: []string{photo.ID}})
		if err != nil {
			test.Fatal(err)
		}
		share, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: album.ID, GranteeUserID: benId, ShareLevel: "view"})
		if err != nil {
			test.Fatal(err)
		}
		shareId = share.ID
		if _, err := world.graph.SavePhotoToLibrary(ctx, PhotoArguments{PhotoID: photo.ID}); err == nil || !strings.Contains(err.Error(), "already") {
			test.Fatalf("saving one's own photo: %v", err)
		}
	})
	blobCount := func() string {
		return dbtest.QueryString(test, world.database, `SELECT count(*) FROM photo_blob`)
	}

	// Without places: a copy of the preview, a new blob of the reader's.
	before := blobCount()
	var savedWithout *models.Photo
	world.ask(benId, photosAllowed, func(ctx context.Context) {
		var err error
		if savedWithout, err = world.graph.SavePhotoToLibrary(ctx, PhotoArguments{PhotoID: photo.ID}); err != nil {
			test.Fatal(err)
		}
		if savedWithout.UserID != benId || savedWithout.PhotoSource != models.PhotoSourceShared || savedWithout.ClientPhotoID != "shared-preview:"+photo.ID ||
			savedWithout.Latitude != nil || savedWithout.PlaceName != "" || savedWithout.OriginalFilename != "photo-"+photo.ID+".jpg" || !savedWithout.IsOwnedByCaller ||
			savedWithout.PhotoProcessingState != models.PhotoProcessingWaiting {
			test.Fatalf("%+v", savedWithout)
		}
		// The copy is not named by the digest of the original, which the reader
		// was kept from.
		if strings.Contains(savedWithout.ClientPhotoID, originalDigest) {
			test.Fatalf("the copy is named by the original's digest: %q", savedWithout.ClientPhotoID)
		}
		again, err := world.graph.SavePhotoToLibrary(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || again.ID != savedWithout.ID {
			test.Fatalf("saving twice is saving once: %+v %v", again, err)
		}
		files, err := world.graph.transaction(ctx).ListPhotoFiles(benId, []string{savedWithout.ID})
		if err != nil || len(files) != 1 || files[0].FileSizeBytes != int64(len(large)) || files[0].BlobSHA256 == originalDigest || files[0].ContentType != "image/jpeg" {
			test.Fatalf("the copy holds the preview, not the original: %+v %v", files, err)
		}
		used, err := world.graph.transaction(ctx).PhotoUsedBytes(benId)
		if err != nil || used != int64(len(large)) {
			test.Fatalf("the copy counts against the reader's space: %d %v", used, err)
		}
	})
	if after, want := blobCount(), strconv.Itoa(mustAtoi(test, before)+1); after != want {
		test.Fatalf("a copy of the preview is one new blob: %s blobs, want %s", after, want)
	}
	stored, err := world.files.GetFile(test.Context(), models.PhotoBlobFileID(blobDigestOf(test, world, savedWithout.ID)))
	if err != nil || string(stored) != string(large) {
		test.Fatalf("the saved bytes are %q %v", stored, err)
	}

	// With places: the same blob, nothing new stored, and the place.
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.UpdatePhotoAlbumShare(ctx, UpdatePhotoAlbumShareArguments{PhotoAlbumShareID: shareId, ShareLevel: "view", IsLocationIncluded: true}); err != nil {
			test.Fatal(err)
		}
	})
	world.circleOf("Club", adaId, daveId)
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Second", PhotoIDs: []string{photo.ID}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: album.ID, GranteeUserID: daveId, ShareLevel: "view", IsLocationIncluded: true}); err != nil {
			test.Fatal(err)
		}
	})
	before = blobCount()
	world.ask(daveId, photosAllowed, func(ctx context.Context) {
		saved, err := world.graph.SavePhotoToLibrary(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || saved.UserID != daveId || saved.ClientPhotoID != "shared:"+originalDigest || saved.Latitude == nil || saved.PlaceName != "Harbor Quay" {
			test.Fatalf("%+v %v", saved, err)
		}
		files, err := world.graph.transaction(ctx).ListPhotoFiles(daveId, []string{saved.ID})
		if err != nil || len(files) != 1 || files[0].BlobSHA256 != originalDigest || files[0].OriginalFilename != "IMG_0002.HEIC" {
			test.Fatalf("the copy names the owner's blob: %+v %v", files, err)
		}
		used, err := world.graph.transaction(ctx).PhotoUsedBytes(daveId)
		if err != nil || used != int64(len(original)) {
			test.Fatalf("the copy counts against the reader's space: %d %v", used, err)
		}
	})
	if after := blobCount(); after != before {
		test.Fatalf("saving with places stored something new: %s blobs, was %s", after, before)
	}
	// Somebody the album was not given to saves nothing.
	strangerId := world.person("save-stranger", models.PermissionPhotosUse)
	world.ask(strangerId, photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.SavePhotoToLibrary(ctx, PhotoArguments{PhotoID: photo.ID}); !isNotFoundError(err) {
			test.Fatalf("%v", err)
		}
	})

	// The reader's space is theirs: a copy that does not fit is refused.
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Photos.MaxAccountBytes = 10
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	fullId := world.person("save-full", models.PermissionPhotosUse)
	world.circleOf("Full", adaId, fullId)
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Third", PhotoIDs: []string{photo.ID}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: album.ID, GranteeUserID: fullId, ShareLevel: "view", IsLocationIncluded: true}); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(fullId, photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.SavePhotoToLibrary(ctx, PhotoArguments{PhotoID: photo.ID}); err == nil || !strings.Contains(err.Error(), "limited") {
			test.Fatalf("a copy over the reader's limit: %v", err)
		}
	})
}

func mustAtoi(test *testing.T, text string) int {
	test.Helper()
	value, err := strconv.Atoi(text)
	if err != nil {
		test.Fatal(err)
	}
	return value
}

// blobDigestOf is the digest of the first file of a photo.
func blobDigestOf(test *testing.T, world *photoWorld, photoId string) string {
	test.Helper()
	return dbtest.QueryString(test, world.database, `SELECT blob_sha256 FROM photo_file WHERE photo_id = '`+photoId+`' LIMIT 1`)
}

// Photos added to an album other people read are announced to their phones, once
// per album every ten minutes.
func TestPhotosAddedToASharedAlbumAreAnnouncedOncePerTenMinutes(test *testing.T) {
	world := newPhotoWorld(test)
	adaId := world.person("notice-ada", models.PermissionPhotosUse)
	benId := world.person("notice-ben", models.PermissionPhotosUse)
	coraId := world.person("notice-cora", models.PermissionPhotosUse)
	noPhotosId := world.person("notice-nophotos", models.PermissionCalendarUse)
	world.circleOf("Home", adaId, benId, coraId, noPhotosId)

	configuration := config.Default()
	configuration.Server.Secret = "a secret for sealing in this test"
	relay := &chatRelay{}
	hub := chat.New(&chat.Settings{
		Database: world.database, Configuration: func() *config.Configuration { return configuration }, Instance: "photo-test",
		Phones: agent.NewPhoneNotifier(&agent.PhoneNotifierSettings{
			Database: world.database, Configuration: func() *config.Configuration { return configuration },
			Context: context.Background(), WaitGroup: &sync.WaitGroup{}, Sender: relay,
		}),
	})
	world.graph.settings = &api.Settings{Chat: hub}

	// Ben and the user with no photo library have phones; Cora has none.
	keys := map[string][]byte{}
	for _, userId := range []string{benId, noPhotosId, adaId} {
		key := make([]byte, push.PhoneKeyBytes)
		_, _ = rand.Read(key)
		keys[userId] = key
		sealedKey, err := agent.SealConnectionSecret(configuration, base64.StdEncoding.EncodeToString(key))
		if err != nil {
			test.Fatal(err)
		}
		dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
			person, err := transaction.GetAgentByUser(userId)
			if err != nil {
				test.Fatal(err)
			}
			if person == nil {
				if person, err = transaction.CreateAgent(&models.Agent{UserID: userId}); err != nil {
					test.Fatal(err)
				}
			}
			if _, err := transaction.PutAgentPhone(&models.AgentPhone{AgentID: person.ID, ClientID: "phone-" + strconv.Itoa(len(keys)), PhoneName: "A test phone", PushRegistration: "sealed:a-registration", PayloadKey: sealedKey}); err != nil {
				test.Fatal(err)
			}
		})
		// A phone is reached while the token it signed in with is good.
		if _, err := world.database.CreateAuthorizedToken(&models.Token{
			ID: "token-" + strconv.Itoa(len(keys)), UserID: userId, Name: "A test phone", ClientID: "phone-" + strconv.Itoa(len(keys)), ExpiresAt: time.Now().Add(time.Hour),
		}, "key-hash-"+strconv.Itoa(len(keys)), "refresh-hash-"+strconv.Itoa(len(keys))); err != nil {
			test.Fatal(err)
		}
	}
	photos := []*models.Photo{}
	for index := 0; index < 3; index++ {
		photos = append(photos, world.storePhoto(adaId, "photo-"+strconv.Itoa(index), "p.jpg", []byte("original "+strconv.Itoa(index)), nil))
	}
	var albumId string
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Family", CircleID: world.circleIdOf("Home")})
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
	})
	add := func(photoIds ...string) {
		world.ask(adaId, photosAllowed, func(ctx context.Context) {
			if _, err := world.graph.AddPhotosToAlbum(ctx, PhotoAlbumPhotosArguments{PhotoAlbumID: albumId, PhotoIDs: photoIds}); err != nil {
				test.Fatal(err)
			}
		})
	}
	add(photos[0].ID, photos[1].ID)
	sent := relay.settled(1)
	if len(sent) != 1 {
		test.Fatalf("%d notifications went out, want one, to Ben alone (Cora has no phone, the adder is not told, and a person who may not keep photos could not open it)", len(sent))
	}
	content, err := push.DecryptContent(keys[benId], sent[0].Sealed)
	if err != nil {
		test.Fatalf("the phone cannot read its notification: %s", err)
	}
	if content.NotificationKind != models.NotificationPhoto || content.Body != "notice-ada added 2 photos to Family" || content.ThreadID != "photo-album-"+albumId {
		test.Fatalf("%+v", content)
	}
	// Another photo within the ten minutes is not announced again.
	add(photos[2].ID)
	if sent := relay.settled(2); len(sent) != 1 {
		test.Fatalf("a second notification within ten minutes: %d in all", len(sent))
	}
	// And the interval is ten minutes.
	var notices photoAddNoticeCache
	now := time.Now()
	if !notices.take("album", "reader", now) || notices.take("album", "reader", now.Add(photoAddNoticeInterval-time.Second)) || !notices.take("album", "reader", now.Add(photoAddNoticeInterval)) {
		test.Fatal("a reader is told once an interval")
	}
	if !notices.take("album", "another", now) || !notices.take("other album", "reader", now) {
		test.Fatal("the interval is per reader and per album")
	}
}

// circleIdOf is the identifier of a circle made with circleOf, by name.
func (self *photoWorld) circleIdOf(name string) string {
	self.test.Helper()
	return dbtest.QueryString(self.test, self.database, `SELECT id FROM circle WHERE name = '`+name+`'`)
}

// Where a photo was taken can be given away by more than its place: the name
// of its file, what a model wrote it shows, the words read in it, and the title
// of an album of the owner's it is also in. A reader whose share leaves places
// out gets none of those, in what they read, in what they search, and in the
// name of the file they download; the share that includes places gets all of it.
func TestAReaderWithoutPlacesGetsNoTextThatCouldNameThePlace(test *testing.T) {
	world := newPhotoWorld(test)
	adaId := world.person("text-ada", models.PermissionPhotosUse)
	benId := world.person("text-ben", models.PermissionPhotosUse)
	world.circleOf("Home", adaId, benId)
	photo := world.storePhoto(adaId, "lighthouse", "Quayside_Lighthouse.HEIC", []byte("the original"), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: []byte("large"), models.PhotoPreviewSmall: []byte("small")})
	dbtest.Exec(test, world.database, "UPDATE photo SET original_filename = 'Quayside_Lighthouse.HEIC' WHERE id = '"+photo.ID+"'")
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if err := transaction.UpdatePhotoMetadata(photo.ID, &db.PhotoMetadata{CameraMake: "Example Camera Co", CameraModel: "Model 7"}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SetPhotoDescription(adaId, photo.ID, &db.PhotoDescription{PhotoDescription: "A red lighthouse on the Quayside.", VisibleText: "WELCOME TO QUAYSIDE", ModelName: "p:thinker"}); err != nil {
			test.Fatal(err)
		}
	})
	var albumId, shareId string
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		// An album of the owner's own, never shared, with a title that says where.
		if _, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Honeymoon Zanzibar", PhotoIDs: []string{photo.ID}}); err != nil {
			test.Fatal(err)
		}
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Weekend", PhotoIDs: []string{photo.ID}})
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
		share, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: albumId, GranteeUserID: benId, ShareLevel: "view"})
		if err != nil {
			test.Fatal(err)
		}
		shareId = share.ID
	})

	world.ask(benId, photosAllowed, func(ctx context.Context) {
		listed, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId})
		if err != nil || len(listed) != 1 {
			test.Fatalf("%v %v", listed, err)
		}
		shown := listed[0]
		if shown.OriginalFilename != "" || shown.PhotoDescription != "" || shown.DescriptionVisibleText != "" || shown.DescribedAt != nil || shown.IsDescriptionRequested {
			test.Fatalf("a listing gave text away: %+v", shown)
		}
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view.Photo.OriginalFilename != "" || view.Photo.PhotoDescription != "" || view.Photo.DescriptionVisibleText != "" || view.Photo.DescribedAt != nil {
			test.Fatalf("a read gave text away: %+v %v", view, err)
		}
		for _, file := range view.PhotoFiles {
			if strings.Contains(file.OriginalFilename, "Quayside") {
				test.Fatalf("the file is named %q", file.OriginalFilename)
			}
		}
		// Nothing that names the place finds the photo: not its description, the
		// words read in it, its file name or the title of the owner's album.
		for _, words := range []string{"lighthouse", "quayside", "welcome", "Quayside_Lighthouse", "Quay", "Zanzibar", "honeymoon"} {
			if found, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId, SearchText: words}); err != nil || len(found) != 0 {
				test.Fatalf("searching for %q found %v %v", words, found, err)
			}
			if count, err := world.graph.CountPhotos(ctx, ListPhotosArguments{AlbumID: albumId, SearchText: words}); err != nil || count != 0 {
				test.Fatalf("counting %q found %d %v", words, count, err)
			}
		}
		// What cannot name a place still finds it: the camera, and the title of
		// the album the reader reached the photo by.
		for _, words := range []string{"Example Camera", "Weekend"} {
			if found, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId, SearchText: words}); err != nil || len(found) != 1 {
				test.Fatalf("%q: %v %v", words, found, err)
			}
		}
	})
	response := world.do(http.MethodGet, api.PhotoFilePath(photo.ID, "original"), asSession("text-ben"), nil)
	_ = readBody(test, response)
	if disposition := response.Header.Get("Content-Disposition"); response.StatusCode != http.StatusOK || strings.Contains(disposition, "Quayside") {
		test.Fatalf("the download is named %q (%d)", disposition, response.StatusCode)
	}

	// A copy the reader keeps does not carry the owner's name for it.
	world.ask(benId, photosAllowed, func(ctx context.Context) {
		saved, err := world.graph.SavePhotoToLibrary(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || strings.Contains(saved.OriginalFilename, "Quayside") || saved.PhotoDescription != "" {
			test.Fatalf("%+v %v", saved, err)
		}
	})

	// With places included the reader has all of it.
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.UpdatePhotoAlbumShare(ctx, UpdatePhotoAlbumShareArguments{PhotoAlbumShareID: shareId, ShareLevel: "view", IsLocationIncluded: true}); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(benId, photosAllowed, func(ctx context.Context) {
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view.Photo.OriginalFilename != "Quayside_Lighthouse.HEIC" || view.Photo.PhotoDescription != "A red lighthouse on the Quayside." || view.Photo.DescriptionVisibleText != "WELCOME TO QUAYSIDE" {
			test.Fatalf("%+v %v", view, err)
		}
		if found, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId, SearchText: "quayside"}); err != nil || len(found) != 1 {
			test.Fatalf("%v %v", found, err)
		}
	})
}

// The offset from UTC that a photo was taken at says which part of the world it
// was taken in, so it goes with the place: a reader whose share does not
// include places gets the instant and no offset, in the listing, in the photo
// and in the copy they keep, and has it once the share includes places.
func TestTheOffsetFromUTCIsHeldBackWithThePlace(test *testing.T) {
	world := newPhotoWorld(test)
	adaId := world.person("offset-ada", models.PermissionPhotosUse)
	benId := world.person("offset-ben", models.PermissionPhotosUse)
	world.circleOf("Home", adaId, benId)
	photo := world.storePhoto(adaId, "quay", "IMG_0003.HEIC", []byte("original"), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: []byte("large"), models.PhotoPreviewSmall: []byte("small")})
	dbtest.Exec(test, world.database, `UPDATE photo SET taken_at = '2030-05-01T10:00:00Z', taken_at_offset_minutes = 330 WHERE id = '`+photo.ID+`'`)
	var albumId, shareId string
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Trip", PhotoIDs: []string{photo.ID}})
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
		share, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: albumId, GranteeUserID: benId, ShareLevel: "view"})
		if err != nil {
			test.Fatal(err)
		}
		shareId = share.ID
		if view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID}); err != nil || view.Photo.TakenAtOffsetMinutes == nil || *view.Photo.TakenAtOffsetMinutes != 330 {
			test.Fatalf("the owner keeps her offset: %+v %v", view, err)
		}
	})
	world.ask(benId, photosAllowed, func(ctx context.Context) {
		listed, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AlbumID: albumId})
		if err != nil || len(listed) != 1 || listed[0].TakenAt == nil || listed[0].TakenAtOffsetMinutes != nil {
			test.Fatalf("a listing shows the instant and no offset: %+v %v", listed, err)
		}
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view.Photo.TakenAt == nil || view.Photo.TakenAtOffsetMinutes != nil {
			test.Fatalf("a photo shows the instant and no offset: %+v %v", view, err)
		}
		saved, err := world.graph.SavePhotoToLibrary(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || saved.TakenAtOffsetMinutes != nil {
			test.Fatalf("the copy keeps the offset the reader was not given: %+v %v", saved, err)
		}
	})
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.UpdatePhotoAlbumShare(ctx, UpdatePhotoAlbumShareArguments{PhotoAlbumShareID: shareId, ShareLevel: "view", IsLocationIncluded: true}); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(benId, photosAllowed, func(ctx context.Context) {
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view.Photo.TakenAtOffsetMinutes == nil || *view.Photo.TakenAtOffsetMinutes != 330 {
			test.Fatalf("with places the reader has the offset: %+v %v", view, err)
		}
	})
}
