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
	photoAlbumSelection = `{ id userId albumTitle coverPhotoId createdAt updatedAt photoCount firstTakenAt lastTakenAt coverPreviewUrl }`

	photoAlbumSuggestionSelection = `{ takenAfter takenBefore placeNames albumTitle photoCount samplePhotoIds }`
)

const (
	DocumentListPhotoAlbums = `query { ListPhotoAlbums ` + photoAlbumSelection + ` }`

	DocumentReadPhotoAlbum = `query ($photoAlbumId: String!) { ReadPhotoAlbum(photoAlbumId: $photoAlbumId) ` + photoAlbumSelection + ` }`

	DocumentSuggestPhotoAlbums = `query ($takenAfter: DateTime, $takenBefore: DateTime) { SuggestPhotoAlbums(takenAfter: $takenAfter, takenBefore: $takenBefore) ` + photoAlbumSuggestionSelection + ` }`

	DocumentCreatePhotoAlbum = `mutation ($albumTitle: String!, $photoIds: [String!]) { CreatePhotoAlbum(albumTitle: $albumTitle, photoIds: $photoIds) ` + photoAlbumSelection + ` }`

	DocumentRenamePhotoAlbum = `mutation ($photoAlbumId: String!, $albumTitle: String!) { RenamePhotoAlbum(photoAlbumId: $photoAlbumId, albumTitle: $albumTitle) ` + photoAlbumSelection + ` }`

	DocumentDeletePhotoAlbum = `mutation ($photoAlbumId: String!) { DeletePhotoAlbum(photoAlbumId: $photoAlbumId) }`

	DocumentAddPhotosToAlbum = `mutation ($photoAlbumId: String!, $photoIds: [String!]!) { AddPhotosToAlbum(photoAlbumId: $photoAlbumId, photoIds: $photoIds) }`

	DocumentRemovePhotosFromAlbum = `mutation ($photoAlbumId: String!, $photoIds: [String!]!) { RemovePhotosFromAlbum(photoAlbumId: $photoAlbumId, photoIds: $photoIds) }`

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
	variables := map[string]any{"albumTitle": albumTitle}
	if len(photoIds) > 0 {
		variables["photoIds"] = photoIds
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
