package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A piece of a capture still being recorded is partial text. The memory sync
// and the naming job read the whole transcript, so they wait for the stop and
// then run once, however many pieces came before.
func TestACaptureIsSyncedAndNamedOnceWhenItStops(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 500, EndMS: 3500, Text: "A statement made during the capture."}}
	}
	fixture.transcribe(fixture.addPiece(5000, 0))
	fixture.transcribe(fixture.addPiece(5000, 0))
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM agent_document") != "0" {
		test.Fatal("pieces of a running capture were synced to memory")
	}
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM agent_job WHERE kind = 'recording_brief'") != "0" {
		test.Fatal("a running capture was queued for naming")
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		stopped, err := transaction.UpdateRecordingCapture(fixture.userId, source.ID, source.SourceGeneration, models.RecordingCaptureStopped, 10000, nil)
		if err != nil {
			test.Fatal(err)
		}
		if stopped.RecordingCaptureState != models.RecordingCaptureStopped {
			test.Fatal("the capture did not stop")
		}
		if err := fixture.worker.FinishRecordingCapture(transaction, fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
		// A second stop report must not queue a second job.
		if err := fixture.worker.FinishRecordingCapture(transaction, fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
	})
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM agent_job WHERE kind = 'recording_sync' AND status = 'queued'") != "1" {
		test.Fatal("stopping did not queue exactly one memory sync")
	}
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM agent_document") != "0" {
		test.Fatal("stopping synced the pieces inside the request")
	}
	if err := fixture.worker.SyncRecordingKnowledge(test.Context(), fixture.userId, fixture.source.ID); err != nil {
		test.Fatal(err)
	}
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM agent_document") != "2" {
		test.Fatal("stopping did not sync the pieces to memory")
	}
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM agent_job WHERE kind = 'recording_brief'") != "1" {
		test.Fatal("stopping did not queue exactly one naming job")
	}
}

// The last piece is uploaded after the stop, so its own completion does what
// stopping could not.
func TestTheLastPieceUploadedAfterTheStopSyncsAndNames(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 500, EndMS: 3500, Text: "A statement made during the capture."}}
	}
	fixture.transcribe(fixture.addPiece(5000, 0))
	finalPiece := fixture.addPiece(5000, 0)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.UpdateRecordingCapture(fixture.userId, source.ID, source.SourceGeneration, models.RecordingCaptureStopped, 10000, nil); err != nil {
			test.Fatal(err)
		}
		if err := fixture.worker.FinishRecordingCapture(transaction, fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
	})
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM agent_job WHERE kind = 'recording_brief'") != "0" {
		test.Fatal("naming was queued while a piece was still waiting for its transcript")
	}
	fixture.transcribe(finalPiece)
	if err := fixture.worker.SyncRecordingKnowledge(test.Context(), fixture.userId, fixture.source.ID); err != nil {
		test.Fatal(err)
	}
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM agent_job WHERE kind = 'recording_brief'") != "1" || dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM agent_document") != "2" {
		test.Fatal("the last piece did not sync and name the stopped capture")
	}
}
