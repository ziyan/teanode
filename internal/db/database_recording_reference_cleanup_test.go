package db_test

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

func TestRevokedRecordingReferenceCleanupRetainsRefusalAndOwnership(test *testing.T) {
	fixture := newTranscriptFixture(test)
	reference := createConfirmedReference(test, fixture)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		pending, err := transaction.ListRevokedRecordingVoiceReferencesForCleanup("", 1)
		if err != nil || len(pending) != 0 {
			test.Fatal("active reference queued for deletion", err)
		}
		source, err := transaction.GetRecording(fixture.userId, reference.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RevokeRecordingVoiceReference(fixture.userId, source.ID, reference.ID, source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
	})
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "cleanup-other-owner")
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.LockRevokedRecordingVoiceReferenceForCleanup(otherOwnerId, reference.RecordingID, reference.ID)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("other owner locked reference bytes", err)
	}
	storageFailure := errors.New("synthetic storage deletion failure")
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		locked, err := transaction.LockRevokedRecordingVoiceReferenceForCleanup(fixture.userId, reference.RecordingID, reference.ID)
		if err != nil || locked.RecordingBlobID != reference.RecordingBlobID {
			test.Fatal("revoked blob provenance lost", err)
		}
		return storageFailure
	})
	if !errors.Is(err, storageFailure) {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		pending, err := transaction.ListRevokedRecordingVoiceReferencesForCleanup("", 1)
		if err != nil || len(pending) != 1 || pending[0].UserID != fixture.userId || pending[0].Reference.ID != reference.ID || pending[0].Reference.RecordingBlobID != reference.RecordingBlobID {
			test.Fatal("failed cleanup was not recoverable", err, len(pending))
		}
		afterPage, err := transaction.ListRevokedRecordingVoiceReferencesForCleanup(reference.ID, 1)
		if err != nil || len(afterPage) != 0 {
			test.Fatal("cleanup ignored cursor", err)
		}
		if _, err := transaction.LockRevokedRecordingVoiceReferenceForCleanup(fixture.userId, reference.RecordingID, reference.ID); err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeleteRevokedRecordingVoiceReference(fixture.userId, reference.RecordingID, reference.ID); err != nil {
			test.Fatal(err)
		}
		retained, err := transaction.ListRecordingVoiceReferences(fixture.userId, reference.RecordingID)
		if err != nil || len(retained) != 1 || !retained[0].IsRevoked || retained[0].RecordingBlobID != "" {
			test.Fatal("cleanup erased explicit refusal history", err)
		}
		pending, err = transaction.ListRevokedRecordingVoiceReferencesForCleanup("", 100)
		if err != nil || len(pending) != 0 {
			test.Fatal("deleted bytes remained queued", err)
		}
		_, err = transaction.GetRecordingBlob(fixture.userId, reference.RecordingBlobID)
		if !errors.Is(err, db.ErrNotFound) {
			test.Fatal("cleanup retained storage reservation", err)
		}
		usable, err := transaction.ListUsableRecordingVoiceReferences(fixture.userId, 4)
		if err != nil || len(usable) != 0 {
			test.Fatal("revoked reference became usable", err)
		}
	})
}
