package apigraph

import (
	"context"
	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// RecordingTranscriptQuery reads generated evidence without requiring an active agent.
type RecordingTranscriptQuery interface {
	ReadRecordingTranscriptTimeline(context.Context, ReadRecordingTranscriptTimelineArguments) (*RecordingTranscriptTimelineView, error)
	ReadRecordingTranscript(context.Context, ReadRecordingTranscriptArguments) (*RecordingTranscriptView, error)
}

// RecordingTranscriptMutation requests paid versions or cancels further processing.
type RecordingTranscriptMutation interface {
	RetryRecordingTranscription(context.Context, RecordingTranscriptArguments) (*models.RecordingTranscriptRun, error)
	RequestRecordingTranscription(context.Context, RequestRecordingTranscriptionArguments) (*models.RecordingTranscriptRun, error)
	CancelRecordingTranscription(context.Context, RecordingTranscriptArguments) (*models.RecordingTranscriptRun, error)
}

// RecordingTranscriptArguments names an account-owned generated version.
type RecordingTranscriptArguments struct {
	RecordingTranscriptRunID string `json:"recordingTranscriptRunId"`
}

// ReadRecordingTranscriptArguments bounds transcript pages.
type ReadRecordingTranscriptArguments struct {
	RecordingTranscriptRunID string `json:"recordingTranscriptRunId"`
	SegmentOffset            int    `json:"segmentOffset"`
	PageSize                 int    `json:"pageSize"`
}

// RequestRecordingTranscriptionArguments preserves retry identity for a paid request.
type RequestRecordingTranscriptionArguments struct {
	RecordingMediaID        string `json:"recordingMediaId"`
	ClientRunID             string `json:"clientRunId"`
	IsReprocessingRequested bool   `json:"isReprocessingRequested"`
}

// RecordingTranscriptView retains request-local attribution and usage provenance.
type RecordingTranscriptView struct {
	TranscriptRun   *models.RecordingTranscriptRun          `json:"transcriptRun"`
	Segments        []*models.RecordingTranscriptSegment    `json:"segments"`
	Attempts        []*models.RecordingTranscriptionAttempt `json:"attempts"`
	SegmentCount    int64                                   `json:"segmentCount"`
	HasMoreSegments bool                                    `json:"hasMoreSegments"`
}

func (self *graph) recordingAccount(ctx context.Context) (*models.User, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal.User, nil
}
func (self *graph) ReadRecordingTranscript(ctx context.Context, arguments ReadRecordingTranscriptArguments) (*RecordingTranscriptView, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	transcriptRun, err := transaction.GetRecordingTranscriptRun(owner.ID, arguments.RecordingTranscriptRunID)
	if err != nil {
		return nil, translateError(err)
	}
	segments, segmentCount, err := transaction.ReadRecordingTranscriptPage(owner.ID, transcriptRun.ID, arguments.SegmentOffset, arguments.PageSize)
	if err != nil {
		return nil, translateError(err)
	}
	if err := transaction.ProjectRecordingTranscriptTextCorrections(owner.ID, segments); err != nil {
		return nil, translateError(err)
	}
	attempts, err := transaction.ListRecordingTranscriptionAttempts(owner.ID, transcriptRun.ID)
	if err != nil {
		return nil, translateError(err)
	}
	return &RecordingTranscriptView{TranscriptRun: transcriptRun, Segments: segments, Attempts: attempts, SegmentCount: segmentCount, HasMoreSegments: int64(arguments.SegmentOffset)+int64(len(segments)) < segmentCount}, nil
}
func (self *graph) RequestRecordingTranscription(ctx context.Context, arguments RequestRecordingTranscriptionArguments) (*models.RecordingTranscriptRun, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	transcriptRun, err := worker.QueueRecordingTranscription(self.transaction(ctx), owner.ID, arguments.RecordingMediaID, arguments.ClientRunID, arguments.IsReprocessingRequested)
	return transcriptRun, translateError(err)
}
func (self *graph) CancelRecordingTranscription(ctx context.Context, arguments RecordingTranscriptArguments) (*models.RecordingTranscriptRun, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	if worker := self.agentWorker(); worker != nil {
		transcriptRun, err := worker.CancelRecordingTranscription(transaction, owner.ID, arguments.RecordingTranscriptRunID)
		return transcriptRun, translateError(err)
	}
	transcriptRun, err := transaction.CancelRecordingTranscriptRun(owner.ID, arguments.RecordingTranscriptRunID)
	return transcriptRun, translateError(err)
}

func (self *graph) RetryRecordingTranscription(ctx context.Context, arguments RecordingTranscriptArguments) (*models.RecordingTranscriptRun, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	transcriptRun, err := worker.RetryRecordingTranscription(self.transaction(ctx), owner.ID, arguments.RecordingTranscriptRunID)
	return transcriptRun, translateError(err)
}
