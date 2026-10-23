package photo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A photo in an album somebody shared is read where it is, through its
// owner's rows. A reader who may not see where it was taken gets a preview in
// place of its files: the original carries the place in its metadata, and a
// preview is a picture made afresh, which carries none. This file is that
// rule and the saving of such a photo to a library of one's own.

// SharedCopyPreviewSize says which preview stands for a photo's original file
// when the reader may not have the original: the video preview for a video
// that has one, else the large picture. False when the photo has neither yet.
func SharedCopyPreviewSize(photoKind models.PhotoKind, hasPreview func(models.PhotoPreviewSize) bool) (models.PhotoPreviewSize, bool) {
	switch {
	case photoKind == models.PhotoKindVideo && hasPreview(models.PhotoPreviewVideo):
		return models.PhotoPreviewVideo, true
	case hasPreview(models.PhotoPreviewLarge):
		return models.PhotoPreviewLarge, true
	}
	return "", false
}

// HasPreviewIn is the question SharedCopyPreviewSize asks, of these rows.
func HasPreviewIn(previews []*models.PhotoPreview) func(models.PhotoPreviewSize) bool {
	return func(photoPreviewSize models.PhotoPreviewSize) bool {
		return slices.ContainsFunc(previews, func(preview *models.PhotoPreview) bool { return preview.PhotoPreviewSize == photoPreviewSize })
	}
}

// SharedCopyFilename is the name the preview is saved under: "photo-" and the
// photo's identifier, with the extension of what the preview is. It is never
// the photo's own name or its file's: those are the owner's, and are given to a
// reader only with the place, because a name is often the place's.
func SharedCopyFilename(photoId string, photoPreviewSize models.PhotoPreviewSize) string {
	extension := ".jpg"
	if photoPreviewSize == models.PhotoPreviewVideo {
		extension = ".mp4"
	}
	return "photo-" + photoId + extension
}

// SharedClientPhotoID is the name a saved copy has for its owner's uploads: the
// digest of the file it was made from, so that saving the same photo twice is
// saving it once. It is for a copy made over the owner's files, which the
// reader has the right to, and so to their digest.
func SharedClientPhotoID(sourceBlobSha256 string) string {
	return "shared:" + sourceBlobSha256
}

// SharedPreviewClientPhotoID is the same for a copy made from a preview, which
// is what a reader who may not have the original gets. The digest of the
// original is not theirs to know, as it names the very file they were kept
// from and anyone holding the file could match it, so the copy is named by the
// photo it was made from instead.
func SharedPreviewClientPhotoID(photoId string) string {
	return "shared-preview:" + photoId
}

// SharedPhoto is a photo somebody else owns, as the saving of it needs it.
type SharedPhoto struct {
	Photo    *models.Photo
	Files    []*models.PhotoFile
	Previews []*models.PhotoPreview

	// IsLocationIncluded says the share that lets the reader in includes where
	// photos were taken, and so the files themselves. Without it the copy is
	// made from a preview.
	IsLocationIncluded bool
}

// sourceFileOf is the file that names the photo for a copy of it: the
// original, else the edited version.
func sourceFileOf(files []*models.PhotoFile) *models.PhotoFile {
	var edited *models.PhotoFile
	for _, file := range files {
		switch file.PhotoFileRole {
		case models.PhotoFileRoleOriginal:
			return file
		case models.PhotoFileRoleEdited:
			edited = file
		}
	}
	return edited
}

// ErrNothingToSave is a shared photo that has no file or no preview yet to
// make a copy of; it is told to try again once the owner's server has made
// its previews.
var ErrNothingToSave = errors.New("photo: the photo has no preview yet; try again in a minute")

// SaveSharedPhoto makes a photo of the reader's own from a photo in an album
// somebody shared with them. When the share includes where photos were taken
// the copy is made over the same blobs: its file rows name the owner's bytes,
// so nothing new is stored, the library's quota counts the files, and the
// photo is processed like any new photo, getting previews of its own and, if
// the reader has faces on, faces of its own. Without that the copy is the
// preview the reader could see, stored as a file of its own, because the
// original's bytes carry the place. Either way the copy has a name that is
// the same each time, so saving a photo twice is saving it once.
func (self *Store) SaveSharedPhoto(ctx context.Context, readerUserId string, shared *SharedPhoto) (*models.Photo, error) {
	source := sourceFileOf(shared.Files)
	if source == nil {
		return nil, ErrNothingToSave
	}
	if shared.IsLocationIncluded {
		return self.saveSharedFiles(ctx, readerUserId, SharedClientPhotoID(source.BlobSHA256), shared)
	}
	return self.saveSharedPreview(ctx, readerUserId, SharedPreviewClientPhotoID(shared.Photo.ID), shared)
}

// saveSharedFiles records the owner's files as files of a new photo of the
// reader's.
func (self *Store) saveSharedFiles(ctx context.Context, readerUserId, clientPhotoId string, shared *SharedPhoto) (*models.Photo, error) {
	maximumAccountBytes := int64(self.configuration().Photos.MaxAccountBytes)
	var saved *models.Photo
	err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		made, _, err := transaction.CreatePhoto(&models.Photo{
			UserID: readerUserId, ClientPhotoID: clientPhotoId, PhotoKind: shared.Photo.PhotoKind, PhotoSource: models.PhotoSourceShared,
			TakenAt: shared.Photo.TakenAt, TakenAtOffsetMinutes: shared.Photo.TakenAtOffsetMinutes,
			WidthPixels: shared.Photo.WidthPixels, HeightPixels: shared.Photo.HeightPixels, DurationMS: shared.Photo.DurationMS,
			// A location the owner removed is not read back from the copied original.
			Latitude: shared.Photo.Latitude, Longitude: shared.Photo.Longitude, PlaceName: shared.Photo.PlaceName, IsLocationRemoved: shared.Photo.IsLocationRemoved,
			CameraMake: shared.Photo.CameraMake, CameraModel: shared.Photo.CameraModel,
			OriginalFilename: shared.Photo.OriginalFilename, IsScreenshot: shared.Photo.IsScreenshot,
		})
		if err != nil {
			return err
		}
		for _, file := range shared.Files {
			_, err := transaction.SetPhotoFile(readerUserId, made.ID, &models.PhotoFile{
				PhotoFileRole: file.PhotoFileRole, BlobSHA256: file.BlobSHA256, ContentType: file.ContentType,
				FileSizeBytes: file.FileSizeBytes, OriginalFilename: file.OriginalFilename,
			}, maximumAccountBytes)
			if errors.Is(err, db.ErrTooMuchAsked) {
				return &QuotaError{LimitBytes: maximumAccountBytes}
			}
			if err != nil {
				return err
			}
		}
		saved, err = transaction.GetPhoto(readerUserId, made.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// saveSharedPreview stores the preview the reader could see as the original of
// a new photo of theirs.
func (self *Store) saveSharedPreview(ctx context.Context, readerUserId, clientPhotoId string, shared *SharedPhoto) (*models.Photo, error) {
	// A copy made earlier is kept as it is: saving again must not replace a
	// whole file with a preview of it.
	var earlier []*models.Photo
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		earlier, err = transaction.FindPhotosByClientIDs(readerUserId, []string{clientPhotoId})
		return err
	}); err != nil {
		return nil, err
	}
	if len(earlier) > 0 {
		return earlier[0], nil
	}
	photoPreviewSize, isAvailable := SharedCopyPreviewSize(shared.Photo.PhotoKind, HasPreviewIn(shared.Previews))
	if !isAvailable {
		return nil, ErrNothingToSave
	}
	var preview *models.PhotoPreview
	for _, candidate := range shared.Previews {
		if candidate.PhotoPreviewSize == photoPreviewSize {
			preview = candidate
		}
	}
	if preview.PreviewSizeBytes <= 0 {
		return nil, ErrNothingToSave
	}
	entry := segment{fileId: models.PhotoPreviewFileID(shared.Photo.ID, photoPreviewSize), sizeBytes: preview.PreviewSizeBytes}
	digest := sha256.New()
	var firstBytes []byte
	if _, err := self.readSegment(ctx, entry, digest, &firstBytes); err != nil {
		return nil, err
	}
	photoKind := models.PhotoKindPicture
	if photoPreviewSize == models.PhotoPreviewVideo {
		photoKind = models.PhotoKindVideo
	}
	headers := &models.PhotoUploadHeaders{
		ClientPhotoID: clientPhotoId, PhotoFileRole: models.PhotoFileRoleOriginal, PhotoKind: photoKind, PhotoSource: models.PhotoSourceShared,
		OriginalFilename: SharedCopyFilename(shared.Photo.ID, photoPreviewSize),
		ContentType:      preview.ContentType,
		// The offset from UTC is withheld with the place, which it says a part of.
		TakenAt:      shared.Photo.TakenAt,
		IsScreenshot: shared.Photo.IsScreenshot,
	}
	return self.keepFile(ctx, readerUserId, headers, newSegmentReader(ctx, self.files, []segment{entry}), hex.EncodeToString(digest.Sum(nil)), preview.PreviewSizeBytes, "")
}
