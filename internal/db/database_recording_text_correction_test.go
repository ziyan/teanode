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
		// Naming and correcting queue memory syncs ahead of this job; the transcription is the one wanted.
		jobs, err := transaction.ClaimAgentJobs("text-rerun-worker", 10, time.Now())
		if err != nil {
			test.Fatal(err)
		}
		transcriptionJobs := []*models.AgentJob{}
		for _, job := range jobs {
			if job.Kind == models.AgentJobRecording {
				transcriptionJobs = append(transcriptionJobs, job)
			}
		}
		if len(transcriptionJobs) != 1 {
			test.Fatal(len(transcriptionJobs))
		}
		jobs = transcriptionJobs
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
		// The correction queues the memory sync; this is what its job runs.
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
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

func TestACorrectedPassageCarriesTheNotesItRetired(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	var knowledge *models.AgentKnowledgeSource
	var correctedExternalId, untouchedExternalId string
	var untouchedHash string
	var input db.RecordingTranscriptTextCorrectionInput
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		_, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{
			{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 2500, TranscriptText: "I will send the draft on Friday."},
			{LocalSpeakerID: "speaker-two", SegmentStartMS: 2500, SegmentEndMS: 3000, TranscriptText: "Thanks, that works."},
		})
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		knowledge, err = transaction.GetAgentSourceByName(fixture.agentId, "recording:"+source.ID)
		if err != nil {
			test.Fatal(err)
		}
		correctedExternalId = fixture.transcriptRun.ID + ":" + segments[0].ID
		untouchedExternalId = fixture.transcriptRun.ID + ":" + segments[1].ID
		corrected, err := transaction.GetAgentDocumentByExternal(knowledge.ID, correctedExternalId)
		if err != nil {
			test.Fatal(err)
		}
		untouched, err := transaction.GetAgentDocumentByExternal(knowledge.ID, untouchedExternalId)
		if err != nil {
			test.Fatal(err)
		}
		untouchedHash = untouched.Hash
		node, err := transaction.PutAgentNode(&models.AgentNode{AgentID: fixture.agentId, Path: "projects/draft", Kind: models.NodeProject, Name: "Draft fixture"})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.AddAgentFact(&models.AgentFact{AgentID: fixture.agentId, NodeID: node.ID, Text: "Speaker one committed to send the draft on Friday.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: corrected.ID, Quote: "Friday"}}}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.AddAgentFact(&models.AgentFact{AgentID: fixture.agentId, NodeID: node.ID, Text: "Speaker two agreed to the plan.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: untouched.ID, Quote: "works"}}}); err != nil {
			test.Fatal(err)
		}
		input = db.RecordingTranscriptTextCorrectionInput{RecordingID: source.ID, RecordingTranscriptSegmentID: segments[0].ID, ClientCorrectionID: "thursday", CorrectedTranscriptText: "I will send the draft on Thursday.", ExpectedSourceGeneration: source.SourceGeneration}
	})
	passageText := func(transaction db.Transaction, externalId string) (string, string) {
		document, err := transaction.GetAgentDocumentByExternal(knowledge.ID, externalId)
		if err != nil {
			test.Fatal(err)
		}
		chunks, err := transaction.ListAgentChunks(fixture.agentId, document.ID)
		if err != nil || len(chunks) != 1 {
			test.Fatal(err, len(chunks))
		}
		return chunks[0].Text, document.Hash
	}
	var firstHash string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, input); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		text, hash := passageText(transaction, correctedExternalId)
		if !strings.Contains(text, "Thursday") || !strings.Contains(text, "now retired") || !strings.Contains(text, "- Speaker one committed to send the draft on Friday.") {
			test.Fatalf("the corrected passage does not carry the note it retired:\n%s", text)
		}
		firstHash = hash
		untouchedText, hash := passageText(transaction, untouchedExternalId)
		if hash != untouchedHash || strings.Contains(untouchedText, "retired") {
			test.Fatalf("an uncorrected passage changed:\n%s", untouchedText)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		if _, hash := passageText(transaction, correctedExternalId); hash != firstHash {
			test.Fatal("syncing again changed the corrected passage, so it would be read again")
		}
	})
}

func TestACorrectedPassageKeepsTheNotesFiledAfterItsCorrection(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	var knowledge *models.AgentKnowledgeSource
	var correctedExternalId, nodeId, documentId, retiredFactId, forgottenFactId string
	var input db.RecordingTranscriptTextCorrectionInput
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		_, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{
			{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 2500, TranscriptText: "I will send the draft on Friday."},
		})
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		knowledge, err = transaction.GetAgentSourceByName(fixture.agentId, "recording:"+source.ID)
		if err != nil {
			test.Fatal(err)
		}
		correctedExternalId = fixture.transcriptRun.ID + ":" + segments[0].ID
		document, err := transaction.GetAgentDocumentByExternal(knowledge.ID, correctedExternalId)
		if err != nil {
			test.Fatal(err)
		}
		documentId = document.ID
		node, err := transaction.PutAgentNode(&models.AgentNode{AgentID: fixture.agentId, Path: "projects/draft", Kind: models.NodeProject, Name: "Draft fixture"})
		if err != nil {
			test.Fatal(err)
		}
		nodeId = node.ID
		evidence := []models.Evidence{{Kind: models.EvidenceDocument, ID: document.ID, Quote: "Friday"}}
		forgotten, err := transaction.AddAgentFact(&models.AgentFact{AgentID: fixture.agentId, NodeID: node.ID, Text: "Speaker one likes green folders.", Evidence: evidence})
		if err != nil {
			test.Fatal(err)
		}
		forgottenFactId = forgotten.ID
		if _, err := transaction.StrikeAgentFact(fixture.agentId, forgotten.ID, "the person asked to forget it"); err != nil {
			test.Fatal(err)
		}
		retired, err := transaction.AddAgentFact(&models.AgentFact{AgentID: fixture.agentId, NodeID: node.ID, Text: "Speaker one committed to send the draft on Friday.", Evidence: evidence})
		if err != nil {
			test.Fatal(err)
		}
		retiredFactId = retired.ID
		input = db.RecordingTranscriptTextCorrectionInput{RecordingID: source.ID, RecordingTranscriptSegmentID: segments[0].ID, ClientCorrectionID: "thursday", CorrectedTranscriptText: "I will send the draft on Thursday.", ExpectedSourceGeneration: source.SourceGeneration}
	})
	var correctedText, correctedHash string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, input); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		document, err := transaction.GetAgentDocumentByExternal(knowledge.ID, correctedExternalId)
		if err != nil {
			test.Fatal(err)
		}
		chunks, err := transaction.ListAgentChunks(fixture.agentId, document.ID)
		if err != nil || len(chunks) != 1 {
			test.Fatal(err, len(chunks))
		}
		correctedText, correctedHash = chunks[0].Text, document.Hash
		if !strings.Contains(correctedText, "- Speaker one committed to send the draft on Friday.") {
			test.Fatalf("the corrected passage does not carry the note it retired:\n%s", correctedText)
		}
		if strings.Contains(correctedText, "green folders") {
			test.Fatalf("a note forgotten before the correction was listed as retired by it:\n%s", correctedText)
		}
		if document.ID != documentId {
			test.Fatal("the corrected passage was stored as a different document")
		}
	})
	var filedFactId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		filed, err := transaction.AddAgentFact(&models.AgentFact{AgentID: fixture.agentId, NodeID: nodeId, Text: "Speaker one will send the draft on Thursday.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: documentId, Quote: "Thursday"}}})
		if err != nil {
			test.Fatal(err)
		}
		filedFactId = filed.ID
		if _, err := transaction.FoldAgentFact(fixture.agentId, retiredFactId, filed.ID, "the corrected note says it again"); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		document, err := transaction.GetAgentDocumentByExternal(knowledge.ID, correctedExternalId)
		if err != nil {
			test.Fatal(err)
		}
		chunks, err := transaction.ListAgentChunks(fixture.agentId, document.ID)
		if err != nil || len(chunks) != 1 {
			test.Fatal(err, len(chunks))
		}
		if document.Hash != correctedHash || chunks[0].Text != correctedText {
			test.Fatal("a later sync changed the corrected passage, so it would be read again")
		}
		facts, err := transaction.GetAgentFacts(fixture.agentId, []string{filedFactId, forgottenFactId})
		if err != nil || len(facts) != 2 {
			test.Fatal(err, len(facts))
		}
		for _, fact := range facts {
			if fact.ID == filedFactId && fact.Dormant {
				test.Fatal("the note filed after the correction was struck by a later sync")
			}
		}
	})
}
