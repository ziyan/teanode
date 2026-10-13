package apigraph

import "context"

type ReadRecordingTranscriptPassageOffsetArguments struct {
	RecordingTranscriptRunID string `json:"recordingTranscriptRunId"`
	PassageStartMS           int64  `json:"passageStartMS"`
	PassageEndMS             int64  `json:"passageEndMS"`
}

func (self *graph) ReadRecordingTranscriptPassageOffset(ctx context.Context, arguments ReadRecordingTranscriptPassageOffsetArguments) (int, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return 0, err
	}
	segmentOffset, err := self.transaction(ctx).ReadRecordingTranscriptPassageOffset(userId, arguments.RecordingTranscriptRunID, arguments.PassageStartMS, arguments.PassageEndMS)
	return segmentOffset, translateError(err)
}
