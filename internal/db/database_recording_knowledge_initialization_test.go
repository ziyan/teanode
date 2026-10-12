package db_test

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func completeLegacyRecording(test *testing.T, fixture *transcriptFixture) {
	test.Helper()
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "fixture-speaker", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "A synthetic proposal remains undecided."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		role, err := transaction.CreateRole(&models.Role{Name: "Recording member", Permissions: []models.Permission{models.PermissionAgentUse}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateGroup(&models.Group{Name: "Recording members", UserIDs: []string{fixture.userId}, RoleIDs: []string{role.ID}}); err != nil {
			test.Fatal(err)
		}
	})
}

func TestRecordingKnowledgeInitializationRecoversOnceWithModelsDisabled(test *testing.T) {
	fixture := newTranscriptFixture(test)
	completeLegacyRecording(test, fixture)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.UpdateAgent(fixture.agentId, func(ownerAgent *models.Agent) error { ownerAgent.Enabled = false; return nil }); err != nil {
			test.Fatal(err)
		}
		candidates, err := transaction.ListRecordingsAwaitingKnowledgeInitialization("", 1)
		if err != nil || len(candidates) != 1 || candidates[0].UserID != fixture.userId {
			test.Fatal("legacy recording was not discovered", err, len(candidates))
		}
		afterPage, err := transaction.ListRecordingsAwaitingKnowledgeInitialization(candidates[0].ID, 1)
		if err != nil || len(afterPage) != 0 {
			test.Fatal("discovery ignored cursor", err)
		}
		didInitialize, err := transaction.InitializeRecordingKnowledge(fixture.userId, fixture.media.RecordingID)
		if err != nil || !didInitialize {
			test.Fatal("disabled personal model blocked free initialization", err)
		}
		source, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+fixture.media.RecordingID)
		if err != nil || source == nil || !source.Enabled {
			test.Fatal(err)
		}
		documents, chunks, err := transaction.CountAgentSourceDocuments(source.ID)
		if err != nil || documents != 1 || chunks != 1 {
			test.Fatal("initialization did not publish evidence", err, documents, chunks)
		}
		publishedDocuments, _, err := transaction.ListAgentDocumentsToDigest(fixture.agentId, nil, 10)
		if err != nil || len(publishedDocuments) != 1 {
			test.Fatal(err)
		}
		citationURL, err := url.Parse(publishedDocuments[0].URL)
		if err != nil || citationURL.Query().Get("version") != fixture.transcriptRun.ID || citationURL.Query().Get("startMS") != "2000" || citationURL.Query().Get("endMS") != "3000" {
			test.Fatal("citation does not select the original version and interval", err)
		}

		repeated, err := transaction.InitializeRecordingKnowledge(fixture.userId, fixture.media.RecordingID)
		if err != nil || repeated {
			test.Fatal("repeated sweep initialized twice", err)
		}
		remaining, err := transaction.ListRecordingsAwaitingKnowledgeInitialization("", 100)
		if err != nil || len(remaining) != 0 {
			test.Fatal("initialized recording remained pending", err)
		}
		documents, chunks, err = transaction.CountAgentSourceDocuments(source.ID)
		if err != nil || documents != 1 || chunks != 1 {
			test.Fatal("sweep duplicated evidence", err)
		}
	})
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "other-recording-owner")
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.InitializeRecordingKnowledge(otherOwnerId, fixture.media.RecordingID)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("another owner initialized recording", err)
	}
}

func TestRecordingKnowledgeInitializationPreservesStoppedAndPausedSources(test *testing.T) {
	for _, sourceCondition := range []string{"paused", "deleted", "renamed", "disabled-owner", "no-permission"} {
		test.Run(sourceCondition, func(test *testing.T) {
			fixture := newTranscriptFixture(test)
			completeLegacyRecording(test, fixture)
			dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
				switch sourceCondition {
				case "paused", "deleted", "renamed":
					if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
						test.Fatal(err)
					}
					source, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+fixture.media.RecordingID)
					if err != nil || source == nil {
						test.Fatal(err)
					}
					if sourceCondition == "deleted" {
						if err := transaction.DeleteAgentSource(fixture.agentId, source.ID); err != nil {
							test.Fatal(err)
						}
					} else {
						if sourceCondition == "paused" {
							source.Enabled = false
						} else {
							source.Name = "changed-internal-source"
						}
						if _, err := transaction.PutAgentSource(source); err != nil {
							test.Fatal(err)
						}
					}
				case "disabled-owner":
					if _, err := transaction.UpdateUser(fixture.userId, func(owner *models.User) error { now := time.Now(); owner.DisabledAt = &now; return nil }); err != nil {
						test.Fatal(err)
					}
				case "no-permission":
					owner, err := transaction.GetUser(fixture.userId)
					if err != nil {
						test.Fatal(err)
					}
					if _, err := transaction.UpdateUser(owner.ID, func(owner *models.User) error { owner.GroupIDs = nil; return nil }); err != nil {
						test.Fatal(err)
					}
				}
				candidates, err := transaction.ListRecordingsAwaitingKnowledgeInitialization("", 100)
				if err != nil || len(candidates) != 0 {
					test.Fatal("explicit reading choice or permissions ignored", err, len(candidates))
				}
				didInitialize, err := transaction.InitializeRecordingKnowledge(fixture.userId, fixture.media.RecordingID)
				if err != nil || didInitialize {
					test.Fatal("reading restarted without authority", err)
				}
			})
		})
	}
}

func TestRecordingKnowledgeInitializationSeparatesOwners(test *testing.T) {
	fixture := newTranscriptFixture(test)
	completeLegacyRecording(test, fixture)
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "second-legacy-owner")
	var otherSource *models.Recording
	var otherMedia *models.RecordingMedia
	var otherRun *models.RecordingTranscriptRun
	var otherAgentId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		ownerAgent, err := transaction.CreateAgent(&models.Agent{UserID: otherOwnerId, Name: "Second fixture assistant", Enabled: false})
		if err != nil {
			test.Fatal(err)
		}
		otherAgentId = ownerAgent.ID
		role, err := transaction.CreateRole(&models.Role{Name: "Second recording role", Permissions: []models.Permission{models.PermissionAgentUse}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateGroup(&models.Group{Name: "Second recording members", UserIDs: []string{otherOwnerId}, RoleIDs: []string{role.ID}}); err != nil {
			test.Fatal(err)
		}
		otherSource, err = transaction.CreateRecording(&models.Recording{UserID: otherOwnerId, ClientRecordingID: "second-legacy-source", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		otherMedia, err = transaction.CreateRecordingMedia(otherOwnerId, &models.RecordingMedia{RecordingID: otherSource.ID, ClientMediaID: "second-legacy-media", OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: fixture.media.OriginalSizeBytes, OriginalSHA256: fixture.media.OriginalSHA256}, 1<<20, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.PutRecordingMediaPart(otherOwnerId, &models.RecordingMediaPart{RecordingMediaID: otherMedia.ID, PartIndex: 0, PartSizeBytes: otherMedia.OriginalSizeBytes, PartSHA256: otherMedia.OriginalSHA256}); err != nil {
			test.Fatal(err)
		}
		if _, _, err := transaction.CompleteRecordingMedia(otherOwnerId, otherMedia.ID, otherMedia.OriginalSHA256); err != nil {
			test.Fatal(err)
		}
		otherRun, err = transaction.CreateRecordingTranscriptRun(otherOwnerId, otherMedia.ID, ownerAgent.ID, "legacy-fixture", "fixture:diarize", false)
		if err != nil {
			test.Fatal(err)
		}
	})
	// Historical completion is seeded without a provider or a paid worker.
	dbtest.Exec(test, fixture.database, `INSERT INTO recording_blob SELECT 'legacy-second-blob','`+otherSource.ID+`','`+otherMedia.ID+`',created_at,recording_blob_purpose,blob_size_bytes,blob_sha256,is_stored_verified FROM recording_blob LIMIT 1`)
	dbtest.Exec(test, fixture.database, `INSERT INTO recording_audio_chunk (id,recording_transcript_run_id,recording_blob_id,chunk_index,timeline_start_ms,audio_duration_ms) VALUES ('legacy-second-chunk','`+otherRun.ID+`','legacy-second-blob',0,2000,1000)`)
	dbtest.Exec(test, fixture.database, `INSERT INTO recording_transcription_attempt (id,recording_transcript_run_id,recording_audio_chunk_id,attempt_index,created_at,recording_attempt_state,provider_model_name,currency_code) VALUES ('legacy-second-attempt','`+otherRun.ID+`','legacy-second-chunk',1,now(),'complete','fixture:diarize','USD')`)
	dbtest.Exec(test, fixture.database, `INSERT INTO recording_transcript_segment (id,recording_transcript_run_id,recording_transcription_attempt_id,local_speaker_id,segment_start_ms,segment_end_ms,transcript_text) VALUES ('legacy-second-segment','`+otherRun.ID+`','legacy-second-attempt','second-speaker',2000,3000,'A second synthetic proposal.')`)
	dbtest.Exec(test, fixture.database, `UPDATE recording_transcript_run SET recording_processing_state='complete',completed_at=now() WHERE id='`+otherRun.ID+`'`)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		candidates, err := transaction.ListRecordingsAwaitingKnowledgeInitialization("", 100)
		if err != nil || len(candidates) != 2 {
			test.Fatal("completed owners were not both discovered", err, len(candidates))
		}
		for _, candidate := range candidates {
			didInitialize, err := transaction.InitializeRecordingKnowledge(candidate.UserID, candidate.ID)
			if err != nil || !didInitialize {
				test.Fatal(err)
			}
		}
		for _, ownedSource := range []struct{ ownerId, agentId, recordingId string }{{fixture.userId, fixture.agentId, fixture.media.RecordingID}, {otherOwnerId, otherAgentId, otherSource.ID}} {
			reading, err := transaction.ReadRecordingLearning(ownedSource.ownerId, ownedSource.recordingId)
			if err != nil || reading.KnowledgeSource == nil || reading.KnowledgeSource.AgentID != ownedSource.agentId || reading.PendingDocumentCount != 1 {
				test.Fatal("backfill mixed account evidence", err)
			}
		}
	})
}

func TestRecordingKnowledgeCorrectionsDoNotRestartStoppedReading(test *testing.T) {
	fixture := newTranscriptFixture(test)
	completeLegacyRecording(test, fixture)
	var fact *models.AgentFact
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		documents, _, err := transaction.ListAgentDocumentsToDigest(fixture.agentId, nil, 10)
		if err != nil || len(documents) != 1 {
			test.Fatal(err)
		}
		page, err := transaction.PutAgentNode(&models.AgentNode{AgentID: fixture.agentId, Path: "projects/stopped-fixture", Kind: models.NodeProject, Name: "Stopped fixture"})
		if err != nil {
			test.Fatal(err)
		}
		fact, err = transaction.AddAgentFact(&models.AgentFact{AgentID: fixture.agentId, NodeID: page.ID, Text: "A synthetic proposal was undecided.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: documents[0].ID, Quote: "synthetic proposal"}}})
		if err != nil {
			test.Fatal(err)
		}
		reading, err := transaction.ReadRecordingLearning(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeleteAgentSource(fixture.agentId, reading.KnowledgeSource.ID); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, db.RecordingTranscriptTextCorrectionInput{RecordingID: source.ID, RecordingTranscriptSegmentID: segments[0].ID, ClientCorrectionID: "stopped-reading-edit", ExpectedSourceGeneration: source.SourceGeneration, CorrectedTranscriptText: "An edited synthetic proposal is still undecided."}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
		reading, err = transaction.ReadRecordingLearning(fixture.userId, source.ID)
		if err != nil || (reading.KnowledgeSource != nil || !reading.HasStoppedReading) {
			test.Fatal("correction restarted stopped reading", err)
		}
		retired, err := transaction.GetAgentFacts(fixture.agentId, []string{fact.ID})
		if err != nil || len(retired) != 1 || !retired[0].Dormant {
			test.Fatal("stopped-source correction retained obsolete facts", err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
		if didInitialize, err := transaction.InitializeRecordingKnowledge(fixture.userId, source.ID); err != nil || didInitialize {
			test.Fatal("recovery restarted stopped reading", err)
		}
	})
}

func TestRecordingKnowledgeDoesNotCreateSourceWithoutSpeech(test *testing.T) {
	fixture := newTranscriptFixture(test)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		reading, err := transaction.ReadRecordingLearning(fixture.userId, fixture.media.RecordingID)
		if err != nil || (reading.KnowledgeSource != nil || reading.HasStoppedReading) {
			test.Fatal("incomplete recording created an empty source", err)
		}
	})
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, nil); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		reading, err := transaction.ReadRecordingLearning(fixture.userId, fixture.media.RecordingID)
		if err != nil || (reading.KnowledgeSource != nil || reading.HasStoppedReading) {
			test.Fatal("no-speech version created an empty source", err)
		}
	})
}

func TestRecordingKnowledgeExplicitResumeUsesCurrentCorrections(test *testing.T) {
	fixture := newTranscriptFixture(test)
	completeLegacyRecording(test, fixture)
	var sourceGeneration int64
	var resumedSourceId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		reading, err := transaction.ReadRecordingLearning(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeleteAgentSource(fixture.agentId, reading.KnowledgeSource.ID); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, db.RecordingTranscriptTextCorrectionInput{RecordingID: source.ID, RecordingTranscriptSegmentID: segments[0].ID, ClientCorrectionID: "resume-current-edit", ExpectedSourceGeneration: source.SourceGeneration, CorrectedTranscriptText: "The synthetic proposal remains optional."}); err != nil {
			test.Fatal(err)
		}
		source, err = transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		sourceGeneration = source.SourceGeneration
		if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
		reading, err = transaction.ReadRecordingLearning(fixture.userId, source.ID)
		if err != nil || !reading.HasStoppedReading || reading.KnowledgeSource != nil {
			test.Fatal("edit resumed without explicit intent", err)
		}
		resumed, err := transaction.ResumeRecordingLearning(fixture.userId, source.ID, source.SourceGeneration)
		if err != nil || resumed.HasStoppedReading || resumed.KnowledgeSource == nil || !resumed.KnowledgeSource.Enabled || resumed.PendingDocumentCount != 1 {
			test.Fatal("explicit resume did not publish pending current evidence", err)
		}
		resumedSourceId = resumed.KnowledgeSource.ID
		documents, _, err := transaction.ListAgentDocumentsToDigest(fixture.agentId, nil, 10)
		if err != nil || len(documents) != 1 {
			test.Fatal(err)
		}
		chunks, err := transaction.ListAgentChunks(fixture.agentId, documents[0].ID)
		if err != nil || len(chunks) != 1 || !strings.Contains(chunks[0].Text, "The synthetic proposal remains optional.") {
			test.Fatal("resume used obsolete transcript", err)
		}
		repeated, err := transaction.ResumeRecordingLearning(fixture.userId, source.ID, source.SourceGeneration)
		if err != nil || repeated.KnowledgeSource.ID != resumedSourceId || repeated.PendingDocumentCount != 1 {
			test.Fatal("repeat resume duplicated source", err)
		}
		resumed.KnowledgeSource.Enabled = false
		if _, err := transaction.PutAgentSource(resumed.KnowledgeSource); err != nil {
			test.Fatal(err)
		}
		paused, err := transaction.ResumeRecordingLearning(fixture.userId, source.ID, source.SourceGeneration)
		if err != nil || paused.KnowledgeSource.Enabled {
			test.Fatal("resume bypassed paused source control", err)
		}
		runs, err := transaction.ListRecordingTranscriptRuns(fixture.userId, source.ID)
		if err != nil || len(runs) != 1 {
			test.Fatal("resume requested paid reprocessing", err)
		}
	})
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "resume-other-owner")
	for _, resumeRequest := range []struct {
		ownerId       string
		generation    int64
		expectedError error
	}{{fixture.userId, sourceGeneration - 1, db.ErrAlreadyExists}, {otherOwnerId, sourceGeneration, db.ErrNotFound}} {
		err := fixture.database.Transaction(func(transaction db.Transaction) error {
			_, err := transaction.ResumeRecordingLearning(resumeRequest.ownerId, fixture.media.RecordingID, resumeRequest.generation)
			return err
		})
		if !errors.Is(err, resumeRequest.expectedError) {
			test.Fatal("invalid resume was admitted", err)
		}
	}
}
