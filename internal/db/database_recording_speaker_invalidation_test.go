package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
)

func createConfirmedReference(test *testing.T, fixture *transcriptFixture) *models.RecordingVoiceReference {
	test.Helper()
	var chunk *models.RecordingAudioChunk
	blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(fixture.transcriptRun.ID, 0), BlobSizeBytes: 128, BlobSHA256: strings.Repeat("a", 64)}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		chunk, err = transaction.CreateRecordingAudioChunk(fixture.userId, fixture.transcriptRun.ID, fixture.claim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 2000, AudioDurationMS: 4000}, blob, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
	})
	attempt := fixture.beginAttempt(test, chunk, blob)
	var reference *models.RecordingVoiceReference
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "source-speaker", SegmentStartMS: 2000, SegmentEndMS: 6000, TranscriptText: "A fixture proposal was discussed."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 4000); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		corrections, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: attempt.ID, ClientCorrectionID: "reference-confirmation", LocalSpeakerID: "source-speaker", ConfirmedName: "Participant One", ExpectedSourceGeneration: source.SourceGeneration, PassageStartMS: -1, PassageEndMS: -1})
		if err != nil {
			test.Fatal(err)
		}
		source, err = transaction.GetRecording(fixture.userId, source.ID)
		if err != nil {
			test.Fatal(err)
		}
		clip := &models.RecordingBlob{BlobSizeBytes: 96044, BlobSHA256: strings.Repeat("b", 64)}
		reference, err = transaction.CreateRecordingVoiceReference(fixture.userId, &models.RecordingVoiceReference{RecordingID: source.ID, RecordingMediaID: fixture.media.ID, RecordingTranscriptRunID: fixture.transcriptRun.ID, RecordingSpeakerCorrectionID: corrections[0].ID, SpeakerIdentityID: corrections[0].SpeakerIdentityID, PassageStartMS: 2200, PassageEndMS: 5200, SourceGeneration: source.SourceGeneration}, clip, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.VerifyRecordingBlob(fixture.userId, reference.RecordingBlobID, clip.BlobSHA256); err != nil {
			test.Fatal(err)
		}
	})
	return reference
}

func createReferenceDependent(test *testing.T, fixture *transcriptFixture, reference *models.RecordingVoiceReference) *transcriptFixture {
	test.Helper()
	dependent := &transcriptFixture{database: fixture.database, userId: fixture.userId, agentId: fixture.agentId}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "dependent-recording", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		dependent.media, err = transaction.CreateRecordingMedia(fixture.userId, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "dependent-media", OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: fixture.media.OriginalSizeBytes, OriginalSHA256: fixture.media.OriginalSHA256, TimelineStartMS: 2000}, 1<<20, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.PutRecordingMediaPart(fixture.userId, &models.RecordingMediaPart{RecordingMediaID: dependent.media.ID, PartIndex: 0, PartSizeBytes: dependent.media.OriginalSizeBytes, PartSHA256: dependent.media.OriginalSHA256}); err != nil {
			test.Fatal(err)
		}
		if _, _, err := transaction.CompleteRecordingMedia(fixture.userId, dependent.media.ID, dependent.media.OriginalSHA256); err != nil {
			test.Fatal(err)
		}
		dependent.transcriptRun, err = transaction.CreateRecordingTranscriptRun(fixture.userId, dependent.media.ID, fixture.agentId, "dependent-run", "fixture:diarize", false)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: fixture.agentId, Kind: models.AgentJobRecording, SubjectID: dependent.transcriptRun.ID}); err != nil {
			test.Fatal(err)
		}
		jobs, err := transaction.ClaimAgentJobs("dependent-worker", 1, time.Now())
		if err != nil || len(jobs) != 1 {
			test.Fatal(err, len(jobs))
		}
		dependent.claim = db.RecordingWorkerClaim{JobID: jobs[0].ID, ClaimID: jobs[0].ClaimID}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, dependent.transcriptRun.ID, dependent.claim, models.RecordingProcessingPreparing, "", 0); err != nil {
			test.Fatal(err)
		}
	})
	var chunk *models.RecordingAudioChunk
	blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(dependent.transcriptRun.ID, 0), BlobSizeBytes: 128, BlobSHA256: strings.Repeat("c", 64)}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		chunk, err = transaction.CreateRecordingAudioChunk(fixture.userId, dependent.transcriptRun.ID, dependent.claim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 2000, AudioDurationMS: 1000}, blob, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.VerifyRecordingBlob(fixture.userId, blob.ID, blob.BlobSHA256); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, dependent.transcriptRun.ID, dependent.claim, models.RecordingProcessingTranscribing, "", 1000); err != nil {
			test.Fatal(err)
		}
		attempt, err := transaction.CreateRecordingTranscriptionAttempt(fixture.userId, dependent.transcriptRun.ID, chunk.ID, dependent.claim, "USD", []string{reference.RecordingBlobID})
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.CheckRecordingVoiceReferenceNames(fixture.userId, []string{reference.RecordingBlobID}, []string{"Participant One"}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SubmitRecordingTranscriptionAttempt(fixture.userId, attempt.ID, dependent.claim); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, dependent.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "Participant One", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The fixture mechanism will be inspected."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, dependent.transcriptRun.ID, dependent.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, dependent.media.RecordingID); err != nil {
			test.Fatal(err)
		}
	})
	return dependent
}

func TestRecordingReferenceInvalidationReconcilesDependentMemory(test *testing.T) {
	for _, mutation := range []string{"revoke", "rename", "delete", "remove"} {
		test.Run(mutation, func(test *testing.T) {
			fixture := newTranscriptFixture(test)
			reference := createConfirmedReference(test, fixture)
			dependent := createReferenceDependent(test, fixture, reference)
			var fact *models.AgentFact
			dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
				source, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+dependent.media.RecordingID)
				if err != nil {
					test.Fatal(err)
				}
				document, err := transaction.GetAgentDocumentByExternal(source.ID, dependent.transcriptRun.ID+":"+func() string {
					segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, dependent.transcriptRun.ID)
					if err != nil {
						test.Fatal(err)
					}
					return segments[0].ID
				}())
				if err != nil {
					test.Fatal(err)
				}
				node, err := transaction.PutAgentNode(&models.AgentNode{AgentID: fixture.agentId, Path: "projects/dependent", Kind: models.NodeProject, Name: "Dependent"})
				if err != nil {
					test.Fatal(err)
				}
				fact, err = transaction.AddAgentFact(&models.AgentFact{AgentID: fixture.agentId, NodeID: node.ID, Text: "A participant committed to inspection.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: document.ID, Quote: "will be inspected"}}})
				if err != nil {
					test.Fatal(err)
				}
				recording, err := transaction.GetRecording(fixture.userId, reference.RecordingID)
				if err != nil {
					test.Fatal(err)
				}
				switch mutation {
				case "revoke":
					err = transaction.RevokeRecordingVoiceReference(fixture.userId, recording.ID, reference.ID, recording.SourceGeneration)
				case "rename":
					if err := transaction.RenameRecordingSpeaker(fixture.userId, recording.ID, reference.SpeakerIdentityID, "Participant One", recording.SourceGeneration); err != nil {
						test.Fatal(err)
					}
					var unchanged *models.Recording
					unchanged, err = transaction.GetRecording(fixture.userId, recording.ID)
					if err != nil || unchanged.SourceGeneration != recording.SourceGeneration {
						test.Fatal("unchanged identity created a source edit", err)
					}
					var invalidations []*models.RecordingReferenceInvalidation
					invalidations, err = transaction.ListRecordingReferenceInvalidations(fixture.userId, dependent.media.RecordingID)
					if err != nil || len(invalidations) != 0 {
						test.Fatal("unchanged name invalidated conditioning evidence", err)
					}
					err = transaction.RenameRecordingSpeaker(fixture.userId, recording.ID, reference.SpeakerIdentityID, "Participant Renamed", recording.SourceGeneration)
				case "delete":
					err = transaction.DeleteRecording(fixture.userId, recording.ID)
				case "remove":
					err = transaction.RemoveRecordingSpeakerCorrection(fixture.userId, recording.ID, reference.RecordingSpeakerCorrectionID, recording.SourceGeneration)
				}
				if err != nil {
					test.Fatal(err)
				}
			})
			otherUserId := dbtest.CreateUser(test, fixture.database, "invalidation-other")
			err := fixture.database.Transaction(func(transaction db.Transaction) error {
				_, err := transaction.ListRecordingReferenceInvalidations(otherUserId, dependent.media.RecordingID)
				return err
			})
			if !errors.Is(err, db.ErrNotFound) {
				test.Fatal("cross-account invalidation read", err)
			}
			// The storage coordinator exists even when model workers are disabled.
			if err := recording.New(fixture.database, nil).ReconcileReferences(test.Context(), 25); err != nil {
				test.Fatal("storage-only reconciliation failed", err)
			}
			dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
				invalidations, err := transaction.ListRecordingReferenceInvalidations(fixture.userId, dependent.media.RecordingID)
				if err != nil || len(invalidations) != 1 || invalidations[0].ReferenceBlobID != reference.RecordingBlobID || invalidations[0].InvalidatedConfirmedName != "Participant One" {
					test.Fatal("lost reference provenance", err, len(invalidations))
				}
				if !invalidations[0].IsReconciled {
					test.Fatal("reconciliation depended on a model worker")
				}
				if err := transaction.ReconcileRecordingReferenceInvalidations(fixture.userId, dependent.media.RecordingID); err != nil {
					test.Fatal(err)
				}
				facts, err := transaction.GetAgentFacts(fixture.agentId, []string{fact.ID})
				if err != nil || len(facts) != 1 || !facts[0].Dormant {
					test.Fatal("obsolete attribution survived reconciliation", err)
				}
				segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, dependent.transcriptRun.ID)
				if err != nil || segments[0].LocalSpeakerID != "Participant One" {
					test.Fatal("generated evidence was changed", err)
				}
				source, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+dependent.media.RecordingID)
				if err != nil {
					test.Fatal(err)
				}
				document, err := transaction.GetAgentDocumentByExternal(source.ID, dependent.transcriptRun.ID+":"+segments[0].ID)
				if err != nil || !strings.Contains(document.Title, "attribution uncertain") {
					test.Fatal("learning still claimed automatic identity", err)
				}
				if err := transaction.ReconcileRecordingReferenceInvalidations(fixture.userId, dependent.media.RecordingID); err != nil {
					test.Fatal(err)
				}
			})
		})
	}
}

func TestRecordingReferenceAdmissionAndDeletionLockOrder(test *testing.T) {
	fixture := newTranscriptFixture(test)
	reference := createConfirmedReference(test, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hasOwnerLock := make(chan struct{})
	canFinishAdmission := make(chan struct{})
	admissionResult := make(chan error, 1)
	deletionResult := make(chan error, 1)
	deletionStarted := make(chan struct{})
	go func() {
		admissionResult <- fixture.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			if err := transaction.CheckRecordingVoiceReferenceNames(fixture.userId, []string{reference.RecordingBlobID}, []string{"Participant One"}); err != nil {
				return err
			}
			close(hasOwnerLock)
			select {
			case <-canFinishAdmission:
			case <-ctx.Done():
				return ctx.Err()
			}
			_, err := transaction.LockRecording(fixture.userId, reference.RecordingID)
			return err
		})
	}()
	select {
	case <-hasOwnerLock:
	case <-ctx.Done():
		test.Fatal(ctx.Err())
	}
	go func() {
		deletionResult <- fixture.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			close(deletionStarted)
			return transaction.DeleteRecording(fixture.userId, reference.RecordingID)
		})
	}()
	<-deletionStarted
	select {
	case err := <-deletionResult:
		test.Fatal("deletion passed an active admission", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(canFinishAdmission)
	if err := <-admissionResult; err != nil {
		test.Fatal("admission could not lock its source", err)
	}
	if err := <-deletionResult; err != nil {
		test.Fatal("deletion deadlocked with admission", err)
	}
}
