package apigraph

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
	"github.com/ziyan/teanode/internal/storage"
)

// photoBytes is a stored file ready to be sent: what it is, how big, and how
// the answer should be labelled.
type photoBytes struct {
	fileId        string
	sizeBytes     int64
	contentType   string
	etag          string
	disposition   string
	cacheControl  string
	isRangeServed bool
}

// photoPreviewBytes is the preview of a photo of the owner's, ready to send.
// The address of a preview carries the time the photo last changed, so a
// preview that is made again has a new address and this one can be cached for
// good. Only a video preview is read in ranges, which is what a player does.
func (self *graph) photoPreviewBytes(ctx context.Context, ownerId, photoId string, photoPreviewSize models.PhotoPreviewSize) (*photoBytes, error) {
	var preview *models.PhotoPreview
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		preview, err = transaction.GetPhotoPreview(ownerId, photoId, photoPreviewSize)
		return err
	}); err != nil {
		return nil, err
	}
	return &photoBytes{
		fileId: models.PhotoPreviewFileID(preview.PhotoID, preview.PhotoPreviewSize), sizeBytes: preview.PreviewSizeBytes, contentType: preview.ContentType,
		etag:         fmt.Sprintf(`"%d-%d"`, preview.CreatedAt.UnixMicro(), preview.PreviewSizeBytes),
		disposition:  "inline",
		cacheControl: "private, max-age=31536000, immutable", isRangeServed: preview.PhotoPreviewSize == models.PhotoPreviewVideo,
	}, nil
}

// photoFileBytes is one file of a photo of the owner's, ready to send:
// disposition is "attachment" to save it and "inline" to play it.
func (self *graph) photoFileBytes(ctx context.Context, ownerId, photoId string, photoFileRole models.PhotoFileRole, disposition string) (*photoBytes, error) {
	var file *models.PhotoFile
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		file, err = transaction.GetPhotoFile(ownerId, photoId, photoFileRole)
		return err
	}); err != nil {
		return nil, err
	}
	return &photoBytes{
		fileId: models.PhotoBlobFileID(file.BlobSHA256), sizeBytes: file.FileSizeBytes, contentType: file.ContentType,
		etag:          `"` + file.BlobSHA256 + `"`,
		disposition:   mime.FormatMediaType(disposition, map[string]string{"filename": downloadName(file)}),
		cacheControl:  "private, no-cache",
		isRangeServed: true,
	}, nil
}

// photoAccessOf is why the person may read the photo, or db.ErrNotFound. The
// routes below serve a photo through its owner's rows once this has let the
// person in.
func (self *graph) photoAccessOf(ctx context.Context, userId, photoId string) (*models.PhotoAccess, error) {
	var access *models.PhotoAccess
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		access, err = transaction.GetPhotoAccess(userId, photoId)
		return err
	}); err != nil {
		return nil, err
	}
	return access, nil
}

// photoFileBytesFor is one file of a photo as the person may have it: the file
// itself for its owner and for a reader whose share includes where photos were
// taken, and for any other reader the preview that stands for it, under the
// name of a picture or a video, because the original's bytes carry the place.
// A Live Photo's video has no such preview and is not found.
func (self *graph) photoFileBytesFor(ctx context.Context, access *models.PhotoAccess, photoFileRole models.PhotoFileRole, disposition string) (*photoBytes, error) {
	if access.IsOwner || access.IsLocationIncluded {
		return self.photoFileBytes(ctx, access.OwnerUserID, access.PhotoID, photoFileRole, disposition)
	}
	var photoPreviewSize models.PhotoPreviewSize
	var found *models.Photo
	var files []*models.PhotoFile
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if photoFileRole == models.PhotoFileRoleLiveVideo {
			return db.ErrNotFound
		}
		var err error
		if found, err = transaction.GetPhoto(access.OwnerUserID, access.PhotoID); err != nil {
			return err
		}
		if files, err = transaction.ListPhotoFiles(access.OwnerUserID, []string{access.PhotoID}); err != nil {
			return err
		}
		previews, err := transaction.ListPhotoPreviews(access.OwnerUserID, []string{access.PhotoID})
		if err != nil {
			return err
		}
		var isAvailable bool
		if photoPreviewSize, isAvailable = photo.SharedCopyPreviewSize(found.PhotoKind, photo.HasPreviewIn(previews)); !isAvailable {
			return db.ErrNotFound
		}
		return nil
	}); err != nil {
		return nil, err
	}
	content, err := self.photoPreviewBytes(ctx, access.OwnerUserID, access.PhotoID, photoPreviewSize)
	if err != nil {
		return nil, err
	}
	content.disposition = mime.FormatMediaType(disposition, map[string]string{"filename": photo.SharedCopyFilename(found, files, photoPreviewSize)})
	content.cacheControl, content.isRangeServed = "private, no-cache", true
	return content, nil
}

// photoPreviewView serves a smaller copy of a photo to whoever may read the
// photo. A preview is made afresh and carries no place, so every reader has it.
func (self *graph) photoPreviewView(response http.ResponseWriter, request *http.Request) {
	user, ctx, isAuthorized := self.photoPerson(response, request, false)
	if !isAuthorized {
		return
	}
	photoPreviewSize := models.PhotoPreviewSize(mux.Vars(request)["photoPreviewSize"])
	if !photoPreviewSize.IsValid() {
		http.Error(response, "no such preview", http.StatusNotFound)
		return
	}
	access, err := self.photoAccessOf(ctx, user.ID, mux.Vars(request)["photoId"])
	if err != nil {
		photoHttpError(response, err)
		return
	}
	content, err := self.photoPreviewBytes(ctx, access.OwnerUserID, access.PhotoID, photoPreviewSize)
	if err != nil {
		photoHttpError(response, err)
		return
	}
	self.servePhotoBytes(response, request, ctx, content)
}

// photoFileView serves one file of a photo to whoever may read the photo, to
// save: the file itself where the person may have it, else the preview that
// stands for it (see photoFileBytesFor).
func (self *graph) photoFileView(response http.ResponseWriter, request *http.Request) {
	user, ctx, isAuthorized := self.photoPerson(response, request, false)
	if !isAuthorized {
		return
	}
	photoFileRole := models.PhotoFileRole(mux.Vars(request)["photoFileRole"])
	if !photoFileRole.IsValid() {
		http.Error(response, "no such file", http.StatusNotFound)
		return
	}
	access, err := self.photoAccessOf(ctx, user.ID, mux.Vars(request)["photoId"])
	if err != nil {
		photoHttpError(response, err)
		return
	}
	content, err := self.photoFileBytesFor(ctx, access, photoFileRole, "attachment")
	if err != nil {
		photoHttpError(response, err)
		return
	}
	self.servePhotoBytes(response, request, ctx, content)
}

// playbackPhotoOwner settles who a signed playback address is for, before any
// byte is read. The address itself is the credential: it names the owner,
// the photo, and one preview or file, and expires, and anything that does not
// check out is the same answer, so that an address cannot be used to learn
// which photos exist. The owner must still be able to keep a photo library,
// as when the address was made.
func (self *graph) playbackPhotoOwner(response http.ResponseWriter, request *http.Request, playbackKind, name string) (*models.User, context.Context, bool) {
	refuse := func() (*models.User, context.Context, bool) {
		http.Error(response, "the link is not good, or no longer", http.StatusNotFound)
		return nil, nil, false
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return nil, nil, false
	}
	query := request.URL.Query()
	ownerId, signature := query.Get("owner"), query.Get("signature")
	if self.config == nil || ownerId == "" || signature == "" || !self.isServiceEnabled(models.ServicePhotos) ||
		!photo.VerifyPlayback(self.config.Current().Secret(), signature, ownerId, mux.Vars(request)["photoId"], playbackKind, name, time.Now()) {
		return refuse()
	}
	var owner *models.User
	var permissions *models.EffectivePermissions
	if err := self.database.TransactionContext(request.Context(), func(transaction db.Transaction) (err error) {
		if owner, err = transaction.GetUser(ownerId); err != nil || owner == nil {
			return err
		}
		permissions, err = transaction.EffectivePermissions(ownerId)
		return err
	}); err != nil {
		log.Errorf("a photo owner could not be read: %s", err)
		http.Error(response, "the photo could not be read", http.StatusInternalServerError)
		return nil, nil, false
	}
	if owner == nil || owner.Disabled() || !permissions.Has(models.PermissionPhotosUse) {
		return refuse()
	}
	return owner, request.Context(), true
}

// photoPlaybackPreviewView serves a preview to whoever holds its signed
// address, for a player that cannot send a header.
func (self *graph) photoPlaybackPreviewView(response http.ResponseWriter, request *http.Request) {
	photoPreviewSize := models.PhotoPreviewSize(mux.Vars(request)["photoPreviewSize"])
	if !photoPreviewSize.IsValid() {
		http.Error(response, "the link is not good, or no longer", http.StatusNotFound)
		return
	}
	owner, ctx, isAuthorized := self.playbackPhotoOwner(response, request, photo.PlaybackKindPreview, string(photoPreviewSize))
	if !isAuthorized {
		return
	}
	content, err := self.photoPreviewBytes(ctx, owner.ID, mux.Vars(request)["photoId"], photoPreviewSize)
	if err != nil {
		photoHttpError(response, err)
		return
	}
	// The address is good for hours, not a year.
	content.cacheControl = "private, max-age=3600"
	self.servePhotoBytes(response, request, ctx, content)
}

// photoPlaybackFileView serves a file to whoever holds its signed address.
func (self *graph) photoPlaybackFileView(response http.ResponseWriter, request *http.Request) {
	photoFileRole := models.PhotoFileRole(mux.Vars(request)["photoFileRole"])
	if !photoFileRole.IsValid() {
		http.Error(response, "the link is not good, or no longer", http.StatusNotFound)
		return
	}
	owner, ctx, isAuthorized := self.playbackPhotoOwner(response, request, photo.PlaybackKindFile, string(photoFileRole))
	if !isAuthorized {
		return
	}
	content, err := self.photoFileBytes(ctx, owner.ID, mux.Vars(request)["photoId"], photoFileRole, "inline")
	if err != nil {
		photoHttpError(response, err)
		return
	}
	content.cacheControl = "private, max-age=3600"
	self.servePhotoBytes(response, request, ctx, content)
}

// downloadName is the name a downloaded file is saved under: the name it was
// uploaded with, or one made from where it belongs.
func downloadName(file *models.PhotoFile) string {
	if file.OriginalFilename != "" {
		return file.OriginalFilename
	}
	extensions := map[string]string{
		"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp", "image/heic": ".heic", "image/heif": ".heif",
		"video/mp4": ".mp4", "video/quicktime": ".mov",
	}
	extension, isKnown := extensions[file.ContentType]
	if !isKnown {
		extension = ".bin"
	}
	return "photo-" + file.PhotoID + "-" + string(file.PhotoFileRole) + extension
}

func photoHttpError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrNotFound), errors.Is(err, storage.ErrNotFound):
		http.Error(response, "not found", http.StatusNotFound)
	default:
		log.Errorf("a photo could not be read: %s", err)
		http.Error(response, "the photo could not be read", http.StatusInternalServerError)
	}
}

// servePhotoBytes sends a stored file with its validators, whole or, where
// the file is read in ranges, the range asked for. A range is honored only
// when the client's If-Range still names this version of the file.
func (self *graph) servePhotoBytes(response http.ResponseWriter, request *http.Request, ctx context.Context, content *photoBytes) {
	files, isStreaming := self.storage.(storage.StreamFiles)
	if !isStreaming {
		http.Error(response, "photo storage is unavailable", http.StatusServiceUnavailable)
		return
	}
	if content.sizeBytes <= 0 {
		http.Error(response, "not found", http.StatusNotFound)
		return
	}
	header := response.Header()
	header.Set("ETag", content.etag)
	header.Set("Cache-Control", content.cacheControl)
	if matched := request.Header.Get("If-None-Match"); matched != "" && etagMatches(matched, content.etag) {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	var err error
	offsetBytes, sizeBytes := int64(0), content.sizeBytes
	responseStatus := http.StatusOK
	requestedRange := request.Header.Get("Range")
	if content.isRangeServed {
		header.Set("Accept-Ranges", "bytes")
	}
	if content.isRangeServed && request.Method == http.MethodGet && requestedRange != "" && (request.Header.Get("If-Range") == "" || request.Header.Get("If-Range") == content.etag) {
		offsetBytes, sizeBytes, err = recordingByteRange(requestedRange, content.sizeBytes)
		if err != nil {
			header.Set("Content-Range", fmt.Sprintf("bytes */%d", content.sizeBytes))
			http.Error(response, "invalid byte range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		responseStatus = http.StatusPartialContent
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offsetBytes, offsetBytes+sizeBytes-1, content.sizeBytes))
	}
	header.Set("Content-Type", content.contentType)
	header.Set("Content-Length", strconv.FormatInt(sizeBytes, 10))
	header.Set("Content-Disposition", content.disposition)
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	header.Set("X-Content-Type-Options", "nosniff")
	if request.Method == http.MethodHead {
		response.WriteHeader(responseStatus)
		return
	}
	stream, err := files.OpenFileRange(ctx, content.fileId, offsetBytes, sizeBytes)
	if err != nil {
		// Before anything is sent, so the answer can still say what happened.
		header.Del("Content-Length")
		header.Del("Content-Range")
		header.Del("Content-Disposition")
		header.Del("ETag")
		photoHttpError(response, err)
		return
	}
	defer func() { _ = stream.Close() }()
	response.WriteHeader(responseStatus)
	if _, err := io.Copy(response, stream); err != nil {
		log.Debugf("sending a photo stopped: %s", err)
	}
}

// etagMatches says whether an If-None-Match names the version of the file.
func etagMatches(ifNoneMatch, etag string) bool {
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}
