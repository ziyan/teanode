package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// ReadRecordingTranscriptTimelineArguments bounds the completed recording timeline.
type ReadRecordingTranscriptTimelineArguments struct {
	RecordingID   string `json:"recordingId"`
	SegmentOffset int    `json:"segmentOffset"`
	PageSize      int    `json:"pageSize"`
	// IsUnnamedOnly reads only the passages whose speaker nobody has named
	// (no confirmed name, no voice sample matched): what is left to label.
	// The offset and the count are then of those passages alone.
	IsUnnamedOnly *bool `json:"isUnnamedOnly" graphapi:"nullable"`
}

// RecordingTranscriptTimelineView retains original and immutable version provenance.
type RecordingTranscriptTimelineView struct {
	SourceGeneration int64                                `json:"sourceGeneration"`
	Segments         []*models.RecordingTranscriptSegment `json:"segments"`
	TranscriptRuns   []*models.RecordingTranscriptRun     `json:"transcriptRuns"`
	// TemporarySpeakers are the speakers of a capture still being recorded that nobody has named yet, or has named since.
	TemporarySpeakers []*models.RecordingTemporarySpeaker `json:"temporarySpeakers"`
	SegmentCount      int64                               `json:"segmentCount"`
	HasMoreSegments   bool                                `json:"hasMoreSegments"`

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
	isUnnamedOnly := arguments.IsUnnamedOnly != nil && *arguments.IsUnnamedOnly
	var segments []*models.RecordingTranscriptSegment
	var transcriptRuns []*models.RecordingTranscriptRun
	var segmentCount int64
	if isUnnamedOnly {
		if arguments.SegmentOffset < 0 || arguments.PageSize < 1 || arguments.PageSize > 250 {
			return nil, translateError(db.ErrInvalidArguments)
		}
		var unnamed []*models.RecordingTranscriptSegment
		if unnamed, transcriptRuns, err = transaction.ReadUnnamedRecordingTranscriptSegments(owner.ID, source.ID); err != nil {
			return nil, translateError(err)
		}
		segmentCount = int64(len(unnamed))
		start := min(arguments.SegmentOffset, len(unnamed))
		segments = unnamed[start:min(start+arguments.PageSize, len(unnamed))]
	} else {
		if segments, transcriptRuns, segmentCount, err = transaction.ReadRecordingTranscriptTimelinePage(owner.ID, source.ID, arguments.SegmentOffset, arguments.PageSize); err != nil {
			return nil, translateError(err)
		}
		if err := transaction.ProjectRecordingTranscriptTextCorrections(owner.ID, segments); err != nil {
			return nil, translateError(err)
		}
		if err := transaction.ProjectRecordingSpeakerCorrections(owner.ID, segments); err != nil {
			return nil, translateError(err)
		}
	}
	temporarySpeakers, err := transaction.ListRecordingTemporarySpeakers(owner.ID, source.ID)
	if err != nil {
		return nil, translateError(err)
	}
	liveUtterances := []*models.RecordingLiveUtterance{}
	if arguments.SegmentOffset == 0 && !isUnnamedOnly {
		if liveUtterances, err = transaction.ListRecordingLiveUtterances(owner.ID, source.ID); err != nil {
			return nil, translateError(err)
		}
	}
	return &RecordingTranscriptTimelineView{SourceGeneration: source.SourceGeneration, Segments: segments, TranscriptRuns: transcriptRuns, TemporarySpeakers: temporarySpeakers, SegmentCount: segmentCount, HasMoreSegments: int64(arguments.SegmentOffset)+int64(len(segments)) < segmentCount, LiveUtterances: liveUtterances}, nil
}
