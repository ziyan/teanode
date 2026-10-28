package photo

import (
	"context"
	"errors"
	"image"
	"io"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/faces"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/picture"
)

const (
	// faceBatchSize is how many photos that are only waiting for their faces
	// one idle pass reads. Each takes the reader a second or less, and the
	// pass comes every few seconds, so a library of tens of thousands is
	// caught up in hours without the server being busy with nothing else.
	faceBatchSize = 4

	// faceRetryDelay is how long a photo whose faces could not be read is left
	// alone before the next try, so that one broken picture is not read every
	// pass for ever.
	faceRetryDelay = time.Hour

	// maximumFacePreviewBytes bounds the large preview read to find faces in:
	// a preview of 2048 pixels on its long side is some hundreds of
	// kilobytes.
	maximumFacePreviewBytes = 64 << 20
)

// faceFinder is what a Store keeps to find faces: the reader, made when it is
// first wanted, and the photos it failed on lately.
type faceFinder struct {
	readerOnce sync.Once
	reader     faces.Reader
	readerErr  error

	failureMutex sync.Mutex
	failedAt     map[string]time.Time
}

// faceReader is the program that finds faces, or why there is none. It is
// looked for once: the program is installed with the image.
func (self *Store) faceReader() (faces.Reader, error) {
	self.faceFinder.readerOnce.Do(func() {
		self.faceFinder.reader, self.faceFinder.readerErr = faces.NewReader()
		if self.faceFinder.readerErr != nil {
			log.Infof("faces are not found in photos: %s", self.faceFinder.readerErr)
		}
	})
	return self.faceFinder.reader, self.faceFinder.readerErr
}

// FaceRecognitionUnavailableMessage says why faces are not being found in
// photos on this server, or is empty when they are: the operator has them on
// and the program that finds them is installed.
func FaceRecognitionUnavailableMessage(configuration *config.Configuration) string {
	if !configuration.Photos.IsFaceRecognitionEnabled {
		return "Finding faces in photos is switched off on this server."
	}
	if _, err := faces.NewReader(); err != nil {
		return err.Error()
	}
	return ""
}

// areFacesWanted says whether faces are being found on this server at all,
// before anybody's own switch is looked at.
func (self *Store) areFacesWanted() (faces.Reader, bool) {
	if !self.configuration().Photos.IsFaceRecognitionEnabled {
		return nil, false
	}
	reader, err := self.faceReader()
	return reader, err == nil
}

func (self *Store) noteFaceFailure(photoId string) {
	self.faceFinder.failureMutex.Lock()
	defer self.faceFinder.failureMutex.Unlock()
	if self.faceFinder.failedAt == nil {
		self.faceFinder.failedAt = map[string]time.Time{}
	}
	self.faceFinder.failedAt[photoId] = self.now()
}

// recentFaceFailures are the photos to leave out of the next question. Those
// whose delay has passed are forgotten here.
func (self *Store) recentFaceFailures() []string {
	self.faceFinder.failureMutex.Lock()
	defer self.faceFinder.failureMutex.Unlock()
	photoIds := make([]string, 0, len(self.faceFinder.failedAt))
	for photoId, failedAt := range self.faceFinder.failedAt {
		if self.now().Sub(failedAt) >= faceRetryDelay {
			delete(self.faceFinder.failedAt, photoId)
			continue
		}
		photoIds = append(photoIds, photoId)
	}
	return photoIds
}

// findFaces finds the faces in a photo's large preview and keeps them, with
// the groups they go in, when faces are wanted for the photo. It never fails
// the photo: a face reader that cannot read a picture leaves the faces unfound
// and the photo ready, to be tried again later.
func (self *Store) findFaces(ctx context.Context, photo *models.Photo, large image.Image) {
	reader, isWanted := self.areFacesWanted()
	if !isWanted || photo.FacesFoundAt != nil {
		return
	}
	var isEnabled bool
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		isEnabled, err = transaction.IsPhotoFaceRecognitionEnabled(photo.UserID)
		return err
	}); err != nil {
		if ctx.Err() == nil {
			log.Warningf("cannot tell whether faces are wanted for photo %s: %s", photo.ID, err)
		}
		return
	}
	if !isEnabled {
		return
	}
	if err := self.readAndKeepFaces(ctx, reader, photo, large); err != nil && ctx.Err() == nil && !errors.Is(err, errPhotoGone) {
		self.noteFaceFailure(photo.ID)
		log.Warningf("cannot find the faces in photo %s: %s", photo.ID, err)
	}
}

// readAndKeepFaces reads a picture with the reader, which takes a while, and
// then keeps what it found in one transaction under the person's grouping lock.
func (self *Store) readAndKeepFaces(ctx context.Context, reader faces.Reader, photo *models.Photo, large image.Image) error {
	found, err := reader.ReadFaces(ctx, large)
	if err != nil {
		return err
	}
	bounds := large.Bounds()
	candidates := make([]*models.PhotoFace, 0, len(found))
	for _, face := range found {
		candidates = append(candidates, &models.PhotoFace{
			BoxLeftFraction: face.BoxLeft, BoxTopFraction: face.BoxTop, BoxWidthFraction: face.BoxWidth, BoxHeightFraction: face.BoxHeight,
			DetectionScore: face.DetectionScore, FaceVector: faces.Normalize(face.FaceVector),
		})
	}
	err = self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if err := transaction.LockPhotoFaceGrouping(photo.UserID); err != nil {
			return err
		}
		// Switching faces off takes the same lock, so a switch made while the
		// picture was being read is seen here, and nothing is kept after it.
		isEnabled, err := transaction.IsPhotoFaceRecognitionEnabled(photo.UserID)
		if err != nil || !isEnabled {
			return err
		}
		stored, err := transaction.StorePhotoFaces(photo.UserID, photo.ID, candidates)
		if err != nil {
			return err
		}
		_, err = groupFaces(transaction, photo.UserID, stored, bounds.Dx(), bounds.Dy())
		return err
	})
	switch {
	case errors.Is(err, db.ErrNotFound):
		return errPhotoGone
	case errors.Is(err, db.ErrAlreadyExists):
		// Another instance found them while this one was reading.
		return nil
	}
	return err
}

// findWaitingFaces reads the faces of a few photos that are ready and have not
// had them read: the photos from before faces were switched on, from while
// the person had them off, or from before the program was installed. It
// does nothing, and asks the database nothing, while faces are not wanted.
func (self *Store) findWaitingFaces(ctx context.Context) error {
	reader, isWanted := self.areFacesWanted()
	if !isWanted {
		return nil
	}
	var waiting []*models.Photo
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		waiting, err = transaction.ListPhotosWaitingForFaces(faceBatchSize, self.recentFaceFailures())
		return err
	}); err != nil {
		return err
	}
	for _, photo := range waiting {
		if ctx.Err() != nil {
			return nil
		}
		// Reading a picture takes seconds, too long to hold a row lock for, so
		// the photo is claimed with an advisory lock on its id for as long as
		// it is read: an instance that lists the same photos meanwhile skips
		// the ones another is reading instead of reading them again.
		if _, err := self.database.WhileLocked(ctx, db.LockKey("photo:faces:"+photo.ID), func(ctx context.Context) error {
			self.findWaitingFacesOf(ctx, reader, photo)
			return nil
		}); err != nil && ctx.Err() == nil {
			log.Warningf("cannot claim photo %s to find its faces: %s", photo.ID, err)
		}
	}
	return nil
}

// findWaitingFacesOf reads the faces of one photo that findWaitingFaces holds
// the claim on, unless another instance found them between the listing and
// the claim.
func (self *Store) findWaitingFacesOf(ctx context.Context, reader faces.Reader, photo *models.Photo) {
	var current *models.Photo
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		current, err = transaction.GetPhoto(photo.UserID, photo.ID)
		return err
	}); err != nil {
		if ctx.Err() == nil && !errors.Is(err, db.ErrNotFound) {
			log.Warningf("cannot tell whether the faces of photo %s were found: %s", photo.ID, err)
		}
		return
	}
	if current.FacesFoundAt != nil {
		return
	}
	large, err := self.readLargePreview(ctx, photo)
	if err != nil {
		if ctx.Err() == nil {
			self.noteFaceFailure(photo.ID)
			log.Warningf("cannot read the large preview of photo %s to find its faces: %s", photo.ID, err)
		}
		return
	}
	if err := self.readAndKeepFaces(ctx, reader, photo, large); err != nil && ctx.Err() == nil && !errors.Is(err, errPhotoGone) {
		self.noteFaceFailure(photo.ID)
		log.Warningf("cannot find the faces in photo %s: %s", photo.ID, err)
	}
}

// readLargePreview reads a photo's stored large preview and decodes it.
func (self *Store) readLargePreview(ctx context.Context, photo *models.Photo) (image.Image, error) {
	var preview *models.PhotoPreview
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		preview, err = transaction.GetPhotoPreview(photo.UserID, photo.ID, models.PhotoPreviewLarge)
		return err
	}); err != nil {
		return nil, err
	}
	if preview.PreviewSizeBytes <= 0 || preview.PreviewSizeBytes > maximumFacePreviewBytes {
		return nil, errors.New("the large preview is empty or larger than may be read")
	}
	stream, err := self.files.OpenFileRange(ctx, models.PhotoPreviewFileID(photo.ID, models.PhotoPreviewLarge), 0, preview.PreviewSizeBytes)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	content, err := io.ReadAll(io.LimitReader(stream, preview.PreviewSizeBytes+1))
	if err != nil {
		return nil, err
	}
	return picture.Decode(content)
}

// cleanFaceGroups deletes the groups no face is in any longer.
func (self *Store) cleanFaceGroups(ctx context.Context) error {
	return self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		deletedCount, err := transaction.DeleteEmptyPhotoFaceGroups()
		if err == nil && deletedCount > 0 {
			log.Infof("%d groups of faces had no face left and were deleted", deletedCount)
		}
		return err
	})
}
