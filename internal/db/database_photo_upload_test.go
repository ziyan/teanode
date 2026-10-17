package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func newTestUpload(test *testing.T, database db.Database, userId, clientPhotoId string) *models.PhotoUpload {
	test.Helper()
	var upload *models.PhotoUpload
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		upload, err = transaction.CreatePhotoUpload(&models.PhotoUpload{UserID: userId, PhotoUploadHeaders: models.PhotoUploadHeaders{ClientPhotoID: clientPhotoId, PhotoFileRole: models.PhotoFileRoleOriginal, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine}})
		if err != nil {
			test.Fatal(err)
		}
	})
	return upload
}

func TestPhotoUploadPartsAreAppendedAtTheOffsetTheCallerSaw(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-upload-owner")
	otherId := dbtest.CreateUser(test, database, "photo-upload-other")
	upload := newTestUpload(test, database, ownerId, "upload-client-id")
	if upload.PhotoUploadState != models.PhotoUploadReceiving || upload.PhotoUploadHeaders.ClientPhotoID != "upload-client-id" {
		test.Fatalf("a new upload is %+v", upload)
	}
	part := func(index int, size int64) *models.PhotoUploadPart {
		return &models.PhotoUploadPart{PhotoUploadID: upload.ID, PartIndex: index, PartSizeBytes: size, PartSHA256: photoDigest("part")}
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		after, err := transaction.AppendPhotoUploadPart(ownerId, part(0, 10), 0)
		if err != nil || after.ReceivedSizeBytes != 10 || after.PartCount != 1 {
			test.Fatalf("first part: %+v %v", after, err)
		}
		if _, err := transaction.AppendPhotoUploadPart(ownerId, part(1, 5), 0); !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatalf("a part at a stale offset was appended: %v", err)
		}
		if _, err := transaction.AppendPhotoUploadPart(ownerId, part(0, 5), 10); !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatalf("a part took the index of another: %v", err)
		}
		if _, err := transaction.AppendPhotoUploadPart(otherId, part(1, 5), 10); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody else appended: %v", err)
		}
		if _, err := transaction.AppendPhotoUploadPart(ownerId, part(1, models.PhotoUploadPartBytes+1), 10); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("a part over the limit was appended: %v", err)
		}
		after, err = transaction.AppendPhotoUploadPart(ownerId, part(1, 5), 10)
		if err != nil || after.ReceivedSizeBytes != 15 || after.PartCount != 2 {
			test.Fatalf("second part: %+v %v", after, err)
		}
		// The upload says how many parts it has, so the next index is not
		// found by listing them.
		if reread, err := transaction.GetPhotoUpload(ownerId, upload.ID); err != nil || reread.PartCount != 2 {
			test.Fatalf("the upload says %+v %v", reread, err)
		}
		parts, err := transaction.ListPhotoUploadParts(ownerId, upload.ID)
		if err != nil || len(parts) != 2 || parts[0].PartIndex != 0 || parts[1].PartIndex != 1 {
			test.Fatalf("%v %v", parts, err)
		}
		if _, err := transaction.GetPhotoUpload(otherId, upload.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody else read the upload: %v", err)
		}
	})
}

func TestPhotoUploadCompletesOnceAndFailedOnesAreNotAppendedTo(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-upload-complete-owner")
	photo := newTestPhoto(test, database, ownerId, "photo", nil)
	completed := newTestUpload(test, database, ownerId, "completed-client-id")
	failed := newTestUpload(test, database, ownerId, "failed-client-id")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		done, err := transaction.CompletePhotoUpload(ownerId, completed.ID, photo.ID)
		if err != nil || done.PhotoUploadState != models.PhotoUploadComplete || done.PhotoID == nil || *done.PhotoID != photo.ID {
			test.Fatalf("%+v %v", done, err)
		}
		again, err := transaction.CompletePhotoUpload(ownerId, completed.ID, photo.ID)
		if err != nil || again.PhotoUploadState != models.PhotoUploadComplete {
			test.Fatalf("completing twice failed: %v", err)
		}
		if _, err := transaction.AppendPhotoUploadPart(ownerId, &models.PhotoUploadPart{PhotoUploadID: completed.ID, PartIndex: 0, PartSizeBytes: 1, PartSHA256: photoDigest("x")}, 0); !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatalf("a part was appended to a complete upload: %v", err)
		}
		ended, err := transaction.FailPhotoUpload(ownerId, failed.ID)
		if err != nil || ended.PhotoUploadState != models.PhotoUploadFailed {
			test.Fatalf("%+v %v", ended, err)
		}
		if _, err := transaction.AppendPhotoUploadPart(ownerId, &models.PhotoUploadPart{PhotoUploadID: failed.ID, PartIndex: 0, PartSizeBytes: 1, PartSHA256: photoDigest("x")}, 0); !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatalf("a part was appended to a failed upload: %v", err)
		}
		unchanged, err := transaction.FailPhotoUpload(ownerId, completed.ID)
		if err != nil || unchanged.PhotoUploadState != models.PhotoUploadComplete {
			test.Fatalf("a complete upload was failed: %+v %v", unchanged, err)
		}
	})
}

func TestIdlePhotoUploadsAreFoundForCleanup(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-upload-idle-owner")
	idle := newTestUpload(test, database, ownerId, "idle-client-id")
	newTestUpload(test, database, ownerId, "moving-client-id")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.LockPhotoUploadForCleanup(time.Now().Add(-7 * 24 * time.Hour)); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("an upload that is moving was picked: %v", err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo_upload SET updated_at = now() - interval '8 days' WHERE id = '"+idle.ID+"'")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		picked, err := transaction.LockPhotoUploadForCleanup(time.Now().Add(-7 * 24 * time.Hour))
		if err != nil || picked.ID != idle.ID {
			test.Fatalf("%+v %v", picked, err)
		}
		if err := transaction.DeletePhotoUpload(picked.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.GetPhotoUpload(ownerId, idle.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("the upload is still there: %v", err)
		}
	})
}

func TestStartingAnUploadAgainEndsTheEarlierTryOfTheSameFile(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-upload-restart-owner")
	otherId := dbtest.CreateUser(test, database, "photo-upload-restart-other")
	first := newTestUpload(test, database, ownerId, "restarted")
	different := newTestUpload(test, database, ownerId, "a-different-file")
	foreign := newTestUpload(test, database, otherId, "restarted")
	var edited *models.PhotoUpload
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		// The same photo in another role is another file.
		edited, err = transaction.CreatePhotoUpload(&models.PhotoUpload{UserID: ownerId, PhotoUploadHeaders: models.PhotoUploadHeaders{ClientPhotoID: "restarted", PhotoFileRole: models.PhotoFileRoleEdited, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine}})
		if err != nil {
			test.Fatal(err)
		}
	})
	second := newTestUpload(test, database, ownerId, "restarted")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if ended, err := transaction.GetPhotoUpload(ownerId, first.ID); err != nil || ended.PhotoUploadState != models.PhotoUploadFailed {
			test.Fatalf("the earlier try is %+v %v", ended, err)
		}
		for _, kept := range []struct {
			name     string
			upload   *models.PhotoUpload
			ownerId  string
			wantOpen bool
		}{{"another file", different, ownerId, true}, {"another role", edited, ownerId, true}, {"another account", foreign, otherId, true}, {"the new try", second, ownerId, true}} {
			found, err := transaction.GetPhotoUpload(kept.ownerId, kept.upload.ID)
			if err != nil || found.PhotoUploadState != models.PhotoUploadReceiving {
				test.Fatalf("%s: %+v %v", kept.name, found, err)
			}
		}
	})
	// Restarting the same file over and over leaves one try open, so restarts
	// do not pile up towards the limit.
	for range 20 {
		newTestUpload(test, database, ownerId, "restarted")
	}
	if open := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_upload WHERE user_id = '"+ownerId+"' AND photo_upload_state = 'receiving' AND upload_headers->>'clientPhotoId' = 'restarted' AND upload_headers->>'photoFileRole' = 'original'"); open != "1" {
		test.Fatalf("%s tries of one file are open", open)
	}
}

func TestOpenUploadsCountTowardsWhatAnAccountHolds(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-upload-bytes-owner")
	otherId := dbtest.CreateUser(test, database, "photo-upload-bytes-other")
	first := newTestUpload(test, database, ownerId, "first")
	second := newTestUpload(test, database, ownerId, "second")
	foreign := newTestUpload(test, database, otherId, "foreign")
	ended := newTestUpload(test, database, ownerId, "ended")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		for _, entry := range []struct {
			userId string
			upload *models.PhotoUpload
			size   int64
		}{{ownerId, first, 100}, {ownerId, second, 50}, {otherId, foreign, 7}, {ownerId, ended, 9}} {
			if _, err := transaction.AppendPhotoUploadPart(entry.userId, &models.PhotoUploadPart{PhotoUploadID: entry.upload.ID, PartIndex: 0, PartSizeBytes: entry.size, PartSHA256: photoDigest("x")}, 0); err != nil {
				test.Fatal(err)
			}
		}
		if _, err := transaction.FailPhotoUpload(ownerId, ended.ID); err != nil {
			test.Fatal(err)
		}
		if openBytes, err := transaction.PhotoOpenUploadBytes(ownerId, "", ""); err != nil || openBytes != 150 {
			test.Fatalf("open bytes %d: %v", openBytes, err)
		}
		// The file about to be started again is not counted.
		if openBytes, err := transaction.PhotoOpenUploadBytes(ownerId, "first", models.PhotoFileRoleOriginal); err != nil || openBytes != 50 {
			test.Fatalf("open bytes without the first: %d %v", openBytes, err)
		}
		if openBytes, err := transaction.PhotoOpenUploadBytes(ownerId, "first", models.PhotoFileRoleEdited); err != nil || openBytes != 150 {
			test.Fatalf("open bytes of another role: %d %v", openBytes, err)
		}
	})
}
