package db

import (
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// ErrRecordingRetryInFlight prevents an old stopping worker racing a resumed claim.
var ErrRecordingRetryInFlight = errors.New("db: recording is still stopping; retry after its current job finishes")

// ErrRecordingRetryUncertain preserves potentially paid outcomes for explicit reprocessing.
var ErrRecordingRetryUncertain = errors.New("db: a provider outcome cannot be safely reused; request explicit paid reprocessing")

// RecordingRetryOperation resumes known unfinished processing without replacing generated evidence.
type RecordingRetryOperation interface {
	RetryRecordingTranscriptRun(userId, runId, agentId string) (*models.RecordingTranscriptRun, error)
}

func (self *transaction) RetryRecordingTranscriptRun(userId, runId, agentId string) (*models.RecordingTranscriptRun, error) {
	transcriptRun, err := self.GetRecordingTranscriptRun(userId, runId)
	if err != nil {
		return nil, err
	}
	if _, err := self.LockRecording(userId, transcriptRun.RecordingID); err != nil {
		return nil, err
	}
	transcriptRun, err = self.GetRecordingTranscriptRun(userId, runId)
	if err != nil {
		return nil, err
	}
	assistant, err := self.GetAgent(agentId)
	if err != nil {
		return nil, err
	}
	if assistant == nil || assistant.UserID != userId {
		return nil, ErrNotFound
	}
	if transcriptRun.RecordingProcessingState == models.RecordingProcessingComplete {
		return nil, ErrAlreadyExists
	}
	if transcriptRun.RecordingProcessingState == models.RecordingProcessingUncertain {
		return nil, ErrRecordingRetryUncertain
	}
	var activeJobCount int64
	if err := self.tx.Model(&agentJobModel{}).Where("kind = ? AND subject_id = ? AND status IN ?", models.AgentJobRecording, runId, []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning}).Count(&activeJobCount).Error; err != nil {
		return nil, err
	}
	if activeJobCount > 0 {
		if transcriptRun.RecordingProcessingState == models.RecordingProcessingQueued || transcriptRun.RecordingProcessingState == models.RecordingProcessingPreparing || transcriptRun.RecordingProcessingState == models.RecordingProcessingTranscribing {
			return transcriptRun, nil
		}
		return nil, ErrRecordingRetryInFlight
	}
	var uncertainAttemptCount int64
	if err := self.tx.Model(&recordingAttemptModel{}).Where("recording_transcript_run_id = ? AND recording_attempt_state IN ?", runId, []models.RecordingAttemptState{models.RecordingAttemptSubmitted, models.RecordingAttemptUncertain}).Count(&uncertainAttemptCount).Error; err != nil {
		return nil, err
	}
	if uncertainAttemptCount > 0 {
		return nil, ErrRecordingRetryUncertain
	}
	if transcriptRun.RecordingProcessingState == models.RecordingProcessingCanceled && transcriptRun.CompletedAt != nil {
		var discardedResultCount int64
		if err := self.tx.Model(&recordingAttemptModel{}).Where("recording_transcript_run_id = ? AND recording_attempt_state = ? AND completed_at >= ?", runId, models.RecordingAttemptComplete, transcriptRun.CompletedAt).Count(&discardedResultCount).Error; err != nil {
			return nil, err
		}
		if discardedResultCount > 0 {
			return nil, ErrRecordingRetryUncertain
		}
	}
	if err := self.tx.Model(&recordingRunModel{}).Where("id = ?", runId).Updates(map[string]any{"agent_id": agentId, "recording_processing_state": models.RecordingProcessingQueued, "processing_error_message": "", "completed_at": nil, "updated_at": time.Now().UTC().Truncate(time.Microsecond)}).Error; err != nil {
		return nil, err
	}
	return self.GetRecordingTranscriptRun(userId, runId)
}
