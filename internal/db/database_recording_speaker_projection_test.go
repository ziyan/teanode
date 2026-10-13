package db_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingCorrectionsDoNotNameOrRewriteOverlappingSpeakers(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	var firstSegmentId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The first participant proposed a change."}, {LocalSpeakerID: "speaker-two", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The other participant asked a question."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Participant One", ClientCorrectionID: "overlap-name", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
		source, err = transaction.GetRecording(fixture.userId, source.ID)
		if err != nil {
			test.Fatal(err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.ProjectRecordingSpeakerCorrections(fixture.userId, segments); err != nil {
			test.Fatal(err)
		}
		for _, segment := range segments {
			if segment.LocalSpeakerID == "speaker-one" {
				firstSegmentId = segment.ID
				if !segment.HasUserConfirmedSpeaker || segment.EffectiveSpeakerName != "Participant One" {
					test.Fatal("original request-local binding was lost")
				}
			} else if segment.HasUserConfirmedSpeaker || segment.EffectiveSpeakerIdentityID != "" {
				test.Fatal("another overlapping speaker became confirmed")
			}
		}
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, db.RecordingTranscriptTextCorrectionInput{RecordingID: source.ID, RecordingTranscriptSegmentID: firstSegmentId, ClientCorrectionID: "overlap-text", CorrectedTranscriptText: "The first participant proposed an optional change.", ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
		// The text correction queues the memory sync; this is what its job runs.
		if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
		knowledge, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+source.ID)
		if err != nil {
			test.Fatal(err)
		}
		for _, segment := range segments {
			document, err := transaction.GetAgentDocumentByExternal(knowledge.ID, fixture.transcriptRun.ID+":"+segment.ID)
			if err != nil {
				test.Fatal(err)
			}
			if segment.LocalSpeakerID == "speaker-two" && (document.Metadata["hasUserConfirmedSpeaker"] == true || strings.Contains(document.Title, "Participant One")) {
				test.Fatal("knowledge named the unrelated speaker")
			}
		}
	})
	merged := createTextCorrectionRerun(test, fixture, "merged-overlap", []*models.RecordingTranscriptSegment{{LocalSpeakerID: "merged-speaker", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The first participant proposed a change while another asked a question."}})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, merged.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.ProjectRecordingSpeakerCorrections(fixture.userId, segments); err != nil {
			test.Fatal(err)
		}
		if err := transaction.ProjectRecordingTranscriptTextCorrections(fixture.userId, segments); err != nil {
			test.Fatal(err)
		}
		if segments[0].HasUserConfirmedSpeaker || !segments[0].HasUnalignedSpeakerCorrection {
			test.Fatal("merged speaker inherited a guessed identity")
		}
		if segments[0].AppliedTextCorrectionID != "" || !segments[0].HasUnalignedTextCorrection || segments[0].EffectiveTranscriptText != segments[0].TranscriptText {
			test.Fatal("one original speaker's edit replaced merged speech")
		}
		knowledge, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		confirmedOriginal, err := transaction.GetAgentDocumentByExternal(knowledge.ID, fmt.Sprintf("speaker-correction:%s:speaker-one:2000:3000", attempt.ID))
		if err != nil || confirmedOriginal == nil || confirmedOriginal.Metadata["isSpeakerAlignmentUncertain"] != true {
			test.Fatal("unaligned original confirmation evidence was lost", err)
		}
		independent, err := transaction.GetAgentDocumentByExternal(knowledge.ID, "text-correction:"+firstSegmentId)
		if err != nil || independent == nil {
			test.Fatal("unaligned original text correction lost", err)
		}
	})
}

func TestRecordingTextProjectionRejectsCompetingOriginalSegments(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "An original proposal."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
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
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, db.RecordingTranscriptTextCorrectionInput{RecordingID: source.ID, RecordingTranscriptSegmentID: segments[0].ID, ClientCorrectionID: "origin-one", CorrectedTranscriptText: "The first original correction.", ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
	})
	second := createTextCorrectionRerun(test, fixture, "second-origin", []*models.RecordingTranscriptSegment{{LocalSpeakerID: "another-speaker", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "A differently interpreted proposal."}})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, second.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, db.RecordingTranscriptTextCorrectionInput{RecordingID: source.ID, RecordingTranscriptSegmentID: segments[0].ID, ClientCorrectionID: "origin-two", CorrectedTranscriptText: "The second original correction.", ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
	})
	third := createTextCorrectionRerun(test, fixture, "third-origin", []*models.RecordingTranscriptSegment{{LocalSpeakerID: "merged-speaker", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "A merged interpretation of proposals."}})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, third.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.ProjectRecordingTranscriptTextCorrections(fixture.userId, segments); err != nil {
			test.Fatal(err)
		}
		if segments[0].AppliedTextCorrectionID != "" || !segments[0].HasUnalignedTextCorrection || segments[0].EffectiveTranscriptText != segments[0].TranscriptText {
			test.Fatal("competing origin edits chose a guessed merged wording")
		}
		knowledge, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		for _, originalRun := range []*models.RecordingTranscriptRun{fixture.transcriptRun, second} {
			originals, err := transaction.ListRecordingTranscriptSegments(fixture.userId, originalRun.ID)
			if err != nil {
				test.Fatal(err)
			}
			independent, err := transaction.GetAgentDocumentByExternal(knowledge.ID, "text-correction:"+originals[0].ID)
			if err != nil || independent == nil {
				test.Fatal("competing correction evidence was discarded", err)
			}
		}
	})
}

func TestRecordingKnowledgeTitleKeepsStableSourceIdentity(test *testing.T) {
	fixture := newTranscriptFixture(test)
	completeLegacyRecording(test, fixture)
	var originalSource *models.AgentKnowledgeSource
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		var err error
		originalSource, err = transaction.GetAgentSourceByName(fixture.agentId, "recording:"+fixture.media.RecordingID)
		if err != nil || originalSource == nil {
			test.Fatal(err)
		}
		originalSource.Enabled = false
		if _, err := transaction.PutAgentSource(originalSource); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.UpdateRecordingMetadata(fixture.userId, source.ID, source.SourceGeneration, new("Fixture \"quoted\" title"), nil); err != nil {
			test.Fatal(err)
		}
		// The title edit queues the memory sync; this is what its job runs.
		if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
		knowledge, err := transaction.GetAgentSourceByName(fixture.agentId, originalSource.Name)
		if err != nil || knowledge.ID != originalSource.ID || knowledge.Enabled {
			test.Fatal("title edit changed source identity or reading preference", err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
		unchanged, err := transaction.GetAgentSourceByName(fixture.agentId, originalSource.Name)
		if err != nil || unchanged.Generation != knowledge.Generation {
			test.Fatal("unchanged display metadata created another source revision", err)
		}
		var settings map[string]string
		if err := json.Unmarshal(knowledge.Specification.Settings, &settings); err != nil {
			test.Fatal(err)
		}
		if settings["recordingId"] != source.ID || settings["recordingTitle"] != "Fixture \"quoted\" title" {
			test.Fatal("source display metadata was not safely published", settings)
		}
	})
}

func TestRecordingRenamedSourceCannotReviveOrRetainEvidence(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	var staleDocument *models.AgentDocument
	var renamedSourceId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "A fixture proposal."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		knowledge, err := transaction.GetAgentSourceByName(fixture.agentId, "recording:"+fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		staleDocument, err = transaction.GetAgentDocumentByExternal(knowledge.ID, fixture.transcriptRun.ID+":"+segments[0].ID)
		if err != nil {
			test.Fatal(err)
		}
		knowledge.Name = "renamed-fixture-source"
		renamed, err := transaction.PutAgentSource(knowledge)
		if err != nil {
			test.Fatal(err)
		}
		renamedSourceId = renamed.ID
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Fixture Participant", ClientCorrectionID: "renamed-source-confirmation", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.CheckRecordingKnowledgeEvidence(fixture.userId, []*models.AgentDocument{staleDocument})
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("renamed-source evidence could recreate outdated attribution", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		renamed, err := transaction.GetAgentSource(fixture.agentId, renamedSourceId)
		if err != nil || renamed != nil {
			test.Fatal("raw deletion left renamed recording source", err)
		}
		document, err := transaction.GetAgentDocument(fixture.agentId, staleDocument.ID)
		if err != nil || document != nil {
			test.Fatal("raw deletion retained cached transcript bytes", err)
		}
	})
}
