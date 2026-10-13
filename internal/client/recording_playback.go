package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"

	"github.com/ziyan/teanode/internal/models"
)

const recordingPlaybackSelection = `{ recordingMediaId playbackPath playbackContentType playbackSizeBytes playbackSha256 playbackDurationMS timelineStartMS }`

func init() {
	RecordingDocuments["ReadRecordingPlayback"] = `query ($recordingMediaId: String!) { ReadRecordingPlayback(recordingMediaId: $recordingMediaId) ` + recordingPlaybackSelection + ` }`
	RecordingDocuments["PrepareRecordingPlayback"] = `mutation ($recordingMediaId: String!) { PrepareRecordingPlayback(recordingMediaId: $recordingMediaId) ` + recordingPlaybackSelection + ` }`
}

// RecordingPlayback describes a retained compatible audio preview, not an original.
type RecordingPlayback = models.RecordingPlayback

// RecordingPlaybackPath names the account-owned ready preview route.
func RecordingPlaybackPath(mediaId string) string { return RecordingMediaPath(mediaId) + "/playback" }

func validateRecordingPlayback(mediaId string, playback *RecordingPlayback) error {
	if playback == nil || mediaId == "" || playback.RecordingMediaID != mediaId || playback.PlaybackPath != RecordingPlaybackPath(mediaId) || playback.PlaybackContentType != "audio/wav" || playback.PlaybackSizeBytes <= 0 || playback.PlaybackSizeBytes == math.MaxInt64 || playback.PlaybackDurationMS <= 0 || playback.TimelineStartMS < 0 {
		return fmt.Errorf("client: compatible playback receipt does not match the owned original")
	}
	digest, err := hex.DecodeString(playback.PlaybackSHA256)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("client: compatible playback receipt has no valid digest")
	}
	return nil
}

// ReadRecordingPlayback inspects an existing preview without creating one.
func ReadRecordingPlayback(ctx context.Context, executor Executor, mediaId string) (*RecordingPlayback, error) {
	var playback *RecordingPlayback
	if err := RunRecording(ctx, executor, "ReadRecordingPlayback", map[string]any{"recordingMediaId": mediaId}, &playback); err != nil {
		return nil, err
	}
	if playback != nil {
		if err := validateRecordingPlayback(mediaId, playback); err != nil {
			return nil, err
		}
	}
	return playback, nil
}

// PrepareRecordingPlayback explicitly creates local compatible audio. It writes
// owned storage and must not be called during a read-only metadata refresh.
func PrepareRecordingPlayback(ctx context.Context, executor Executor, mediaId string) (*RecordingPlayback, error) {
	var playback *RecordingPlayback
	if err := RunRecording(ctx, executor, "PrepareRecordingPlayback", map[string]any{"recordingMediaId": mediaId}, &playback); err != nil {
		return nil, err
	}
	if err := validateRecordingPlayback(mediaId, playback); err != nil {
		return nil, err
	}
	return playback, nil
}

// DownloadRecordingPlayback verifies ready preview bytes using bounded memory.
// Discard the destination on failure, as with DownloadRecordingMedia.
func (self *Client) DownloadRecordingPlayback(ctx context.Context, playback *RecordingPlayback, destination io.Writer) error {
	if playback == nil {
		return fmt.Errorf("client: compatible playback is not ready")
	}
	if err := validateRecordingPlayback(playback.RecordingMediaID, playback); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, self.url+playback.PlaybackPath, nil)
	if err != nil {
		return err
	}
	if self.token != "" {
		request.Header.Set("Authorization", "Bearer "+self.token)
	}
	response, err := self.client.Do(request)
	if err != nil {
		return &ConnectionError{URL: self.url, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return &StatusError{URL: self.url, StatusCode: response.StatusCode}
	}
	digest := sha256.New()
	copiedSizeBytes, err := io.Copy(io.MultiWriter(destination, digest), io.LimitReader(response.Body, playback.PlaybackSizeBytes+1))
	if err != nil {
		return err
	}
	if copiedSizeBytes != playback.PlaybackSizeBytes || hex.EncodeToString(digest.Sum(nil)) != playback.PlaybackSHA256 {
		return fmt.Errorf("client: downloaded compatible audio does not match its receipt")
	}
	return nil
}
