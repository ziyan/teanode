package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ziyan/teanode/internal/models"
)

const recordingSelection = `{ id userId createdAt updatedAt clientRecordingId recordingTitle recordingTitleSource recordingSummary recordingOrigin recordingCaptureState timelineDurationMS recordedAt sourceGeneration captureGaps { gapId gapStartMS gapEndMS recordingGapReason } }`
const recordingMediaSelection = `{ id recordingId createdAt clientMediaId originalFilename originalContentType originalSizeBytes originalSha256 timelineStartMS mediaDurationMS partCount recordingUploadState verifiedAt }`

// RecordingDocuments are shared by the CLI and the recording tool. All calls
// use the signed-in account's API permissions and original-media ownership.
var RecordingDocuments = map[string]string{
	"ListRecordings":         `query ($beforeRecordingId: String!, $pageSize: Int!) { ListRecordings(beforeRecordingId: $beforeRecordingId, pageSize: $pageSize) ` + recordingSelection + ` }`,
	"ReadRecording":          `query ($recordingId: String!) { ReadRecording(recordingId: $recordingId) { recording ` + recordingSelection + ` mediaFiles ` + recordingMediaSelection + ` uploadParts { recordingMediaId partIndex partSizeBytes partSha256 storedAt } } }`,
	"ReadRecordingLimits":    `query { ReadRecordingLimits { maximumMediaBytes maximumAccountBytes uploadPartBytes } }`,
	"CreateRecording":        `mutation ($clientRecordingId: String!, $recordingTitle: String!, $recordingOrigin: String!) { CreateRecording(clientRecordingId: $clientRecordingId, recordingTitle: $recordingTitle, recordingOrigin: $recordingOrigin) ` + recordingSelection + ` }`,
	"CreateRecordingMedia":   `mutation ($recordingId: String!, $clientMediaId: String!, $originalFilename: String!, $originalContentType: String!, $originalSizeBytes: Int64!, $originalSha256: String!, $timelineStartMS: Int64!, $mediaDurationMS: Int64!) { CreateRecordingMedia(recordingId: $recordingId, clientMediaId: $clientMediaId, originalFilename: $originalFilename, originalContentType: $originalContentType, originalSizeBytes: $originalSizeBytes, originalSha256: $originalSha256, timelineStartMS: $timelineStartMS, mediaDurationMS: $mediaDurationMS) ` + recordingMediaSelection + ` }`,
	"UpdateRecordingCapture": `mutation ($recordingId: String!, $expectedGeneration: Int64!, $recordingCaptureState: String!, $timelineDurationMS: Int64!, $captureGaps: [RecordingGapInput!]!) { UpdateRecordingCapture(recordingId: $recordingId, expectedGeneration: $expectedGeneration, recordingCaptureState: $recordingCaptureState, timelineDurationMS: $timelineDurationMS, captureGaps: $captureGaps) ` + recordingSelection + ` }`,
	"DeleteRecording":        `mutation ($recordingId: String!) { DeleteRecording(recordingId: $recordingId) }`,
}

// RecordingView includes the original manifests and durable transport receipts.
type RecordingView struct {
	Recording   *models.Recording            `json:"recording"`
	MediaFiles  []*models.RecordingMedia     `json:"mediaFiles"`
	UploadParts []*models.RecordingMediaPart `json:"uploadParts"`
}

// RecordingLimits names the dedicated upload and account-storage bounds.
type RecordingLimits struct {
	MaximumMediaBytes   int64 `json:"maximumMediaBytes"`
	MaximumAccountBytes int64 `json:"maximumAccountBytes"`
	UploadPartBytes     int64 `json:"uploadPartBytes"`
}

// RunRecording runs the same named operation for a CLI connection or agent run.
func RunRecording(ctx context.Context, executor Executor, operationName string, variables map[string]any, answered any) error {
	document, isKnown := RecordingDocuments[operationName]
	if !isKnown {
		return fmt.Errorf("client: unknown recording operation %q", operationName)
	}
	var envelope map[string]json.RawMessage
	if err := executor.Execute(ctx, document, variables, &envelope); err != nil {
		return err
	}
	if answered == nil {
		return nil
	}
	responseJSON, hasResponse := envelope[operationName]
	if !hasResponse {
		return fmt.Errorf("client: the server did not answer %s", operationName)
	}
	return json.Unmarshal(responseJSON, answered)
}

// RecordingMediaPath names private playback/download through the account API.
func RecordingMediaPath(mediaId string) string {
	return "/api/v1/recordings/media/" + url.PathEscape(mediaId)
}

// PutRecordingPart sends bounded unchanged bytes and validates the durable receipt.
func (self *Client) PutRecordingPart(ctx context.Context, mediaId string, partIndex int, content []byte) (*models.RecordingMediaPart, error) {
	if mediaId == "" || partIndex < 0 || len(content) == 0 || int64(len(content)) > models.RecordingUploadPartBytes {
		return nil, fmt.Errorf("client: invalid recording upload part")
	}
	digest := sha256.Sum256(content)
	partSha256 := hex.EncodeToString(digest[:])
	var receipt models.RecordingMediaPart
	if err := self.writeRecordingMedia(ctx, http.MethodPut, RecordingMediaPath(mediaId)+"/parts/"+strconv.Itoa(partIndex), content, partSha256, &receipt); err != nil {
		return nil, err
	}
	if receipt.RecordingMediaID != mediaId || receipt.PartIndex != partIndex || receipt.PartSizeBytes != int64(len(content)) || receipt.PartSHA256 != partSha256 || receipt.StoredAt.IsZero() {
		return nil, fmt.Errorf("client: recording part receipt does not match the uploaded bytes")
	}
	return &receipt, nil
}

// CompleteRecordingUpload verifies the whole-file receipt before a caller may
// release its local source. A missing or mismatched receipt is never success.
func (self *Client) CompleteRecordingUpload(ctx context.Context, expected *models.RecordingMedia) (*models.RecordingMedia, error) {
	if expected == nil || expected.ID == "" {
		return nil, fmt.Errorf("client: recording manifest is required")
	}
	var receipt models.RecordingMedia
	if err := self.writeRecordingMedia(ctx, http.MethodPost, RecordingMediaPath(expected.ID)+"/complete", []byte("{}"), "", &receipt); err != nil {
		return nil, err
	}
	if receipt.ID != expected.ID || receipt.RecordingID != expected.RecordingID || receipt.OriginalSizeBytes != expected.OriginalSizeBytes || receipt.OriginalSHA256 != expected.OriginalSHA256 || receipt.RecordingUploadState != models.RecordingUploadComplete || receipt.VerifiedAt == nil || receipt.VerifiedAt.IsZero() {
		return nil, fmt.Errorf("client: recording completion receipt does not match the original")
	}
	return &receipt, nil
}

func (self *Client) writeRecordingMedia(ctx context.Context, method, resourcePath string, content []byte, partSha256 string, answered any) error {
	if self.readOnly {
		return &ReadOnlyError{URL: self.url}
	}
	request, err := http.NewRequestWithContext(ctx, method, self.url+resourcePath, bytes.NewReader(content))
	if err != nil {
		return err
	}
	contentType := "application/json"
	if partSha256 != "" {
		contentType = "application/octet-stream"
		request.Header.Set("X-Recording-Part-SHA256", partSha256)
	}
	request.Header.Set("Content-Type", contentType)
	if self.token != "" {
		request.Header.Set("Authorization", "Bearer "+self.token)
	}
	response, err := self.client.Do(request)
	if err != nil {
		return &ConnectionError{URL: self.url, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: recording upload was refused", ErrUnauthorized)
	}
	if response.StatusCode != http.StatusOK {
		return &StatusError{URL: self.url, StatusCode: response.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<10)).Decode(answered); err != nil {
		return fmt.Errorf("client: recording receipt could not be read: %w", err)
	}
	return nil
}
