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
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

// A recording remains playable when its agent is paused or unconfigured.
// Permission and current account checks still precede reading any upload body.
func (self *graph) recordingPerson(response http.ResponseWriter, request *http.Request) (*models.User, context.Context, bool) {
	username := api.UsernameFromRequest(request)
	if username == "" || username == config.LocalUsername {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return nil, nil, false
	}
	user, err := self.database.GetUserByUsername(username)
	if err != nil {
		http.Error(response, "failed to read the account", http.StatusInternalServerError)
		return nil, nil, false
	}
	if user == nil || user.Disabled() {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return nil, nil, false
	}
	err = self.database.TransactionContext(request.Context(), func(transaction db.Transaction) error {
		permissions, err := transaction.EffectivePermissions(user.ID)
		if err != nil {
			return err
		}
		if !permissions.Has(models.PermissionAgentUse) {
			return api.ErrPermissionDenied
		}
		return nil
	})
	if err != nil {
		http.Error(response, "recordings are unavailable for this account", http.StatusForbidden)
		return nil, nil, false
	}
	impersonator, err := self.impersonatorOf(request, user)
	if err != nil {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return nil, nil, false
	}
	ctx := db.ContextWithAuditPrincipal(request.Context(), self.auditPrincipal(request, user, impersonator))
	return user, ctx, true
}

func recordingHTTPError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrNotFound), errors.Is(err, storage.ErrNotFound):
		http.Error(response, "no such recording media", http.StatusNotFound)
	case errors.Is(err, db.ErrRecordingDeleted):
		http.Error(response, "recording was deleted", http.StatusGone)
	case errors.Is(err, db.ErrAlreadyExists):
		http.Error(response, "upload conflicts with existing bytes", http.StatusConflict)
	case errors.Is(err, db.ErrInvalidArguments):
		http.Error(response, "invalid recording upload", http.StatusBadRequest)
	default:
		log.Errorf("recording media operation failed: %s", err)
		http.Error(response, "recording media could not be stored or read", http.StatusInternalServerError)
	}
}

func (self *graph) recordingStore(response http.ResponseWriter) (*recording.Store, bool) {
	files, isStreaming := self.storage.(storage.StreamFiles)
	if !isStreaming {
		http.Error(response, "recording storage is unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	return recording.New(self.database, files), true
}

func (self *graph) recordingPartView(response http.ResponseWriter, request *http.Request) {
	user, ctx, isAuthorized := self.recordingPerson(response, request)
	if !isAuthorized {
		return
	}
	store, isAvailable := self.recordingStore(response)
	if !isAvailable {
		return
	}
	mediaId := mux.Vars(request)["mediaId"]
	partIndex, err := strconv.Atoi(mux.Vars(request)["partIndex"])
	if err != nil || partIndex < 0 {
		recordingHTTPError(response, db.ErrInvalidArguments)
		return
	}
	var media *models.RecordingMedia
	err = self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		media, err = transaction.GetRecordingMedia(user.ID, mediaId)
		return err
	})
	if err != nil {
		recordingHTTPError(response, err)
		return
	}
	expectedPartBytes := media.PartSizeBytes(partIndex)
	partSHA256 := request.Header.Get("X-Recording-Part-SHA256")
	contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if expectedPartBytes <= 0 || len(partSHA256) != 64 || err != nil || contentType != "application/octet-stream" {
		recordingHTTPError(response, db.ErrInvalidArguments)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, expectedPartBytes)
	content, err := io.ReadAll(request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(response, "upload part exceeds its declared size", http.StatusRequestEntityTooLarge)
			return
		}
		recordingHTTPError(response, db.ErrInvalidArguments)
		return
	}
	if int64(len(content)) != expectedPartBytes {
		recordingHTTPError(response, db.ErrInvalidArguments)
		return
	}
	receipt, err := store.PutPart(ctx, user.ID, mediaId, partIndex, partSHA256, content)
	if err != nil {
		recordingHTTPError(response, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, receipt)
}

func (self *graph) recordingCompleteView(response http.ResponseWriter, request *http.Request) {
	user, ctx, isAuthorized := self.recordingPerson(response, request)
	if !isAuthorized {
		return
	}
	if err := readableAsJSON(request); err != nil {
		http.Error(response, err.Error(), http.StatusUnsupportedMediaType)
		return
	}
	store, isAvailable := self.recordingStore(response)
	if !isAvailable {
		return
	}
	media, _, err := store.Complete(ctx, user.ID, mux.Vars(request)["mediaId"])
	if err != nil {
		recordingHTTPError(response, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, media)
}

func (self *graph) recordingMediaView(response http.ResponseWriter, request *http.Request) {
	user, ctx, isAuthorized := self.recordingPerson(response, request)
	if !isAuthorized {
		return
	}
	store, isAvailable := self.recordingStore(response)
	if !isAvailable {
		return
	}
	var media *models.RecordingMedia
	err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		media, err = transaction.GetRecordingMedia(user.ID, mux.Vars(request)["mediaId"])
		return err
	})
	if err != nil {
		recordingHTTPError(response, err)
		return
	}
	if media.RecordingUploadState != models.RecordingUploadComplete {
		http.Error(response, "recording media is not yet verified", http.StatusConflict)
		return
	}
	offsetBytes, sizeBytes := int64(0), media.OriginalSizeBytes
	responseStatus := http.StatusOK
	etag := fmt.Sprintf("\"%s\"", media.OriginalSHA256)
	requestedRange := request.Header.Get("Range")
	if request.Method == http.MethodGet && requestedRange != "" && (request.Header.Get("If-Range") == "" || request.Header.Get("If-Range") == etag) {
		offsetBytes, sizeBytes, err = recordingByteRange(requestedRange, media.OriginalSizeBytes)
		if err != nil {
			response.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", media.OriginalSizeBytes))
			http.Error(response, "invalid recording byte range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		responseStatus = http.StatusPartialContent
		response.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offsetBytes, offsetBytes+sizeBytes-1, media.OriginalSizeBytes))
	}
	response.Header().Set("Accept-Ranges", "bytes")
	response.Header().Set("ETag", etag)
	response.Header().Set("Content-Type", media.OriginalContentType)
	response.Header().Set("Content-Length", strconv.FormatInt(sizeBytes, 10))
	response.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": media.OriginalFilename}))
	response.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Cache-Control", "private, no-store")
	response.WriteHeader(responseStatus)
	if request.Method == http.MethodHead {
		return
	}
	if err := store.CopyRange(ctx, user.ID, media.ID, response, offsetBytes, sizeBytes); err != nil {
		log.Debugf("recording playback stopped: %s", err)
	}
}

// Only one range is needed for media playback. Multipart ranges would create
// extra unbounded reads without improving sequential player seek behavior.
func recordingByteRange(rangeHeader string, totalBytes int64) (int64, int64, error) {
	if totalBytes <= 0 || !strings.HasPrefix(rangeHeader, "bytes=") || strings.Contains(rangeHeader, ",") {
		return 0, 0, db.ErrInvalidArguments
	}
	bounds := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
	if len(bounds) != 2 {
		return 0, 0, db.ErrInvalidArguments
	}
	if bounds[0] == "" {
		suffixBytes, err := strconv.ParseInt(bounds[1], 10, 64)
		if err != nil || suffixBytes <= 0 {
			return 0, 0, db.ErrInvalidArguments
		}
		suffixBytes = min(suffixBytes, totalBytes)
		return totalBytes - suffixBytes, suffixBytes, nil
	}
	offsetBytes, err := strconv.ParseInt(bounds[0], 10, 64)
	if err != nil || offsetBytes < 0 || offsetBytes >= totalBytes {
		return 0, 0, db.ErrInvalidArguments
	}
	lastByte := totalBytes - 1
	if bounds[1] != "" {
		lastByte, err = strconv.ParseInt(bounds[1], 10, 64)
		if err != nil || lastByte < offsetBytes {
			return 0, 0, db.ErrInvalidArguments
		}
		lastByte = min(lastByte, totalBytes-1)
	}
	return offsetBytes, lastByte - offsetBytes + 1, nil
}
