package db_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func createTextCorrectionRerun(test *testing.T, fixture *transcriptFixture, clientRunId string, segments []*models.RecordingTranscriptSegment) *models.RecordingTranscriptRun {
	test.Helper()
	var transcriptRun *models.RecordingTranscriptRun
	var claim db.RecordingWorkerClaim
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		transcriptRun, err = transaction.CreateRecordingTranscriptRun(fixture.userId, fixture.media.ID, fixture.agentId, clientRunId, "fixture:diarize", true)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: fixture.agentId, Kind: models.AgentJobRecording, SubjectID: transcriptRun.ID}); err != nil {
			test.Fatal(err)
		}
		jobs, err := transaction.ClaimAgentJobs("text-rerun-worker", 1, time.Now())
		if err != nil || len(jobs) != 1 {
			test.Fatal(err, len(jobs))
		}
		claim = db.RecordingWorkerClaim{JobID: jobs[0].ID, ClaimID: jobs[0].ClaimID}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, transcriptRun.ID, claim, models.RecordingProcessingPreparing, "", 0); err != nil {
			test.Fatal(err)
		}
		blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(transcriptRun.ID, 0), BlobSizeBytes: 128, BlobSHA256: strings.Repeat("c", 64)}
		chunk, err := transaction.CreateRecordingAudioChunk(fixture.userId, transcriptRun.ID, claim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 2000, AudioDurationMS: 1000}, blob, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.VerifyRecordingBlob(fixture.userId, blob.ID, blob.BlobSHA256); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, transcriptRun.ID, claim, models.RecordingProcessingTranscribing, "", 1000); err != nil {
			test.Fatal(err)
		}
		attempt, err := transaction.CreateRecordingTranscriptionAttempt(fixture.userId, transcriptRun.ID, chunk.ID, claim, "USD", nil)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.SubmitRecordingTranscriptionAttempt(fixture.userId, attempt.ID, claim); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, segments); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, transcriptRun.ID, claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.FinishAgentJob(claim.JobID, claim.ClaimID, &db.AgentJobOutcome{JobStatus: models.AgentJobDone, FinishedAt: time.Now()}); err != nil {
			test.Fatal(err)
		}
	})
	return transcriptRun
}

func TestRecordingTextCorrectionsPreserveVersionsAndReconcileMemory(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	var generatedSegment *models.RecordingTranscriptSegment
	var fact *models.AgentFact
	var correction *models.RecordingTranscriptTextCorrection
	var input db.RecordingTranscriptTextCorrectionInput
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		_, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "A decision was made."}})
		if err != nil {
			test.Fatal(err)
		}
		generated, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		generatedSegment = generated[0]
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		knowledge, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+source.ID)
		if err != nil {
			test.Fatal(err)
		}
		document, err := transaction.GetAgentDocumentByExternal(knowledge.ID, fixture.transcriptRun.ID+":"+generatedSegment.ID)
		if err != nil {
			test.Fatal(err)
		}
		node, err := transaction.PutAgentNode(&models.AgentNode{AgentID: fixture.agentId, Path: "projects/text", Kind: models.NodeProject, Name: "Text fixture"})
		if err != nil {
			test.Fatal(err)
		}
		fact, err = transaction.AddAgentFact(&models.AgentFact{AgentID: fixture.agentId, NodeID: node.ID, Text: "The proposal was accepted.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: document.ID, Quote: "decision"}}})
		if err != nil {
			test.Fatal(err)
		}
		input = db.RecordingTranscriptTextCorrectionInput{RecordingID: source.ID, RecordingTranscriptSegmentID: generatedSegment.ID, ClientCorrectionID: "text-edit-one", CorrectedTranscriptText: "A proposal was discussed; no decision was made.", ExpectedSourceGeneration: source.SourceGeneration}
	})
	otherUserId := dbtest.CreateUser(test, fixture.database, "text-other")
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CorrectRecordingTranscriptText(otherUserId, input)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("cross-account text edit accepted", err)
	}
	var mismatchedInput db.RecordingTranscriptTextCorrectionInput
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		unrelated, err := transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "unrelated-text-source", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		mismatchedInput = input
		mismatchedInput.RecordingID = unrelated.ID
		mismatchedInput.ExpectedSourceGeneration = unrelated.SourceGeneration
	})
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CorrectRecordingTranscriptText(fixture.userId, mismatchedInput)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("unrelated parent source accepted text edit", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		correction, err = transaction.CorrectRecordingTranscriptText(fixture.userId, input)
		if err != nil {
			test.Fatal(err)
		}
		repeated, err := transaction.CorrectRecordingTranscriptText(fixture.userId, input)
		if err != nil || repeated.ID != correction.ID {
			test.Fatal("lost response duplicated text edit", err)
		}
		facts, err := transaction.GetAgentFacts(fixture.agentId, []string{fact.ID})
		if err != nil || !facts[0].Dormant {
			test.Fatal("corrected text left obsolete fact active", err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.ProjectRecordingTranscriptTextCorrections(fixture.userId, segments); err != nil {
			test.Fatal(err)
		}
		if segments[0].TranscriptText != "A decision was made." || segments[0].EffectiveTranscriptText != input.CorrectedTranscriptText || segments[0].AppliedTextCorrectionID != correction.ID {
			test.Fatal("generated speech was changed or edit not projected")
		}
	})
	stale := input
	stale.ClientCorrectionID = "stale-edit"
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CorrectRecordingTranscriptText(fixture.userId, stale)
		return err
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("stale text edit accepted", err)
	}
	conflicting := input
	conflicting.CorrectedTranscriptText = "A different edit."
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CorrectRecordingTranscriptText(fixture.userId, conflicting)
		return err
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("client ID was reused with changed wording", err)
	}
	for _, variant := range []string{"unique", "overlap", "changed"} {
		generated := []*models.RecordingTranscriptSegment{{LocalSpeakerID: "new-local", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "Generated rerun statement."}}
		if variant == "overlap" {
			generated = append(generated, &models.RecordingTranscriptSegment{LocalSpeakerID: "another-local", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "Another speaker's original statement."})
		}
		if variant == "changed" {
			generated[0].SegmentStartMS = 2200
		}
		rerun := createTextCorrectionRerun(test, fixture, "rerun-"+variant, generated)
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, rerun.ID)
			if err != nil {
				test.Fatal(err)
			}
			if err := transaction.ProjectRecordingTranscriptTextCorrections(fixture.userId, segments); err != nil {
				test.Fatal(err)
			}
			for _, segment := range segments {
				if variant == "unique" {
					if segment.EffectiveTranscriptText != input.CorrectedTranscriptText || segment.AppliedTextCorrectionID != correction.ID || segment.HasUnalignedTextCorrection {
						test.Fatal("unique exact interval failed correction projection")
					}
				} else if segment.AppliedTextCorrectionID != "" || segment.EffectiveTranscriptText != segment.TranscriptText || !segment.HasUnalignedTextCorrection {
					test.Fatal("ambiguous correction overwrote unrelated generated speech")
				}
			}
			knowledge, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+fixture.media.RecordingID)
			if err != nil {
				test.Fatal(err)
			}
			independent, err := transaction.GetAgentDocumentByExternal(knowledge.ID, "text-correction:"+generatedSegment.ID)
			if err != nil {
				test.Fatal(err)
			}
			if variant == "unique" && independent != nil {
				test.Fatal("aligned edit duplicated independent evidence")
			}
			if variant != "unique" && (independent == nil || independent.Metadata["isTextAlignmentUncertain"] != true) {
				test.Fatal("unaligned user text evidence was lost")
			}
		})
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		input.ClientCorrectionID = "text-edit-two"
		input.ExpectedSourceGeneration = source.SourceGeneration
		input.CorrectedTranscriptText = "A second user text correction."
		newest, err := transaction.CorrectRecordingTranscriptText(fixture.userId, input)
		if err != nil {
			test.Fatal(err)
		}
		source, err = transaction.GetRecording(fixture.userId, source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RemoveRecordingTranscriptTextCorrection(fixture.userId, source.ID, newest.ID, source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.ProjectRecordingTranscriptTextCorrections(fixture.userId, segments); err != nil {
			test.Fatal(err)
		}
		if segments[0].AppliedTextCorrectionID != "" || segments[0].EffectiveTranscriptText != segments[0].TranscriptText {
			test.Fatal("removal fell back to older edited wording")
		}
		history, err := transaction.ListRecordingTranscriptTextCorrections(fixture.userId, source.ID)
		if err != nil || len(history) != 2 || !history[0].IsRemoved {
			test.Fatal("correction history was not preserved", err)
		}
	})
}
