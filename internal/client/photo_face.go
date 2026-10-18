package client

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/models"
)

// People: the faces found in the library, grouped by person, and named by
// choosing a contact. The same operations as the dashboard's People page, the
// iPhone app and the agent's photo tool. The photos of a group are ListPhotos
// with PhotoFilter.FaceGroupID, and the photos of a contact with
// PhotoFilter.ContactID.

const (
	photoFaceSelection = `id photoId userId createdAt boxLeftFraction boxTopFraction boxWidthFraction boxHeightFraction detectionScore photoFaceGroupId isGroupConfirmed contactId contactName isOwnContact facePictureUrl`

	photoFaceGroupSelection = `{ id userId createdAt updatedAt contactId contactAddressBookId contactName isOwnContact faceCount photoCount facePictureUrl isHidden coverPhotoFaceId }`
)

const (
	DocumentListPhotoFaceGroups = `query ($isHiddenIncluded: Boolean) { ListPhotoFaceGroups(isHiddenIncluded: $isHiddenIncluded) ` + photoFaceGroupSelection + ` }`

	DocumentReadPhotoFaceGroup = `query ($photoFaceGroupId: String!) { ReadPhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId) ` + photoFaceGroupSelection + ` }`

	DocumentNamePhotoFaceGroup = `mutation ($photoFaceGroupId: String!, $contactId: String!) { NamePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId, contactId: $contactId) ` + photoFaceGroupSelection + ` }`

	DocumentUnnamePhotoFaceGroup = `mutation ($photoFaceGroupId: String!) { UnnamePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId) ` + photoFaceGroupSelection + ` }`

	DocumentMergePhotoFaceGroups = `mutation ($photoFaceGroupIds: [String!]!) { MergePhotoFaceGroups(photoFaceGroupIds: $photoFaceGroupIds) ` + photoFaceGroupSelection + ` }`

	DocumentRemovePhotoFaceFromGroup = `mutation ($photoFaceId: String!) { RemovePhotoFaceFromGroup(photoFaceId: $photoFaceId) ` + photoFaceGroupSelection + ` }`

	DocumentHidePhotoFaceGroup = `mutation ($photoFaceGroupId: String!, $isHidden: Boolean!) { HidePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId, isHidden: $isHidden) ` + photoFaceGroupSelection + ` }`

	DocumentSetPhotoFaceRecognition = `mutation ($isEnabled: Boolean!) { SetPhotoFaceRecognition(isEnabled: $isEnabled) }`
)

// MaximumMergedPhotoFaceGroups is how many groups one merge may name.
const MaximumMergedPhotoFaceGroups = 50

// ListPhotoFaceGroups is the people worth showing: the groups a contact names
// first, by name, then the groups of at least three faces nobody has named,
// the largest first. Hidden groups are left out unless asked for.
func ListPhotoFaceGroups(ctx context.Context, executor Executor, isHiddenIncluded bool) ([]*models.PhotoFaceGroup, error) {
	var result struct {
		ListPhotoFaceGroups []*models.PhotoFaceGroup `json:"ListPhotoFaceGroups"`
	}
	if err := executor.Execute(ctx, DocumentListPhotoFaceGroups, map[string]any{"isHiddenIncluded": isHiddenIncluded}, &result); err != nil {
		return nil, err
	}
	return result.ListPhotoFaceGroups, nil
}

// ReadPhotoFaceGroup is one group with its count and picture.
func ReadPhotoFaceGroup(ctx context.Context, executor Executor, photoFaceGroupId string) (*models.PhotoFaceGroup, error) {
	var result struct {
		ReadPhotoFaceGroup *models.PhotoFaceGroup `json:"ReadPhotoFaceGroup"`
	}
	if err := executor.Execute(ctx, DocumentReadPhotoFaceGroup, map[string]any{"photoFaceGroupId": photoFaceGroupId}, &result); err != nil {
		return nil, err
	}
	if result.ReadPhotoFaceGroup == nil {
		return nil, fmt.Errorf("%w: no group of faces %s", ErrNotFound, photoFaceGroupId)
	}
	return result.ReadPhotoFaceGroup, nil
}

// NamePhotoFaceGroup names a group by a contact from the person's address
// books.
func NamePhotoFaceGroup(ctx context.Context, executor Executor, photoFaceGroupId, contactId string) (*models.PhotoFaceGroup, error) {
	var result struct {
		NamePhotoFaceGroup *models.PhotoFaceGroup `json:"NamePhotoFaceGroup"`
	}
	if err := executor.Execute(ctx, DocumentNamePhotoFaceGroup, map[string]any{"photoFaceGroupId": photoFaceGroupId, "contactId": contactId}, &result); err != nil {
		return nil, err
	}
	return result.NamePhotoFaceGroup, nil
}

// UnnamePhotoFaceGroup takes the contact off a group. The faces stay grouped.
func UnnamePhotoFaceGroup(ctx context.Context, executor Executor, photoFaceGroupId string) (*models.PhotoFaceGroup, error) {
	var result struct {
		UnnamePhotoFaceGroup *models.PhotoFaceGroup `json:"UnnamePhotoFaceGroup"`
	}
	if err := executor.Execute(ctx, DocumentUnnamePhotoFaceGroup, map[string]any{"photoFaceGroupId": photoFaceGroupId}, &result); err != nil {
		return nil, err
	}
	return result.UnnamePhotoFaceGroup, nil
}

// MergePhotoFaceGroups moves the faces of the other groups into the first
// and deletes the others.
func MergePhotoFaceGroups(ctx context.Context, executor Executor, photoFaceGroupIds []string) (*models.PhotoFaceGroup, error) {
	var result struct {
		MergePhotoFaceGroups *models.PhotoFaceGroup `json:"MergePhotoFaceGroups"`
	}
	if err := executor.Execute(ctx, DocumentMergePhotoFaceGroups, map[string]any{"photoFaceGroupIds": photoFaceGroupIds}, &result); err != nil {
		return nil, err
	}
	return result.MergePhotoFaceGroups, nil
}

// RemovePhotoFaceFromGroup takes a face out of its group, into a group of its
// own that the person has confirmed.
func RemovePhotoFaceFromGroup(ctx context.Context, executor Executor, photoFaceId string) (*models.PhotoFaceGroup, error) {
	var result struct {
		RemovePhotoFaceFromGroup *models.PhotoFaceGroup `json:"RemovePhotoFaceFromGroup"`
	}
	if err := executor.Execute(ctx, DocumentRemovePhotoFaceFromGroup, map[string]any{"photoFaceId": photoFaceId}, &result); err != nil {
		return nil, err
	}
	return result.RemovePhotoFaceFromGroup, nil
}

// HidePhotoFaceGroup hides a group from the people listing, or shows it again.
func HidePhotoFaceGroup(ctx context.Context, executor Executor, photoFaceGroupId string, isHidden bool) (*models.PhotoFaceGroup, error) {
	var result struct {
		HidePhotoFaceGroup *models.PhotoFaceGroup `json:"HidePhotoFaceGroup"`
	}
	if err := executor.Execute(ctx, DocumentHidePhotoFaceGroup, map[string]any{"photoFaceGroupId": photoFaceGroupId, "isHidden": isHidden}, &result); err != nil {
		return nil, err
	}
	return result.HidePhotoFaceGroup, nil
}

// SetPhotoFaceRecognition switches the finding of faces in the person's
// photos on or off, and answers the new state. Off deletes every face and
// group they have.
func SetPhotoFaceRecognition(ctx context.Context, executor Executor, isEnabled bool) (bool, error) {
	var result struct {
		SetPhotoFaceRecognition bool `json:"SetPhotoFaceRecognition"`
	}
	if err := executor.Execute(ctx, DocumentSetPhotoFaceRecognition, map[string]any{"isEnabled": isEnabled}, &result); err != nil {
		return false, err
	}
	return result.SetPhotoFaceRecognition, nil
}
