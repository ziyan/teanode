package apigraph

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Who may read or change which photo and which album. The database decides
// (GetPhotoAccess and GetPhotoAlbumAccess are the one answer); these are the
// ways in for the operations. Every refusal is "not found", because whether a
// photo or an album exists and whose it is is not the caller's business.
//
// A photo is read by its owner, by anybody an album that holds it is shared
// with (directly, or through a circle they are in), and by every member of a
// circle that owns an album that holds it. Each reads it through the owner's
// rows: the photo, its files, its previews and its faces are fetched with the
// owner's identifier once the caller has been let in, so that the owner's
// queries stay scoped by the owner and the caller's reach is checked once, here.

// requirePhotoReader is the caller, if they may keep a photo library, with why
// they may read this photo.
func (self *graph) requirePhotoReader(ctx context.Context, photoId string) (*api.Principal, *models.PhotoAccess, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, nil, err
	}
	access, err := self.transaction(ctx).GetPhotoAccess(principal.User.ID, strings.TrimSpace(photoId))
	if err != nil {
		return nil, nil, translateError(err)
	}
	return principal, access, nil
}

// requirePhotoAlbumReader is the caller with the album they reach, at any
// level.
func (self *graph) requirePhotoAlbumReader(ctx context.Context, photoAlbumId string) (*api.Principal, *models.PhotoAlbumAccess, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, nil, err
	}
	access, err := self.transaction(ctx).GetPhotoAlbumAccess(principal.User.ID, strings.TrimSpace(photoAlbumId))
	if err != nil {
		return nil, nil, translateError(err)
	}
	return principal, access, nil
}

// requirePhotoAlbumContributor is requirePhotoAlbumReader for something only
// somebody who may add photos to the album may do: its owner, a member of the
// circle that owns it, or somebody it is shared with to contribute.
func (self *graph) requirePhotoAlbumContributor(ctx context.Context, photoAlbumId string) (*api.Principal, *models.PhotoAlbumAccess, error) {
	principal, access, err := self.requirePhotoAlbumReader(ctx, photoAlbumId)
	if err != nil {
		return nil, nil, err
	}
	if !access.CanContribute() {
		return nil, nil, api.ErrNotFound
	}
	return principal, access, nil
}

// requirePhotoAlbumManager is requirePhotoAlbumReader for something only the
// album's owner may do, or for a circle's album an admin of the circle:
// renaming it, deleting it and sharing it.
func (self *graph) requirePhotoAlbumManager(ctx context.Context, photoAlbumId string) (*api.Principal, *models.PhotoAlbumAccess, error) {
	principal, access, err := self.requirePhotoAlbumReader(ctx, photoAlbumId)
	if err != nil {
		return nil, nil, err
	}
	if !access.IsManager {
		return nil, nil, api.ErrNotFound
	}
	return principal, access, nil
}
