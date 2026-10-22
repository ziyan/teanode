package apigraph

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// A signed playback address is for the reader it was made for, and is checked
// against what that reader may open each time it is fetched: it stops when the
// share ends, and a file address stops when the share stops including places,
// though it has hours left. Another person with the same address is refused
// by the signature, which names the reader.
func TestAPlaybackAddressEndsWhenTheShareThatLetTheReaderInDoes(test *testing.T) {
	world := newPhotoWorld(test)
	adaId := world.person("playback-ada", models.PermissionPhotosUse)
	benId := world.person("playback-ben", models.PermissionPhotosUse)
	world.circleOf("Home", adaId, benId)
	original, large := []byte("the original, with its place in it"), []byte("the large preview")
	photo := world.storePhoto(adaId, "quay", "IMG_0004.HEIC", original, map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: large, models.PhotoPreviewSmall: []byte("small")})
	var albumId, shareId string
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Trip", PhotoIDs: []string{photo.ID}})
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
		share, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: albumId, GranteeUserID: benId, ShareLevel: "view", IsLocationIncluded: true})
		if err != nil {
			test.Fatal(err)
		}
		shareId = share.ID
	})
	var previewAddress, fileAddress string
	world.ask(benId, photosAllowed, func(ctx context.Context) {
		size, role := models.PhotoPreviewLarge, models.PhotoFileRoleOriginal
		var err error
		if previewAddress, err = world.graph.PhotoPlaybackAddress(ctx, PhotoPlaybackAddressArguments{PhotoID: photo.ID, PhotoPreviewSize: &size}); err != nil {
			test.Fatal(err)
		}
		if fileAddress, err = world.graph.PhotoPlaybackAddress(ctx, PhotoPlaybackAddressArguments{PhotoID: photo.ID, PhotoFileRole: &role}); err != nil {
			test.Fatal(err)
		}
	})
	if !strings.HasPrefix(fileAddress, api.PhotoPlaybackFilePath(photo.ID, "original")+"?owner="+adaId+"&reader="+benId+"&") {
		test.Fatalf("the address is %q", fileAddress)
	}
	fetch := func(address string) (int, string) {
		response := world.do(http.MethodGet, address, nil, nil)
		return response.StatusCode, readBody(test, response)
	}
	if status, body := fetch(previewAddress); status != http.StatusOK || body != string(large) {
		test.Fatalf("the preview address: %d %q", status, body)
	}
	if status, body := fetch(fileAddress); status != http.StatusOK || body != string(original) {
		test.Fatalf("the file address: %d %q", status, body)
	}
	// The signature names the reader: the same address with another reader's
	// name is not good.
	if status, _ := fetch(strings.Replace(fileAddress, "&reader="+benId, "&reader="+adaId, 1)); status != http.StatusNotFound {
		test.Fatalf("an address with another reader's name got %d", status)
	}

	// Places off: the file address ends, the preview address does not.
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		if _, err := world.graph.UpdatePhotoAlbumShare(ctx, UpdatePhotoAlbumShareArguments{PhotoAlbumShareID: shareId, ShareLevel: "view"}); err != nil {
			test.Fatal(err)
		}
	})
	if status, _ := fetch(fileAddress); status != http.StatusNotFound {
		test.Fatalf("a file address after the places were taken away got %d", status)
	}
	if status, body := fetch(previewAddress); status != http.StatusOK || body != string(large) {
		test.Fatalf("the preview address after the places were taken away: %d %q", status, body)
	}

	// The share ends: so does the preview address, with hours left on it.
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		if removed, err := world.graph.RemovePhotoAlbumShare(ctx, PhotoAlbumShareArguments{PhotoAlbumShareID: shareId}); err != nil || !removed {
			test.Fatalf("%v %v", removed, err)
		}
	})
	if status, _ := fetch(previewAddress); status != http.StatusNotFound {
		test.Fatalf("a preview address after the share was taken back got %d", status)
	}
}
