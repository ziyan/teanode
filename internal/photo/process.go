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
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/picture"
	"github.com/ziyan/teanode/internal/place"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/deferutil"
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

	// maximumPixelsInFlight is how many pixels of decoded pictures all the
	// workers of a process may hold at once: 128 megapixels, two pictures of
	// the largest size, or many small ones. A decoded picture takes four
	// bytes a pixel or less, and the biggest ones take the memory of a small
	// server by themselves.
	maximumPixelsInFlight = 128_000_000

	// claimPollInterval is how long a worker that finds nothing waiting looks
	// again, as long as the other worker is busy: a photo that arrives while a
	// long video is being made is taken by the worker that is free, and does
	// not wait for the video.
	claimPollInterval = 2 * time.Second

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
// so the photo is failed at once.
var (
	errUnsupportedFile = errors.New("this kind of file is not supported")
	errPictureTooLarge = errors.New("this picture is too large to make previews of")
)

// errPhotoGone is a photo that was deleted while it was being processed.
var errPhotoGone = errors.New("photo: the photo was deleted while it was being processed")

// The reasons a photo that failed is given. They are fixed, plain sentences:
// the person and the model read them, so nothing of what went wrong inside, a
// path or the words of a program, may be in them. What went wrong is logged.
const (
	pictureFailureMessage     = "The picture could not be read."
	videoFailureMessage       = "The video could not be read."
	unsupportedFailureMessage = "This kind of file is not supported."
	tooLargeFailureMessage    = "This picture is too large to make previews of."
	storeFailureMessage       = "The previews could not be made yet; they will be tried again."
	panicFailureMessage       = "The file could not be processed."
)

// processingFailure is why processing a photo failed. IsFileAtFault says that
// reading the file again would fail the same way, as opposed to a failure of
// the store, the database or the server's disk, which says nothing of the
// file and is not counted against the photo's attempts. Message is what the
// photo is given; when it is empty the failure is a picture or a video that
// could not be read, whichever the file is.
type processingFailure struct {
	Cause         error
	Message       string
	IsFileAtFault bool
}

func (self *processingFailure) Error() string { return self.Cause.Error() }
func (self *processingFailure) Unwrap() error { return self.Cause }

// readFailure is a failure to read the file, which is the file's fault unless
// it was the server that lacked a program or the context that ended.
func readFailure(err error) error {
	if err == nil || errors.Is(err, picture.ErrProgramMissing) || errors.Is(err, context.Canceled) {
		return err
	}
	return &processingFailure{Cause: err, IsFileAtFault: true}
}

// Process gives previews and metadata to photos waiting for them, at most
// maximumPhotoCount in a pass, with up to processingConcurrency workers. Each
// worker claims one photo at a time in a transaction of its own before any work
// is done on it, so that a long video does not hold up the pictures behind it,
// several instances running this share the waiting photos instead of repeating
// each other's work, and a claim older than processingClaimRetention is the
// work of an instance that has gone and is taken again. When nothing was
// waiting, it also puts back photos that are marked ready and have no
// previews, which an earlier version of this marked ready without making any,
// and finds the faces of photos that wait only for them.
func (self *Store) Process(ctx context.Context, maximumPhotoCount int) error {
	pass := &processingPass{maximumPhotoCount: maximumPhotoCount}
	var workers sync.WaitGroup
	for range processingConcurrency {
		workers.Add(1)
		go func() {
			defer deferutil.Recover()
			defer workers.Done()
			self.processWaitingPhotos(ctx, pass)
		}()
	}
	workers.Wait()
	failures := pass.failures
	if err := ctx.Err(); err != nil {
		failures = append(failures, err)
	} else if pass.startedCount == 0 && len(failures) == 0 {
		// Idle, so the photos that wait only for their faces get their turn.
		failures = append(failures, self.requeuePhotosWithoutPreviews(ctx), self.findWaitingFaces(ctx))
	}
	return errors.Join(failures...)
}

// processingPass is what the workers of one pass share.
type processingPass struct {
	maximumPhotoCount int

	mutex sync.Mutex
	// startedCount is the photos the pass has taken, and activeCount those
	// being worked on now.
	startedCount int
	activeCount  int
	failures     []error
}

// reserve counts one more photo as started and active, unless the pass has
// taken as many as it may.
func (self *processingPass) reserve() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.startedCount >= self.maximumPhotoCount {
		return false
	}
	self.startedCount++
	self.activeCount++
	return true
}

// release gives back a reservation that found no photo, and says whether
// another worker is still busy, in which case a photo may yet arrive for this
// one.
func (self *processingPass) release() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.startedCount--
	self.activeCount--
	return self.activeCount > 0
}

// finish ends the work on a photo.
func (self *processingPass) finish() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.activeCount--
}

func (self *processingPass) fail(err error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.failures = append(self.failures, err)
}

// processWaitingPhotos is one worker: it claims a photo, works on it, and
// claims the next, until nothing waits and the other worker is idle too, the
// pass has its share, or the context ends. A worker that finds nothing waits
// and looks again while the other is busy, so that the photos behind a long
// video are not held up by it.
func (self *Store) processWaitingPhotos(ctx context.Context, pass *processingPass) {
	for ctx.Err() == nil {
		if !pass.reserve() {
			return
		}
		var photos []*models.Photo
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			photos, err = transaction.ClaimPhotosForProcessing(1, self.now().Add(-processingClaimRetention), maximumProcessingAttemptCount)
			return err
		}); err != nil {
			pass.release()
			pass.fail(err)
			return
		}
		if len(photos) == 0 {
			if !pass.release() {
				return
			}
			timer := time.NewTimer(self.claimPollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}
		self.processClaimed(ctx, pass, photos[0])
	}
}

// processClaimed works on a photo a worker claimed, and always ends the work in
// the pass's count, so that the other worker does not wait for a photo that
// nobody is working on any more.
func (self *Store) processClaimed(ctx context.Context, pass *processingPass, photo *models.Photo) {
	defer pass.finish()
	defer deferutil.RecoverWith("recording how a photo was processed")
	if err := self.processAndRecord(ctx, photo); err != nil {
		pass.fail(err)
	}
}

// processAndRecord processes a photo and records how it went: ready, or a
// failure with a plain message. Claiming counted the attempt; a failure that is
// the file's keeps it and makes the photo wait before the next, and is failed
// for good after the attempts, or at once for a kind of file that cannot be
// read. A panic is a failure of the file too: the photo is recorded as failed
// and the process goes on.
func (self *Store) processAndRecord(ctx context.Context, photo *models.Photo) error {
	// The claim runs out in minutes and a long video takes longer, so it is
	// renewed until the work ends.
	renewalContext, stopRenewal := context.WithCancel(ctx)
	renewalDone := make(chan struct{})
	go func() {
		defer deferutil.Recover()
		defer close(renewalDone)
		self.renewClaim(renewalContext, photo.ID)
	}()
	processed := &processedPhoto{photo: photo, isVideo: photo.PhotoKind == models.PhotoKindVideo}
	processError := self.processGuarded(ctx, processed)
	stopRenewal()
	<-renewalDone

	if ctx.Err() != nil {
		// Stopping is not the photo's fault. Its claim runs out and it is
		// taken again.
		return nil
	}
	if errors.Is(processError, errPhotoGone) {
		return nil
	}
	message, isFileAtFault, maximumAttemptCount := "", false, maximumProcessingAttemptCount
	if processError != nil {
		message = storeFailureMessage
		var failure *processingFailure
		if errors.As(processError, &failure) {
			isFileAtFault = failure.IsFileAtFault
			switch {
			case failure.Message != "":
				message = failure.Message
			case processed.isVideo:
				message = videoFailureMessage
			default:
				message = pictureFailureMessage
			}
		}
		if errors.Is(processError, errUnsupportedFile) || errors.Is(processError, errPictureTooLarge) {
			maximumAttemptCount = 1
		}
	}
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if processError == nil {
			return transaction.MarkPhotoReady(photo.ID)
		}
		return transaction.MarkPhotoProcessingFailed(photo.ID, message, isFileAtFault, maximumAttemptCount)
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

// processGuarded is processPhoto with a panic turned into a failure of the
// file. A file that makes a decoder panic would otherwise take the server down
// every time it was claimed.
func (self *Store) processGuarded(ctx context.Context, processed *processedPhoto) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Errorf("a panic while processing photo %s: %v\n%s", processed.photo.ID, recovered, debug.Stack())
			err = &processingFailure{Cause: fmt.Errorf("panic: %v", recovered), Message: panicFailureMessage, IsFileAtFault: true}
		}
	}()
	return self.processPhoto(ctx, processed)
}

// renewClaim keeps the claim on a photo alive until the context ends.
func (self *Store) renewClaim(ctx context.Context, photoId string) {
	ticker := time.NewTicker(claimRenewalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
				return transaction.TouchPhotoProcessingClaims([]string{photoId})
			}); err != nil && ctx.Err() == nil {
				log.Warningf("cannot renew the claim on photo %s being processed: %s", photoId, err)
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

	// isVideo says the file is a video, for choosing the words of a failure.
	isVideo bool

	// directory is a private directory inside the work directory, made when a
	// tool needs a path to read, and removed when processing ends.
	directory string
}

// workDirectoryOf is the private directory a photo's temporary files go in.
func (self *Store) workDirectoryOf(processed *processedPhoto) (string, error) {
	if processed.directory == "" {
		parent, err := self.prepareWorkDirectory()
		if err != nil {
			return "", err
		}
		directory, err := os.MkdirTemp(parent, "photo-")
		if err != nil {
			return "", fmt.Errorf("a private directory to work in could not be made: %w", err)
		}
		processed.directory = directory
	}
	return processed.directory, nil
}

// processPhoto reads what the photo's file says of itself and makes its
// previews. What was read is recorded before the previews are tried, so that
// a photo whose previews fail still has the right kind, size and date.
func (self *Store) processPhoto(ctx context.Context, processed *processedPhoto) error {
	photo := processed.photo
	var files []*models.PhotoFile
	var owner *models.User
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		if files, err = transaction.ListPhotoFiles(photo.UserID, []string{photo.ID}); err != nil {
			return err
		}
		owner, err = transaction.GetUser(photo.UserID)
		return err
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return errPhotoGone
		}
		return err
	}
	if owner == nil {
		return errPhotoGone
	}
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
		return &processingFailure{Cause: errors.New("the photo has no picture or video to read"), IsFileAtFault: true}
	}
	processed.metadata.OwnerLocation = ownerLocation(owner)
	processed.metadata.PhotoKind = settlePhotoKind(photo.PhotoKind, processed.source.ContentType, hasLiveVideo)
	processed.isVideo = strings.HasPrefix(processed.source.ContentType, "video/")

	if processed.isVideo {
		return self.processVideo(ctx, processed)
	}
	return self.processPicture(ctx, processed)
}

// ownerLocation is the time zone the owner is in, where a date the file wrote
// as a clock reading with no zone is taken to have been written. An account
// that has not set one, or whose name this server does not know, is on UTC.
func ownerLocation(owner *models.User) *time.Location {
	if owner.Timezone != "" {
		if location, err := time.LoadLocation(owner.Timezone); err == nil {
			return location
		}
	}
	return time.UTC
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

// openFile opens the bytes of one of a photo's files. Bytes the store does not
// have are lost for good, which is a failure of the file and not one to wait
// out.
func (self *Store) openFile(ctx context.Context, file *models.PhotoFile) (*storage.FileStream, error) {
	stream, err := self.files.OpenFileRange(ctx, models.PhotoBlobFileID(file.BlobSHA256), 0, file.FileSizeBytes)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, &processingFailure{Cause: err, IsFileAtFault: true}
	}
	return stream, err
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
	directory, err := self.workDirectoryOf(processed)
	if err != nil {
		return "", err
	}
	// A file larger than the room left is not started: it would fill the disk
	// the server's other files are on.
	if freeBytes, isKnown := freeDiskBytes(directory); isKnown && processed.source.FileSizeBytes > freeBytes {
		return "", fmt.Errorf("the file is %d bytes and the work directory has %d bytes free", processed.source.FileSizeBytes, freeBytes)
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
		return &processingFailure{
			Cause:         fmt.Errorf("%w: it is over %d MB", errPictureTooLarge, maximumPictureBytes>>20),
			Message:       tooLargeFailureMessage,
			IsFileAtFault: true,
		}
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

	var decodable []byte
	if isHeif {
		var err error
		workDirectory, err := self.prepareWorkDirectory()
		if err != nil {
			return err
		}
		if decodable, err = picture.DecodeHEIC(ctx, bytes.NewReader(content), workDirectory); err != nil {
			return readFailure(err)
		}
	} else {
		decodable = content
	}
	decoded, releasePixels, err := self.decodeWithinBudget(ctx, decodable)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, errPixelBudget) {
			return err
		}
		if errors.Is(err, picture.ErrTooLarge) {
			return &processingFailure{
				Cause:         fmt.Errorf("%w: %s", errPictureTooLarge, err),
				Message:       tooLargeFailureMessage,
				IsFileAtFault: true,
			}
		}
		if !isHeif && !isGoDecodableContentType(source.ContentType) {
			return &processingFailure{
				Cause:         fmt.Errorf("%w: %s files cannot be made into previews (%s)", errUnsupportedFile, source.ContentType, err),
				Message:       unsupportedFailureMessage,
				IsFileAtFault: true,
			}
		}
		return readFailure(err)
	}
	defer releasePixels()

	// The decoded picture is upright, so its size is the size it is shown at.
	bounds := decoded.Bounds()
	processed.metadata = db.PhotoMetadata{OwnerLocation: processed.metadata.OwnerLocation, PhotoKind: processed.metadata.PhotoKind, WidthPixels: bounds.Dx(), HeightPixels: bounds.Dy()}
	if err := self.saveMetadata(ctx, processed); err != nil {
		return err
	}
	large, err := self.keepPicturePreviews(ctx, processed.photo, decoded)
	// The full-size pixels are not needed again, and what faces are read from
	// is the large preview.
	releasePixels()
	if err != nil {
		return err
	}
	// Step 4: the faces. A failure here is logged and leaves the faces to be
	// found later; the photo is ready either way.
	self.findFaces(ctx, processed.photo, large)
	return nil
}

// errPixelBudget is a wait for room in the pixel budget that ended with the
// context.
var errPixelBudget = errors.New("photo: waiting for room to decode a picture was cut short")

// decodeWithinBudget decodes a JPEG, PNG or GIF once the pictures being decoded
// by all the workers leave room for this one. The caller calls the function it
// gets back when it has no more use for the full-size pixels; calling it more
// than once is harmless.
func (self *Store) decodeWithinBudget(ctx context.Context, content []byte) (image.Image, func(), error) {
	widthPixels, heightPixels, err := picture.ReadSize(content)
	if err != nil {
		return nil, nil, err
	}
	weight := min(int64(widthPixels)*int64(heightPixels), maximumPixelsInFlight)
	if err := self.pixelBudget.Acquire(ctx, weight); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", errPixelBudget, err)
	}
	var once sync.Once
	release := func() { once.Do(func() { self.pixelBudget.Release(weight) }) }
	decoded, err := picture.Decode(content)
	if err != nil {
		release()
		return nil, nil, err
	}
	return decoded, release, nil
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
	// A picture's date with no offset is a reading of the camera's clock.
	processed.metadata.IsTakenAtWallClock = metadata.TakenAt != nil && metadata.TakenAtOffsetMinutes == nil
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
// upright picture, and returns the large one, which is what faces are read
// from.
func (self *Store) keepPicturePreviews(ctx context.Context, photo *models.Photo, upright image.Image) (image.Image, error) {
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
			return nil, readFailure(err)
		}
		bounds := preview.scaled.Bounds()
		if err := self.keepPreview(ctx, photo, preview.photoPreviewSize, "image/jpeg", bytes.NewReader(encoded), int64(len(encoded)), bounds.Dx(), bounds.Dy()); err != nil {
			return nil, err
		}
	}
	return large, nil
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
		// A video's date is an instant, with an offset when its tags had one.
		processed.metadata.TakenAt, processed.metadata.TakenAtOffsetMinutes = probed.TakenAt, probed.TakenAtOffsetMinutes
		processed.metadata.Latitude, processed.metadata.Longitude = probed.Latitude, probed.Longitude
		processed.namePlace()
	}
	if err := self.saveMetadata(ctx, processed); err != nil {
		return err
	}
	if probeErr != nil {
		return readFailure(probeErr)
	}

	poster, err := picture.VideoPoster(ctx, path)
	if err != nil {
		return readFailure(err)
	}
	decoded, releasePixels, err := self.decodeWithinBudget(ctx, poster)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, errPixelBudget) {
			return err
		}
		return readFailure(err)
	}
	defer releasePixels()
	large, err := self.keepPicturePreviews(ctx, processed.photo, decoded)
	releasePixels()
	if err != nil {
		return err
	}
	// The faces in a video are those of its poster.
	self.findFaces(ctx, processed.photo, large)

	if probed.DurationMS > maximumVideoPreviewDuration.Milliseconds() {
		return nil
	}
	previewPath := filepath.Join(processed.directory, "preview.mp4")
	if err := picture.VideoPreview(ctx, path, previewPath); err != nil {
		return readFailure(err)
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
