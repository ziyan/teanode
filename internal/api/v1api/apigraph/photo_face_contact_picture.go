package apigraph

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ziyan/teanode/internal/addressbook"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
	// Aliased: the package has a test helper of its own named picture.
	photopicture "github.com/ziyan/teanode/internal/picture"
	"github.com/ziyan/teanode/internal/storage"
)

// giveContactPictureFromGroup gives a contact that has no picture one cut from
// the best face of the group that now names it, and says whether it did. A
// picture the contact has is never replaced. Only the person's own photos are
// read: a group is the owner's, and so are the faces in it.
//
// Naming is what the person asked for and a picture is a courtesy to it, so
// nothing that goes wrong with the picture undoes the naming: a photo that
// cannot be read or decoded, a card too large to take a picture, an address
// book that is full, or a database error while the card is written. The write
// runs in a savepoint of its own, which a failure rolls back, so the
// transaction the naming is in carries on. Only a context that has stopped
// ends the request, because nothing after it can be done.
func (self *graph) giveContactPictureFromGroup(ctx context.Context, principal *api.Principal, contact *models.Contact, photoFaceGroupId string) (bool, error) {
	if contacts.HasPhoto([]byte(contact.Card)) {
		return false, nil
	}
	jpegPicture, err := self.cutContactPicture(ctx, principal.User.ID, photoFaceGroupId)
	if err != nil {
		if ctx.Err() != nil {
			return false, translateError(err)
		}
		log.Warningf("cannot cut a picture for a contact from a face: %s", err)
		return false, nil
	}
	if jpegPicture == nil {
		return false, nil
	}
	_, isSet, err := addressbook.New(self.transaction(ctx)).SetPicture(ctx, principal, contact.ID, jpegPicture)
	if err != nil {
		if ctx.Err() != nil {
			return false, translateError(err)
		}
		// A card too large to take a picture (db.ErrInvalidArguments), a book
		// that is full (db.ErrBookFull), or anything else: the contact keeps
		// the card it had.
		log.Warningf("cannot put a picture on a contact: %s", err)
		return false, nil
	}
	return isSet, nil
}

// cutContactPicture cuts the picture from the group's best face, from the
// photo's large preview, or answers nil when no face of the group qualifies.
func (self *graph) cutContactPicture(ctx context.Context, userId, photoFaceGroupId string) ([]byte, error) {
	transaction := self.transaction(ctx)
	face, err := transaction.FindPhotoFaceForContactPicture(userId, photoFaceGroupId)
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	preview, err := transaction.GetPhotoPreview(userId, face.PhotoID, models.PhotoPreviewLarge)
	if err != nil {
		return nil, err
	}
	if preview.PreviewSizeBytes <= 0 || preview.PreviewSizeBytes > maximumFacePreviewBytes {
		return nil, fmt.Errorf("the large preview of photo %s has %d bytes", face.PhotoID, preview.PreviewSizeBytes)
	}
	files, isStreaming := self.storage.(storage.StreamFiles)
	if !isStreaming {
		return nil, errors.New("photo storage is unavailable")
	}
	if err := facePictureSlots.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer facePictureSlots.Release(1)
	stream, err := files.OpenFileRange(ctx, models.PhotoPreviewFileID(face.PhotoID, models.PhotoPreviewLarge), 0, preview.PreviewSizeBytes)
	if err != nil {
		return nil, err
	}
	content, err := io.ReadAll(io.LimitReader(stream, preview.PreviewSizeBytes+1))
	_ = stream.Close()
	if err != nil {
		return nil, err
	}
	decoded, err := photopicture.Decode(content)
	if err != nil {
		return nil, err
	}
	return photo.ContactPicture(decoded, face)
}
