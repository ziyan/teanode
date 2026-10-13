package db

import (
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm/clause"
)

// RecordingLiveUtteranceOperation keeps the words heard while a recording
// is being captured until the saved audio has been transcribed.
type RecordingLiveUtteranceOperation interface {
	// SaveRecordingLiveUtterance stores one utterance of a capture that is
	// still going; the same provider utterance id again replaces it.
	SaveRecordingLiveUtterance(userId, recordingId string, utterance *models.RecordingLiveUtterance) error

	// ListRecordingLiveUtterances are the utterances no completed run
	// covers, in timeline order.
	ListRecordingLiveUtterances(userId, recordingId string) ([]*models.RecordingLiveUtterance, error)

	// DeleteSettledRecordingLiveUtterances deletes the utterances of up to
	// limit recordings whose words are in the saved transcript now.
	DeleteSettledRecordingLiveUtterances(limit int) (int64, error)
}

// The bounds of what a capture keeps.
const (
	// RecordingLiveUtterancesPerRecordingMaximum bounds the utterances one
	// recording keeps; a capture saying more than this is not being kept up
	// with by anyone reading it.
	RecordingLiveUtterancesPerRecordingMaximum = 5000

	// RecordingLiveUtteranceTextBytesMaximum bounds one utterance's words.
	RecordingLiveUtteranceTextBytesMaximum = 8192

	// recordingLiveUtterancesListed bounds the utterances a timeline read
	// returns: the latest of them.
	recordingLiveUtterancesListed = 500

	// recordingLiveUtterancesEndToleranceMS is how far short of the capture's
	// length the originals may end and still be taken as all of it.
	recordingLiveUtterancesEndToleranceMS = 2000

	// recordingLiveUtterancesStaleAfter is how long after its last change a
	// stopped capture keeps its live words when its audio is never
	// transcribed, for example because recording transcription is off.
	recordingLiveUtterancesStaleAfter = 24 * time.Hour
)

type recordingLiveUtteranceModel models.RecordingLiveUtterance

func (recordingLiveUtteranceModel) TableName() string { return "recording_live_utterance" }

func (self *transaction) SaveRecordingLiveUtterance(userId, recordingId string, utterance *models.RecordingLiveUtterance) error {
	if utterance == nil || utterance.UtteranceID == "" || len(utterance.UtteranceID) > 128 || utterance.UtteranceStartMS < 0 || utterance.UtteranceEndMS < utterance.UtteranceStartMS ||
		strings.TrimSpace(utterance.TranscriptText) == "" || len(utterance.TranscriptText) > RecordingLiveUtteranceTextBytesMaximum || !utf8.ValidString(utterance.TranscriptText) || strings.ContainsRune(utterance.TranscriptText, 0) {
		return ErrInvalidArguments
	}
	recording, err := self.GetRecording(userId, recordingId)
	if err != nil {
		return err
	}
	if !IsRecordingCaptureLive(recording.RecordingCaptureState) {
		return ErrInvalidArguments
	}
	var existingCount int64
	if err := self.tx.Model(&recordingLiveUtteranceModel{}).Where("recording_id = ? AND utterance_id <> ?", recordingId, utterance.UtteranceID).Count(&existingCount).Error; err != nil {
		return err
	}
	if existingCount >= RecordingLiveUtterancesPerRecordingMaximum {
		return ErrInvalidArguments
	}
	row := recordingLiveUtteranceModel{ID: newID(), RecordingID: recordingId, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), UtteranceID: utterance.UtteranceID, UtteranceStartMS: utterance.UtteranceStartMS, UtteranceEndMS: utterance.UtteranceEndMS, TranscriptText: utterance.TranscriptText}
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "recording_id"}, {Name: "utterance_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"utterance_start_ms", "utterance_end_ms", "transcript_text"}),
	}).Create(&row).Error
}

// IsRecordingCaptureLive says whether a capture can still be growing: it has
// not been stopped, and it was not imported from a file.
func IsRecordingCaptureLive(captureState models.RecordingCaptureState) bool {
	return captureState == models.RecordingCaptureActive || captureState == models.RecordingCapturePaused || captureState == models.RecordingCaptureInterrupted
}

func (self *transaction) ListRecordingLiveUtterances(userId, recordingId string) ([]*models.RecordingLiveUtterance, error) {
	if _, err := self.GetRecording(userId, recordingId); err != nil {
		return nil, err
	}
	var rows []recordingLiveUtteranceModel
	if err := self.tx.Where("recording_id = ?", recordingId).Order("utterance_start_ms DESC, id DESC").Limit(recordingLiveUtterancesListed + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	// Spans of the originals whose speaker-separated transcript is done,
	// where the live words are no longer needed. An utterance the cut
	// between two originals runs across is covered once both are done.
	var spans []struct {
		SpanStartMS int64
		SpanEndMS   int64
	}
	if err := self.tx.Raw(`SELECT timeline_start_ms AS span_start_ms, timeline_start_ms + media_duration_ms AS span_end_ms FROM recording_media
		WHERE recording_id = ? AND EXISTS (SELECT 1 FROM recording_transcript_run WHERE recording_transcript_run.recording_media_id = recording_media.id AND recording_processing_state = ?)
		ORDER BY timeline_start_ms, id`, recordingId, models.RecordingProcessingComplete).Scan(&spans).Error; err != nil {
		return nil, err
	}
	merged := make([][2]int64, 0, len(spans))
	for _, span := range spans {
		if last := len(merged) - 1; last >= 0 && span.SpanStartMS <= merged[last][1] {
			merged[last][1] = max(merged[last][1], span.SpanEndMS)
			continue
		}
		merged = append(merged, [2]int64{span.SpanStartMS, span.SpanEndMS})
	}
	utterances := make([]*models.RecordingLiveUtterance, 0, len(rows))
	for index := range rows {
		row := &rows[index]
		isCovered := slices.ContainsFunc(merged, func(span [2]int64) bool {
			return row.UtteranceStartMS >= span[0] && row.UtteranceEndMS <= span[1]
		})
		if !isCovered {
			utterances = append(utterances, (*models.RecordingLiveUtterance)(row))
		}
	}
	if len(utterances) > recordingLiveUtterancesListed {
		utterances = utterances[:recordingLiveUtterancesListed]
	}
	slices.Reverse(utterances)
	return utterances, nil
}

func (self *transaction) DeleteSettledRecordingLiveUtterances(limit int) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalidArguments
	}
	// Stopped, and every original has a completed run and the originals
	// reach the end of the capture (a phone publishes the stop before it has
	// uploaded the last piece, whose words must stay until it is read), or
	// stopped so long ago that the audio is evidently not being
	// transcribed.
	result := self.tx.Exec(`DELETE FROM recording_live_utterance WHERE recording_id IN (
		SELECT recording.id FROM recording
		WHERE recording.recording_capture_state = ?
		AND (recording.updated_at < ? OR (
			COALESCE((SELECT MAX(recording_media.timeline_start_ms + recording_media.media_duration_ms) FROM recording_media WHERE recording_media.recording_id = recording.id), 0) >= recording.timeline_duration_ms - ?
			AND NOT EXISTS (
				SELECT 1 FROM recording_media WHERE recording_media.recording_id = recording.id
				AND NOT EXISTS (SELECT 1 FROM recording_transcript_run WHERE recording_transcript_run.recording_media_id = recording_media.id AND recording_transcript_run.recording_processing_state = ?))))
		AND EXISTS (SELECT 1 FROM recording_live_utterance WHERE recording_live_utterance.recording_id = recording.id)
		LIMIT ?)`, models.RecordingCaptureStopped, time.Now().Add(-recordingLiveUtterancesStaleAfter).UTC(), recordingLiveUtterancesEndToleranceMS, models.RecordingProcessingComplete, limit)
	return result.RowsAffected, result.Error
}

// deleteRecordingLiveUtterances removes what a recording's live words were
// kept for; it does not fail when there are none.
func (self *transaction) deleteRecordingLiveUtterances(recordingId string) error {
	if recordingId == "" {
		return errors.New("db: a recording is needed")
	}
	return self.tx.Where("recording_id = ?", recordingId).Delete(&recordingLiveUtteranceModel{}).Error
}
