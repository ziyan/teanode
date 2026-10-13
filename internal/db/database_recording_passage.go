package db

import (
	"errors"

	"gorm.io/gorm"
)

// ReadRecordingTranscriptPassageOffset locates original evidence without loading transcript bodies.
func (self *transaction) ReadRecordingTranscriptPassageOffset(userId, runId string, passageStartMS, passageEndMS int64) (int, error) {
	if passageStartMS < 0 || passageEndMS <= passageStartMS {
		return 0, ErrInvalidArguments
	}
	if _, err := self.GetRecordingTranscriptRun(userId, runId); err != nil {
		return 0, err
	}
	var firstSegment recordingSegmentModel
	if err := self.tx.Select("id,segment_start_ms").Where("recording_transcript_run_id = ? AND segment_start_ms < ? AND segment_end_ms > ?", runId, passageEndMS, passageStartMS).Order("segment_start_ms,id").Take(&firstSegment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	var segmentOffset int64
	if err := self.tx.Model(&recordingSegmentModel{}).Where("recording_transcript_run_id = ? AND (segment_start_ms < ? OR (segment_start_ms = ? AND id < ?))", runId, firstSegment.SegmentStartMS, firstSegment.SegmentStartMS, firstSegment.ID).Count(&segmentOffset).Error; err != nil {
		return 0, err
	}
	return int(segmentOffset), nil
}
