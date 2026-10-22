package apigraph

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
)

// PhotoAlbumQuery reads the albums the person reaches: named sets of photos.
// They are the person's own, their circles', and those shared with them, each
// marked with whose it is and what the person may do with it. The photos of
// one album are ListPhotos with its albumId.
type PhotoAlbumQuery interface {
	ListPhotoAlbums(context.Context) ([]*models.PhotoAlbum, error)
	ReadPhotoAlbum(context.Context, PhotoAlbumArguments) (*models.PhotoAlbum, error)
	SuggestPhotoAlbums(context.Context, SuggestPhotoAlbumsArguments) ([]*models.PhotoAlbumSuggestion, error)

	// ListPhotoAlbumShares are who an album is shared with, for the album's
	// owner, or for a circle's album an admin of the circle.
	ListPhotoAlbumShares(context.Context, PhotoAlbumArguments) ([]*PhotoAlbumShareView, error)
}

// PhotoAlbumMutation changes the albums the person may change. An album holds
// references, so nothing here deletes a photo.
type PhotoAlbumMutation interface {
	CreatePhotoAlbum(context.Context, CreatePhotoAlbumArguments) (*models.PhotoAlbum, error)
	RenamePhotoAlbum(context.Context, RenamePhotoAlbumArguments) (*models.PhotoAlbum, error)
	DeletePhotoAlbum(context.Context, PhotoAlbumArguments) (bool, error)
	AddPhotosToAlbum(context.Context, PhotoAlbumPhotosArguments) (int, error)
	RemovePhotosFromAlbum(context.Context, PhotoAlbumPhotosArguments) (int, error)
	SetPhotoAlbumCover(context.Context, SetPhotoAlbumCoverArguments) (*models.PhotoAlbum, error)

	// SharePhotoAlbum gives a person or a circle a level of access to an
	// album, or changes the level they have.
	SharePhotoAlbum(context.Context, SharePhotoAlbumArguments) (*PhotoAlbumShareView, error)

	// UpdatePhotoAlbumShare changes the level of one share and whether it
	// includes where photos were taken and the names of the faces in them.
	UpdatePhotoAlbumShare(context.Context, UpdatePhotoAlbumShareArguments) (*PhotoAlbumShareView, error)

	// RemovePhotoAlbumShare takes one share back; the photos its grantees put
	// in the album leave it with their access.
	RemovePhotoAlbumShare(context.Context, PhotoAlbumShareArguments) (bool, error)
}

// PhotoAlbumArguments names one album.
type PhotoAlbumArguments struct {
	PhotoAlbumID string `json:"photoAlbumId"`
}

// CreatePhotoAlbumArguments makes an album, empty or holding photos of the
// person's, at most 500.
type CreatePhotoAlbumArguments struct {
	// AlbumTitle is from 1 to 255 characters once trimmed.
	AlbumTitle string   `json:"albumTitle"`
	PhotoIDs   []string `json:"photoIds" graphapi:"nullable"`

	// CircleID makes the album the circle's rather than the person's: every
	// member of the circle, which the person must be in, adds photos to it.
	// The photos in it stay the photos of whoever added them.
	CircleID string `json:"circleId" graphapi:"nullable"`
}

// RenamePhotoAlbumArguments gives an album another title.
type RenamePhotoAlbumArguments struct {
	PhotoAlbumID string `json:"photoAlbumId"`
	AlbumTitle   string `json:"albumTitle"`
}

// PhotoAlbumPhotosArguments names photos of the person's, at most 500, to put
// in an album or take out of it.
type PhotoAlbumPhotosArguments struct {
	PhotoAlbumID string   `json:"photoAlbumId"`
	PhotoIDs     []string `json:"photoIds"`
}

// SetPhotoAlbumCoverArguments chooses the photo an album is shown by; it must
// be one of the album's photos.
type SetPhotoAlbumCoverArguments struct {
	PhotoAlbumID string `json:"photoAlbumId"`
	PhotoID      string `json:"photoId"`
}

// SuggestPhotoAlbumsArguments narrows the photos suggestions are made from to
// those taken within the dates; both are optional.
type SuggestPhotoAlbumsArguments struct {
	TakenAfter  *time.Time `json:"takenAfter"`
	TakenBefore *time.Time `json:"takenBefore"`
}

// withCoverAddresses fills in where each album's cover picture is fetched
// from, the small preview of the photo that stands for it, addressed the way
// any photo's preview is, and who owns the album.
func withCoverAddresses(albums ...*models.PhotoAlbum) {
	for _, album := range albums {
		if album.OwnerUserID != nil {
			album.OwnerAvatarURL, album.OwnerAvatarColorIndex = avatarUrl(album.OwnerUser), avatar.InitialColorIndex(*album.OwnerUserID)
		}
		if album.CoverPreviewPhotoID == "" || album.CoverPreviewPhotoUpdatedAt == nil {
			continue
		}
		cover := &models.Photo{ID: album.CoverPreviewPhotoID, UpdatedAt: *album.CoverPreviewPhotoUpdatedAt}
		album.CoverPreviewURL = photoPreviewAddress(cover, models.PhotoPreviewSmall)
	}
}

func (self *graph) ListPhotoAlbums(ctx context.Context) ([]*models.PhotoAlbum, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	albums, err := self.transaction(ctx).ListPhotoAlbums(principal.User.ID)
	if err != nil {
		return nil, translateError(err)
	}
	withCoverAddresses(albums...)
	return albums, nil
}

func (self *graph) ReadPhotoAlbum(ctx context.Context, arguments PhotoAlbumArguments) (*models.PhotoAlbum, error) {
	principal, _, err := self.requirePhotoAlbumReader(ctx, arguments.PhotoAlbumID)
	if err != nil {
		return nil, err
	}
	album, err := self.transaction(ctx).GetPhotoAlbum(principal.User.ID, arguments.PhotoAlbumID)
	if err != nil {
		return nil, translateError(err)
	}
	withCoverAddresses(album)
	return album, nil
}

func (self *graph) CreatePhotoAlbum(ctx context.Context, arguments CreatePhotoAlbumArguments) (*models.PhotoAlbum, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	circleId := strings.TrimSpace(arguments.CircleID)
	var album *models.PhotoAlbum
	if circleId == "" {
		album, err = transaction.CreatePhotoAlbum(principal.User.ID, arguments.AlbumTitle, arguments.PhotoIDs)
	} else {
		album, err = transaction.CreateCirclePhotoAlbum(principal.User.ID, circleId, arguments.AlbumTitle, arguments.PhotoIDs)
	}
	if err != nil {
		return nil, translateError(err)
	}
	if album.PhotoCount > 0 {
		self.notifyPhotosAdded(ctx, principal.User, album, album.PhotoCount)
	}
	withCoverAddresses(album)
	return album, nil
}

func (self *graph) RenamePhotoAlbum(ctx context.Context, arguments RenamePhotoAlbumArguments) (*models.PhotoAlbum, error) {
	principal, _, err := self.requirePhotoAlbumManager(ctx, arguments.PhotoAlbumID)
	if err != nil {
		return nil, err
	}
	album, err := self.transaction(ctx).RenamePhotoAlbum(principal.User.ID, arguments.PhotoAlbumID, arguments.AlbumTitle)
	if err != nil {
		return nil, translateError(err)
	}
	withCoverAddresses(album)
	return album, nil
}

// DeletePhotoAlbum deletes the album. Its photos stay in their owners'
// libraries.
func (self *graph) DeletePhotoAlbum(ctx context.Context, arguments PhotoAlbumArguments) (bool, error) {
	principal, _, err := self.requirePhotoAlbumManager(ctx, arguments.PhotoAlbumID)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).DeletePhotoAlbum(principal.User.ID, arguments.PhotoAlbumID); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

// AddPhotosToAlbum says how many photos were put in: those already in the
// album are left alone and not counted. The photos must be the caller's own,
// whoever the album belongs to.
func (self *graph) AddPhotosToAlbum(ctx context.Context, arguments PhotoAlbumPhotosArguments) (int, error) {
	principal, _, err := self.requirePhotoAlbumContributor(ctx, arguments.PhotoAlbumID)
	if err != nil {
		return 0, err
	}
	transaction := self.transaction(ctx)
	addedCount, err := transaction.AddPhotosToAlbum(principal.User.ID, arguments.PhotoAlbumID, arguments.PhotoIDs)
	if err != nil {
		return 0, translateError(err)
	}
	if addedCount > 0 {
		album, err := transaction.GetPhotoAlbum(principal.User.ID, arguments.PhotoAlbumID)
		if err != nil {
			return 0, translateError(err)
		}
		self.notifyPhotosAdded(ctx, principal.User, album, addedCount)
	}
	return addedCount, nil
}

// RemovePhotosFromAlbum says how many photos were taken out. They stay in
// their owners' libraries. The album's manager may take out any photo, and
// somebody who contributes the photos they added.
func (self *graph) RemovePhotosFromAlbum(ctx context.Context, arguments PhotoAlbumPhotosArguments) (int, error) {
	principal, _, err := self.requirePhotoAlbumContributor(ctx, arguments.PhotoAlbumID)
	if err != nil {
		return 0, err
	}
	removedCount, err := self.transaction(ctx).RemovePhotosFromAlbum(principal.User.ID, arguments.PhotoAlbumID, arguments.PhotoIDs)
	return removedCount, translateError(err)
}

func (self *graph) SetPhotoAlbumCover(ctx context.Context, arguments SetPhotoAlbumCoverArguments) (*models.PhotoAlbum, error) {
	principal, _, err := self.requirePhotoAlbumManager(ctx, arguments.PhotoAlbumID)
	if err != nil {
		return nil, err
	}
	album, err := self.transaction(ctx).SetPhotoAlbumCover(principal.User.ID, arguments.PhotoAlbumID, arguments.PhotoID)
	if err != nil {
		return nil, translateError(err)
	}
	withCoverAddresses(album)
	return album, nil
}

// SuggestPhotoAlbums offers the trips and events in photos that are in no
// album. Nothing is created until somebody asks.
func (self *graph) SuggestPhotoAlbums(ctx context.Context, arguments SuggestPhotoAlbumsArguments) ([]*models.PhotoAlbumSuggestion, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	photos, err := self.transaction(ctx).ListPhotosInNoAlbum(principal.User.ID, arguments.TakenAfter, arguments.TakenBefore)
	if err != nil {
		return nil, translateError(err)
	}
	return photo.SuggestAlbums(photos), nil
}
