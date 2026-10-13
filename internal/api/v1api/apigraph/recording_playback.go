package apigraph

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

// RecordingPlaybackQuery reads prepared owned audio without processing it.
type RecordingPlaybackQuery interface {
	ReadRecordingPlayback(context.Context, RecordingPlaybackArguments) (*models.RecordingPlayback, error)
}

// RecordingPlaybackMutation prepares a local derivative without model use.
type RecordingPlaybackMutation interface {
	PrepareRecordingPlayback(context.Context, RecordingPlaybackArguments) (*models.RecordingPlayback, error)
}

// RecordingPlaybackArguments identifies the immutable original media.
type RecordingPlaybackArguments struct {
	RecordingMediaID string `json:"recordingMediaId"`
}

func recordingPlaybackPath(playback *models.RecordingPlayback) *models.RecordingPlayback {
	if playback != nil {
		playback.PlaybackPath = api.Prefix + "/recordings/media/" + url.PathEscape(playback.RecordingMediaID) + "/playback"
	}
	return playback
}

func (self *graph) ReadRecordingPlayback(ctx context.Context, arguments RecordingPlaybackArguments) (*models.RecordingPlayback, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	files, isStreaming := self.storage.(storage.StreamFiles)
	if !isStreaming {
		return nil, fmt.Errorf("recording storage is unavailable")
	}
	playback, err := recording.New(self.database, files).ReadPlayback(ctx, owner.ID, arguments.RecordingMediaID)
	return recordingPlaybackPath(playback), translateError(err)
}

func (self *graph) PrepareRecordingPlayback(ctx context.Context, arguments RecordingPlaybackArguments) (*models.RecordingPlayback, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	files, isStreaming := self.storage.(storage.StreamFiles)
	if !isStreaming {
		return nil, fmt.Errorf("recording storage is unavailable")
	}
	limits, err := self.recordingLimits()
	if err != nil {
		return nil, err
	}
	// Finish authorization before local decoding. Reservation and publication
	// use separate transactions so failed storage remains discoverable for cleanup.
	if err := self.transaction(ctx).Commit(); err != nil {
		return nil, err
	}
	playback, err := recording.New(self.database, files).PreparePlayback(ctx, owner.ID, arguments.RecordingMediaID, limits.MaximumAccountBytes)
	return recordingPlaybackPath(playback), translateError(err)
}

func (self *graph) recordingPlaybackView(response http.ResponseWriter, request *http.Request) {
	owner, ctx, isAuthorized := self.recordingPerson(response, request)
	if !isAuthorized {
		return
	}
	store, isAvailable := self.recordingStore(response)
	if !isAvailable {
		return
	}
	mediaId := mux.Vars(request)["mediaId"]
	playback, err := store.ReadPlayback(ctx, owner.ID, mediaId)
	if err != nil {
		recordingHTTPError(response, err)
		return
	}
	if playback == nil {
		http.Error(response, "recording playback is not yet prepared", http.StatusNotFound)
		return
	}
	offsetBytes, sizeBytes := int64(0), playback.PlaybackSizeBytes
	responseStatus := http.StatusOK
	etag := fmt.Sprintf("\"%s\"", playback.PlaybackSHA256)
	requestedRange := request.Header.Get("Range")
	if request.Method == http.MethodGet && requestedRange != "" && (request.Header.Get("If-Range") == "" || request.Header.Get("If-Range") == etag) {
		offsetBytes, sizeBytes, err = recordingByteRange(requestedRange, playback.PlaybackSizeBytes)
		if err != nil {
			response.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", playback.PlaybackSizeBytes))
			http.Error(response, "invalid recording byte range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		responseStatus = http.StatusPartialContent
		response.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offsetBytes, offsetBytes+sizeBytes-1, playback.PlaybackSizeBytes))
	}
	response.Header().Set("Accept-Ranges", "bytes")
	response.Header().Set("ETag", etag)
	response.Header().Set("Content-Type", playback.PlaybackContentType)
	response.Header().Set("Content-Length", strconv.FormatInt(sizeBytes, 10))
	response.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": "recording-playback.wav"}))
	response.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Cache-Control", "private, no-store")
	response.WriteHeader(responseStatus)
	if request.Method == http.MethodHead {
		return
	}
	if err := store.CopyPlaybackRange(ctx, owner.ID, mediaId, response, offsetBytes, sizeBytes); err != nil {
		log.Debugf("recording playback stopped: %s", err)
	}
}
