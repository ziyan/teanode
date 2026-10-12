package recording_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

func digestBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

type mediaFixture struct {
	database    db.Database
	files       storage.Storage
	store       *recording.Store
	userId      string
	otherUserId string
	source      *models.Recording
	media       *models.RecordingMedia
	content     []byte
}

func newMediaFixture(test *testing.T, sizeBytes int) *mediaFixture {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = files.Close() })
	fixture := &mediaFixture{database: database, files: files, store: recording.New(database, files.(storage.StreamFiles)), content: bytes.Repeat([]byte("r"), sizeBytes)}
	fixture.userId = dbtest.CreateUser(test, database, "owner")
	fixture.otherUserId = dbtest.CreateUser(test, database, "other-owner")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		fixture.source, err = transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "source-stable", RecordingTitle: "Imported audio", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		fixture.media, err = transaction.CreateRecordingMedia(fixture.userId, &models.RecordingMedia{RecordingID: fixture.source.ID, ClientMediaID: "media-stable", OriginalFilename: "meeting.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: int64(sizeBytes), OriginalSHA256: digestBytes(fixture.content), MediaDurationMS: 30000}, 1<<30, 2<<30)
		if err != nil {
			test.Fatal(err)
		}
	})
	return fixture
}

func (self *mediaFixture) upload(test *testing.T) {
	test.Helper()
	for partIndex := 0; partIndex < self.media.PartCount; partIndex++ {
		offsetBytes := int64(partIndex) * models.RecordingUploadPartBytes
		portion := self.content[offsetBytes : offsetBytes+self.media.PartSizeBytes(partIndex)]
		if _, err := self.store.PutPart(test.Context(), self.userId, self.media.ID, partIndex, digestBytes(portion), portion); err != nil {
			test.Fatal(err)
		}
	}
}

func TestRecordingDurableRetryIntegrityOwnershipAndPlayback(test *testing.T) {
	fixture := newMediaFixture(test, int(models.RecordingUploadPartBytes)+19)
	firstPart := fixture.content[:models.RecordingUploadPartBytes]
	firstReceipt, err := fixture.store.PutPart(test.Context(), fixture.userId, fixture.media.ID, 0, digestBytes(firstPart), firstPart)
	if err != nil {
		test.Fatal(err)
	}
	retryReceipt, err := fixture.store.PutPart(test.Context(), fixture.userId, fixture.media.ID, 0, digestBytes(firstPart), firstPart)
	if err != nil || !retryReceipt.StoredAt.Equal(firstReceipt.StoredAt) {
		test.Fatal("lost receipt retry did not preserve the receipt", err)
	}
	changedPart := bytes.Repeat([]byte("x"), len(firstPart))
	if _, err := fixture.store.PutPart(test.Context(), fixture.userId, fixture.media.ID, 0, digestBytes(changedPart), changedPart); !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("a conflicting retry was accepted", err)
	}
	if _, err := fixture.store.PutPart(test.Context(), fixture.otherUserId, fixture.media.ID, 0, digestBytes(firstPart), firstPart); !errors.Is(err, db.ErrNotFound) {
		test.Fatal("another account accessed the upload", err)
	}
	if _, _, err := fixture.store.Complete(test.Context(), fixture.userId, fixture.media.ID); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("partial media was published", err)
	}
	fixture.upload(test)
	var completedCount atomic.Int32
	var uploadGroup sync.WaitGroup
	uploadErrors := make(chan error, 4)
	for attemptIndex := 0; attemptIndex < 4; attemptIndex++ {
		uploadGroup.Go(func() {
			_, didComplete, err := fixture.store.Complete(test.Context(), fixture.userId, fixture.media.ID)
			uploadErrors <- err
			if didComplete {
				completedCount.Add(1)
			}
		})
	}
	uploadGroup.Wait()
	close(uploadErrors)
	for err := range uploadErrors {
		if err != nil {
			test.Fatal(err)
		}
	}
	if completedCount.Load() != 1 {
		test.Fatal("completion was published more than once")
	}
	var playback bytes.Buffer
	if err := fixture.store.CopyRange(test.Context(), fixture.userId, fixture.media.ID, &playback, models.RecordingUploadPartBytes-7, 20); err != nil {
		test.Fatal(err)
	}
	if !bytes.Equal(playback.Bytes(), fixture.content[models.RecordingUploadPartBytes-7:models.RecordingUploadPartBytes+13]) {
		test.Fatal("playback changed the original across parts")
	}
	if err := fixture.store.CopyRange(test.Context(), fixture.otherUserId, fixture.media.ID, io.Discard, 0, 1); !errors.Is(err, db.ErrNotFound) {
		test.Fatal("another account played the source", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		retrySource, err := transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "source-stable", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil || retrySource.ID != fixture.source.ID {
			test.Fatal("recording retry duplicated the source", err)
		}
		if err := transaction.DeleteRecording(fixture.userId, fixture.source.ID); err != nil {
			test.Fatal(err)
		}
	})
	if _, err := fixture.store.PutPart(test.Context(), fixture.userId, fixture.media.ID, 0, digestBytes(firstPart), firstPart); !errors.Is(err, db.ErrRecordingDeleted) {
		test.Fatal("a late upload recreated deleted media", err)
	}
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	for partIndex := 0; partIndex < fixture.media.PartCount; partIndex++ {
		if _, err := fixture.files.GetFile(test.Context(), db.RecordingPartFileID(fixture.media.ID, partIndex)); !errors.Is(err, storage.ErrNotFound) {
			test.Fatal("deleted media remained in storage", err)
		}
	}
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "source-stable", RecordingOrigin: models.RecordingOriginAudioImport})
		return err
	})
	if !errors.Is(err, db.ErrRecordingDeleted) {
		test.Fatal("an offline retry recreated the deleted source", err)
	}
}

type failingFiles struct {
	storage.StreamFiles
	shouldFailWrite  atomic.Bool
	shouldFailDelete atomic.Bool
}

func (self *failingFiles) PutFileStream(ctx context.Context, fileId string, content io.ReadSeeker, sizeBytes int64) error {
	if err := self.StreamFiles.PutFileStream(ctx, fileId, content, sizeBytes); err != nil {
		return err
	}
	if self.shouldFailWrite.Load() {
		return errors.New("fixture write receipt failed")
	}
	return nil
}
func (self *failingFiles) DeleteFileEverywhere(ctx context.Context, fileId string) error {
	if err := self.StreamFiles.DeleteFileEverywhere(ctx, fileId); err != nil {
		return err
	}
	if self.shouldFailDelete.Load() {
		return errors.New("fixture mirror deletion failed")
	}
	return nil
}

func TestRecordingUncommittedBytesRemainDiscoverableAndCleanupResumes(test *testing.T) {
	fixture := newMediaFixture(test, 128)
	files := &failingFiles{StreamFiles: fixture.files.(storage.StreamFiles)}
	files.shouldFailWrite.Store(true)
	store := recording.New(fixture.database, files)
	if _, err := store.PutPart(test.Context(), fixture.userId, fixture.media.ID, 0, digestBytes(fixture.content), fixture.content); err == nil {
		test.Fatal("failed storage produced a receipt")
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		parts, err := transaction.ListRecordingMediaParts(fixture.userId, fixture.media.ID)
		if err != nil || len(parts) != 0 {
			test.Fatal("a failed write published a part", err)
		}
		if err := transaction.DeleteRecording(fixture.userId, fixture.source.ID); err != nil {
			test.Fatal(err)
		}
	})
	files.shouldFailDelete.Store(true)
	if err := store.Cleanup(test.Context(), 10); err == nil {
		test.Fatal("mirror failure was forgotten")
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		mediaFiles, err := transaction.ListRecordingMediaForCleanup(fixture.source.ID)
		if err != nil || len(mediaFiles) != 1 || mediaFiles[0].CleanupNextPartIndex != 0 {
			test.Fatal("cleanup dropped the retry reference", err)
		}
	})
	files.shouldFailDelete.Store(false)
	if err := store.Cleanup(test.Context(), 1); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		mediaFiles, err := transaction.ListRecordingMediaForCleanup(fixture.source.ID)
		if err != nil || len(mediaFiles) != 1 || mediaFiles[0].CleanupNextPartIndex != 1 {
			test.Fatal("cleanup did not persist its progress", err)
		}
	})
	if err := store.Cleanup(test.Context(), 1); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		mediaFiles, err := transaction.ListRecordingMediaForCleanup(fixture.source.ID)
		if err != nil || len(mediaFiles) != 0 {
			test.Fatal("cleanup did not remove private metadata", err)
		}
	})
}

func TestRecordingCorruptionIsRefusedAndAccountDeletionKeepsCleanupReferences(test *testing.T) {
	fixture := newMediaFixture(test, 64)
	fixture.upload(test)
	if err := fixture.files.PutFile(test.Context(), db.RecordingPartFileID(fixture.media.ID, 0), bytes.Repeat([]byte("s"), 64)); err != nil {
		test.Fatal(err)
	}
	if _, _, err := fixture.store.Complete(test.Context(), fixture.userId, fixture.media.ID); err == nil {
		test.Fatal("stored corruption was published")
	}
	if _, err := fixture.store.PutPart(test.Context(), fixture.userId, fixture.media.ID, 0, digestBytes(fixture.content), fixture.content); err != nil {
		test.Fatal("same-digest retry could not repair corruption", err)
	}
	if _, didComplete, err := fixture.store.Complete(test.Context(), fixture.userId, fixture.media.ID); err != nil || !didComplete {
		test.Fatal("repaired media could not complete", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteUser(fixture.userId); err != nil {
			test.Fatal(err)
		}
	})
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if _, err := fixture.files.GetFile(test.Context(), db.RecordingPartFileID(fixture.media.ID, 0)); !errors.Is(err, storage.ErrNotFound) {
		test.Fatal("account deletion orphaned raw speech", err)
	}
}
