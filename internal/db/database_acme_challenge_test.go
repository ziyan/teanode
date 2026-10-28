package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// An answer put by one transaction is found by another, until it expires or is
// deleted, and a kind that does not match is not found.
func TestAcmeChallengePutFindDelete(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	now := time.Now()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.PutAcmeChallenge("token-one", "http-01", "token-one.thumbprint", now.Add(time.Minute), now); err != nil {
			t.Fatalf("PutAcmeChallenge: %s", err)
		}
		// Putting again under the same key replaces the answer.
		if err := tx.PutAcmeChallenge("token-one", "http-01", "token-one.other", now.Add(time.Minute), now); err != nil {
			t.Fatalf("PutAcmeChallenge again: %s", err)
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		keyAuthorization, err := tx.FindAcmeChallenge("token-one", "http-01", now)
		if err != nil || keyAuthorization != "token-one.other" {
			t.Errorf("FindAcmeChallenge: %q %v", keyAuthorization, err)
		}
		if _, err := tx.FindAcmeChallenge("token-one", "tls-alpn-01", now); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a different kind is not found, got %v", err)
		}
		if _, err := tx.FindAcmeChallenge("token-one", "http-01", now.Add(2*time.Minute)); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("an expired answer is not found, got %v", err)
		}
		if err := tx.DeleteAcmeChallenge("token-one"); err != nil {
			t.Fatalf("DeleteAcmeChallenge: %s", err)
		}
		if err := tx.DeleteAcmeChallenge("token-one"); err != nil {
			t.Errorf("deleting a missing answer is not an error: %s", err)
		}
		if _, err := tx.FindAcmeChallenge("token-one", "http-01", now); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a deleted answer is not found, got %v", err)
		}
	})
}

// Putting an answer removes the ones that expired, which an instance that
// stopped in the middle of an order never cleaned up.
func TestAcmeChallengePutRemovesExpired(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	now := time.Now()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.PutAcmeChallenge("mail.example.test", "tls-alpn-01", "stale", now.Add(-time.Minute), now.Add(-2*time.Minute)); err != nil {
			t.Fatalf("PutAcmeChallenge: %s", err)
		}
		if err := tx.PutAcmeChallenge("token-two", "http-01", "fresh", now.Add(time.Minute), now); err != nil {
			t.Fatalf("PutAcmeChallenge: %s", err)
		}
	})
	if count := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "acme_challenge"`); count != "1" {
		t.Errorf("%s answers are stored, want only the fresh one", count)
	}
}
