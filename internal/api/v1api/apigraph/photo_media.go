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

	"github.com/gorilla/mux"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
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

// photoPreviewView serves a smaller copy of a photo to its owner. The address
// carries the time the photo last changed, so a preview that is made again
// has a new address and this one can be cached for good. Only a video
// preview is read in ranges, which is what a player does.
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
	var preview *models.PhotoPreview
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		preview, err = transaction.GetPhotoPreview(user.ID, mux.Vars(request)["photoId"], photoPreviewSize)
		return err
	}); err != nil {
		photoHttpError(response, err)
		return
	}
	self.servePhotoBytes(response, request, ctx, &photoBytes{
		fileId: models.PhotoPreviewFileID(preview.PhotoID, preview.PhotoPreviewSize), sizeBytes: preview.PreviewSizeBytes, contentType: preview.ContentType,
		etag:         fmt.Sprintf(`"%d-%d"`, preview.CreatedAt.UnixMicro(), preview.PreviewSizeBytes),
		disposition:  "inline",
		cacheControl: "private, max-age=31536000, immutable", isRangeServed: preview.PhotoPreviewSize == models.PhotoPreviewVideo,
	})
}

// photoFileView serves one file of a photo to its owner, to save.
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
	var file *models.PhotoFile
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		file, err = transaction.GetPhotoFile(user.ID, mux.Vars(request)["photoId"], photoFileRole)
		return err
	}); err != nil {
		photoHttpError(response, err)
		return
	}
	self.servePhotoBytes(response, request, ctx, &photoBytes{
		fileId: models.PhotoBlobFileID(file.BlobSHA256), sizeBytes: file.FileSizeBytes, contentType: file.ContentType,
		etag:          `"` + file.BlobSHA256 + `"`,
		disposition:   mime.FormatMediaType("attachment", map[string]string{"filename": downloadName(file)}),
		cacheControl:  "private, no-cache",
		isRangeServed: true,
	})
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
