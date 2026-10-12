package apigraph

import (
	"context"
	"fmt"
	"math"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// RecordingQuery exposes the owner's library and recoverable upload receipts.
type RecordingQuery interface {
	ListRecordings(context.Context, ListRecordingsArguments) ([]*models.Recording, error)
	ReadRecording(context.Context, RecordingArguments) (*RecordingView, error)
	ReadRecordingLimits(context.Context) (*RecordingLimitsView, error)
}

// RecordingMutation manages evidence metadata without submitting a user turn.
type RecordingMutation interface {
	CreateRecording(context.Context, CreateRecordingArguments) (*models.Recording, error)
	CreateRecordingMedia(context.Context, CreateRecordingMediaArguments) (*models.RecordingMedia, error)
	UpdateRecordingCapture(context.Context, UpdateRecordingCaptureArguments) (*models.Recording, error)
	DeleteRecording(context.Context, RecordingArguments) (bool, error)
}

// ListRecordingsArguments pages by the last server recording identifier.
type ListRecordingsArguments struct {
	BeforeRecordingID string `json:"beforeRecordingId"`
	PageSize          int    `json:"pageSize"`
}

// RecordingArguments names one account-owned source.
type RecordingArguments struct {
	RecordingID string `json:"recordingId"`
}

// RecordingView includes media and durable receipts for upload recovery.
type RecordingView struct {
	Recording   *models.Recording            `json:"recording"`
	MediaFiles  []*models.RecordingMedia     `json:"mediaFiles"`
	UploadParts []*models.RecordingMediaPart `json:"uploadParts"`
}

// RecordingLimitsView gives clients transport and original-media limits.
type RecordingLimitsView struct {
	MaximumMediaBytes   int64 `json:"maximumMediaBytes"`
	MaximumAccountBytes int64 `json:"maximumAccountBytes"`
	UploadPartBytes     int64 `json:"uploadPartBytes"`
}

// CreateRecordingArguments uses a stable local identifier for lost-response retries.
type CreateRecordingArguments struct {
	ClientRecordingID string                 `json:"clientRecordingId"`
	RecordingTitle    string                 `json:"recordingTitle"`
	RecordingOrigin   models.RecordingOrigin `json:"recordingOrigin"`
}

// CreateRecordingMediaArguments declares unchanged bytes and their timeline.
type CreateRecordingMediaArguments struct {
	RecordingID         string `json:"recordingId"`
	ClientMediaID       string `json:"clientMediaId"`
	OriginalFilename    string `json:"originalFilename"`
	OriginalContentType string `json:"originalContentType"`
	OriginalSizeBytes   int64  `json:"originalSizeBytes"`
	OriginalSHA256      string `json:"originalSha256"`
	TimelineStartMS     int64  `json:"timelineStartMS"`
	MediaDurationMS     int64  `json:"mediaDurationMS"`
}

// UpdateRecordingCaptureArguments preserves capture history under a generation check.
type UpdateRecordingCaptureArguments struct {
	RecordingID           string                       `json:"recordingId"`
	ExpectedGeneration    int64                        `json:"expectedGeneration"`
	RecordingCaptureState models.RecordingCaptureState `json:"recordingCaptureState"`
	TimelineDurationMS    int64                        `json:"timelineDurationMS"`
	CaptureGaps           []models.RecordingGap        `json:"captureGaps"`
}

func (self *graph) recordingLimits() (*RecordingLimitsView, error) {
	limits := self.config.Current().Agent.Limits
	maximumMediaBytes := limits.MaxRecordingMediaBytes.Bytes()
	maximumAccountBytes := limits.MaxRecordingAccountBytes.Bytes()
	if maximumMediaBytes == 0 {
		maximumMediaBytes = 2 << 30
	}
	if maximumAccountBytes == 0 {
		maximumAccountBytes = 64 << 30
	}
	if maximumMediaBytes > math.MaxInt64 || maximumAccountBytes > math.MaxInt64 {
		return nil, fmt.Errorf("recording limits exceed supported byte sizes")
	}
	return &RecordingLimitsView{MaximumMediaBytes: int64(maximumMediaBytes), MaximumAccountBytes: int64(maximumAccountBytes), UploadPartBytes: models.RecordingUploadPartBytes}, nil
}

func (self *graph) ListRecordings(ctx context.Context, arguments ListRecordingsArguments) ([]*models.Recording, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	pageSize := arguments.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	recordings, err := self.transaction(ctx).ListRecordings(principal.User.ID, arguments.BeforeRecordingID, pageSize)
	return recordings, translateError(err)
}

func (self *graph) ReadRecording(ctx context.Context, arguments RecordingArguments) (*RecordingView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	transaction := self.transaction(ctx)
	source, err := transaction.GetRecording(principal.User.ID, arguments.RecordingID)
	if err != nil {
		return nil, translateError(err)
	}
	mediaFiles, err := transaction.ListRecordingMedia(principal.User.ID, source.ID)
	if err != nil {
		return nil, translateError(err)
	}
	uploadParts := []*models.RecordingMediaPart{}
	for _, media := range mediaFiles {
		parts, err := transaction.ListRecordingMediaParts(principal.User.ID, media.ID)
		if err != nil {
			return nil, translateError(err)
		}
		uploadParts = append(uploadParts, parts...)
	}
	return &RecordingView{Recording: source, MediaFiles: mediaFiles, UploadParts: uploadParts}, nil
}

func (self *graph) ReadRecordingLimits(ctx context.Context) (*RecordingLimitsView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAgentUse); err != nil {
		return nil, err
	}
	if _, err := self.requireAccount(ctx); err != nil {
		return nil, err
	}
	return self.recordingLimits()
}

func (self *graph) CreateRecording(ctx context.Context, arguments CreateRecordingArguments) (*models.Recording, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	source, err := self.transaction(ctx).CreateRecording(&models.Recording{UserID: principal.User.ID, ClientRecordingID: arguments.ClientRecordingID, RecordingTitle: arguments.RecordingTitle, RecordingOrigin: arguments.RecordingOrigin})
	return source, translateError(err)
}

func (self *graph) CreateRecordingMedia(ctx context.Context, arguments CreateRecordingMediaArguments) (*models.RecordingMedia, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	limits, err := self.recordingLimits()
	if err != nil {
		return nil, err
	}
	media, err := self.transaction(ctx).CreateRecordingMedia(principal.User.ID, &models.RecordingMedia{RecordingID: arguments.RecordingID, ClientMediaID: arguments.ClientMediaID, OriginalFilename: arguments.OriginalFilename, OriginalContentType: arguments.OriginalContentType, OriginalSizeBytes: arguments.OriginalSizeBytes, OriginalSHA256: arguments.OriginalSHA256, TimelineStartMS: arguments.TimelineStartMS, MediaDurationMS: arguments.MediaDurationMS}, limits.MaximumMediaBytes, limits.MaximumAccountBytes)
	return media, translateError(err)
}

func (self *graph) UpdateRecordingCapture(ctx context.Context, arguments UpdateRecordingCaptureArguments) (*models.Recording, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	source, err := self.transaction(ctx).UpdateRecordingCapture(principal.User.ID, arguments.RecordingID, arguments.ExpectedGeneration, arguments.RecordingCaptureState, arguments.TimelineDurationMS, arguments.CaptureGaps)
	return source, translateError(err)
}

func (self *graph) DeleteRecording(ctx context.Context, arguments RecordingArguments) (bool, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return false, err
	}
	if principal.User == nil {
		return false, api.ErrNotLoggedIn
	}
	err = self.transaction(ctx).DeleteRecording(principal.User.ID, arguments.RecordingID)
	return err == nil, translateError(err)
}
