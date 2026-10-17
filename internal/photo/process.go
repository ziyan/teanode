package photo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/picture"
	"github.com/ziyan/teanode/internal/place"
	"github.com/ziyan/teanode/internal/storage"
)

const (
	// The longest side of each preview, and the JPEG quality they are written
	// at.
	smallPreviewLongSidePixels = 400
	largePreviewLongSidePixels = 2048
	previewJpegQuality         = 82

	// maximumPictureBytes is the largest picture that is read into memory and
	// decoded. A bigger one has its date and place read and is then refused,
	// because decoding it would take more of the server than one photo is
	// worth.
	maximumPictureBytes = 200 << 20

	// maximumVideoPreviewDuration is the longest video that a playable copy
	// is made of. Longer ones play from the original.
	maximumVideoPreviewDuration = 30 * time.Minute

	// processingConcurrency is how many photos one process works on at once.
	// A video takes a core or more for as long as it is long.
	processingConcurrency = 2

	// claimRenewalInterval is how often the claims on photos being worked on
	// are renewed, well inside processingClaimRetention.
	claimRenewalInterval = time.Minute

	// requeueInterval is how often an idle pass looks for ready photos that
	// have no previews, and requeueBatchSize how many it takes back each time.
	requeueInterval  = time.Minute
	requeueBatchSize = 20
)

// errUnsupportedFile is a file of a kind previews cannot be made from, and
// errPictureTooLarge one too big to decode. Trying again would find the same,
// so the photo is failed at once. The message is shown to the person.
var (
	errUnsupportedFile = errors.New("this kind of file is not supported")
	errPictureTooLarge = errors.New("this picture is too large to make previews of")
)

// errPhotoGone is a photo that was deleted while it was being processed.
var errPhotoGone = errors.New("photo: the photo was deleted while it was being processed")

// Process gives previews and metadata to up to batchSize photos waiting for
// them, a few at a time. A photo is claimed in a transaction of its own
// before any work is done on it, so several instances running this share the
// waiting photos instead of repeating each other's work, and a claim older
// than processingClaimRetention is the work of an instance that has gone and
// is taken again. When nothing is waiting, it also puts back photos that are
// marked ready and have no previews, which an earlier version of this
// marked ready without making any.
func (self *Store) Process(ctx context.Context, batchSize int) error {
	var photos []*models.Photo
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		photos, err = transaction.ClaimPhotosForProcessing(batchSize, self.now().Add(-processingClaimRetention))
		return err
	}); err != nil {
		return err
	}
	if len(photos) == 0 {
		return self.requeuePhotosWithoutPreviews(ctx)
	}

	// The photos claimed and not yet reached are waiting on a claim that
	// ages, and a long video can outlast it, so the claims are renewed.
	renewalContext, stopRenewal := context.WithCancel(ctx)
	var renewalGroup sync.WaitGroup
	renewalGroup.Add(1)
	go func() {
		defer renewalGroup.Done()
		self.renewClaims(renewalContext, photos)
	}()
	defer func() {
		stopRenewal()
		renewalGroup.Wait()
	}()

	var failuresMutex sync.Mutex
	var failures []error
	var workGroup sync.WaitGroup
	slots := make(chan struct{}, processingConcurrency)
	for _, photo := range photos {
		slots <- struct{}{}
		workGroup.Add(1)
		go func() {
			defer workGroup.Done()
			defer func() { <-slots }()
			if err := self.processAndRecord(ctx, photo); err != nil {
				failuresMutex.Lock()
				failures = append(failures, err)
				failuresMutex.Unlock()
			}
		}()
	}
	workGroup.Wait()
	if err := ctx.Err(); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

// processAndRecord processes a photo and records how it went: ready, or one
// more failure with its message, and failed for good after the attempts or at
// once for a kind of file that cannot be read.
func (self *Store) processAndRecord(ctx context.Context, photo *models.Photo) error {
	processError := self.processPhoto(ctx, photo)
	if ctx.Err() != nil {
		// Stopping is not the photo's fault. Its claim runs out and it is
		// taken again.
		return nil
	}
	if errors.Is(processError, errPhotoGone) {
		return nil
	}
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if processError == nil {
			return transaction.MarkPhotoReady(photo.ID)
		}
		maximumAttemptCount := maximumProcessingAttemptCount
		if errors.Is(processError, errUnsupportedFile) || errors.Is(processError, errPictureTooLarge) {
			maximumAttemptCount = 1
		}
		return transaction.MarkPhotoProcessingFailed(photo.ID, processError.Error(), maximumAttemptCount)
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if processError != nil {
		log.Warningf("cannot process photo %s: %s", photo.ID, processError)
	}
	return nil
}

// renewClaims keeps the claims on the photos of a pass alive until it ends.
func (self *Store) renewClaims(ctx context.Context, photos []*models.Photo) {
	photoIds := make([]string, 0, len(photos))
	for _, photo := range photos {
		photoIds = append(photoIds, photo.ID)
	}
	ticker := time.NewTicker(claimRenewalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
				return transaction.TouchPhotoProcessingClaims(photoIds)
			}); err != nil && ctx.Err() == nil {
				log.Warningf("cannot renew the claims on photos being processed: %s", err)
			}
		}
	}
}

// requeuePhotosWithoutPreviews makes ready photos that have no small preview
// wait for processing again, a few at a time and not more than once a minute,
// because the question is asked of the whole library. It asks again at once
// after finding some, so a library of them is worked through.
func (self *Store) requeuePhotosWithoutPreviews(ctx context.Context) error {
	self.requeueMutex.Lock()
	defer self.requeueMutex.Unlock()
	if now := self.now(); !self.lastRequeueAt.IsZero() && now.Sub(self.lastRequeueAt) < requeueInterval {
		return nil
	}
	var requeuedCount int
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		requeuedCount, err = transaction.RequeuePhotosWithoutPreviews(requeueBatchSize)
		return err
	}); err != nil {
		return err
	}
	if requeuedCount > 0 {
		self.lastRequeueAt = time.Time{}
		log.Infof("%d photos marked ready have no previews and wait for them", requeuedCount)
	} else {
		self.lastRequeueAt = self.now()
	}
	return nil
}

// processedPhoto is what is known of a photo while it is being processed.
type processedPhoto struct {
	photo    *models.Photo
	source   *models.PhotoFile
	metadata db.PhotoMetadata

	// directory is a private temporary directory, made when a tool needs a
	// path to read, and removed when processing ends.
	directory string
}

func (self *processedPhoto) workDirectory() (string, error) {
	if self.directory == "" {
		directory, err := os.MkdirTemp("", "teanode-photo-")
		if err != nil {
			return "", fmt.Errorf("a private directory to work in could not be made: %w", err)
		}
		self.directory = directory
	}
	return self.directory, nil
}

// processPhoto reads what the photo's file says of itself and makes its
// previews. What was read is recorded before the previews are tried, so that
// a photo whose previews fail still has the right kind, size and date.
func (self *Store) processPhoto(ctx context.Context, photo *models.Photo) error {
	var files []*models.PhotoFile
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		files, err = transaction.ListPhotoFiles(photo.UserID, []string{photo.ID})
		return err
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return errPhotoGone
		}
		return err
	}
	processed := &processedPhoto{photo: photo}
	defer func() {
		if processed.directory != "" {
			_ = os.RemoveAll(processed.directory)
		}
	}()
	hasLiveVideo := false
	for _, file := range files {
		switch file.PhotoFileRole {
		case models.PhotoFileRoleEdited:
			processed.source = file
		case models.PhotoFileRoleOriginal:
			// The edited version, when there is one, is what is shown.
			if processed.source == nil {
				processed.source = file
			}
		case models.PhotoFileRoleLiveVideo:
			hasLiveVideo = true
		}
	}
	if processed.source == nil {
		return errors.New("the photo has no picture or video to read")
	}
	processed.metadata.PhotoKind = settlePhotoKind(photo.PhotoKind, processed.source.ContentType, hasLiveVideo)

	if strings.HasPrefix(processed.source.ContentType, "video/") {
		return self.processVideo(ctx, processed)
	}
	return self.processPicture(ctx, processed)
}

// settlePhotoKind is what a photo is made of: a video when its file is one,
// a Live Photo when it has a video beside its picture or the uploader said
// it would, and a picture otherwise. The file decides over what the uploader
// said, except that a Live Photo whose video has not arrived yet stays one.
func settlePhotoKind(storedKind models.PhotoKind, sourceContentType string, hasLiveVideo bool) models.PhotoKind {
	switch {
	case strings.HasPrefix(sourceContentType, "video/"):
		return models.PhotoKindVideo
	case hasLiveVideo || storedKind == models.PhotoKindLivePhoto:
		return models.PhotoKindLivePhoto
	}
	return models.PhotoKindPicture
}

// saveMetadata records what has been read. It is called before previews are
// made and again when the picture's size is known for certain.
func (self *Store) saveMetadata(ctx context.Context, processed *processedPhoto) error {
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		return transaction.UpdatePhotoMetadata(processed.photo.ID, &processed.metadata)
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return errPhotoGone
		}
		return err
	}
	return nil
}

// namePlace sets the name of the place the photo was taken, from the
// coordinates it has, or the file gave when it had none.
func (self *processedPhoto) namePlace() {
	latitude, longitude := self.photo.Latitude, self.photo.Longitude
	if latitude == nil || longitude == nil {
		latitude, longitude = self.metadata.Latitude, self.metadata.Longitude
	}
	if latitude == nil || longitude == nil || self.photo.PlaceName != "" {
		return
	}
	if name, isFound := place.Nearest(*latitude, *longitude); isFound {
		self.metadata.PlaceName = name
	}
}

// openFile opens the bytes of one of a photo's files.
func (self *Store) openFile(ctx context.Context, file *models.PhotoFile) (*storage.FileStream, error) {
	return self.files.OpenFileRange(ctx, models.PhotoBlobFileID(file.BlobSHA256), 0, file.FileSizeBytes)
}

// readFile reads a whole file into memory. The caller has checked it is small
// enough.
func (self *Store) readFile(ctx context.Context, file *models.PhotoFile) ([]byte, error) {
	stream, err := self.openFile(ctx, file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	content := make([]byte, 0, file.FileSizeBytes)
	buffer := bytes.NewBuffer(content)
	if _, err := io.Copy(buffer, io.LimitReader(stream, file.FileSizeBytes+1)); err != nil {
		return nil, err
	}
	if int64(buffer.Len()) != file.FileSizeBytes {
		return nil, fmt.Errorf("the stored file is %d bytes and was recorded as %d", buffer.Len(), file.FileSizeBytes)
	}
	return buffer.Bytes(), nil
}

// copyFileToDirectory writes a file where a tool that needs a path can read
// it, and returns the path.
func (self *Store) copyFileToDirectory(ctx context.Context, processed *processedPhoto) (string, error) {
	directory, err := processed.workDirectory()
	if err != nil {
		return "", err
	}
	stream, err := self.openFile(ctx, processed.source)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()
	path := filepath.Join(directory, "source")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("a file to work on could not be made: %w", err)
	}
	_, err = io.Copy(file, stream)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("the file could not be copied to work on: %w", err)
	}
	return path, nil
}

// isHeifContentType says whether the picture is one libheif reads.
func isHeifContentType(contentType string) bool {
	switch contentType {
	case "image/heic", "image/heif", "image/heic-sequence", "image/heif-sequence":
		return true
	}
	return false
}

// isGoDecodableContentType says whether the standard library reads the
// picture, which is what lets a failure to read it be the file's fault.
func isGoDecodableContentType(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/gif":
		return true
	}
	return false
}

func (self *Store) processPicture(ctx context.Context, processed *processedPhoto) error {
	source := processed.source
	isHeif := isHeifContentType(source.ContentType)
	hasReadableMetadata := isHeif || source.ContentType == "image/jpeg"

	if source.FileSizeBytes > maximumPictureBytes {
		// Too big to decode, but not too big to ask its date and place of.
		if hasReadableMetadata {
			path, err := self.copyFileToDirectory(ctx, processed)
			if err != nil {
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			self.readPictureMetadata(processed, file, source.FileSizeBytes)
			_ = file.Close()
		}
		processed.namePlace()
		if err := self.saveMetadata(ctx, processed); err != nil {
			return err
		}
		return fmt.Errorf("%w: it is over %d MB", errPictureTooLarge, maximumPictureBytes>>20)
	}
	content, err := self.readFile(ctx, source)
	if err != nil {
		return err
	}
	if hasReadableMetadata {
		self.readPictureMetadata(processed, bytes.NewReader(content), source.FileSizeBytes)
	}
	processed.namePlace()
	if err := self.saveMetadata(ctx, processed); err != nil {
		return err
	}

	var decoded image.Image
	if isHeif {
		jpegContent, err := picture.DecodeHEIC(ctx, bytes.NewReader(content))
		if err != nil {
			return err
		}
		if decoded, err = picture.Decode(jpegContent); err != nil {
			return err
		}
	} else {
		if decoded, err = picture.Decode(content); err != nil {
			if !isGoDecodableContentType(source.ContentType) {
				return fmt.Errorf("%w: %s files cannot be made into previews (%s)", errUnsupportedFile, source.ContentType, err)
			}
			return err
		}
	}

	// The decoded picture is upright, so its size is the size it is shown at.
	bounds := decoded.Bounds()
	processed.metadata = db.PhotoMetadata{PhotoKind: processed.metadata.PhotoKind, WidthPixels: bounds.Dx(), HeightPixels: bounds.Dy()}
	if err := self.saveMetadata(ctx, processed); err != nil {
		return err
	}
	return self.keepPicturePreviews(ctx, processed.photo, decoded)
}

// readPictureMetadata sets what a picture's EXIF block says. A picture whose
// block cannot be read is not a failed photo: its pixels may be fine, and the
// date and place it would have had are the only loss.
func (self *Store) readPictureMetadata(processed *processedPhoto, content io.ReaderAt, sizeBytes int64) {
	metadata, err := picture.ReadMetadata(content, sizeBytes)
	if err != nil {
		log.Infof("cannot read the metadata of photo %s: %s", processed.photo.ID, err)
		return
	}
	processed.metadata.TakenAt, processed.metadata.TakenAtOffsetMinutes = metadata.TakenAt, metadata.TakenAtOffsetMinutes
	processed.metadata.Latitude, processed.metadata.Longitude = metadata.Latitude, metadata.Longitude
	processed.metadata.CameraMake, processed.metadata.CameraModel = metadata.CameraMake, metadata.CameraModel
	processed.metadata.WidthPixels, processed.metadata.HeightPixels = metadata.WidthPixels, metadata.HeightPixels
	// The size is the stored one; a picture turned on its side is shown the
	// other way round. A HEIC is turned by its own boxes, which the decoder
	// applies, and its real size is set when it is decoded.
	if metadata.Orientation >= 5 && metadata.Orientation <= 8 {
		processed.metadata.WidthPixels, processed.metadata.HeightPixels = metadata.HeightPixels, metadata.WidthPixels
	}
}

// keepPicturePreviews makes and stores the small and large previews of an
// upright picture.
func (self *Store) keepPicturePreviews(ctx context.Context, photo *models.Photo, upright image.Image) error {
	large := picture.Scale(upright, largePreviewLongSidePixels)
	// The small one is made from the large one: it is much less to read, and
	// the difference cannot be seen at 400 pixels.
	small := picture.Scale(large, smallPreviewLongSidePixels)
	for _, preview := range []struct {
		photoPreviewSize models.PhotoPreviewSize
		scaled           image.Image
	}{{models.PhotoPreviewLarge, large}, {models.PhotoPreviewSmall, small}} {
		encoded, err := picture.EncodeJPEG(preview.scaled, previewJpegQuality)
		if err != nil {
			return err
		}
		bounds := preview.scaled.Bounds()
		if err := self.keepPreview(ctx, photo, preview.photoPreviewSize, "image/jpeg", bytes.NewReader(encoded), int64(len(encoded)), bounds.Dx(), bounds.Dy()); err != nil {
			return err
		}
	}
	return nil
}

// keepPreview stores a preview's bytes and then records it, in that order, so
// that no row names bytes that are not there.
func (self *Store) keepPreview(ctx context.Context, photo *models.Photo, photoPreviewSize models.PhotoPreviewSize, contentType string, content io.ReadSeeker, sizeBytes int64, widthPixels, heightPixels int) error {
	fileId := models.PhotoPreviewFileID(photo.ID, photoPreviewSize)
	if err := self.files.PutFileStream(ctx, fileId, content, sizeBytes); err != nil {
		return err
	}
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		_, err := transaction.UpsertPhotoPreview(photo.UserID, &models.PhotoPreview{
			PhotoID: photo.ID, PhotoPreviewSize: photoPreviewSize, ContentType: contentType,
			PreviewSizeBytes: sizeBytes, WidthPixels: widthPixels, HeightPixels: heightPixels,
		})
		return err
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// Deleted while it was being processed: nothing will name these
			// bytes, so they go now.
			_ = self.files.DeleteFileEverywhere(context.WithoutCancel(ctx), fileId)
			return errPhotoGone
		}
		return err
	}
	return nil
}

func (self *Store) processVideo(ctx context.Context, processed *processedPhoto) error {
	// A video is never read into memory: ffmpeg reads the file from disk.
	path, err := self.copyFileToDirectory(ctx, processed)
	if err != nil {
		return err
	}
	probed, probeErr := picture.ProbeVideo(ctx, path)
	if probeErr == nil {
		processed.metadata.WidthPixels, processed.metadata.HeightPixels = probed.WidthPixels, probed.HeightPixels
		processed.metadata.DurationMS = probed.DurationMS
		processed.metadata.TakenAt, processed.metadata.TakenAtOffsetMinutes = probed.TakenAt, probed.TakenAtOffsetMinutes
		processed.metadata.Latitude, processed.metadata.Longitude = probed.Latitude, probed.Longitude
		processed.namePlace()
	}
	if err := self.saveMetadata(ctx, processed); err != nil {
		return err
	}
	if probeErr != nil {
		return probeErr
	}

	poster, err := picture.VideoPoster(ctx, path)
	if err != nil {
		return err
	}
	decoded, err := picture.Decode(poster)
	if err != nil {
		return err
	}
	if err := self.keepPicturePreviews(ctx, processed.photo, decoded); err != nil {
		return err
	}

	if probed.DurationMS > maximumVideoPreviewDuration.Milliseconds() {
		return nil
	}
	previewPath := filepath.Join(processed.directory, "preview.mp4")
	if err := picture.VideoPreview(ctx, path, previewPath); err != nil {
		return err
	}
	previewFile, err := os.Open(previewPath)
	if err != nil {
		return err
	}
	defer func() { _ = previewFile.Close() }()
	info, err := previewFile.Stat()
	if err != nil {
		return err
	}
	widthPixels, heightPixels := fitLongSide(probed.WidthPixels, probed.HeightPixels, picture.VideoPreviewLongSidePixels)
	return self.keepPreview(ctx, processed.photo, models.PhotoPreviewVideo, "video/mp4", previewFile, info.Size(), widthPixels, heightPixels)
}

// fitLongSide is the size a picture of the given size has once its longest
// side is no longer than the limit, as the video preview is made, with sides
// kept even as H.264 needs.
func fitLongSide(widthPixels, heightPixels, limitPixels int) (int, int) {
	longSidePixels := max(widthPixels, heightPixels)
	if longSidePixels <= limitPixels || longSidePixels == 0 {
		return widthPixels, heightPixels
	}
	shrink := func(sidePixels int) int {
		return max(sidePixels*limitPixels/longSidePixels/2*2, 2)
	}
	return shrink(widthPixels), shrink(heightPixels)
}
