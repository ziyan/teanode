package client

import (
	"context"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// Albums: named sets of the library's photos. The same operations as the
// dashboard's Albums page, the iPhone app and the agent's photo tool. The
// photos of an album are ListPhotos with PhotoFilter.AlbumID.

const (
	photoAlbumSelection = `{ id ownerUserId circleId albumTitle coverPhotoId createdAt updatedAt photoCount firstTakenAt lastTakenAt coverPreviewUrl ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex circleName isOwnedByCaller shareLevel canCallerContribute canCallerManage isLocationIncluded }`

	photoAlbumShareSelection = `{ id photoAlbumId granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex granteeCircleId granteeCircleName shareLevel isLocationIncluded createdAt }`

	photoAlbumSuggestionSelection = `{ takenAfter takenBefore placeNames albumTitle photoCount samplePhotoIds }`
)

const (
	DocumentListPhotoAlbums = `query { ListPhotoAlbums ` + photoAlbumSelection + ` }`

	DocumentReadPhotoAlbum = `query ($photoAlbumId: String!) { ReadPhotoAlbum(photoAlbumId: $photoAlbumId) ` + photoAlbumSelection + ` }`

	DocumentSuggestPhotoAlbums = `query ($takenAfter: DateTime, $takenBefore: DateTime) { SuggestPhotoAlbums(takenAfter: $takenAfter, takenBefore: $takenBefore) ` + photoAlbumSuggestionSelection + ` }`

	DocumentCreatePhotoAlbum = `mutation ($albumTitle: String!, $photoIds: [String!], $circleId: String) { CreatePhotoAlbum(albumTitle: $albumTitle, photoIds: $photoIds, circleId: $circleId) ` + photoAlbumSelection + ` }`

	DocumentRenamePhotoAlbum = `mutation ($photoAlbumId: String!, $albumTitle: String!) { RenamePhotoAlbum(photoAlbumId: $photoAlbumId, albumTitle: $albumTitle) ` + photoAlbumSelection + ` }`

	DocumentDeletePhotoAlbum = `mutation ($photoAlbumId: String!) { DeletePhotoAlbum(photoAlbumId: $photoAlbumId) }`

	DocumentAddPhotosToAlbum = `mutation ($photoAlbumId: String!, $photoIds: [String!]!) { AddPhotosToAlbum(photoAlbumId: $photoAlbumId, photoIds: $photoIds) }`

	DocumentRemovePhotosFromAlbum = `mutation ($photoAlbumId: String!, $photoIds: [String!]!) { RemovePhotosFromAlbum(photoAlbumId: $photoAlbumId, photoIds: $photoIds) }`

	DocumentListPhotoAlbumShares = `query ($photoAlbumId: String!) { ListPhotoAlbumShares(photoAlbumId: $photoAlbumId) ` + photoAlbumShareSelection + ` }`

	DocumentSharePhotoAlbum = `mutation ($photoAlbumId: String!, $granteeUserId: String, $granteeCircleId: String, $shareLevel: String!, $isLocationIncluded: Boolean) { SharePhotoAlbum(photoAlbumId: $photoAlbumId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, shareLevel: $shareLevel, isLocationIncluded: $isLocationIncluded) ` + photoAlbumShareSelection + ` }`

	DocumentUpdatePhotoAlbumShare = `mutation ($photoAlbumShareId: String!, $shareLevel: String!, $isLocationIncluded: Boolean) { UpdatePhotoAlbumShare(photoAlbumShareId: $photoAlbumShareId, shareLevel: $shareLevel, isLocationIncluded: $isLocationIncluded) ` + photoAlbumShareSelection + ` }`

	DocumentRemovePhotoAlbumShare = `mutation ($photoAlbumShareId: String!) { RemovePhotoAlbumShare(photoAlbumShareId: $photoAlbumShareId) }`

	DocumentSetPhotoAlbumCover = `mutation ($photoAlbumId: String!, $photoId: String!) { SetPhotoAlbumCover(photoAlbumId: $photoAlbumId, photoId: $photoId) ` + photoAlbumSelection + ` }`
)

// ListPhotoAlbums is the person's albums, the most recently changed first.
func ListPhotoAlbums(ctx context.Context, executor Executor) ([]*models.PhotoAlbum, error) {
	var result struct {
		ListPhotoAlbums []*models.PhotoAlbum `json:"ListPhotoAlbums"`
	}
	if err := executor.Execute(ctx, DocumentListPhotoAlbums, nil, &result); err != nil {
		return nil, err
	}
	return result.ListPhotoAlbums, nil
}

// ReadPhotoAlbum is one album with its count, cover and dates.
func ReadPhotoAlbum(ctx context.Context, executor Executor, photoAlbumId string) (*models.PhotoAlbum, error) {
	var result struct {
		ReadPhotoAlbum *models.PhotoAlbum `json:"ReadPhotoAlbum"`
	}
	if err := executor.Execute(ctx, DocumentReadPhotoAlbum, map[string]any{"photoAlbumId": photoAlbumId}, &result); err != nil {
		return nil, err
	}
	if result.ReadPhotoAlbum == nil {
		return nil, fmt.Errorf("%w: no album %s", ErrNotFound, photoAlbumId)
	}
	return result.ReadPhotoAlbum, nil
}

// SuggestPhotoAlbums offers the trips and events among the photos that are in
// no album, newest first. The dates narrow the photos considered; both are
// optional.
func SuggestPhotoAlbums(ctx context.Context, executor Executor, takenAfter, takenBefore *time.Time) ([]*models.PhotoAlbumSuggestion, error) {
	variables := map[string]any{}
	if takenAfter != nil {
		variables["takenAfter"] = takenAfter.UTC().Format(time.RFC3339Nano)
	}
	if takenBefore != nil {
		variables["takenBefore"] = takenBefore.UTC().Format(time.RFC3339Nano)
	}
	var result struct {
		SuggestPhotoAlbums []*models.PhotoAlbumSuggestion `json:"SuggestPhotoAlbums"`
	}
	if err := executor.Execute(ctx, DocumentSuggestPhotoAlbums, variables, &result); err != nil {
		return nil, err
	}
	return result.SuggestPhotoAlbums, nil
}

// CreatePhotoAlbum makes an album holding the photos named, which may be none.
func CreatePhotoAlbum(ctx context.Context, executor Executor, albumTitle string, photoIds []string) (*models.PhotoAlbum, error) {
	return CreateCirclePhotoAlbum(ctx, executor, "", albumTitle, photoIds)
}

// CreateCirclePhotoAlbum makes an album that belongs to a circle the caller is
// in, holding the photos named, which are the caller's own and stay theirs. An
// empty circle makes the caller's own album.
func CreateCirclePhotoAlbum(ctx context.Context, executor Executor, circleId, albumTitle string, photoIds []string) (*models.PhotoAlbum, error) {
	variables := map[string]any{"albumTitle": albumTitle}
	if len(photoIds) > 0 {
		variables["photoIds"] = photoIds
	}
	if circleId != "" {
		variables["circleId"] = circleId
	}
	var result struct {
		CreatePhotoAlbum *models.PhotoAlbum `json:"CreatePhotoAlbum"`
	}
	if err := executor.Execute(ctx, DocumentCreatePhotoAlbum, variables, &result); err != nil {
		return nil, err
	}
	return result.CreatePhotoAlbum, nil
}

// RenamePhotoAlbum gives an album another title.
func RenamePhotoAlbum(ctx context.Context, executor Executor, photoAlbumId, albumTitle string) (*models.PhotoAlbum, error) {
	var result struct {
		RenamePhotoAlbum *models.PhotoAlbum `json:"RenamePhotoAlbum"`
	}
	if err := executor.Execute(ctx, DocumentRenamePhotoAlbum, map[string]any{"photoAlbumId": photoAlbumId, "albumTitle": albumTitle}, &result); err != nil {
		return nil, err
	}
	return result.RenamePhotoAlbum, nil
}

// DeletePhotoAlbum deletes an album. Its photos stay in the library.
func DeletePhotoAlbum(ctx context.Context, executor Executor, photoAlbumId string) error {
	return executor.Execute(ctx, DocumentDeletePhotoAlbum, map[string]any{"photoAlbumId": photoAlbumId}, nil)
}

// AddPhotosToAlbum puts photos in an album and says how many it added: those
// already in it are not counted.
func AddPhotosToAlbum(ctx context.Context, executor Executor, photoAlbumId string, photoIds []string) (int, error) {
	var result struct {
		AddPhotosToAlbum int `json:"AddPhotosToAlbum"`
	}
	if err := executor.Execute(ctx, DocumentAddPhotosToAlbum, map[string]any{"photoAlbumId": photoAlbumId, "photoIds": photoIds}, &result); err != nil {
		return 0, err
	}
	return result.AddPhotosToAlbum, nil
}

// RemovePhotosFromAlbum takes photos out of an album, which leaves them in the
// library, and says how many were in it.
func RemovePhotosFromAlbum(ctx context.Context, executor Executor, photoAlbumId string, photoIds []string) (int, error) {
	var result struct {
		RemovePhotosFromAlbum int `json:"RemovePhotosFromAlbum"`
	}
	if err := executor.Execute(ctx, DocumentRemovePhotosFromAlbum, map[string]any{"photoAlbumId": photoAlbumId, "photoIds": photoIds}, &result); err != nil {
		return 0, err
	}
	return result.RemovePhotosFromAlbum, nil
}

// SetPhotoAlbumCover chooses which of an album's photos stands for it.
func SetPhotoAlbumCover(ctx context.Context, executor Executor, photoAlbumId, photoId string) (*models.PhotoAlbum, error) {
	var result struct {
		SetPhotoAlbumCover *models.PhotoAlbum `json:"SetPhotoAlbumCover"`
	}
	if err := executor.Execute(ctx, DocumentSetPhotoAlbumCover, map[string]any{"photoAlbumId": photoAlbumId, "photoId": photoId}, &result); err != nil {
		return nil, err
	}
	return result.SetPhotoAlbumCover, nil
}

// PhotoAlbumShare gives one person or one circle a level of access to one
// album. Exactly one of GranteeUserID and GranteeCircleID is set.
type PhotoAlbumShare struct {
	ID           string `json:"id"`
	PhotoAlbumID string `json:"photoAlbumId"`

	GranteeUserID           *string `json:"granteeUserId"`
	GranteeUsername         string  `json:"granteeUsername"`
	GranteeName             string  `json:"granteeName"`
	GranteeAvatarURL        *string `json:"granteeAvatarUrl"`
	GranteeAvatarColorIndex int     `json:"granteeAvatarColorIndex"`
	GranteeCircleID         *string `json:"granteeCircleId"`
	GranteeCircleName       string  `json:"granteeCircleName"`

	// ShareLevel is view or contribute.
	ShareLevel         string    `json:"shareLevel"`
	IsLocationIncluded bool      `json:"isLocationIncluded"`
	CreatedAt          time.Time `json:"createdAt"`
}

// ListPhotoAlbumShares are who an album is shared with, for its manager.
func ListPhotoAlbumShares(ctx context.Context, executor Executor, photoAlbumId string) ([]*PhotoAlbumShare, error) {
	var result struct {
		ListPhotoAlbumShares []*PhotoAlbumShare `json:"ListPhotoAlbumShares"`
	}
	if err := executor.Execute(ctx, DocumentListPhotoAlbumShares, map[string]any{"photoAlbumId": photoAlbumId}, &result); err != nil {
		return nil, err
	}
	return result.ListPhotoAlbumShares, nil
}

// SharePhotoAlbum gives a person or a circle (exactly one of the two
// identifiers) a level of access to an album, or changes the level they have.
func SharePhotoAlbum(ctx context.Context, executor Executor, photoAlbumId, granteeUserId, granteeCircleId, shareLevel string, isLocationIncluded bool) (*PhotoAlbumShare, error) {
	variables := map[string]any{"photoAlbumId": photoAlbumId, "shareLevel": shareLevel, "isLocationIncluded": isLocationIncluded}
	if granteeUserId != "" {
		variables["granteeUserId"] = granteeUserId
	}
	if granteeCircleId != "" {
		variables["granteeCircleId"] = granteeCircleId
	}
	var result struct {
		SharePhotoAlbum *PhotoAlbumShare `json:"SharePhotoAlbum"`
	}
	if err := executor.Execute(ctx, DocumentSharePhotoAlbum, variables, &result); err != nil {
		return nil, err
	}
	return result.SharePhotoAlbum, nil
}

// UpdatePhotoAlbumShare changes the level of one share and whether it includes
// where photos were taken.
func UpdatePhotoAlbumShare(ctx context.Context, executor Executor, photoAlbumShareId, shareLevel string, isLocationIncluded bool) (*PhotoAlbumShare, error) {
	var result struct {
		UpdatePhotoAlbumShare *PhotoAlbumShare `json:"UpdatePhotoAlbumShare"`
	}
	if err := executor.Execute(ctx, DocumentUpdatePhotoAlbumShare, map[string]any{"photoAlbumShareId": photoAlbumShareId, "shareLevel": shareLevel, "isLocationIncluded": isLocationIncluded}, &result); err != nil {
		return nil, err
	}
	return result.UpdatePhotoAlbumShare, nil
}

// RemovePhotoAlbumShare takes one share back.
func RemovePhotoAlbumShare(ctx context.Context, executor Executor, photoAlbumShareId string) error {
	return executor.Execute(ctx, DocumentRemovePhotoAlbumShare, map[string]any{"photoAlbumShareId": photoAlbumShareId}, nil)
}
