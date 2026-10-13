package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

const (
	scalePieceCount            = 150
	scalePassagesPerPiece      = 20
	scalePieceDurationMS       = 20000
	scaleSpeakerCallLimit      = 2 * time.Second
	scaleTimelineFirstPageSize = 250
)

// scaleFixture is a captured recording of scalePieceCount pieces, each with
// scalePassagesPerPiece passages. The first speaker of the first piece is a
// temporary speaker whose sample was sent to every later piece, so naming it
// writes a correction in every piece.
type scaleFixture struct {
	*transcriptFixture
	recordingId  string
	speaker      *models.RecordingTemporarySpeaker
	firstAttempt *models.RecordingTranscriptionAttempt
	attemptIds   []string
}

func newScaleFixture(test *testing.T) *scaleFixture {
	test.Helper()
	fixture := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	scale := &scaleFixture{transcriptFixture: fixture, recordingId: fixture.media.RecordingID}
	blobs := make([]*models.RecordingBlob, scalePieceCount)
	chunks := make([]*models.RecordingAudioChunk, scalePieceCount)
	for pieceIndex := 0; pieceIndex < scalePieceCount; pieceIndex++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("derived-wave-bytes-%d", pieceIndex)))
		blobs[pieceIndex] = &models.RecordingBlob{ID: db.RecordingAudioFileID(fixture.transcriptRun.ID, pieceIndex), BlobSizeBytes: 128, BlobSHA256: hex.EncodeToString(digest[:])}
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		for pieceIndex := 0; pieceIndex < scalePieceCount; pieceIndex++ {
			var err error
			chunks[pieceIndex], err = transaction.CreateRecordingAudioChunk(fixture.userId, fixture.transcriptRun.ID, fixture.claim, &models.RecordingAudioChunk{ChunkIndex: pieceIndex, TimelineStartMS: int64(2000 + pieceIndex*scalePieceDurationMS), AudioDurationMS: scalePieceDurationMS}, blobs[pieceIndex], 1<<30)
			if err != nil {
				test.Fatal(err)
			}
		}
		for pieceIndex := 0; pieceIndex < scalePieceCount; pieceIndex++ {
			if err := transaction.VerifyRecordingBlob(fixture.userId, blobs[pieceIndex].ID, blobs[pieceIndex].BlobSHA256); err != nil {
				test.Fatal(err)
			}
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingTranscribing, "", scalePieceCount*scalePieceDurationMS); err != nil {
			test.Fatal(err)
		}
	})
	finishPiece := func(transaction db.Transaction, pieceIndex int, referenceBlobIds []string, targetLabel, otherLabel string) *models.RecordingTranscriptionAttempt {
		attempt, err := transaction.CreateRecordingTranscriptionAttempt(fixture.userId, fixture.transcriptRun.ID, chunks[pieceIndex].ID, fixture.claim, "USD", referenceBlobIds)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.SubmitRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim); err != nil {
			test.Fatal(err)
		}
		segments := make([]*models.RecordingTranscriptSegment, 0, scalePassagesPerPiece)
		for passageIndex := 0; passageIndex < scalePassagesPerPiece; passageIndex++ {
			localSpeakerId := targetLabel
			if passageIndex%2 == 1 {
				localSpeakerId = otherLabel
			}
			startMS := int64(2000 + pieceIndex*scalePieceDurationMS + passageIndex*1000)
			segments = append(segments, &models.RecordingTranscriptSegment{LocalSpeakerID: localSpeakerId, SegmentStartMS: startMS, SegmentEndMS: startMS + 1000, TranscriptText: fmt.Sprintf("Passage %d of piece %d.", passageIndex, pieceIndex)})
		}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, segments); err != nil {
			test.Fatal(err)
		}
		return attempt
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		for pieceIndex := 0; pieceIndex < scalePieceCount; pieceIndex++ {
			targetLabel := "Speaker 1"
			otherLabel := "Speaker 2"
			if pieceIndex == 0 {
				targetLabel, otherLabel = "A", "B"
			}
			attempt := finishPiece(transaction, pieceIndex, nil, targetLabel, otherLabel)
			scale.attemptIds = append(scale.attemptIds, attempt.ID)
		}
		scale.firstAttempt = &models.RecordingTranscriptionAttempt{ID: scale.attemptIds[0]}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", scalePieceCount*scalePieceDurationMS); err != nil {
			test.Fatal(err)
		}
		var err error
		scale.speaker, err = transaction.CreateRecordingTemporarySpeaker(fixture.userId, &models.RecordingTemporarySpeaker{RecordingID: scale.recordingId, SourceAttemptID: scale.firstAttempt.ID, SourceLocalSpeakerID: "A", PassageStartMS: 2000, PassageEndMS: 4000}, temporarySpeakerSample("scale sample"), 1<<30)
		if err != nil {
			test.Fatal(err)
		}
		if scale.speaker.SpeakerLabel != "Speaker 1" {
			test.Fatal("the fixture expects the first speaker label", scale.speaker.SpeakerLabel)
		}
		sampleDigest := sha256.Sum256([]byte("scale sample"))
		if err := transaction.VerifyRecordingBlob(fixture.userId, scale.speaker.RecordingBlobID, hex.EncodeToString(sampleDigest[:])); err != nil {
			test.Fatal(err)
		}
	})
	// Every later piece was sent the speaker's sample, as a capture does.
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording_transcription_attempt SET reference_blob_ids = jsonb_build_array('%s'::text) WHERE id <> '%s' AND recording_transcript_run_id = '%s'", scale.speaker.RecordingBlobID, scale.firstAttempt.ID, fixture.transcriptRun.ID))
	return scale
}

func stopScaleCapture(test *testing.T, scale *scaleFixture) {
	test.Helper()
	dbtest.Exec(test, scale.database, fmt.Sprintf("UPDATE recording SET recording_capture_state = '%s' WHERE id = '%s'", models.RecordingCaptureStopped, scale.recordingId))
}

// knowledgeOf is what the memory holds of a recording: each document's place in
// the transcript, the hash of its words and the text of its chunk.
func knowledgeOf(test *testing.T, scale *scaleFixture) string {
	test.Helper()
	return dbtest.QueryString(test, scale.database, fmt.Sprintf(`SELECT COALESCE(md5(string_agg(document.external_id || ':' || document.hash || ':' || COALESCE(chunk.text, ''), '|' ORDER BY document.external_id)), '') || ':' || COUNT(*)
		FROM agent_document document LEFT JOIN agent_chunk chunk ON chunk.document_id = document.id
		JOIN recording_knowledge_document link ON link.agent_document_id = document.id WHERE link.recording_id = '%s'`, scale.recordingId))
}

// Naming a speaker of a recording of 150 pieces stores the naming and returns;
// the passages read the new name at once and the memory is left to a job.
func TestNamingATemporarySpeakerOfALongRecordingIsQuick(test *testing.T) {
	scale := newScaleFixture(test)
	stopScaleCapture(test, scale)
	var elapsed time.Duration
	dbtest.RunTransactionOn(test, scale.database, func(transaction db.Transaction) {
		startedAt := time.Now()
		if err := transaction.NameRecordingTemporarySpeaker(scale.userId, scale.recordingId, scale.speaker.SpeakerLabel, "Participant One", false); err != nil {
			test.Fatal(err)
		}
		if err := transaction.QueueRecordingKnowledgeSync(scale.userId, scale.recordingId); err != nil {
			test.Fatal(err)
		}
		elapsed = time.Since(startedAt)
		test.Logf("naming a temporary speaker took %s", elapsed)
		segments := timelineOf(test, transaction, scale.userId, scale.recordingId)
		if len(segments) != scaleTimelineFirstPageSize {
			test.Fatal("unexpected first page", len(segments))
		}
		namedCount := 0
		for _, segment := range segments {
			if segment.EffectiveSpeakerName == "Participant One" && segment.HasUserConfirmedSpeaker {
				namedCount++
			}
		}
		if namedCount != scaleTimelineFirstPageSize/2 {
			test.Fatal("the timeline did not show the new name at once", namedCount)
		}
	})
	if elapsed > scaleSpeakerCallLimit {
		test.Fatalf("naming took %s, limit %s", elapsed, scaleSpeakerCallLimit)
	}
	if dbtest.QueryString(test, scale.database, "SELECT COUNT(*)::text FROM recording_speaker_correction") != fmt.Sprint(scalePieceCount*scalePassagesPerPiece/2) {
		test.Fatal("not every passage of the speaker was named")
	}
	if dbtest.QueryString(test, scale.database, "SELECT COUNT(*)::text FROM recording_knowledge_document") != "0" {
		test.Fatal("naming rewrote the memory inside the request")
	}
}

// Group naming of one speaker of one piece is one batch of inserts however many passages it has.
func TestCorrectingAWholeSpeakerOfALongPieceIsQuick(test *testing.T) {
	scale := newScaleFixture(test)
	var elapsed time.Duration
	dbtest.RunTransactionOn(test, scale.database, func(transaction db.Transaction) {
		current, err := transaction.GetRecording(scale.userId, scale.recordingId)
		if err != nil {
			test.Fatal(err)
		}
		startedAt := time.Now()
		corrected, err := transaction.CorrectRecordingSpeaker(scale.userId, db.RecordingSpeakerCorrectionInput{RecordingID: scale.recordingId, RecordingTranscriptionAttemptID: scale.attemptIds[1], ClientCorrectionID: "whole-speaker", LocalSpeakerID: scale.speaker.SpeakerLabel, ConfirmedName: "Participant Two", ExpectedSourceGeneration: current.SourceGeneration, PassageStartMS: -1, PassageEndMS: -1})
		elapsed = time.Since(startedAt)
		if err != nil || len(corrected) != scalePassagesPerPiece/2 {
			test.Fatal("the speaker was not named", err, len(corrected))
		}
	})
	test.Logf("correcting a speaker took %s", elapsed)
	if elapsed > scaleSpeakerCallLimit {
		test.Fatalf("correcting took %s, limit %s", elapsed, scaleSpeakerCallLimit)
	}
}

// Namings in a row queue one knowledge sync, and a sync read in slices, each in
// a transaction of its own, leaves the memory the one inline pass would.
func TestKnowledgeSyncQueuedByNamingsMatchesTheInlinePass(test *testing.T) {
	scale := newScaleFixture(test)
	stopScaleCapture(test, scale)
	for _, name := range []string{"Participant One", "Participant Two", "Participant One"} {
		if err := scale.database.Transaction(func(transaction db.Transaction) error {
			if err := transaction.NameRecordingTemporarySpeaker(scale.userId, scale.recordingId, scale.speaker.SpeakerLabel, name, false); err != nil {
				return err
			}
			return transaction.QueueRecordingKnowledgeSync(scale.userId, scale.recordingId)
		}); err != nil {
			test.Fatal(err)
		}
	}
	queuedCount := dbtest.QueryString(test, scale.database, fmt.Sprintf("SELECT COUNT(*)::text FROM agent_job WHERE kind = '%s' AND subject_id = '%s' AND status = '%s'", models.AgentJobRecordingKnowledge, scale.recordingId, models.AgentJobQueued))
	if queuedCount != "1" {
		test.Fatal("three namings did not queue exactly one sync", queuedCount)
	}
	// What the job does: a transaction per slice.
	progress := &db.RecordingKnowledgeSyncProgress{}
	sliceCount := 0
	for isFinished := false; !isFinished; sliceCount++ {
		if err := scale.database.Transaction(func(transaction db.Transaction) (err error) {
			isFinished, err = transaction.SyncRecordingKnowledgeStep(scale.userId, scale.recordingId, progress)
			return err
		}); err != nil {
			test.Fatal(err)
		}
	}
	if sliceCount < scalePieceCount*scalePassagesPerPiece/250 {
		test.Fatal("the pass was not read in slices", sliceCount)
	}
	bySlices := knowledgeOf(test, scale)
	if !strings.HasSuffix(bySlices, fmt.Sprintf(":%d", scalePieceCount*scalePassagesPerPiece)) {
		test.Fatal("not every passage reached the memory", bySlices)
	}
	// The inline pass over the same recording changes nothing the job wrote.
	dbtest.RunTransactionOn(test, scale.database, func(transaction db.Transaction) {
		if err := transaction.SyncRecordingKnowledge(scale.userId, scale.recordingId); err != nil {
			test.Fatal(err)
		}
	})
	if inline := knowledgeOf(test, scale); inline != bySlices {
		test.Fatal("the sync read in slices differs from the inline pass", bySlices, inline)
	}
	// A sync of the second name rewrites the passages, and the slices agree with the inline pass again.
	if err := scale.database.Transaction(func(transaction db.Transaction) error {
		return transaction.NameRecordingTemporarySpeaker(scale.userId, scale.recordingId, scale.speaker.SpeakerLabel, "Participant Three", false)
	}); err != nil {
		test.Fatal(err)
	}
	progress = &db.RecordingKnowledgeSyncProgress{}
	for isFinished := false; !isFinished; {
		if err := scale.database.Transaction(func(transaction db.Transaction) (err error) {
			isFinished, err = transaction.SyncRecordingKnowledgeStep(scale.userId, scale.recordingId, progress)
			return err
		}); err != nil {
			test.Fatal(err)
		}
	}
	renamedBySlices := knowledgeOf(test, scale)
	if renamedBySlices == bySlices {
		test.Fatal("renaming the speaker left the memory as it was")
	}
	dbtest.RunTransactionOn(test, scale.database, func(transaction db.Transaction) {
		if err := transaction.SyncRecordingKnowledge(scale.userId, scale.recordingId); err != nil {
			test.Fatal(err)
		}
	})
	if inline := knowledgeOf(test, scale); inline != renamedBySlices {
		test.Fatal("the renamed sync read in slices differs from the inline pass")
	}
}
