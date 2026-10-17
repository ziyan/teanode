package photo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"unicode"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// errBlobGone is a blob that was trusted to be in the store and was not
// by the time the file naming it was recorded: cleanup got to it. The whole
// step is done again, this time storing the bytes.
var errBlobGone = errors.New("photo: the blob was deleted while the file was being recorded")

// errBlobBytesGone is a blob whose row is there and whose bytes are not: cleanup
// deleted the bytes and has not yet dropped the row. The step is done again with
// the bytes stored, whatever the row says.
var errBlobBytesGone = fmt.Errorf("%w: its bytes are not in the store", errBlobGone)

// ErrNotAPhoto is a file that is not a picture or a video that phones and
// cameras write.
var ErrNotAPhoto = errors.New("photo: the file is not a picture or a video of a kind a camera writes")

// ErrDigestMismatch is a file whose bytes are not the ones the uploader said.
var ErrDigestMismatch = errors.New("photo: the uploaded bytes do not match the declared SHA-256")

// ErrPartDamaged is an upload one of whose stored parts no longer matches the
// receipt it was given when it arrived: it was overwritten by a second request
// or damaged in the store. The upload cannot be completed, and ends, so that
// the client starts it again instead of being told to retry for ever.
var ErrPartDamaged = errors.New("photo: a stored part of the upload does not match its receipt; start the upload again")

// ErrEmptyFile is an upload that ended with no bytes.
var ErrEmptyFile = errors.New("photo: the file is empty")

// ErrWrongSize is an upload whose size is not the one declared.
var ErrWrongSize = errors.New("photo: the uploaded size does not match the declared size")

// segment is a stored file that is a stretch of a longer one.
type segment struct {
	fileId    string
	sizeBytes int64
}

// segmentReader reads stored files one after the other as if they were one.
// It seeks because storage rewinds its source before writing and may rewind
// it again to retry; a seek to where it already is costs nothing, because
// an object store sends one piece of a large file by seeking to its start
// on every read.
type segmentReader struct {
	ctx        context.Context
	files      storage.StreamFiles
	segments   []segment
	totalBytes int64
	position   int64

	// stream is the open segment, and streamPosition the position it will
	// read next.
	stream         *storage.FileStream
	streamPosition int64
}

func newSegmentReader(ctx context.Context, files storage.StreamFiles, segments []segment) *segmentReader {
	var totalBytes int64
	for _, entry := range segments {
		totalBytes += entry.sizeBytes
	}
	return &segmentReader{ctx: ctx, files: files, segments: segments, totalBytes: totalBytes}
}

func (self *segmentReader) Read(content []byte) (int, error) {
	if self.position >= self.totalBytes {
		return 0, io.EOF
	}
	if self.stream == nil || self.streamPosition != self.position {
		if err := self.open(); err != nil {
			return 0, err
		}
	}
	count, err := self.stream.Read(content)
	self.position += int64(count)
	self.streamPosition += int64(count)
	if errors.Is(err, io.EOF) {
		// The end of one segment is not the end of the file.
		_ = self.stream.Close()
		self.stream = nil
		if self.position < self.totalBytes {
			err = nil
		}
	}
	return count, err
}

// open opens the segment the position is in, at the position.
func (self *segmentReader) open() error {
	if self.stream != nil {
		_ = self.stream.Close()
		self.stream = nil
	}
	var startBytes int64
	for _, entry := range self.segments {
		if self.position < startBytes+entry.sizeBytes {
			offsetBytes := self.position - startBytes
			stream, err := self.files.OpenFileRange(self.ctx, entry.fileId, offsetBytes, entry.sizeBytes-offsetBytes)
			if err != nil {
				return err
			}
			self.stream, self.streamPosition = stream, self.position
			return nil
		}
		startBytes += entry.sizeBytes
	}
	return io.EOF
}

func (self *segmentReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += self.position
	case io.SeekEnd:
		offset += self.totalBytes
	default:
		return 0, fmt.Errorf("photo: invalid seek")
	}
	if offset < 0 {
		return 0, fmt.Errorf("photo: seek before the start")
	}
	self.position = offset
	return offset, nil
}

func (self *segmentReader) Close() error {
	if self.stream == nil {
		return nil
	}
	err := self.stream.Close()
	self.stream = nil
	return err
}

// completeUpload turns an upload whose parts have all arrived into a photo:
// it reads the parts back in order to check them against their receipts and
// the uploader's digest, stores the bytes under that digest, and records the
// file.
func (self *Store) completeUpload(ctx context.Context, userId string, upload *models.PhotoUpload) (*models.Photo, error) {
	var parts []*models.PhotoUploadPart
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		parts, err = transaction.ListPhotoUploadParts(userId, upload.ID)
		return err
	}); err != nil {
		return nil, err
	}
	segments := make([]segment, 0, len(parts))
	var totalBytes int64
	for partIndex, part := range parts {
		if part.PartIndex != partIndex {
			return nil, fmt.Errorf("photo: upload %s is missing part %d", upload.ID, partIndex)
		}
		segments = append(segments, segment{fileId: models.PhotoUploadPartFileID(upload.ID, partIndex), sizeBytes: part.PartSizeBytes})
		totalBytes += part.PartSizeBytes
	}
	if totalBytes == 0 {
		return nil, ErrEmptyFile
	}
	if upload.DeclaredSizeBytes != nil && *upload.DeclaredSizeBytes != totalBytes {
		return nil, ErrWrongSize
	}
	wholeDigest := sha256.New()
	var firstBytes []byte
	for partIndex, part := range parts {
		partDigest, err := self.readSegment(ctx, segments[partIndex], wholeDigest, &firstBytes)
		if errors.Is(err, storage.ErrNotFound) {
			// A part that has a receipt and no bytes is as unusable as one
			// that changed.
			log.Warningf("part %d of upload %s is not in the store", partIndex, upload.ID)
			return nil, ErrPartDamaged
		}
		if err != nil {
			return nil, err
		}
		// A part overwritten by a second request, or damaged in the store,
		// no longer matches the receipt the first request was given.
		if partDigest != part.PartSHA256 {
			log.Warningf("part %d of upload %s no longer matches its receipt", partIndex, upload.ID)
			return nil, ErrPartDamaged
		}
	}
	blobSha256 := hex.EncodeToString(wholeDigest.Sum(nil))
	headers := upload.PhotoUploadHeaders
	if headers.DeclaredSHA256 != "" && headers.DeclaredSHA256 != blobSha256 {
		return nil, ErrDigestMismatch
	}
	contentType, err := settleContentType(headers.ContentType, headers.OriginalFilename, firstBytes)
	if err != nil {
		return nil, err
	}
	headers.ContentType = contentType
	photo, err := self.keepFile(ctx, userId, &headers, newSegmentReader(ctx, self.files, segments), blobSha256, totalBytes, upload.ID)
	if err != nil {
		return nil, err
	}
	// The parts are the blob's bytes now. A failure here leaves them for
	// cleanup, which does it again once the upload has been idle.
	for _, entry := range segments {
		if err := self.files.DeleteFileEverywhere(ctx, entry.fileId); err != nil {
			log.Warningf("cannot delete %s of a completed photo upload yet: %s", entry.fileId, err)
		}
	}
	return photo, nil
}

// readSegment hashes a stored file into the whole file's digest, and returns
// the file's own, keeping the first bytes of the first one for sniffing a
// type from.
func (self *Store) readSegment(ctx context.Context, entry segment, wholeDigest io.Writer, firstBytes *[]byte) (string, error) {
	stream, err := self.files.OpenFileRange(ctx, entry.fileId, 0, entry.sizeBytes)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()
	partDigest := sha256.New()
	writer := io.MultiWriter(wholeDigest, partDigest)
	if *firstBytes == nil {
		head := make([]byte, min(entry.sizeBytes, 512))
		if _, err := io.ReadFull(stream, head); err != nil {
			return "", err
		}
		*firstBytes = head
		if _, err := writer.Write(head); err != nil {
			return "", err
		}
	}
	if _, err := io.Copy(writer, stream); err != nil {
		return "", err
	}
	return hex.EncodeToString(partDigest.Sum(nil)), nil
}

// extensionContentTypes covers the types a phone's or a camera's files have
// that the standard library's table does not know.
var extensionContentTypes = map[string]string{
	".heic": "image/heic", ".heif": "image/heif", ".avif": "image/avif", ".webp": "image/webp",
	".tif": "image/tiff", ".tiff": "image/tiff",
	".dng": "image/x-adobe-dng", ".nef": "image/x-nikon-nef", ".cr2": "image/x-canon-cr2", ".cr3": "image/x-canon-cr3",
	".arw": "image/x-sony-arw", ".raf": "image/x-fuji-raf", ".orf": "image/x-olympus-orf", ".rw2": "image/x-panasonic-rw2",
	".mov": "video/quicktime", ".mp4": "video/mp4", ".m4v": "video/x-m4v", ".3gp": "video/3gpp", ".3g2": "video/3gpp2",
	".webm": "video/webm", ".mkv": "video/x-matroska",
}

// allowedContentTypes are the pictures and videos that phones and cameras
// write. A library keeps what a person made with a camera, and serves it back
// to their browser, so what is kept is listed and not "anything that starts
// with image/": an SVG is a document that can carry script, and a type the
// previews cannot read is better refused where the person is told than kept
// where nothing can be done with it.
var allowedContentTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
	"image/heic": true, "image/heif": true, "image/heic-sequence": true, "image/heif-sequence": true,
	"image/avif": true, "image/tiff": true,
	"image/x-adobe-dng": true, "image/x-nikon-nef": true, "image/x-canon-cr2": true, "image/x-canon-cr3": true,
	"image/x-sony-arw": true, "image/x-fuji-raf": true, "image/x-olympus-orf": true, "image/x-panasonic-rw2": true,
	"video/mp4": true, "video/quicktime": true, "video/webm": true,
	"video/3gpp": true, "video/3gpp2": true, "video/x-m4v": true, "video/x-matroska": true,
}

// contentTypeSpellings are the other names uploaders give a type on the list.
var contentTypeSpellings = map[string]string{"image/jpg": "image/jpeg", "image/pjpeg": "image/jpeg", "video/x-quicktime": "video/quicktime"}

// settleContentType is the type to keep for a file. An uploader that says
// nothing, or says what a form says by default, is taken at its file name
// and then at the bytes. What is not a picture or a video that phones and
// cameras write is refused, with its type in the message: this is a photo
// library.
func settleContentType(declared, filename string, firstBytes []byte) (string, error) {
	contentType := strings.ToLower(declared)
	if contentType == "" || contentType == "application/octet-stream" || contentType == "application/x-www-form-urlencoded" {
		extension := strings.ToLower(path.Ext(filename))
		if known, isKnown := extensionContentTypes[extension]; isKnown {
			contentType = known
		} else if guessed := mime.TypeByExtension(extension); extension != "" && guessed != "" {
			contentType, _, _ = strings.Cut(guessed, ";")
		} else {
			contentType, _, _ = strings.Cut(http.DetectContentType(firstBytes), ";")
		}
	}
	if spelling, isRespelled := contentTypeSpellings[contentType]; isRespelled {
		contentType = spelling
	}
	if !allowedContentTypes[contentType] {
		return "", fmt.Errorf("%w: files of type %s are not kept", ErrNotAPhoto, contentType)
	}
	return contentType, nil
}

// checkDeclaredContentType refuses a type the request names that is not on the
// list of kinds that are kept. A request that names none, or names what a form
// says by default, is not judged here: its file name and its bytes decide.
func checkDeclaredContentType(declared string) error {
	contentType := strings.ToLower(declared)
	if contentType == "" || contentType == "application/octet-stream" || contentType == "application/x-www-form-urlencoded" {
		return nil
	}
	if spelling, isRespelled := contentTypeSpellings[contentType]; isRespelled {
		contentType = spelling
	}
	if !allowedContentTypes[contentType] {
		return fmt.Errorf("%w: files of type %s are not kept", ErrNotAPhoto, contentType)
	}
	return nil
}

// keepFile records a file whose bytes are readable from the source, under
// the digest of those bytes, as one file of the uploader's photo. Several
// people, or several photos, with the same bytes share one blob.
func (self *Store) keepFile(ctx context.Context, userId string, headers *models.PhotoUploadHeaders, source *segmentReader, blobSha256 string, sizeBytes int64, photoUploadId string) (*models.Photo, error) {
	configuration := self.configuration()
	maximumAccountBytes := int64(configuration.Photos.MaxAccountBytes)
	defer func() { _ = source.Close() }()
	var photo *models.Photo
	// isStoredAgain says the bytes were found missing from a blob whose row is
	// there, so that the next attempt stores them whatever the row says.
	isStoredAgain := false
	for attempt := 0; attempt < 3; attempt++ {
		// Whether the blob is already stored decides whether it is stored
		// again. A blob marked for deletion has bytes that may be half gone,
		// so it counts as absent.
		var existing *models.PhotoBlob
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			found, err := transaction.GetPhotoBlob(blobSha256)
			if errors.Is(err, db.ErrNotFound) {
				return nil
			}
			existing = found
			return err
		}); err != nil {
			return nil, err
		}
		hasStoredBytes := false
		if existing == nil || existing.DeletingAt != nil || isStoredAgain {
			if err := self.files.PutFileStream(ctx, models.PhotoBlobFileID(blobSha256), source, sizeBytes); err != nil {
				return nil, err
			}
			hasStoredBytes = true
			// The blob's row is written at once, in a transaction of its own,
			// before the one that records the file, which can fail: quota, the
			// database, a cancelled request. Bytes with no row are bytes
			// nothing can find to delete. With the row they are an
			// unreferenced blob, which cleanup deletes once it is an hour old
			// if no file has named it by then. The row goes after the bytes
			// and not before, so that a row is never there with bytes that are
			// not: a file arriving with the same digest trusts a row it finds.
			if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
				_, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: blobSha256, BlobSizeBytes: sizeBytes})
				return err
			}); err != nil {
				return nil, err
			}
		}
		err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			isInserted, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: blobSha256, BlobSizeBytes: sizeBytes})
			if err != nil {
				return err
			}
			// The row that was trusted, or the one made when the bytes were
			// stored, is gone and this made a new one: cleanup deleted the
			// bytes in between, and nothing stored them again.
			if isInserted && (existing != nil || hasStoredBytes) {
				return errBlobGone
			}
			var isRecordedAsOwn bool
			if photo, isRecordedAsOwn, err = self.photoForFile(transaction, userId, headers, blobSha256); err != nil {
				return err
			}
			// A photo that already has these bytes as its original has nothing
			// new to record: the same picture under another name is that
			// photo, and costs no room.
			if isRecordedAsOwn {
				_, err = transaction.SetPhotoFile(userId, photo.ID, &models.PhotoFile{
					PhotoFileRole: headers.PhotoFileRole, BlobSHA256: blobSha256, ContentType: headers.ContentType,
					FileSizeBytes: sizeBytes, OriginalFilename: headers.OriginalFilename,
				}, maximumAccountBytes)
				if errors.Is(err, db.ErrTooMuchAsked) {
					return &QuotaError{LimitBytes: maximumAccountBytes}
				}
				if err != nil {
					return err
				}
			}
			// The bytes are checked to be in the store once the file naming them
			// is recorded: cleanup deletes the bytes of a marked blob before it
			// drops the row, so a file recorded against bytes it had just
			// deleted would name nothing. Missing bytes are stored again.
			if err := self.checkBlobStored(ctx, blobSha256); err != nil {
				return err
			}
			if photoUploadId != "" {
				if _, err := transaction.CompletePhotoUpload(userId, photoUploadId, photo.ID); err != nil {
					return err
				}
			}
			photo, err = transaction.GetPhoto(userId, photo.ID)
			return err
		})
		if errors.Is(err, errBlobGone) || errors.Is(err, db.ErrPhotoBlobGone) {
			isStoredAgain = isStoredAgain || errors.Is(err, errBlobBytesGone)
			continue
		}
		if err != nil {
			return nil, err
		}
		return photo, nil
	}
	return nil, errBlobGone
}

// checkBlobStored says the blob's bytes are in the store, by reading one byte
// of them. A blob that is not is errBlobGone, and the whole step is done again
// with the bytes stored.
func (self *Store) checkBlobStored(ctx context.Context, blobSha256 string) error {
	stream, err := self.files.OpenFileRange(ctx, models.PhotoBlobFileID(blobSha256), 0, 1)
	if errors.Is(err, storage.ErrNotFound) {
		return errBlobBytesGone
	}
	if err != nil {
		return err
	}
	return stream.Close()
}

// photoForFile is the photo a file belongs to. It is the photo the uploader's
// name for it already finds (under the name it was made with or another it was
// recorded under), else, for an original, the photo of the person's that already
// has these bytes as its original: a picture kept from a conversation and then
// backed up by a phone is one photo, whatever the two called it. The name is
// recorded as one the photo has, so that the uploader finds the photo by it
// next time. Failing both a photo is made. The second value says the file is
// still to be recorded against the photo, which it is unless the photo already
// has exactly these bytes as its original.
func (self *Store) photoForFile(transaction db.Transaction, userId string, headers *models.PhotoUploadHeaders, blobSha256 string) (*models.Photo, bool, error) {
	found, err := transaction.FindPhotosByClientIDs(userId, []string{headers.ClientPhotoID})
	if err != nil {
		return nil, false, err
	}
	if len(found) > 0 {
		return found[0], true, nil
	}
	if headers.PhotoFileRole == models.PhotoFileRoleOriginal {
		same, err := transaction.FindPhotoByOriginalBlob(userId, blobSha256)
		switch {
		case err == nil:
			if err := transaction.AddPhotoClientAlias(userId, same.ID, headers.ClientPhotoID); err != nil {
				return nil, false, err
			}
			return same, false, nil
		case !errors.Is(err, db.ErrNotFound):
			return nil, false, err
		}
	}
	made, _, err := transaction.CreatePhoto(newPhoto(userId, headers))
	return made, true, err
}

// newPhoto is the photo an uploader's first file makes, from what the
// uploader said of it. What the file itself says, such as the date in its
// metadata, is read later, and what the uploader said wins.
func newPhoto(userId string, headers *models.PhotoUploadHeaders) *models.Photo {
	photoKind := headers.PhotoKind
	switch {
	case photoKind != "":
	case headers.PhotoFileRole == models.PhotoFileRoleLiveVideo:
		photoKind = models.PhotoKindLivePhoto
	case strings.HasPrefix(headers.ContentType, "video/"):
		photoKind = models.PhotoKindVideo
	default:
		photoKind = models.PhotoKindPicture
	}
	photo := &models.Photo{
		UserID: userId, ClientPhotoID: headers.ClientPhotoID, PhotoKind: photoKind, PhotoSource: headers.PhotoSource,
		TakenAt: headers.TakenAt, TakenAtOffsetMinutes: headers.TakenAtOffsetMinutes, Latitude: headers.Latitude, Longitude: headers.Longitude,
		IsFavorite: headers.IsFavorite, IsScreenshot: headers.IsScreenshot,
	}
	// The name of a Live Photo's video is not the name of the photo.
	if headers.PhotoFileRole != models.PhotoFileRoleLiveVideo {
		photo.OriginalFilename = headers.OriginalFilename
	}
	return photo
}

// SaveAttachment stores a file from a conversation as a photo of the
// person's. The photo is named by the digest of the file, so saving the same
// attachment twice is saving it once.
func (self *Store) SaveAttachment(ctx context.Context, userId string, attachment *models.AgentAttachment) (*models.Photo, error) {
	maximumFileBytes := int64(self.configuration().Photos.MaxFileBytes)
	if attachment.Size <= 0 {
		return nil, ErrEmptyFile
	}
	if attachment.Size > maximumFileBytes {
		return nil, &FileTooLargeError{LimitBytes: maximumFileBytes}
	}
	segments := []segment{{fileId: attachment.ID, sizeBytes: attachment.Size}}
	digest := sha256.New()
	var firstBytes []byte
	if _, err := self.readSegment(ctx, segments[0], digest, &firstBytes); err != nil {
		return nil, err
	}
	blobSha256 := hex.EncodeToString(digest.Sum(nil))
	contentType, err := settleContentType(attachment.ContentType, attachment.Name, firstBytes)
	if err != nil {
		return nil, err
	}
	headers := &models.PhotoUploadHeaders{
		ClientPhotoID: blobSha256, PhotoFileRole: models.PhotoFileRoleOriginal, PhotoSource: models.PhotoSourceConversation,
		OriginalFilename: attachment.Name, ContentType: contentType,
	}
	if len(headers.OriginalFilename) > maximumFilenameLength || strings.ContainsAny(headers.OriginalFilename, `/\`) {
		headers.OriginalFilename = path.Base(strings.ReplaceAll(headers.OriginalFilename, `\`, "/"))
		headers.OriginalFilename = strings.ToValidUTF8(headers.OriginalFilename[:min(len(headers.OriginalFilename), maximumFilenameLength)], "")
	}
	// The name is stored and shown, so it must pass what an uploader's name
	// passes. A file the person has is not refused for a name an attachment
	// came with; what the name may not hold is taken out of it.
	if checkText(headerFilename, headers.OriginalFilename, maximumFilenameLength) != nil {
		headers.OriginalFilename = strings.Map(func(character rune) rune {
			if unicode.IsControl(character) {
				return -1
			}
			return character
		}, strings.ToValidUTF8(headers.OriginalFilename, ""))
		if checkText(headerFilename, headers.OriginalFilename, maximumFilenameLength) != nil {
			headers.OriginalFilename = ""
		}
	}
	return self.keepFile(ctx, userId, headers, newSegmentReader(ctx, self.files, segments), blobSha256, attachment.Size, "")
}
