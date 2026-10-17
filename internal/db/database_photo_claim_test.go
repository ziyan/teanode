package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// claimOnly claims the one photo that waits, as a worker does.
func claimOnly(test *testing.T, database db.Database) *models.Photo {
	test.Helper()
	var claimed []*models.Photo
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if claimed, err = transaction.ClaimPhotosForProcessing(10, time.Now().Add(-10*time.Minute), 5, false); err != nil {
			test.Fatal(err)
		}
	})
	if len(claimed) != 1 || claimed[0].ProcessingClaimedAt == nil {
		test.Fatalf("claimed %+v", claimed)
	}
	return claimed[0]
}

// The outcome of work on a photo is recorded only while the photo is still
// claimed at the time the work was claimed, so work that was overtaken cannot
// write over the newer work's outcome.
func TestAnOutcomeIsRecordedOnlyWhileTheClaimStands(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-claim-owner")
	waiting := newTestPhoto(test, database, ownerId, "claimed", nil)
	first := claimOnly(test, database)
	stateOf := func() models.PhotoProcessingState {
		var found *models.Photo
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			var err error
			if found, err = transaction.GetPhoto(ownerId, waiting.ID); err != nil {
				test.Fatal(err)
			}
		})
		return found.PhotoProcessingState
	}

	// The claim runs out, and a second instance takes the photo.
	dbtest.Exec(test, database, "UPDATE photo SET processing_claimed_at = now() - interval '11 minutes' WHERE id = '"+waiting.ID+"'")
	second := claimOnly(test, database)
	if second.ProcessingClaimedAt.Equal(*first.ProcessingClaimedAt) {
		test.Fatal("a second claim has the time of the first")
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if isCurrent, err := transaction.MarkPhotoReady(waiting.ID, *first.ProcessingClaimedAt); err != nil || isCurrent {
			test.Fatalf("the first claim marked the photo ready after it was lost: %v %v", isCurrent, err)
		}
		if isCurrent, err := transaction.MarkPhotoProcessingFailed(waiting.ID, *first.ProcessingClaimedAt, "The picture could not be read.", true, 5); err != nil || isCurrent {
			test.Fatalf("the first claim failed the photo after it was lost: %v %v", isCurrent, err)
		}
		if isCurrent, err := transaction.ReleasePhotoProcessingClaim(waiting.ID, *first.ProcessingClaimedAt); err != nil || isCurrent {
			test.Fatalf("the first claim was given back after it was lost: %v %v", isCurrent, err)
		}
		if isCurrent, err := transaction.RenewPhotoProcessingClaim(waiting.ID, *first.ProcessingClaimedAt, first.ProcessingClaimedAt.Add(time.Minute)); err != nil || isCurrent {
			test.Fatalf("the first claim was renewed after it was lost: %v %v", isCurrent, err)
		}
	})
	if state := stateOf(); state != models.PhotoProcessingWaiting {
		test.Fatalf("a lost claim changed the photo to %s", state)
	}

	// A claim that is renewed is known by the time it was renewed at.
	renewedAt := second.ProcessingClaimedAt.Add(time.Minute)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if isCurrent, err := transaction.RenewPhotoProcessingClaim(waiting.ID, *second.ProcessingClaimedAt, renewedAt); err != nil || !isCurrent {
			test.Fatalf("%v %v", isCurrent, err)
		}
		if isCurrent, err := transaction.MarkPhotoReady(waiting.ID, *second.ProcessingClaimedAt); err != nil || isCurrent {
			test.Fatalf("the time before the renewal still counts: %v %v", isCurrent, err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if isCurrent, err := transaction.MarkPhotoReady(waiting.ID, renewedAt); err != nil || !isCurrent {
			test.Fatalf("the renewed claim: %v %v", isCurrent, err)
		}
	})
	if state := stateOf(); state != models.PhotoProcessingReady {
		test.Fatalf("the photo is %s", state)
	}
}

// A worker that is stopped gives the attempt back and clears its claim, so
// the photo is taken at once and not after the claim has run out.
func TestAStoppedWorkerGivesBackTheAttempt(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-release-owner")
	newTestPhoto(test, database, ownerId, "released", nil)
	claimed := claimOnly(test, database)
	if claimed.ProcessingAttemptCount != 1 {
		test.Fatalf("attempts %d", claimed.ProcessingAttemptCount)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if isCurrent, err := transaction.ReleasePhotoProcessingClaim(claimed.ID, *claimed.ProcessingClaimedAt); err != nil || !isCurrent {
			test.Fatalf("%v %v", isCurrent, err)
		}
	})
	again := claimOnly(test, database)
	if again.ProcessingAttemptCount != 1 {
		test.Fatalf("the attempt was not given back: %d", again.ProcessingAttemptCount)
	}
}

// Failures that are not the file's make the photo wait longer each time they
// follow one another, and a photo that is made ready or asked for again starts
// from the shortest wait.
func TestFailuresThatAreNotTheFilesWaitLongerEachTime(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-outage-owner")
	waiting := newTestPhoto(test, database, ownerId, "outage", nil)
	for _, wantWait := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour} {
		dbtest.Exec(test, database, "UPDATE photo SET processing_not_before = NULL WHERE id = '"+waiting.ID+"'")
		claimed := claimOnly(test, database)
		if claimed.ProcessingAttemptCount != 1 {
			test.Fatalf("an outage used up attempts: %d", claimed.ProcessingAttemptCount)
		}
		dbtest.MarkPhotoProcessingFailed(test, database, waiting.ID, "The previews could not be made yet; they will be tried again.", false, 5)
		var found *models.Photo
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			var err error
			if found, err = transaction.GetPhoto(ownerId, waiting.ID); err != nil {
				test.Fatal(err)
			}
		})
		if found.ProcessingNotBefore == nil || time.Until(*found.ProcessingNotBefore) > wantWait+10*time.Second || time.Until(*found.ProcessingNotBefore) < wantWait-10*time.Second {
			test.Fatalf("waits %v, want about %v", found.ProcessingNotBefore, wantWait)
		}
	}
	dbtest.Exec(test, database, "UPDATE photo SET processing_not_before = NULL WHERE id = '"+waiting.ID+"'")
	claimOnly(test, database)
	dbtest.MarkPhotoReady(test, database, waiting.ID)
	if outages := dbtest.QueryString(test, database, "SELECT processing_outage_count FROM photo WHERE id = '"+waiting.ID+"'"); outages != "0" {
		test.Fatalf("a ready photo remembers %s outages", outages)
	}
}

// While a program that makes videos is missing, no video is claimed, and the
// photos behind it are.
func TestVideosAreNotClaimedWhileTheirProgramIsMissing(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-video-claim-owner")
	video := newTestPhoto(test, database, ownerId, "video", nil)
	picture := newTestPhoto(test, database, ownerId, "picture", nil)
	dbtest.Exec(test, database, "UPDATE photo SET photo_kind = 'video' WHERE id = '"+video.ID+"'")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		claimed, err := transaction.ClaimPhotosForProcessing(10, time.Now().Add(-10*time.Minute), 5, true)
		if err != nil || len(claimed) != 1 || claimed[0].ID != picture.ID {
			test.Fatalf("claimed %+v %v", claimed, err)
		}
		claimed, err = transaction.ClaimPhotosForProcessing(10, time.Now().Add(-10*time.Minute), 5, false)
		if err != nil || len(claimed) != 1 || claimed[0].ID != video.ID {
			test.Fatalf("claimed %+v %v", claimed, err)
		}
	})
}

// Finding the blobs nothing names is done from a column a trigger keeps, so it
// follows every way a file can go: replaced, its photo deleted, its account
// deleted. A blob that is passed over is not picked again by the same pass.
func TestBlobsNothingNamesAreFoundByWhenTheyLostTheirLastFile(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-unreferenced-owner")
	accountId := dbtest.CreateUser(test, database, "photo-unreferenced-account")
	kept := newTestPhoto(test, database, ownerId, "kept", nil)
	replaced := newTestPhoto(test, database, ownerId, "replaced", nil)
	shared := newTestPhoto(test, database, ownerId, "shared-one", nil)
	newTestPhoto(test, database, accountId, "of-an-account", nil)
	sharedDigest := photoDigest("blob of shared-one")
	unreferencedOf := func(label string) string {
		return dbtest.QueryString(test, database, "SELECT COALESCE(unreferenced_since::text, 'none') FROM photo_blob WHERE blob_sha256 = '"+photoDigest("blob of "+label)+"'")
	}
	for _, label := range []string{"kept", "replaced", "shared-one", "of-an-account"} {
		if unreferenced := unreferencedOf(label); unreferenced != "none" {
			test.Fatalf("a blob a file names is unreferenced since %s", unreferenced)
		}
	}

	// A second file naming a blob keeps it named when the first goes.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.SetPhotoFile(ownerId, kept.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleEdited, BlobSHA256: sharedDigest, FileSizeBytes: 100}, 0); err != nil {
			test.Fatal(err)
		}
		replacementDigest := photoDigest("replacement")
		if _, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: replacementDigest, BlobSizeBytes: 5}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.SetPhotoFile(ownerId, replaced.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleOriginal, BlobSHA256: replacementDigest, FileSizeBytes: 5}, 0); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.TrashPhotos(ownerId, []string{shared.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.MarkPhotosForDeletion(ownerId, []string{shared.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.DeleteMarkedPhotos([]string{shared.ID}); err != nil {
			test.Fatal(err)
		}
	})
	if unreferenced := unreferencedOf("shared-one"); unreferenced != "none" {
		test.Fatalf("a blob another file names is unreferenced since %s", unreferenced)
	}
	if unreferenced := unreferencedOf("replaced"); unreferenced == "none" {
		test.Fatal("the blob of a file that was replaced is not unreferenced")
	}
	// The photo is gone with its account.
	dbtest.Exec(test, database, `DELETE FROM "user" WHERE id = '`+accountId+`'`)
	if unreferenced := unreferencedOf("of-an-account"); unreferenced == "none" {
		test.Fatal("the blob of a deleted account is not unreferenced")
	}

	dbtest.Exec(test, database, "UPDATE photo_blob SET unreferenced_since = now() - interval '2 hours' WHERE unreferenced_since IS NOT NULL")
	picked := map[string]bool{}
	var failed []string
	for range 3 {
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			marked, err := transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-time.Hour), failed)
			if errors.Is(err, db.ErrNotFound) {
				return
			}
			if err != nil {
				test.Fatal(err)
			}
			picked[marked.BlobSHA256] = true
			// This one could not be deleted, so the next pass looks past it.
			failed = append(failed, marked.BlobSHA256)
		})
	}
	if len(picked) != 2 || !picked[photoDigest("blob of replaced")] || !picked[photoDigest("blob of of-an-account")] {
		test.Fatalf("picked %v, want the two blobs nothing names, each once", picked)
	}
}

// Another name an uploader has for a photo finds it, with that name; and the
// photo whose original has the bytes is found without any name.
func TestAPhotoIsFoundByEveryNameItWasRecordedUnder(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-alias-owner")
	otherId := dbtest.CreateUser(test, database, "photo-alias-other")
	original := newTestPhoto(test, database, ownerId, "first-name", nil)
	digest := photoDigest("blob of first-name")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		found, err := transaction.FindPhotoByOriginalBlob(ownerId, digest)
		if err != nil || found.ID != original.ID {
			test.Fatalf("%+v %v", found, err)
		}
		if _, err := transaction.FindPhotoByOriginalBlob(otherId, digest); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody else's photo was found by its bytes: %v", err)
		}
		if _, err := transaction.FindPhotoByOriginalBlob(ownerId, photoDigest("nothing")); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("%v", err)
		}
		if err := transaction.AddPhotoClientAlias(ownerId, original.ID, "second-name"); err != nil {
			test.Fatal(err)
		}
		// Naming it again, or naming it differently, changes nothing.
		if err := transaction.AddPhotoClientAlias(ownerId, original.ID, "second-name"); err != nil {
			test.Fatal(err)
		}
		if err := transaction.AddPhotoClientAlias(otherId, original.ID, "third-name"); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody else's photo was given a name: %v", err)
		}
		photos, err := transaction.FindPhotosByClientIDs(ownerId, []string{"first-name", "second-name", "unknown"})
		if err != nil || len(photos) != 2 {
			test.Fatalf("%+v %v", photos, err)
		}
		names := map[string]string{}
		for _, found := range photos {
			names[found.ClientPhotoID] = found.ID
		}
		if names["first-name"] != original.ID || names["second-name"] != original.ID {
			test.Fatalf("names %v", names)
		}
		if photos, err := transaction.FindPhotosByClientIDs(otherId, []string{"second-name"}); err != nil || len(photos) != 0 {
			test.Fatalf("somebody else found it by a name: %+v %v", photos, err)
		}
	})
	// A photo in the trash is found by its bytes; one on its way to deletion
	// is not.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.TrashPhotos(ownerId, []string{original.ID}); err != nil {
			test.Fatal(err)
		}
		if found, err := transaction.FindPhotoByOriginalBlob(ownerId, digest); err != nil || found.ID != original.ID {
			test.Fatalf("a photo in the trash: %+v %v", found, err)
		}
		if _, err := transaction.MarkPhotosForDeletion(ownerId, []string{original.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.FindPhotoByOriginalBlob(ownerId, digest); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a photo on its way to deletion was found: %v", err)
		}
		if photos, err := transaction.FindPhotosByClientIDs(ownerId, []string{"second-name"}); err != nil || len(photos) != 0 {
			test.Fatalf("a name of a photo on its way to deletion: %+v %v", photos, err)
		}
	})
}

// Uploads that have sat idle for a day do not hold the account's room.
func TestIdleUploadsDoNotCountTowardsTheRoomAnAccountHas(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-idle-owner")
	busy := newTestUpload(test, database, ownerId, "busy")
	idle := newTestUpload(test, database, ownerId, "idle")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		for _, entry := range []struct {
			upload *models.PhotoUpload
			size   int64
		}{{busy, 100}, {idle, 40}} {
			if _, err := transaction.AppendPhotoUploadPart(ownerId, &models.PhotoUploadPart{PhotoUploadID: entry.upload.ID, PartIndex: 0, PartSizeBytes: entry.size, PartSHA256: photoDigest("x")}, 0); err != nil {
				test.Fatal(err)
			}
		}
	})
	dbtest.Exec(test, database, "UPDATE photo_upload SET updated_at = now() - interval '2 days' WHERE id = '"+idle.ID+"'")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if openBytes, err := transaction.PhotoOpenUploadBytes(ownerId, "", "", time.Now().Add(-24*time.Hour)); err != nil || openBytes != 100 {
			test.Fatalf("open bytes %d: %v", openBytes, err)
		}
		if openBytes, err := transaction.PhotoOpenUploadBytes(ownerId, "", "", time.Now().Add(-7*24*time.Hour)); err != nil || openBytes != 140 {
			test.Fatalf("open bytes over a week %d: %v", openBytes, err)
		}
	})
}
