package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// ImportRecordingFile preserves a seekable original with bounded transport memory.
// The caller retains the file on every outcome. Stable IDs make retries resume
// the same source, including a server receipt lost during a disconnected upload.
func (self *Client) ImportRecordingFile(ctx context.Context, source io.ReadSeeker, filename, contentType, recordingTitle, clientRecordingId string) (*RecordingView, error) {
	return self.importRecordingFile(ctx, source, filename, contentType, recordingTitle, clientRecordingId, nil)
}

// ResumeRecordingFile uploads only the exact retained original of an existing recording.
func (self *Client) ResumeRecordingFile(ctx context.Context, source io.ReadSeeker, expected *models.RecordingMedia) (*RecordingView, error) {
	if expected == nil {
		return nil, fmt.Errorf("client: an existing recording manifest is required")
	}
	return self.importRecordingFile(ctx, source, expected.OriginalFilename, expected.OriginalContentType, "", "", expected)
}

func (self *Client) importRecordingFile(ctx context.Context, source io.ReadSeeker, filename, contentType, recordingTitle, clientRecordingId string, expected *models.RecordingMedia) (*RecordingView, error) {
	if self.readOnly {
		return nil, &ReadOnlyError{URL: self.url}
	}
	var limits RecordingLimits
	if err := RunRecording(ctx, self, "ReadRecordingLimits", nil, &limits); err != nil {
		return nil, err
	}
	originalSizeBytes, err := source.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if originalSizeBytes <= 0 || originalSizeBytes > limits.MaximumMediaBytes {
		return nil, fmt.Errorf("client: original size exceeds recording limit or is empty")
	}
	if limits.UploadPartBytes != models.RecordingUploadPartBytes {
		return nil, fmt.Errorf("client: unsupported recording transport part size")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	digest := sha256.New()
	hashedSizeBytes, err := io.Copy(digest, io.LimitReader(&recordingContextReader{ctx: ctx, source: source}, originalSizeBytes+1))
	if err != nil {
		return nil, err
	}
	if hashedSizeBytes != originalSizeBytes {
		return nil, fmt.Errorf("client: original changed while its digest was calculated")
	}
	originalSha256 := hex.EncodeToString(digest.Sum(nil))
	if clientRecordingId == "" {
		clientRecordingId = "file-" + originalSha256
	}
	recordingOrigin := models.RecordingOriginAudioImport
	if strings.HasPrefix(contentType, "video/") {
		recordingOrigin = models.RecordingOriginVideoImport
	} else if !strings.HasPrefix(contentType, "audio/") {
		return nil, fmt.Errorf("client: choose an audio or video original")
	}
	var recording models.Recording
	var media models.RecordingMedia
	if expected != nil {
		if expected.OriginalSizeBytes != originalSizeBytes || expected.OriginalSHA256 != originalSha256 {
			return nil, fmt.Errorf("client: selected original does not match the pending recording manifest")
		}
		var ownedView RecordingView
		if err := RunRecording(ctx, self, "ReadRecording", map[string]any{"recordingId": expected.RecordingID}, &ownedView); err != nil {
			return nil, err
		}
		if ownedView.Recording == nil {
			return nil, fmt.Errorf("client: recording manifest is unavailable")
		}
		recording = *ownedView.Recording
		hasFoundMedia := false
		for _, candidate := range ownedView.MediaFiles {
			if candidate.ID == expected.ID && candidate.OriginalSHA256 == originalSha256 && candidate.OriginalSizeBytes == originalSizeBytes {
				media = *candidate
				hasFoundMedia = true
				break
			}
		}
		if !hasFoundMedia {
			return nil, fmt.Errorf("client: original does not belong to this recording")
		}
	} else {
		if err := RunRecording(ctx, self, "CreateRecording", map[string]any{"clientRecordingId": clientRecordingId, "recordingTitle": recordingTitle, "recordingOrigin": recordingOrigin}, &recording); err != nil {
			return nil, err
		}
		if recording.ID == "" || recording.ClientRecordingID != clientRecordingId || recording.RecordingOrigin != recordingOrigin {
			return nil, fmt.Errorf("client: recording response does not match the requested source")
		}
		if err := RunRecording(ctx, self, "CreateRecordingMedia", map[string]any{"recordingId": recording.ID, "clientMediaId": "original-" + originalSha256, "originalFilename": filepath.Base(filename), "originalContentType": contentType, "originalSizeBytes": originalSizeBytes, "originalSha256": originalSha256, "timelineStartMS": int64(0), "mediaDurationMS": int64(0)}, &media); err != nil {
			return nil, err
		}
	}
	expectedClientMediaId := "original-" + originalSha256
	if expected != nil {
		expectedClientMediaId = expected.ClientMediaID
	}
	if !recordingManifestMatches(&media, recording.ID, expectedClientMediaId, filepath.Base(filename), contentType, originalSizeBytes, originalSha256) {
		return nil, fmt.Errorf("client: recording manifest does not match the original request")
	}
	var view RecordingView
	if err := RunRecording(ctx, self, "ReadRecording", map[string]any{"recordingId": recording.ID}, &view); err != nil {
		return nil, err
	}
	receipts := make(map[int]*models.RecordingMediaPart)
	for _, receipt := range view.UploadParts {
		if receipt.RecordingMediaID == media.ID {
			receipts[receipt.PartIndex] = receipt
		}
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	partBuffer := make([]byte, models.RecordingUploadPartBytes)
	for partIndex := 0; partIndex < media.PartCount; partIndex++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		partSizeBytes := media.PartSizeBytes(partIndex)
		content := partBuffer[:partSizeBytes]
		if _, err := io.ReadFull(source, content); err != nil {
			return nil, err
		}
		partDigest := sha256.Sum256(content)
		partSha256 := hex.EncodeToString(partDigest[:])
		if receipt := receipts[partIndex]; receipt != nil && receipt.PartSHA256 == partSha256 && receipt.PartSizeBytes == partSizeBytes && !receipt.StoredAt.IsZero() {
			continue
		}
		if _, err := self.PutRecordingPart(ctx, media.ID, partIndex, content); err != nil {
			return nil, err
		}
	}
	if _, err := self.CompleteRecordingUpload(ctx, &media); err != nil {
		return nil, err
	}
	if err := RunRecording(ctx, self, "ReadRecording", map[string]any{"recordingId": recording.ID}, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

// DownloadRecordingMedia verifies unchanged bytes against an owned manifest.
// A caller must discard its destination on error rather than publish a partial file.
func (self *Client) DownloadRecordingMedia(ctx context.Context, media *models.RecordingMedia, destination io.Writer) error {
	if media == nil || media.ID == "" || media.RecordingUploadState != models.RecordingUploadComplete {
		return fmt.Errorf("client: a verified recording original is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, self.url+RecordingMediaPath(media.ID), nil)
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
	copiedSizeBytes, err := io.Copy(io.MultiWriter(destination, digest), io.LimitReader(response.Body, media.OriginalSizeBytes+1))
	if err != nil {
		return err
	}
	if copiedSizeBytes != media.OriginalSizeBytes || hex.EncodeToString(digest.Sum(nil)) != media.OriginalSHA256 {
		return fmt.Errorf("client: downloaded original does not match its manifest")
	}
	return nil
}

type recordingContextReader struct {
	ctx    context.Context
	source io.Reader
}

func (self *recordingContextReader) Read(content []byte) (int, error) {
	if err := self.ctx.Err(); err != nil {
		return 0, err
	}
	return self.source.Read(content)
}

func recordingManifestMatches(media *models.RecordingMedia, recordingId, clientMediaId, originalFilename, originalContentType string, originalSizeBytes int64, originalSha256 string) bool {
	if media == nil || media.ID == "" || originalSizeBytes <= 0 || media.RecordingID != recordingId || media.ClientMediaID != clientMediaId || media.OriginalFilename != originalFilename || media.OriginalContentType != originalContentType || media.OriginalSizeBytes != originalSizeBytes || media.OriginalSHA256 != originalSha256 {
		return false
	}
	expectedPartCount := (originalSizeBytes-1)/models.RecordingUploadPartBytes + 1
	if int64(media.PartCount) != expectedPartCount {
		return false
	}
	for partIndex := 0; partIndex < media.PartCount; partIndex++ {
		partSizeBytes := media.PartSizeBytes(partIndex)
		if partSizeBytes <= 0 || partSizeBytes > models.RecordingUploadPartBytes {
			return false
		}
	}
	return true
}
