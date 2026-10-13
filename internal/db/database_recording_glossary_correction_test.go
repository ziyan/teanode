package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

// completeGlossaryFixture finishes the fixture's transcript with the given
// passages, 300 milliseconds each, and returns them in timeline order.
func completeGlossaryFixture(test *testing.T, texts ...string) (*transcriptFixture, []*models.RecordingTranscriptSegment) {
	test.Helper()
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	var passages []*models.RecordingTranscriptSegment
	for index, text := range texts {
		passages = append(passages, &models.RecordingTranscriptSegment{LocalSpeakerID: "speaker-one", SegmentStartMS: int64(2000 + index*300), SegmentEndMS: int64(2300 + index*300), TranscriptText: text})
	}
	var segments []*models.RecordingTranscriptSegment
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, passages); err != nil {
			test.Fatal(err)
		}
		var err error
		if segments, err = transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000*int64(len(texts)+2)); err != nil {
			test.Fatal(err)
		}
	})
	return fixture, segments
}

func effectiveGlossaryTexts(test *testing.T, fixture *transcriptFixture) []string {
	test.Helper()
	var texts []string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.ProjectRecordingTranscriptTextCorrections(fixture.userId, segments); err != nil {
			test.Fatal(err)
		}
		for _, segment := range segments {
			texts = append(texts, segment.EffectiveTranscriptText)
		}
	})
	return texts
}

func applyGlossaryFix(test *testing.T, fixture *transcriptFixture, segment *models.RecordingTranscriptSegment, originalPhrase, correctedPhrase, glossaryTerm string) []*models.RecordingTranscriptTextCorrection {
	test.Helper()
	var applied []*models.RecordingTranscriptTextCorrection
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		applied, err = transaction.ApplyRecordingGlossaryCorrections(fixture.userId, fixture.media.RecordingID, []db.RecordingGlossaryCorrectionInput{{RecordingTranscriptSegmentID: segment.ID, OriginalPhrase: originalPhrase, CorrectedPhrase: correctedPhrase, GlossaryTerm: glossaryTerm}})
		if err != nil {
			test.Fatal(err)
		}
	})
	return applied
}

func recordingGeneration(test *testing.T, fixture *transcriptFixture) int64 {
	test.Helper()
	var generation int64
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		generation = source.SourceGeneration
	})
	return generation
}

func TestGlossaryCorrectionIsProjectedAndKeepsTheOriginal(test *testing.T) {
	fixture, segments := completeGlossaryFixture(test, "We shipped Tea node today.", "Nothing to fix here.")
	applied := applyGlossaryFix(test, fixture, segments[0], "Tea node", "TeaNode", "TeaNode")
	if len(applied) != 1 || applied[0].TextCorrectionOrigin != models.TextCorrectionOriginGlossary || applied[0].GlossaryTerm != "TeaNode" || applied[0].OriginalTranscriptText != "We shipped Tea node today." {
		test.Fatalf("the fix is recorded with its origin and the original text: %+v", applied)
	}
	texts := effectiveGlossaryTexts(test, fixture)
	if texts[0] != "We shipped TeaNode today." || texts[1] != "Nothing to fix here." {
		test.Fatalf("the fix reads through the projection and only there: %v", texts)
	}
	var stored []*models.RecordingTranscriptSegment
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		stored, _ = transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
	})
	if stored[0].TranscriptText != "We shipped Tea node today." {
		test.Fatal("the recognized text was rewritten")
	}
	// A phrase that is not in the passage, or a second pass over fixed text, changes nothing.
	if again := applyGlossaryFix(test, fixture, segments[0], "Tea node", "TeaNode", "TeaNode"); len(again) != 0 {
		test.Fatalf("a phrase that is no longer in the passage was fixed again: %+v", again)
	}
	if missing := applyGlossaryFix(test, fixture, segments[1], "Plover", "Plover Hall", "Plover Hall"); len(missing) != 0 {
		test.Fatalf("a phrase that is not in the passage was applied: %+v", missing)
	}
}

func TestPersonCorrectionBlocksAndSupersedesGlossaryCorrection(test *testing.T) {
	fixture, segments := completeGlossaryFixture(test, "Tea node is up.", "Off line again.")
	// The person corrects the second passage first: the glossary may not touch it.
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, db.RecordingTranscriptTextCorrectionInput{RecordingID: fixture.media.RecordingID, RecordingTranscriptSegmentID: segments[1].ID, ClientCorrectionID: "person-first", CorrectedTranscriptText: "Offline again.", ExpectedSourceGeneration: recordingGeneration(test, fixture)}); err != nil {
			test.Fatal(err)
		}
	})
	if applied := applyGlossaryFix(test, fixture, segments[1], "Offline", "offline", "offline"); len(applied) != 0 {
		test.Fatalf("a glossary fix landed on a passage the person corrected: %+v", applied)
	}
	// The glossary fixes the first passage; the person then rewrites it and wins.
	applyGlossaryFix(test, fixture, segments[0], "Tea node", "TeaNode", "TeaNode")
	if texts := effectiveGlossaryTexts(test, fixture); texts[0] != "TeaNode is up." {
		test.Fatalf("the glossary fix is not showing: %v", texts)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, db.RecordingTranscriptTextCorrectionInput{RecordingID: fixture.media.RecordingID, RecordingTranscriptSegmentID: segments[0].ID, ClientCorrectionID: "person-second", CorrectedTranscriptText: "The Tea Node service is up.", ExpectedSourceGeneration: recordingGeneration(test, fixture)}); err != nil {
			test.Fatal(err)
		}
		corrections, err := transaction.ListRecordingTranscriptTextCorrections(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		for _, correction := range corrections {
			if correction.TextCorrectionOrigin == models.TextCorrectionOriginGlossary && !correction.IsRemoved {
				test.Fatal("the glossary fix on a passage the person rewrote is still active")
			}
		}
	})
	if texts := effectiveGlossaryTexts(test, fixture); texts[0] != "The Tea Node service is up." || texts[1] != "Offline again." {
		test.Fatalf("the person's words win: %v", texts)
	}
}

func TestRemovedGlossaryCorrectionIsNotAppliedAgain(test *testing.T) {
	fixture, segments := completeGlossaryFixture(test, "Tea node is up.")
	applied := applyGlossaryFix(test, fixture, segments[0], "Tea node", "TeaNode", "TeaNode")
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.RemoveRecordingTranscriptTextCorrection(fixture.userId, fixture.media.RecordingID, applied[0].ID, recordingGeneration(test, fixture)); err != nil {
			test.Fatal(err)
		}
	})
	if texts := effectiveGlossaryTexts(test, fixture); texts[0] != "Tea node is up." {
		test.Fatalf("removing the fix restores the recognized text: %v", texts)
	}
	if again := applyGlossaryFix(test, fixture, segments[0], "Tea node", "TeaNode", "TeaNode"); len(again) != 0 {
		test.Fatalf("a fix the person undid was made again: %+v", again)
	}
	// A different fix to the same passage is not the one that was undone.
	if other := applyGlossaryFix(test, fixture, segments[0], "Tea node", "Tea Node", "Tea Node"); len(other) != 1 {
		test.Fatalf("a different fix is still allowed: %+v", other)
	}
}

func TestGlossaryCorrectionMigrationAddsAndDropsTheColumns(test *testing.T) {
	fixture, segments := completeGlossaryFixture(test, "Tea node is up.")
	applyGlossaryFix(test, fixture, segments[0], "Tea node", "TeaNode", "TeaNode")
	var migration migrations.Migration
	for _, candidate := range migrations.Migrations() {
		if candidate.ID == "0172_recording_glossary_corrections" {
			migration = candidate
		}
	}
	if migration.ID == "" {
		test.Fatal("there is no migration 0172_recording_glossary_corrections")
	}
	dbtest.Exec(test, fixture.database, migration.ReverseSQL)
	if columns := dbtest.QueryString(test, fixture.database, `SELECT COUNT(*)::text FROM information_schema.columns WHERE table_name = 'recording_transcript_text_correction' AND column_name IN ('text_correction_origin', 'glossary_term', 'original_phrase', 'corrected_phrase')`); columns != "0" {
		test.Fatalf("the reverse removes the columns: %s left", columns)
	}
	if rows := dbtest.QueryString(test, fixture.database, `SELECT COUNT(*)::text FROM recording_transcript_text_correction`); rows != "0" {
		test.Fatalf("the reverse removes the glossary fixes it cannot describe: %s left", rows)
	}
	dbtest.Exec(test, fixture.database, migration.SQL)
	if columns := dbtest.QueryString(test, fixture.database, `SELECT COUNT(*)::text FROM information_schema.columns WHERE table_name = 'recording_transcript_text_correction' AND column_name IN ('text_correction_origin', 'glossary_term', 'original_phrase', 'corrected_phrase')`); columns != "4" {
		test.Fatalf("the migration adds four columns: %s", columns)
	}
}

func removeGlossaryFix(test *testing.T, fixture *transcriptFixture, correction *models.RecordingTranscriptTextCorrection) {
	test.Helper()
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.RemoveRecordingTranscriptTextCorrection(fixture.userId, fixture.media.RecordingID, correction.ID, recordingGeneration(test, fixture)); err != nil {
			test.Fatal(err)
		}
	})
}

func TestUndoneGlossaryFixBlocksTheTermOnThePassageWhateverTheWords(test *testing.T) {
	fixture, segments := completeGlossaryFixture(test, "Tea node is up.", "Tea node is down.")
	applied := applyGlossaryFix(test, fixture, segments[0], "Tea node", "TeaNode", "TeaNode")
	removeGlossaryFix(test, fixture, applied[0])
	// A rerun that reads one more word of the same misheard term is the same undone fix.
	if again := applyGlossaryFix(test, fixture, segments[0], "Tea node is", "TeaNode is", "TeaNode"); len(again) != 0 {
		test.Fatalf("a rerun brought back a fix the person undid: %+v", again)
	}
	// The same term on a passage the person did not undo it on is still fixed.
	if other := applyGlossaryFix(test, fixture, segments[1], "Tea node", "TeaNode", "TeaNode"); len(other) != 1 {
		test.Fatalf("an undo on one passage does not block the term on another: %+v", other)
	}
}

func TestFoldedGlossaryFixIsNotUndoneUntilTheFixThatTookItInIs(test *testing.T) {
	fixture, segments := completeGlossaryFixture(test, "Fern croft and off line and Ferne croft.")
	applyGlossaryFix(test, fixture, segments[0], "Fern croft", "Ferncroft", "Ferncroft")
	second := applyGlossaryFix(test, fixture, segments[0], "off line", "offline", "offline")
	// The first fix was folded into the second, not undone, so its term is still allowed here.
	third := applyGlossaryFix(test, fixture, segments[0], "Ferne croft", "Ferncroft", "Ferncroft")
	if len(second) != 1 || len(third) != 1 {
		test.Fatalf("a folded fix does not block its term: %+v %+v", second, third)
	}
	if texts := effectiveGlossaryTexts(test, fixture); texts[0] != "Ferncroft and offline and Ferncroft." {
		test.Fatalf("every fix reads through: %v", texts)
	}
	// Undoing the last fix restores the recognized text, which undoes all three.
	removeGlossaryFix(test, fixture, third[0])
	if texts := effectiveGlossaryTexts(test, fixture); texts[0] != "Fern croft and off line and Ferne croft." {
		test.Fatalf("removing the fix restores the recognized text: %v", texts)
	}
	if again := applyGlossaryFix(test, fixture, segments[0], "Fern croft", "Ferncroft", "Ferncroft"); len(again) != 0 {
		test.Fatalf("a fix folded into the one the person undid came back: %+v", again)
	}
	if again := applyGlossaryFix(test, fixture, segments[0], "off line", "offline", "offline"); len(again) != 0 {
		test.Fatalf("a fix folded into the one the person undid came back: %+v", again)
	}
}

func TestGlossaryFixReplacesEveryWholeWordOccurrence(test *testing.T) {
	fixture, segments := completeGlossaryFixture(test, "Fern croft, Fern crofts and Fern croft again.")
	applied := applyGlossaryFix(test, fixture, segments[0], "Fern croft", "Ferncroft", "Ferncroft")
	if len(applied) != 1 {
		test.Fatalf("the fix is made: %+v", applied)
	}
	if texts := effectiveGlossaryTexts(test, fixture); texts[0] != "Ferncroft, Fern crofts and Ferncroft again." {
		test.Fatalf("every whole-word occurrence is replaced and a longer word is left alone: %v", texts)
	}
	only := []string{"Fern crofts are here."}
	other, otherSegments := completeGlossaryFixture(test, only...)
	if applied := applyGlossaryFix(test, other, otherSegments[0], "Fern croft", "Ferncroft", "Ferncroft"); len(applied) != 0 {
		test.Fatalf("a phrase that only starts a longer word was replaced: %+v", applied)
	}
}

func TestGlossaryCorrectionIsLabeledAsAutomaticInTheProjection(test *testing.T) {
	fixture, segments := completeGlossaryFixture(test, "Tea node is up.", "Off line again.")
	applyGlossaryFix(test, fixture, segments[0], "Tea node", "TeaNode", "TeaNode")
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, db.RecordingTranscriptTextCorrectionInput{RecordingID: fixture.media.RecordingID, RecordingTranscriptSegmentID: segments[1].ID, ClientCorrectionID: "person-edit", CorrectedTranscriptText: "Offline again.", ExpectedSourceGeneration: recordingGeneration(test, fixture)}); err != nil {
			test.Fatal(err)
		}
		listed, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.ProjectRecordingTranscriptTextCorrections(fixture.userId, listed); err != nil {
			test.Fatal(err)
		}
		if listed[0].AppliedTextCorrectionOrigin != models.TextCorrectionOriginGlossary || listed[1].AppliedTextCorrectionOrigin != models.TextCorrectionOriginPerson {
			test.Fatalf("the projection says who made the correction: %q %q", listed[0].AppliedTextCorrectionOrigin, listed[1].AppliedTextCorrectionOrigin)
		}
	})
}

func TestDeletingARecordingDeletesTheRunConversationsThatReadItsTranscript(test *testing.T) {
	fixture, _ := completeGlossaryFixture(test, "Tea node is up.")
	recordingId := fixture.media.RecordingID
	otherRecordingId := "other-recording"
	create := func(transaction db.Transaction, kind models.AgentConversationKind, jobKind models.AgentJobKind, subjectId string) string {
		conversation, err := transaction.CreateAgentConversation(&models.AgentConversation{AgentID: fixture.agentId, Kind: kind, Title: "run", JobKind: string(jobKind), SubjectID: subjectId, LastAt: time.Now()})
		if err != nil {
			test.Fatal(err)
		}
		return conversation.ID
	}
	var termsId, summaryId, unrelatedKindId, otherRecordingConversationId, mainId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		termsId = create(transaction, models.AgentConversationRun, models.AgentJobRecordingTerms, recordingId)
		summaryId = create(transaction, models.AgentConversationRun, models.AgentJobRecordingSummary, recordingId)
		unrelatedKindId = create(transaction, models.AgentConversationRun, models.AgentJobRecording, recordingId)
		otherRecordingConversationId = create(transaction, models.AgentConversationRun, models.AgentJobRecordingTerms, otherRecordingId)
		mainId = create(transaction, models.AgentConversationMain, "", recordingId)
		if _, err := transaction.AppendAgentMessage(&models.AgentMessage{ConversationID: termsId, Role: "user", Content: "Tea node is up."}); err != nil {
			test.Fatal(err)
		}
	})
	exists := func(conversationId string) bool {
		found := false
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			conversation, err := transaction.GetAgentConversation(conversationId)
			if err != nil {
				test.Fatal(err)
			}
			found = conversation != nil
		})
		return found
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(fixture.userId, recordingId); err != nil {
			test.Fatal(err)
		}
	})
	if exists(termsId) || exists(summaryId) {
		test.Fatal("the run conversations of the terms and naming jobs outlived the recording")
	}
	if !exists(unrelatedKindId) || !exists(otherRecordingConversationId) || !exists(mainId) {
		test.Fatal("a conversation of another kind, job or recording was deleted")
	}
	// A job that was still running when the recording was deleted writes its
	// conversation afterwards; cleanup removes that one too.
	var lateId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		lateId = create(transaction, models.AgentConversationRun, models.AgentJobRecordingSummary, recordingId)
		blobs, err := transaction.ListRecordingBlobsForCleanup(recordingId, 10)
		if err != nil {
			test.Fatal(err)
		}
		for _, blob := range blobs {
			if err := transaction.DeleteRecordingBlobReference(recordingId, blob.ID); err != nil {
				test.Fatal(err)
			}
		}
		if err := transaction.AdvanceRecordingMediaCleanup(recordingId, fixture.media.ID, 0); err != nil {
			test.Fatal(err)
		}
		if err := transaction.FinishRecordingCleanup(recordingId); err != nil {
			test.Fatal(err)
		}
	})
	if exists(lateId) {
		test.Fatal("cleanup left a run conversation written after the deletion")
	}
}

func TestGlossaryFoldedMigrationAddsAndDropsTheColumn(test *testing.T) {
	fixture, _ := completeGlossaryFixture(test, "Tea node is up.")
	var migration migrations.Migration
	for _, candidate := range migrations.Migrations() {
		if candidate.ID == "0173_recording_glossary_correction_folded" {
			migration = candidate
		}
	}
	if migration.ID == "" {
		test.Fatal("there is no migration 0173_recording_glossary_correction_folded")
	}
	dbtest.Exec(test, fixture.database, migration.ReverseSQL)
	if columns := dbtest.QueryString(test, fixture.database, `SELECT COUNT(*)::text FROM information_schema.columns WHERE table_name = 'recording_transcript_text_correction' AND column_name = 'is_folded'`); columns != "0" {
		test.Fatalf("the reverse removes the column: %s left", columns)
	}
	dbtest.Exec(test, fixture.database, migration.SQL)
	if columns := dbtest.QueryString(test, fixture.database, `SELECT COUNT(*)::text FROM information_schema.columns WHERE table_name = 'recording_transcript_text_correction' AND column_name = 'is_folded'`); columns != "1" {
		test.Fatalf("the migration adds the column: %s", columns)
	}
}
