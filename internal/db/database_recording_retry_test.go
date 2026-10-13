package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func finishRecordingFixtureJob(test *testing.T, fixture *transcriptFixture) {
	test.Helper()
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		didFinish, err := transaction.FinishAgentJob(fixture.claim.JobID, fixture.claim.ClaimID, &db.AgentJobOutcome{JobStatus: models.AgentJobDead, FinishedAt: time.Now()})
		if err != nil || !didFinish {
			test.Fatal("recording worker was not released", err)
		}
	})
}

func TestRecordingRetryKeepsVersionCompletedEvidenceAndOwnership(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		activeRun, err := transaction.RetryRecordingTranscriptRun(fixture.userId, fixture.transcriptRun.ID, fixture.agentId)
		if err != nil || activeRun.ID != fixture.transcriptRun.ID {
			test.Fatal("active retry was not idempotent", err)
		}
		_, err = transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete, PromptTokens: 11, HasProviderUsage: true}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "local", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "Retained evidence."}})
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingFailed, "fixture failure", 1000); err != nil {
			test.Fatal(err)
		}
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.RetryRecordingTranscriptRun(fixture.userId, fixture.transcriptRun.ID, fixture.agentId)
		return err
	})
	if !errors.Is(err, db.ErrRecordingRetryInFlight) {
		test.Fatal("retry raced a stopping worker", err)
	}
	finishRecordingFixtureJob(test, fixture)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		resumed, err := transaction.RetryRecordingTranscriptRun(fixture.userId, fixture.transcriptRun.ID, fixture.agentId)
		if err != nil || resumed.ID != fixture.transcriptRun.ID || resumed.VersionNumber != fixture.transcriptRun.VersionNumber || resumed.RecordingProcessingState != models.RecordingProcessingQueued || resumed.CompletedAt != nil {
			test.Fatal("safe retry replaced the version", err)
		}
		segments, segmentCount, err := transaction.ReadRecordingTranscriptPage(fixture.userId, resumed.ID, 0, 20)
		if err != nil || segmentCount != 1 || len(segments) != 1 || segments[0].TranscriptText != "Retained evidence." {
			test.Fatal("retry discarded completed transcript evidence", err)
		}
		attempts, err := transaction.ListRecordingTranscriptionAttempts(fixture.userId, resumed.ID)
		if err != nil || len(attempts) != 1 || attempts[0].ID != attempt.ID || attempts[0].PromptTokens != 11 {
			test.Fatal("retry discarded paid usage", err)
		}
	})
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "retry-other")
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.RetryRecordingTranscriptRun(otherOwnerId, fixture.transcriptRun.ID, fixture.agentId)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("foreign owner retried recording", err)
	}
	var otherAgentId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		otherAgent, err := transaction.CreateAgent(&models.Agent{UserID: otherOwnerId, Name: "Other assistant", Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		otherAgentId = otherAgent.ID
	})
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.RetryRecordingTranscriptRun(fixture.userId, fixture.transcriptRun.ID, otherAgentId)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("retry assigned a foreign agent", err)
	}
}

func TestRecordingRetryRefusesUnknownAndCanceledDiscardedResults(test *testing.T) {
	for _, outcome := range []string{"submitted", "uncertain", "discarded", "safe-cancel"} {
		test.Run(outcome, func(test *testing.T) {
			fixture := newTranscriptFixture(test)
			chunk, blob := fixture.createChunk(test)
			var attempt *models.RecordingTranscriptionAttempt
			if outcome != "safe-cancel" {
				attempt = fixture.beginAttempt(test, chunk, blob)
			}
			dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
				if outcome == "discarded" || outcome == "safe-cancel" {
					if _, err := transaction.CancelRecordingTranscriptRun(fixture.userId, fixture.transcriptRun.ID); err != nil {
						test.Fatal(err)
					}
				}
				if outcome == "uncertain" || outcome == "discarded" {
					attemptState := models.RecordingAttemptUncertain
					if outcome == "discarded" {
						attemptState = models.RecordingAttemptComplete
					}
					if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: attemptState}, nil); err != nil {
						test.Fatal(err)
					}
				}
			})
			finishRecordingFixtureJob(test, fixture)
			err := fixture.database.Transaction(func(transaction db.Transaction) error {
				_, err := transaction.RetryRecordingTranscriptRun(fixture.userId, fixture.transcriptRun.ID, fixture.agentId)
				return err
			})
			if outcome == "safe-cancel" {
				if err != nil {
					test.Fatal("cancel before submission could not resume", err)
				}
			} else if !errors.Is(err, db.ErrRecordingRetryUncertain) {
				test.Fatal("retry repeated a potentially paid or discarded request", err)
			}
		})
	}
}
