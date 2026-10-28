package autoacme

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// databaseChallengeStore is how a server gives its instances one place to keep
// the answers to challenges: the acme_challenge table.
type databaseChallengeStore struct {
	database db.Database
}

func (self *databaseChallengeStore) Put(ctx context.Context, challengeKind, challengeKey, keyAuthorization string, expiresAt time.Time) error {
	return self.database.Transaction(func(tx db.Transaction) error {
		return tx.PutAcmeChallenge(challengeKey, challengeKind, keyAuthorization, expiresAt, time.Now())
	})
}

func (self *databaseChallengeStore) Delete(ctx context.Context, challengeKey string) error {
	return self.database.Transaction(func(tx db.Transaction) error {
		return tx.DeleteAcmeChallenge(challengeKey)
	})
}

func (self *databaseChallengeStore) Find(ctx context.Context, challengeKind, challengeKey string) (string, bool, error) {
	var keyAuthorization string
	err := self.database.Transaction(func(tx db.Transaction) error {
		var err error
		keyAuthorization, err = tx.FindAcmeChallenge(challengeKey, challengeKind, time.Now())
		return err
	})
	if errors.Is(err, db.ErrNotFound) {
		return "", false, nil
	}
	return keyAuthorization, err == nil, err
}

// With the table as the store, a challenge presented by one instance is
// answered by another, and the order lock is held by one instance at a time.
func TestChallengesAndOrderLockOverTheDatabase(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()

	build := func() *manager {
		manager, err := newManager(&Settings{
			Challenge:      "http-01",
			ACMEEmail:      "you@example.com",
			ChallengeStore: &databaseChallengeStore{database: database},
			Certificates:   []CertificateRequest{{Hosts: []string{"mail.example.com"}}},
		})
		if err != nil {
			t.Fatalf("failed to build an instance: %s", err)
		}
		return manager
	}
	ordering, answering := build(), build()

	challenges := challengesFor("mail.example.com")
	if err := ordering.httpSolver.Present(t.Context(), &fakeACMEClient{}, challenges); err != nil {
		t.Fatalf("failed to present: %s", err)
	}
	recorder := httptest.NewRecorder()
	answering.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, challengePath+"token-mail.example.com", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "response-for-token-mail.example.com" {
		t.Errorf("the other instance answered %d %q, want the response", recorder.Code, recorder.Body.String())
	}
	if err := ordering.httpSolver.CleanUp(t.Context(), challenges); err != nil {
		t.Fatalf("failed to clean up: %s", err)
	}
	recorder = httptest.NewRecorder()
	answering.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, challengePath+"token-mail.example.com", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("after cleanup the other instance answered %d, want 404", recorder.Code)
	}

	// The lock the settings are given: held by one, refused to the other.
	whileOrdering := func(ctx context.Context, work func(context.Context) error) (bool, error) {
		return database.WhileLocked(ctx, db.LockKey("acme:order"), work)
	}
	var isOtherRun bool
	didRun, err := whileOrdering(t.Context(), func(ctx context.Context) error {
		var err error
		isOtherRun, err = whileOrdering(ctx, func(context.Context) error { return nil })
		return err
	})
	if err != nil || !didRun || isOtherRun {
		t.Errorf("the lock: first ran %v, second ran %v, error %v; want the first only", didRun, isOtherRun, err)
	}
}
