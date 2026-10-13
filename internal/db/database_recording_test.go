package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingCaptureHistoryAndReservedQuota(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "capture-owner")
	digest := sha256.Sum256([]byte("media"))
	var source *models.Recording
	var media *models.RecordingMedia
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		source, err = transaction.CreateRecording(&models.Recording{UserID: ownerId, ClientRecordingID: "capture-stable", RecordingOrigin: models.RecordingOriginCapture})
		if err != nil {
			test.Fatal(err)
		}
		source, err = transaction.UpdateRecordingCapture(ownerId, source.ID, 1, models.RecordingCapturePaused, 2000, []models.RecordingGap{{GapID: "pause-one", GapStartMS: 1000, GapEndMS: 2000, RecordingGapReason: models.RecordingGapPaused}})
		if err != nil {
			test.Fatal(err)
		}
		source, err = transaction.UpdateRecordingCapture(ownerId, source.ID, source.SourceGeneration, models.RecordingCaptureActive, 3000, []models.RecordingGap{{GapID: "pause-one", GapStartMS: 1000, GapEndMS: 3000, RecordingGapReason: models.RecordingGapPaused}})
		if err != nil {
			test.Fatal(err)
		}
	})
	err := database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.UpdateRecordingCapture(ownerId, source.ID, source.SourceGeneration, models.RecordingCaptureActive, 4000, nil)
		return err
	})
	if !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("an interruption disappeared from history", err)
	}
	err = database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.UpdateRecordingCapture(ownerId, source.ID, source.SourceGeneration+1, models.RecordingCaptureActive, 4000, source.CaptureGaps)
		return err
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("a generation from the future overwrote capture state", err)
	}
	stoppedFrom := source.SourceGeneration
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		source, err = transaction.UpdateRecordingCapture(ownerId, source.ID, source.SourceGeneration, models.RecordingCaptureStopped, 4000, source.CaptureGaps)
		if err != nil {
			test.Fatal(err)
		}
		media, err = transaction.CreateRecordingMedia(ownerId, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "chunk-one", OriginalFilename: "chunk.m4a", OriginalContentType: "audio/mp4", OriginalSizeBytes: 64, OriginalSHA256: hex.EncodeToString(digest[:]), MediaDurationMS: 1000}, 128, 100)
		if err != nil {
			test.Fatal(err)
		}
		retry, err := transaction.CreateRecordingMedia(ownerId, media, 128, 100)
		if err != nil || retry.ID != media.ID {
			test.Fatal("lost initialization response reserved bytes twice", err)
		}
	})
	err = database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.UpdateRecordingCapture(ownerId, source.ID, stoppedFrom, models.RecordingCaptureStopped, 4000, source.CaptureGaps)
		return err
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("an old generation overwrote the stopped capture", err)
	}
	err = database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.UpdateRecordingCapture(ownerId, source.ID, source.SourceGeneration, models.RecordingCaptureActive, 4000, source.CaptureGaps)
		return err
	})
	if !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("stopped capture resumed", err)
	}
	secondMedia := *media
	secondMedia.ClientMediaID = "chunk-two"
	err = database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingMedia(ownerId, &secondMedia, 128, 100)
		return err
	})
	if !errors.Is(err, db.ErrTooMuchAsked) {
		test.Fatal("account reservation exceeded its quota", err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(ownerId, source.ID); err != nil {
			test.Fatal(err)
		}
	})
	var otherSource *models.Recording
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		otherSource, err = transaction.CreateRecording(&models.Recording{UserID: ownerId, ClientRecordingID: "another-capture", RecordingOrigin: models.RecordingOriginCapture})
		if err != nil {
			test.Fatal(err)
		}
	})
	secondMedia.RecordingID = otherSource.ID
	err = database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingMedia(ownerId, &secondMedia, 128, 100)
		return err
	})
	if !errors.Is(err, db.ErrTooMuchAsked) {
		test.Fatal("pending deletion bypassed the account quota", err)
	}
	err = database.Transaction(func(transaction db.Transaction) error { return transaction.FinishRecordingCleanup(source.ID) })
	if !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("cleanup dropped undeleted blobs", err)
	}
}

func TestRecordingMigrationReversesAndReapplies(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	reversePaths, err := filepath.Glob("migrations/*_recording_*.reverse.sql")
	if err != nil {
		test.Fatal(err)
	}
	sort.Strings(reversePaths)
	for pathIndex := len(reversePaths) - 1; pathIndex >= 0; pathIndex-- {
		reverseSQL, err := os.ReadFile(reversePaths[pathIndex])
		if err != nil {
			test.Fatal(err)
		}
		dbtest.Exec(test, database, string(reverseSQL))
	}
	for _, reversePath := range reversePaths {
		forwardSQL, err := os.ReadFile(strings.TrimSuffix(reversePath, ".reverse.sql") + ".sql")
		if err != nil {
			test.Fatal(err)
		}
		dbtest.Exec(test, database, string(forwardSQL))
	}
	ownerId := dbtest.CreateUser(test, database, "reapplied-owner")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.CreateRecording(&models.Recording{UserID: ownerId, ClientRecordingID: "reapplied-capture", RecordingOrigin: models.RecordingOriginCapture}); err != nil {
			test.Fatal(err)
		}
	})
}

func TestAnImportRetriedAfterItsLengthWasReadIsTheSameMedia(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "import-owner")
	digest := sha256.Sum256([]byte("imported media"))
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		source, err := transaction.CreateRecording(&models.Recording{UserID: ownerId, ClientRecordingID: "import-stable", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		registration := models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "import-one", OriginalFilename: "talk.m4a", OriginalContentType: "audio/mp4", OriginalSizeBytes: 64, OriginalSHA256: hex.EncodeToString(digest[:])}
		media, err := transaction.CreateRecordingMedia(ownerId, &registration, 128, 1000)
		if err != nil {
			test.Fatal(err)
		}
		if isSet, err := transaction.SetRecordingMediaDurationIfUnknown(ownerId, media.ID, 1500); err != nil || !isSet {
			test.Fatal("the length read from the file was not stored", isSet, err)
		}
		retryRegistration := registration
		retry, err := transaction.CreateRecordingMedia(ownerId, &retryRegistration, 128, 1000)
		if err != nil || retry.ID != media.ID || retry.MediaDurationMS != 1500 {
			test.Fatal("a retried import was refused after its length was read", err)
		}
		conflictingRegistration := registration
		conflictingRegistration.MediaDurationMS = 900
		if _, err := transaction.CreateRecordingMedia(ownerId, &conflictingRegistration, 128, 1000); !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatal("a registration naming another length was accepted", err)
		}
	})
}

func TestRecordingSearchMatchesTitleAndSummaryLiterally(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "search-owner")
	otherId := dbtest.CreateUser(test, database, "search-other")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		titled, err := transaction.CreateRecording(&models.Recording{UserID: ownerId, ClientRecordingID: "search-titled", RecordingTitle: "Garden Budget Review", RecordingOrigin: models.RecordingOriginCapture})
		if err != nil {
			test.Fatal(err)
		}
		summarized, err := transaction.CreateRecording(&models.Recording{UserID: ownerId, ClientRecordingID: "search-summarized", RecordingOrigin: models.RecordingOriginCapture})
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.StoreGeneratedRecordingSummary(ownerId, summarized.ID, "Weekly sync", "They agreed the budget is 100% spent."); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateRecording(&models.Recording{UserID: otherId, ClientRecordingID: "search-foreign", RecordingTitle: "Budget of another account", RecordingOrigin: models.RecordingOriginCapture}); err != nil {
			test.Fatal(err)
		}
		found, err := transaction.ListRecordings(ownerId, "", "budget", 10)
		if err != nil || len(found) != 2 {
			test.Fatal("the search did not find the title and the summary of this account only", len(found), err)
		}
		found, err = transaction.ListRecordings(ownerId, "", "100%", 10)
		if err != nil || len(found) != 1 || found[0].ID != summarized.ID {
			test.Fatal("a percent sign was read as a wildcard", len(found), err)
		}
		found, err = transaction.ListRecordings(ownerId, "", "gar_en", 10)
		if err != nil || len(found) != 0 {
			test.Fatal("an underscore was read as a wildcard", len(found), err)
		}
		found, err = transaction.ListRecordings(ownerId, "", "", 10)
		if err != nil || len(found) != 2 || found[1].ID != titled.ID {
			test.Fatal("an empty search did not list everything, newest first", len(found), err)
		}
	})
}

// The search reads what the person reads: the accurate words where the second
// pass produced them, not the first pass's words they replaced.
func TestRecordingSearchFindsTheAccurateWordsOfTheLatestVersion(test *testing.T) {
	fixture := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{
			{LocalSpeakerID: "A", SegmentStartMS: 2000, SegmentEndMS: 2500, TranscriptText: "The sea bass quarterly was discussed."},
			{LocalSpeakerID: "A", SegmentStartMS: 2500, SegmentEndMS: 3000, TranscriptText: "Nothing else was said."},
		}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.Exec(test, fixture.database, "UPDATE recording_transcript_segment SET accurate_transcript_text = 'The Zebrafish quarterly was discussed.' WHERE transcript_text LIKE 'The sea bass%'")
	search := func(searchText string) int {
		var found []*models.Recording
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			var err error
			if found, err = transaction.ListRecordings(fixture.userId, "", searchText, 10); err != nil {
				test.Fatal(err)
			}
		})
		return len(found)
	}
	if search("zebrafish") != 1 {
		test.Fatal("a word only the accurate pass heard was not found")
	}
	if search("else was said") != 1 {
		test.Fatal("a passage the second pass left alone was not found by its first words")
	}
	if search("sea bass") != 0 {
		test.Fatal("words the accurate pass replaced still matched")
	}
}
