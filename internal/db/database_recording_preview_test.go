package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

type recordingPreviewFixture struct {
	database db.Database
	userId   string
	media    *models.RecordingMedia
}

func newRecordingPreviewFixture(test *testing.T) *recordingPreviewFixture {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	fixture := &recordingPreviewFixture{database: database, userId: dbtest.CreateUser(test, database, "preview-owner")}
	fixture.media = fixture.createMedia(test, "original-one", true)
	return fixture
}

func (self *recordingPreviewFixture) createMedia(test *testing.T, clientRecordingId string, shouldVerify bool) *models.RecordingMedia {
	test.Helper()
	digest := sha256.Sum256([]byte("original-audio"))
	digestText := hex.EncodeToString(digest[:])
	var media *models.RecordingMedia
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		recording, err := transaction.CreateRecording(&models.Recording{UserID: self.userId, ClientRecordingID: clientRecordingId, RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		media, err = transaction.CreateRecordingMedia(self.userId, &models.RecordingMedia{RecordingID: recording.ID, ClientMediaID: "original", OriginalFilename: "original.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: 14, OriginalSHA256: digestText}, 1024, 4096)
		if err != nil {
			test.Fatal(err)
		}
		if shouldVerify {
			if _, err := transaction.PutRecordingMediaPart(self.userId, &models.RecordingMediaPart{RecordingMediaID: media.ID, PartIndex: 0, PartSizeBytes: 14, PartSHA256: digestText}); err != nil {
				test.Fatal(err)
			}
			media, _, err = transaction.CompleteRecordingMedia(self.userId, media.ID, digestText)
			if err != nil {
				test.Fatal(err)
			}
		}
	})
	return media
}

func recordingPreviewBlob(media *models.RecordingMedia) *models.RecordingBlob {
	digest := sha256.Sum256([]byte("playback-wave"))
	return &models.RecordingBlob{ID: db.RecordingPreviewFileID(media.ID), RecordingID: media.RecordingID, RecordingMediaID: media.ID, RecordingBlobPurpose: models.RecordingBlobAudioPreview, BlobSizeBytes: 64, BlobSHA256: hex.EncodeToString(digest[:])}
}

func TestRecordingPreviewReservationRecoveryAndCleanup(test *testing.T) {
	fixture := newRecordingPreviewFixture(test)
	blob := recordingPreviewBlob(fixture.media)
	blob.IsStoredVerified = true
	var reserved *models.RecordingBlob
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		reserved, err = transaction.ReserveRecordingPreview(fixture.userId, fixture.media.ID, blob, 78)
		if err != nil || reserved.IsStoredVerified || reserved.CreatedAt.IsZero() {
			test.Fatal("reservation must precede byte verification", reserved, err)
		}
	})
	// A failed storage write leaves the committed reservation discoverable. A
	// retry consumes no additional quota, even if the quota was later reduced.
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		found, err := transaction.ReadRecordingPreview(fixture.userId, fixture.media.ID)
		if err != nil || found.ID != reserved.ID || found.IsStoredVerified {
			test.Fatal("lost pending preview reservation", found, err)
		}
		retry, err := transaction.ReserveRecordingPreview(fixture.userId, fixture.media.ID, blob, 1)
		if err != nil || retry.CreatedAt != reserved.CreatedAt || retry.IsStoredVerified {
			test.Fatal("retry changed a reserved derivative", retry, err)
		}
		if err := transaction.VerifyRecordingBlob(fixture.userId, reserved.ID, reserved.BlobSHA256); err != nil {
			test.Fatal(err)
		}
		verified, err := transaction.ReadRecordingPreview(fixture.userId, fixture.media.ID)
		if err != nil || !verified.IsStoredVerified {
			test.Fatal("preview was not verified", verified, err)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.ReadRecordingPreview(fixture.userId, fixture.media.ID); !errors.Is(err, db.ErrRecordingDeleted) {
			test.Fatal("deleted original still exposed preview", err)
		}
		pending, err := transaction.ListRecordingBlobsForCleanup(fixture.media.RecordingID, 100)
		if err != nil || len(pending) != 1 || pending[0].ID != reserved.ID {
			test.Fatal("cleanup omitted preview", pending, err)
		}
		if err := transaction.FinishRecordingCleanup(fixture.media.RecordingID); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatal("cleanup discarded retained blob", err)
		}
		if err := transaction.DeleteRecordingBlobReference(fixture.media.RecordingID, reserved.ID); err != nil {
			test.Fatal(err)
		}
		if err := transaction.AdvanceRecordingMediaCleanup(fixture.media.RecordingID, fixture.media.ID, 0); err != nil {
			test.Fatal(err)
		}
		if err := transaction.FinishRecordingCleanup(fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
	})
}

func TestRecordingPreviewRejectsForeignAndMismatchedParents(test *testing.T) {
	fixture := newRecordingPreviewFixture(test)
	foreignId := dbtest.CreateUser(test, fixture.database, "preview-other-owner")
	otherMedia := fixture.createMedia(test, "original-two", true)
	pendingMedia := fixture.createMedia(test, "original-pending", false)
	for _, operation := range []string{"read", "reserve"} {
		err := fixture.database.Transaction(func(transaction db.Transaction) error {
			if operation == "read" {
				_, err := transaction.ReadRecordingPreview(foreignId, fixture.media.ID)
				return err
			}
			_, err := transaction.ReserveRecordingPreview(foreignId, fixture.media.ID, recordingPreviewBlob(fixture.media), 4096)
			return err
		})
		if !errors.Is(err, db.ErrNotFound) {
			test.Fatal("foreign owner accessed preview", operation, err)
		}
	}
	for _, mismatch := range []string{"recording", "media", "file", "purpose", "pending", "digest", "size"} {
		blob := recordingPreviewBlob(fixture.media)
		mediaId := fixture.media.ID
		switch mismatch {
		case "recording":
			blob.RecordingID = otherMedia.RecordingID
		case "media":
			blob.RecordingMediaID = otherMedia.ID
		case "file":
			blob.ID = db.RecordingPreviewFileID(otherMedia.ID)
		case "purpose":
			blob.RecordingBlobPurpose = models.RecordingBlobSpeakerReference
		case "pending":
			blob = recordingPreviewBlob(pendingMedia)
			mediaId = pendingMedia.ID
		case "digest":
			blob.BlobSHA256 = "invalid"
		case "size":
			blob.BlobSizeBytes = 0
		}
		err := fixture.database.Transaction(func(transaction db.Transaction) error {
			_, err := transaction.ReserveRecordingPreview(fixture.userId, mediaId, blob, 4096)
			return err
		})
		if !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatal("invalid preview accepted", mismatch, err)
		}
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.ReadRecordingPreview(fixture.userId, pendingMedia.ID); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatal("pending original exposed preview", err)
		}
		if _, err := transaction.ReadRecordingPreview(fixture.userId, fixture.media.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatal("invalid reservation persisted", err)
		}
		if _, err := transaction.ReserveRecordingPreview(fixture.userId, fixture.media.ID, recordingPreviewBlob(fixture.media), 4096); err != nil {
			test.Fatal(err)
		}
	})
	// Independent foreign keys cannot substitute another original from the same
	// account. Validate the stored relationship on every preview read and retry.
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording_blob SET recording_media_id = '%s' WHERE id = '%s'", otherMedia.ID, db.RecordingPreviewFileID(fixture.media.ID)))
	for _, operation := range []string{"read", "reserve"} {
		err := fixture.database.Transaction(func(transaction db.Transaction) error {
			if operation == "read" {
				_, err := transaction.ReadRecordingPreview(fixture.userId, fixture.media.ID)
				return err
			}
			_, err := transaction.ReserveRecordingPreview(fixture.userId, fixture.media.ID, recordingPreviewBlob(fixture.media), 4096)
			return err
		})
		if !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatal("mismatched stored parent accepted", operation, err)
		}
	}
}

func TestRecordingPreviewConcurrentQuotaAndConflictingRetry(test *testing.T) {
	fixture := newRecordingPreviewFixture(test)
	otherMedia := fixture.createMedia(test, "quota-two", true)
	var group sync.WaitGroup
	start := make(chan struct{})
	reservationErrors := make(chan error, 2)
	for _, media := range []*models.RecordingMedia{fixture.media, otherMedia} {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			reservationErrors <- fixture.database.Transaction(func(transaction db.Transaction) error {
				_, err := transaction.ReserveRecordingPreview(fixture.userId, media.ID, recordingPreviewBlob(media), 100)
				return err
			})
		}()
	}
	close(start)
	group.Wait()
	close(reservationErrors)
	var successCount, refusedCount int
	for err := range reservationErrors {
		if err == nil {
			successCount++
		} else if errors.Is(err, db.ErrTooMuchAsked) {
			refusedCount++
		} else {
			test.Fatal("unexpected reservation failure", err)
		}
	}
	if successCount != 1 || refusedCount != 1 {
		test.Fatal("concurrent quota was not serialized", successCount, refusedCount)
	}
	var reservedMedia *models.RecordingMedia
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		for _, media := range []*models.RecordingMedia{fixture.media, otherMedia} {
			if _, err := transaction.ReadRecordingPreview(fixture.userId, media.ID); err == nil {
				reservedMedia = media
			}
		}
	})
	for _, shouldChangeHash := range []bool{false, true} {
		blob := recordingPreviewBlob(reservedMedia)
		if shouldChangeHash {
			digest := sha256.Sum256([]byte("different-playback"))
			blob.BlobSHA256 = hex.EncodeToString(digest[:])
		} else {
			blob.BlobSizeBytes++
		}
		err := fixture.database.Transaction(func(transaction db.Transaction) error {
			_, err := transaction.ReserveRecordingPreview(fixture.userId, reservedMedia.ID, blob, 4096)
			return err
		})
		if !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatal("conflicting retry replaced playback", err)
		}
	}
	// Deleted originals and failed-write previews continue consuming quota until
	// storage cleanup removes their references.
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(fixture.userId, reservedMedia.RecordingID); err != nil {
			test.Fatal(err)
		}
	})
	remainingMedia := fixture.media
	if remainingMedia.ID == reservedMedia.ID {
		remainingMedia = otherMedia
	}
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.ReserveRecordingPreview(fixture.userId, remainingMedia.ID, recordingPreviewBlob(remainingMedia), 100)
		return err
	})
	if !errors.Is(err, db.ErrTooMuchAsked) {
		test.Fatal("pending cleanup bypassed account quota", err)
	}
}

func TestRecordingPreviewConcurrentIdenticalRetryAndDeletion(test *testing.T) {
	fixture := newRecordingPreviewFixture(test)
	start := make(chan struct{})
	var group sync.WaitGroup
	reservationErrors := make(chan error, 2)
	reservedBlobs := make(chan *models.RecordingBlob, 2)
	for reservationIndex := 0; reservationIndex < 2; reservationIndex++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			reservationErrors <- fixture.database.Transaction(func(transaction db.Transaction) error {
				reserved, err := transaction.ReserveRecordingPreview(fixture.userId, fixture.media.ID, recordingPreviewBlob(fixture.media), 78)
				if err == nil {
					reservedBlobs <- reserved
				}
				return err
			})
		}()
	}
	close(start)
	group.Wait()
	close(reservationErrors)
	close(reservedBlobs)
	for err := range reservationErrors {
		if err != nil {
			test.Fatal("identical concurrent retry failed", err)
		}
	}
	var originalReservation *models.RecordingBlob
	for reserved := range reservedBlobs {
		if originalReservation == nil {
			originalReservation = reserved
		} else if reserved.CreatedAt != originalReservation.CreatedAt || reserved.ID != originalReservation.ID {
			test.Fatal("identical concurrent retry created another reservation")
		}
	}
	otherMedia := fixture.createMedia(test, "deletion-race", true)
	start = make(chan struct{})
	deletionErrors := make(chan error, 2)
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		deletionErrors <- fixture.database.Transaction(func(transaction db.Transaction) error {
			_, err := transaction.ReserveRecordingPreview(fixture.userId, otherMedia.ID, recordingPreviewBlob(otherMedia), 4096)
			return err
		})
	}()
	go func() {
		defer group.Done()
		<-start
		deletionErrors <- fixture.database.Transaction(func(transaction db.Transaction) error {
			return transaction.DeleteRecording(fixture.userId, otherMedia.RecordingID)
		})
	}()
	close(start)
	group.Wait()
	close(deletionErrors)
	for err := range deletionErrors {
		if err != nil && !errors.Is(err, db.ErrRecordingDeleted) {
			test.Fatal("reservation and deletion did not serialize", err)
		}
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.ReadRecordingPreview(fixture.userId, otherMedia.ID); !errors.Is(err, db.ErrRecordingDeleted) {
			test.Fatal("racing deletion left readable playback", err)
		}
		retainedBlobs, err := transaction.ListRecordingBlobsForCleanup(otherMedia.RecordingID, 100)
		if err != nil || len(retainedBlobs) > 1 {
			test.Fatal("reservation cleanup references were duplicated", err)
		}
		if len(retainedBlobs) == 1 && retainedBlobs[0].ID != db.RecordingPreviewFileID(otherMedia.ID) {
			test.Fatal("cleanup lost racing preview provenance")
		}
	})
}

func TestRecordingPreviewAccountDeletionRetriesUnverifiedStorage(test *testing.T) {
	fixture := newRecordingPreviewFixture(test)
	blob := recordingPreviewBlob(fixture.media)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.ReserveRecordingPreview(fixture.userId, fixture.media.ID, blob, 4096); err != nil {
			test.Fatal(err)
		}
	})
	files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = files.Close() }()
	originalId := db.RecordingPartFileID(fixture.media.ID, 0)
	for _, fileId := range []string{originalId, blob.ID} {
		if err := files.PutFile(test.Context(), fileId, []byte("incomplete fixture bytes")); err != nil {
			test.Fatal(err)
		}
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteUser(fixture.userId); err != nil {
			test.Fatal(err)
		}
	})
	failingFiles := &recordingCleanupFailure{StreamFiles: files.(storage.StreamFiles), shouldFailDelete: true}
	if err := recording.New(fixture.database, failingFiles).Cleanup(test.Context(), 10); err == nil {
		test.Fatal("storage failure discarded orphan preview")
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		retainedBlobs, err := transaction.ListRecordingBlobsForCleanup(fixture.media.RecordingID, 10)
		if err != nil || len(retainedBlobs) != 1 || retainedBlobs[0].ID != blob.ID || retainedBlobs[0].IsStoredVerified {
			test.Fatal("failed cleanup lost unverified reservation", retainedBlobs, err)
		}
	})
	if err := recording.New(fixture.database, files.(storage.StreamFiles)).Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	for _, fileId := range []string{originalId, blob.ID} {
		if _, err := files.GetFile(test.Context(), fileId); !errors.Is(err, storage.ErrNotFound) {
			test.Fatal("account deletion retained preview or original", err)
		}
	}
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM recording_blob") != "0" {
		test.Fatal("cleanup retained preview reservation")
	}
}
