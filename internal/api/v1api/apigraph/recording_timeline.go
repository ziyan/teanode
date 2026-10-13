package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/models"
)

// ReadRecordingTranscriptTimelineArguments bounds the completed recording timeline.
type ReadRecordingTranscriptTimelineArguments struct {
	RecordingID   string `json:"recordingId"`
	SegmentOffset int    `json:"segmentOffset"`
	PageSize      int    `json:"pageSize"`
}

// RecordingTranscriptTimelineView retains original and immutable version provenance.
type RecordingTranscriptTimelineView struct {
	SourceGeneration int64                                `json:"sourceGeneration"`
	Segments         []*models.RecordingTranscriptSegment `json:"segments"`
	TranscriptRuns   []*models.RecordingTranscriptRun     `json:"transcriptRuns"`
	SegmentCount     int64                                `json:"segmentCount"`
	HasMoreSegments  bool                                 `json:"hasMoreSegments"`

	// LiveUtterances are words heard while the recording was still being
	// captured that no completed transcript run covers yet, in timeline
	// order and without a speaker. Only the first page has them.
	LiveUtterances []*models.RecordingLiveUtterance `json:"liveUtterances"`
}

func (self *graph) ReadRecordingTranscriptTimeline(ctx context.Context, arguments ReadRecordingTranscriptTimelineArguments) (*RecordingTranscriptTimelineView, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	source, err := transaction.GetRecording(owner.ID, arguments.RecordingID)
	if err != nil {
		return nil, translateError(err)
	}
	segments, transcriptRuns, segmentCount, err := transaction.ReadRecordingTranscriptTimelinePage(owner.ID, source.ID, arguments.SegmentOffset, arguments.PageSize)
	if err != nil {
		return nil, translateError(err)
	}
	if err := transaction.ProjectRecordingTranscriptTextCorrections(owner.ID, segments); err != nil {
		return nil, translateError(err)
	}
	if err := transaction.ProjectRecordingSpeakerCorrections(owner.ID, segments); err != nil {
		return nil, translateError(err)
	}
	liveUtterances := []*models.RecordingLiveUtterance{}
	if arguments.SegmentOffset == 0 {
		if liveUtterances, err = transaction.ListRecordingLiveUtterances(owner.ID, source.ID); err != nil {
			return nil, translateError(err)
		}
	}
	return &RecordingTranscriptTimelineView{SourceGeneration: source.SourceGeneration, Segments: segments, TranscriptRuns: transcriptRuns, SegmentCount: segmentCount, HasMoreSegments: int64(arguments.SegmentOffset)+int64(len(segments)) < segmentCount, LiveUtterances: liveUtterances}, nil
}
