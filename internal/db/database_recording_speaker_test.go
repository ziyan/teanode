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

func TestRecordingSpeakerCorrectionsOwnershipVersionsAndLearning(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "We could replace the fixture mechanism. This is a proposal."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
	})
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "speaker-other")
	correctionInput := db.RecordingSpeakerCorrectionInput{RecordingID: fixture.media.RecordingID, RecordingTranscriptionAttemptID: attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Participant One", ClientCorrectionID: "confirmation-one", PassageStartMS: -1, PassageEndMS: -1}
	var sourceGeneration int64
	var correction *models.RecordingSpeakerCorrection
	var document *models.AgentDocument
	var fact *models.AgentFact
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		sourceGeneration = source.SourceGeneration
		correctionInput.ExpectedSourceGeneration = sourceGeneration
		knowledgeSource, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+source.ID)
		if err != nil || knowledgeSource == nil {
			test.Fatal(err)
		}
		documents, _, err := transaction.ListAgentDocumentsToDigest(fixture.agentId, nil, 10)
		if err != nil || len(documents) != 1 {
			test.Fatal("passive recording was not filed", err, len(documents))
		}
		document = documents[0]
		if document.HappenedAt != nil {
			test.Fatal("upload time became meeting occurrence time")
		}
		page, err := transaction.PutAgentNode(&models.AgentNode{AgentID: fixture.agentId, Path: "projects/fixture", Kind: models.NodeProject, Name: "Fixture"})
		if err != nil {
			test.Fatal(err)
		}
		fact, err = transaction.AddAgentFact(&models.AgentFact{AgentID: fixture.agentId, NodeID: page.ID, Text: "An unidentified participant proposed a replacement.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: document.ID, Quote: "This is a proposal."}}})
		if err != nil {
			test.Fatal(err)
		}
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CorrectRecordingSpeaker(otherOwnerId, correctionInput)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("other account corrected evidence", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		corrections, err := transaction.CorrectRecordingSpeaker(fixture.userId, correctionInput)
		if err != nil || len(corrections) != 1 {
			test.Fatal(err)
		}
		correction = corrections[0]
		repeated, err := transaction.CorrectRecordingSpeaker(fixture.userId, correctionInput)
		if err != nil || len(repeated) != 1 || repeated[0].ID != correction.ID {
			test.Fatal("lost reply duplicated corrections", err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		changedFacts, err := transaction.GetAgentFacts(fixture.agentId, []string{fact.ID})
		if err != nil || len(changedFacts) != 1 || !changedFacts[0].Dormant {
			test.Fatal("old attribution stayed active", err)
		}
		revisions, err := transaction.ListAgentRevisions(fixture.agentId, fact.NodeID, 20)
		if err != nil {
			test.Fatal(err)
		}
		hasRetirement := false
		for _, revision := range revisions {
			if revision.Kind == models.RevisionFactStruck {
				hasRetirement = true
			}
		}
		if !hasRetirement {
			test.Fatal("correction lost memory revision history")
		}
		rerun, err := transaction.CreateRecordingTranscriptRun(fixture.userId, fixture.media.ID, fixture.agentId, "rerun-with-references", "fixture:diarize", true)
		if err != nil || rerun.VersionNumber != 2 {
			test.Fatal(err)
		}
		unchangedCorrections, err := transaction.ListRecordingSpeakerCorrections(fixture.userId, fixture.media.RecordingID)
		if err != nil || len(unchangedCorrections) != 1 || unchangedCorrections[0].ID != correction.ID {
			test.Fatal("reprocessing lost manual confirmation", err)
		}
	})
	staleInput := correctionInput
	staleInput.ClientCorrectionID = "stale-confirmation"
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CorrectRecordingSpeaker(fixture.userId, staleInput)
		return err
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("stale generation replaced correction", err)
	}
	mismatchedInput := correctionInput
	mismatchedInput.RecordingID = "missing-source"
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CorrectRecordingSpeaker(fixture.userId, mismatchedInput)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("mismatched source accepted", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RenameRecordingSpeaker(fixture.userId, source.ID, correction.SpeakerIdentityID, "Participant Renamed", source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
		source, err = transaction.GetRecording(fixture.userId, source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RemoveRecordingSpeakerCorrection(fixture.userId, source.ID, correction.ID, source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeleteRecording(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
		deletedKnowledge, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+source.ID)
		if err != nil || deletedKnowledge != nil {
			test.Fatal("deleted recording retained accessible transcript knowledge", err)
		}
	})
}

func TestRecordingReferencesRejectParentMismatchAndKeepRevocation(test *testing.T) {
	fixture := newTranscriptFixture(test)
	var chunk *models.RecordingAudioChunk
	blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(fixture.transcriptRun.ID, 0), BlobSizeBytes: 128, BlobSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		chunk, err = transaction.CreateRecordingAudioChunk(fixture.userId, fixture.transcriptRun.ID, fixture.claim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 2000, AudioDurationMS: 4000}, blob, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
	})
	attempt := fixture.beginAttempt(test, chunk, blob)
	var correction *models.RecordingSpeakerCorrection
	var reference *models.RecordingVoiceReference
	var reprocessed *models.RecordingTranscriptRun
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 6000, TranscriptText: "A fixture proposal was discussed."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 4000); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		corrections, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: attempt.ID, ClientCorrectionID: "reference-confirmation", LocalSpeakerID: "speaker-one", ConfirmedName: "Participant One", ExpectedSourceGeneration: source.SourceGeneration, PassageStartMS: -1, PassageEndMS: -1})
		if err != nil {
			test.Fatal(err)
		}
		correction = corrections[0]
		source, err = transaction.GetRecording(fixture.userId, source.ID)
		if err != nil {
			test.Fatal(err)
		}
		reference = &models.RecordingVoiceReference{RecordingID: source.ID, RecordingMediaID: fixture.media.ID, RecordingTranscriptRunID: fixture.transcriptRun.ID, RecordingSpeakerCorrectionID: correction.ID, SpeakerIdentityID: correction.SpeakerIdentityID, PassageStartMS: 2200, PassageEndMS: 5200, SourceGeneration: source.SourceGeneration}
		reprocessed, err = transaction.CreateRecordingTranscriptRun(fixture.userId, fixture.media.ID, fixture.agentId, "reference-rerun", "fixture:diarize", true)
		if err != nil {
			test.Fatal(err)
		}
	})
	clipBlob := &models.RecordingBlob{BlobSizeBytes: 96044, BlobSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	badReference := *reference
	badReference.RecordingTranscriptRunID = reprocessed.ID
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingVoiceReference(fixture.userId, &badReference, clipBlob, 1<<20)
		return err
	})
	if !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("unrelated generated version accepted as clip provenance", err)
	}
	badReference = *reference
	badReference.RecordingMediaID = "unrelated-media"
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingVoiceReference(fixture.userId, &badReference, clipBlob, 1<<20)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("unrelated media accepted as clip provenance", err)
	}
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingVoiceReference(fixture.userId, reference, clipBlob, 100)
		return err
	})
	if !errors.Is(err, db.ErrTooMuchAsked) {
		test.Fatal("reference bypassed existing account quota", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		reference, err = transaction.CreateRecordingVoiceReference(fixture.userId, reference, clipBlob, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.VerifyRecordingBlob(fixture.userId, reference.RecordingBlobID, clipBlob.BlobSHA256); err != nil {
			test.Fatal(err)
		}
		usable, err := transaction.ListUsableRecordingVoiceReferences(fixture.userId, 4)
		if err != nil || len(usable) != 1 {
			test.Fatal("verified confirmed sample unavailable", err)
		}
		if err := transaction.CheckRecordingVoiceReferenceNames(fixture.userId, []string{reference.RecordingBlobID}, []string{"Participant One"}); err != nil {
			test.Fatal("confirmed reference names were refused", err)
		}
		if err := transaction.CheckRecordingVoiceReferenceNames(fixture.userId, []string{reference.RecordingBlobID}, []string{"Unconfirmed Name"}); !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatal("changed identity name admitted a paid reference", err)
		}
		source, err := transaction.GetRecording(fixture.userId, reference.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RevokeRecordingVoiceReference(fixture.userId, source.ID, reference.ID, source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
		usable, err = transaction.ListUsableRecordingVoiceReferences(fixture.userId, 4)
		if err != nil || len(usable) != 0 {
			test.Fatal("revoked clip still conditions provider", err)
		}
		if err := transaction.CheckRecordingVoiceReferenceNames(fixture.userId, []string{reference.RecordingBlobID}, []string{"Participant One"}); !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatal("revoked reference admitted a paid request", err)
		}
		if err := transaction.DeleteRevokedRecordingVoiceReference(fixture.userId, source.ID, reference.ID); err != nil {
			test.Fatal(err)
		}
		references, err := transaction.ListRecordingVoiceReferences(fixture.userId, source.ID)
		if err != nil || len(references) != 1 || !references[0].IsRevoked || references[0].RecordingBlobID != "" {
			test.Fatal("revocation disappeared after byte cleanup", err)
		}
		source, err = transaction.GetRecording(fixture.userId, source.ID)
		if err != nil {
			test.Fatal(err)
		}
		candidate := *reference
		candidate.SourceGeneration = source.SourceGeneration
		repeated, err := transaction.CreateRecordingVoiceReference(fixture.userId, &candidate, clipBlob, 1<<20)
		if err != nil || !repeated.IsRevoked || repeated.RecordingBlobID != "" {
			test.Fatal("automatic curation recreated deleted sample", err)
		}
		if err := transaction.MarkRecordingSpeakerCuration(fixture.userId, source.ID, source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
		pending, err := transaction.ListRecordingsAwaitingSpeakerCuration(10)
		if err != nil || len(pending) != 0 {
			test.Fatal("unsuitable or revoked generation stays in busy loop", err)
		}
	})
}

func TestRecordingLearningRejectsStaleEvidenceAfterCorrection(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	var originalDocuments []*models.AgentDocument
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The fixture is a proposal, not an accepted commitment."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		var err error
		originalDocuments, _, err = transaction.ListAgentDocumentsToDigest(fixture.agentId, nil, 10)
		if err != nil || len(originalDocuments) != 1 {
			test.Fatal(err)
		}
		if err := transaction.CheckRecordingKnowledgeEvidence(fixture.userId, originalDocuments); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Confirmed Fixture", ClientCorrectionID: "late-confirmation", PassageStartMS: 2200, PassageEndMS: 2700, ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.CheckRecordingKnowledgeEvidence(fixture.userId, originalDocuments)
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("old model evidence survived a passage correction", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		latest, _, err := transaction.ListAgentDocumentsToDigest(fixture.agentId, nil, 10)
		if err != nil || len(latest) != 1 {
			test.Fatal(err)
		}
		if latest[0].Hash == originalDocuments[0].Hash {
			test.Fatal("partial correction did not change digest evidence")
		}
		if err := transaction.CheckRecordingKnowledgeEvidence(fixture.userId, latest); err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeleteRecording(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
	})
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.CheckRecordingKnowledgeEvidence(fixture.userId, originalDocuments)
	})
	if !errors.Is(err, db.ErrRecordingDeleted) {
		test.Fatal("deleted source accepted stale model output", err)
	}
}

func TestRecordingLearningPreservesEqualTimeOverlappingStatements(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		segments := []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The first participant proposed a fixture change."}, {LocalSpeakerID: "speaker-two", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The second participant asked a different open question."}}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, segments); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+fixture.media.RecordingID)
		if err != nil || source == nil {
			test.Fatal(err)
		}
		documentCount, chunkCount, err := transaction.CountAgentSourceDocuments(source.ID)
		if err != nil || documentCount != 2 || chunkCount != 2 {
			test.Fatal("equal-time speakers overwrote a statement", documentCount, chunkCount, err)
		}
		learning, err := transaction.ReadRecordingLearning(fixture.userId, fixture.media.RecordingID)
		if err != nil || !learning.HasAgent || learning.KnowledgeSource == nil || learning.PendingDocumentCount != 2 || learning.DigestedDocumentCount != 0 || learning.LastPublishedSourceGeneration != learning.SourceGeneration {
			test.Fatal("existing source learning counts do not match retained passages", learning, err)
		}
		documents, _, err := transaction.ListAgentDocumentsToDigest(fixture.agentId, nil, 10)
		if err != nil || len(documents) != 2 || documents[0].ExternalID == documents[1].ExternalID {
			test.Fatal("statement provenance is not unique", err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		if err := transaction.MarkAgentDocumentsDigested([]string{documents[0].ID}, time.Now()); err != nil {
			test.Fatal(err)
		}
		learning, err = transaction.ReadRecordingLearning(fixture.userId, fixture.media.RecordingID)
		if err != nil || learning.PendingDocumentCount != 1 || learning.DigestedDocumentCount != 1 {
			test.Fatal("digest bookkeeping was replaced by invented learning state", learning, err)
		}
		documentCount, chunkCount, err = transaction.CountAgentSourceDocuments(source.ID)
		if err != nil || documentCount != 2 || chunkCount != 2 {
			test.Fatal("publication retry changed equal-time statement coverage", err)
		}
	})
}

func TestRecordingRenameReconcilesEverySharedIdentitySourceAtomically(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	var firstCorrection *models.RecordingSpeakerCorrection
	var secondSource *models.Recording
	var secondRun *models.RecordingTranscriptRun
	var secondMedia *models.RecordingMedia
	var secondClaim db.RecordingWorkerClaim
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The fixture participant offered a first proposal."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		corrections, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: attempt.ID, ClientCorrectionID: "shared-first", ConfirmedName: "Shared Participant", LocalSpeakerID: "speaker-one", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: source.SourceGeneration})
		if err != nil {
			test.Fatal(err)
		}
		firstCorrection = corrections[0]
		secondSource, err = transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "shared-second-source", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		secondMedia, err = transaction.CreateRecordingMedia(fixture.userId, &models.RecordingMedia{RecordingID: secondSource.ID, ClientMediaID: "shared-second-media", OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: fixture.media.OriginalSizeBytes, OriginalSHA256: fixture.media.OriginalSHA256, TimelineStartMS: 2000}, 1<<20, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.PutRecordingMediaPart(fixture.userId, &models.RecordingMediaPart{RecordingMediaID: secondMedia.ID, PartIndex: 0, PartSizeBytes: secondMedia.OriginalSizeBytes, PartSHA256: secondMedia.OriginalSHA256}); err != nil {
			test.Fatal(err)
		}
		if _, _, err := transaction.CompleteRecordingMedia(fixture.userId, secondMedia.ID, secondMedia.OriginalSHA256); err != nil {
			test.Fatal(err)
		}
		secondRun, err = transaction.CreateRecordingTranscriptRun(fixture.userId, secondMedia.ID, fixture.agentId, "initial", "fixture:diarize", false)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: fixture.agentId, Kind: models.AgentJobRecording, SubjectID: secondRun.ID}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		jobs, err := transaction.ClaimAgentJobs("second-fixture-worker", 1, time.Now())
		if err != nil || len(jobs) != 1 {
			test.Fatal(err)
		}
		secondClaim = db.RecordingWorkerClaim{JobID: jobs[0].ID, ClaimID: jobs[0].ClaimID}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, secondRun.ID, secondClaim, models.RecordingProcessingPreparing, "", 0); err != nil {
			test.Fatal(err)
		}
		secondBlob := &models.RecordingBlob{ID: db.RecordingAudioFileID(secondRun.ID, 0), BlobSizeBytes: 128, BlobSHA256: blob.BlobSHA256}
		secondChunk, err := transaction.CreateRecordingAudioChunk(fixture.userId, secondRun.ID, secondClaim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 2000, AudioDurationMS: 1000}, secondBlob, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.VerifyRecordingBlob(fixture.userId, secondBlob.ID, secondBlob.BlobSHA256); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, secondRun.ID, secondClaim, models.RecordingProcessingTranscribing, "", 1000); err != nil {
			test.Fatal(err)
		}
		secondAttempt, err := transaction.CreateRecordingTranscriptionAttempt(fixture.userId, secondRun.ID, secondChunk.ID, secondClaim, "USD", nil)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.SubmitRecordingTranscriptionAttempt(fixture.userId, secondAttempt.ID, secondClaim); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, secondAttempt.ID, secondClaim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-local-second", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The fixture participant asked a second question."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, secondRun.ID, secondClaim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, secondSource.ID)
		if err != nil {
			test.Fatal(err)
		}
		corrections, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: secondAttempt.ID, ClientCorrectionID: "shared-second", ConfirmedName: "Shared Participant", LocalSpeakerID: "speaker-local-second", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: source.SourceGeneration})
		if err != nil || len(corrections) != 1 || corrections[0].SpeakerIdentityID != firstCorrection.SpeakerIdentityID {
			test.Fatal("account identity was not reused", err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, secondSource.ID); err != nil {
			test.Fatal(err)
		}
	})
	var previousFirstGeneration, previousSecondGeneration int64
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		first, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		second, err := transaction.GetRecording(fixture.userId, secondSource.ID)
		if err != nil {
			test.Fatal(err)
		}
		previousFirstGeneration = first.SourceGeneration
		previousSecondGeneration = second.SourceGeneration
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		if err := transaction.RenameRecordingSpeaker(fixture.userId, fixture.media.RecordingID, firstCorrection.SpeakerIdentityID, "Rolled Back Participant", previousFirstGeneration); err != nil {
			return err
		}
		return errors.New("fixture rollback")
	})
	if err == nil {
		test.Fatal("fixture transaction did not roll back")
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		first, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil || first.SourceGeneration != previousFirstGeneration {
			test.Fatal("failed rename changed first source", err)
		}
		second, err := transaction.GetRecording(fixture.userId, secondSource.ID)
		if err != nil || second.SourceGeneration != previousSecondGeneration {
			test.Fatal("failed rename changed second source", err)
		}
		if err := transaction.RenameRecordingSpeaker(fixture.userId, first.ID, firstCorrection.SpeakerIdentityID, "Renamed Shared Participant", first.SourceGeneration); err != nil {
			test.Fatal(err)
		}
		first, err = transaction.GetRecording(fixture.userId, first.ID)
		if err != nil || first.SourceGeneration != previousFirstGeneration+1 {
			test.Fatal("rename missed first source", err)
		}
		second, err = transaction.GetRecording(fixture.userId, second.ID)
		if err != nil || second.SourceGeneration != previousSecondGeneration+1 {
			test.Fatal("rename missed second source", err)
		}
		// The rename queues the memory sync of every recording that uses the identity and does not run it.
		jobs, err := transaction.ClaimAgentJobs("rename-worker", 10, time.Now())
		if err != nil {
			test.Fatal(err)
		}
		queuedSubjects := map[string]bool{}
		for _, job := range jobs {
			if job.Kind == models.AgentJobRecordingKnowledge {
				queuedSubjects[job.SubjectID] = true
			}
		}
		if len(queuedSubjects) != 2 || !queuedSubjects[first.ID] || !queuedSubjects[second.ID] {
			test.Fatal("rename did not queue the memory sync of both recordings", queuedSubjects)
		}
		for _, recordingId := range []string{first.ID, second.ID} {
			if err := transaction.SyncRecordingKnowledge(fixture.userId, recordingId); err != nil {
				test.Fatal(err)
			}
		}
		documents, _, err := transaction.ListAgentDocumentsToDigest(fixture.agentId, nil, 10)
		if err != nil || len(documents) != 2 {
			test.Fatal(err)
		}
		for _, document := range documents {
			if !strings.Contains(document.Title, "Renamed Shared Participant") {
				test.Fatal("shared identity rename left old attribution", document.Title)
			}
		}
	})
}

func TestRecordingDeletionReconcilesEvidenceAfterStoppingKnowledgeSource(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	var fact *models.AgentFact
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The fixture participant proposed changing a mechanism."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		documents, _, err := transaction.ListAgentDocumentsToDigest(fixture.agentId, nil, 10)
		if err != nil || len(documents) != 1 {
			test.Fatal(err)
		}
		page, err := transaction.PutAgentNode(&models.AgentNode{AgentID: fixture.agentId, Path: "projects/deletion-fixture", Kind: models.NodeProject, Name: "Deletion Fixture"})
		if err != nil {
			test.Fatal(err)
		}
		fact, err = transaction.AddAgentFact(&models.AgentFact{AgentID: fixture.agentId, NodeID: page.ID, Text: "A fixture change was proposed.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: documents[0].ID, Quote: "proposed changing a mechanism"}}})
		if err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+fixture.media.RecordingID)
		if err != nil || source == nil {
			test.Fatal(err)
		}
		if err := transaction.DeleteAgentSource(fixture.agentId, source.ID); err != nil {
			test.Fatal(err)
		}
		retainedFacts, err := transaction.GetAgentFacts(fixture.agentId, []string{fact.ID})
		if err != nil || len(retainedFacts) != 1 || retainedFacts[0].Dormant {
			test.Fatal("stop reading changed established memory-retention semantics", err)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		reconciledFacts, err := transaction.GetAgentFacts(fixture.agentId, []string{fact.ID})
		if err != nil || len(reconciledFacts) != 1 || !reconciledFacts[0].Dormant {
			test.Fatal("raw deletion lost evidence after index source removal", err)
		}
		revisions, err := transaction.ListAgentRevisions(fixture.agentId, fact.NodeID, 20)
		if err != nil {
			test.Fatal(err)
		}
		hasRetirement := false
		for _, revision := range revisions {
			if revision.Kind == models.RevisionFactStruck {
				hasRetirement = true
			}
		}
		if !hasRetirement {
			test.Fatal("source reconciliation dropped established revision history")
		}
	})
}
