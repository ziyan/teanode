package web_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/web"
)

// Two instances of the server sharing one database share the sign-in limit:
// the tries are spent from one bucket, so running more instances does not give
// a guesser more of them.
func TestInstancesShareTheSignInLimit(t *testing.T) {
	t.Parallel()
	credentials := newMemoryStore(&models.User{Username: "robin", PasswordHash: testPasswordHash})
	first, err := web.NewAuthenticator(newStore(t), credentials)
	if err != nil {
		t.Fatalf("failed to build the first authenticator: %s", err)
	}
	second, err := web.NewAuthenticator(newStore(t), credentials)
	if err != nil {
		t.Fatalf("failed to build the second authenticator: %s", err)
	}

	// One wrong try on each instance is counted in the bucket they share.
	for name, authenticator := range map[string]web.Authenticator{"first": first, "second": second} {
		if err := attemptLogin(t, authenticator, "203.0.113.20", "wrong"); !errors.Is(err, web.ErrInvalidCredentials) {
			t.Fatalf("the %s instance returned %v, want invalid credentials", name, err)
		}
	}
	if err := credentials.TransactionContext(context.Background(), func(tx db.Transaction) error {
		left, err := tx.CountRateLimitTokens("sign-in:203.0.113.20", 10.0/60, 20, time.Now())
		// Two taken from twenty, give or take what refilled while the two
		// passwords were checked, which is slow under the race detector.
		if err == nil && left > 19 {
			t.Errorf("%f tries left after one on each instance, want about 18", left)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The rest spent, as by tries through any instance, neither instance lets
	// another try through. A refusal comes before any password is checked, so
	// nothing refills meanwhile.
	if err := credentials.TransactionContext(context.Background(), func(tx db.Transaction) error {
		for {
			isTaken, err := tx.TakeRateLimitToken("sign-in:203.0.113.20", 10.0/60, 20, time.Now())
			if err != nil || !isTaken {
				return err
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	for name, authenticator := range map[string]web.Authenticator{"first": first, "second": second} {
		if err := attemptLogin(t, authenticator, "203.0.113.20", "wrong"); !errors.Is(err, web.ErrTooManyAttempts) {
			t.Errorf("the %s instance returned %v after the shared tries were spent, want too many attempts", name, err)
		}
	}
	if err := attemptLogin(t, second, "203.0.113.21", "wrong"); errors.Is(err, web.ErrTooManyAttempts) {
		t.Error("another address was refused because the first one was guessing")
	}
}

// Two people reaching a fresh server at the same moment, on different
// instances, do not both become the administrator.
func TestOnlyOneOfTwoFirstClaimsWins(t *testing.T) {
	t.Parallel()
	credentials := newMemoryStore()
	claimFirstUserTogether(t, credentials, credentials)
	if count, _ := credentials.CountUsers(); count != 1 {
		t.Errorf("%d accounts were created, want 1", count)
	}
}

func TestOnlyOneOfTwoFirstClaimsWinsInTheDatabase(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	claimFirstUserTogether(t, database, database)
	if count, err := database.CountUsers(); err != nil || count != 1 {
		t.Errorf("%d accounts were created (%v), want 1", count, err)
	}
}

// claimFirstUserTogether has several claims run at once on authenticators of
// two instances over the stores, and checks that one succeeded and the rest
// were told the server is already claimed.
func claimFirstUserTogether(t *testing.T, firstStore, secondStore web.CredentialStore) {
	t.Helper()
	first, err := web.NewAuthenticator(newStore(t), firstStore)
	if err != nil {
		t.Fatalf("failed to build the first authenticator: %s", err)
	}
	second, err := web.NewAuthenticator(newStore(t), secondStore)
	if err != nil {
		t.Fatalf("failed to build the second authenticator: %s", err)
	}

	const claimCount = 6
	var wonCount, refusedCount, otherCount atomic.Int32
	var group sync.WaitGroup
	start := make(chan struct{})
	for claim := range claimCount {
		authenticator := first
		if claim%2 == 1 {
			authenticator = second
		}
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			switch err := authenticator.CreateFirstUser(context.Background(), "owner", "a-proper-long-password"); {
			case err == nil:
				wonCount.Add(1)
			case errors.Is(err, web.ErrAccountExists):
				refusedCount.Add(1)
			default:
				t.Errorf("a claim returned %v", err)
				otherCount.Add(1)
			}
		}()
	}
	close(start)
	group.Wait()
	if wonCount.Load() != 1 || refusedCount.Load() != claimCount-1 {
		t.Errorf("%d claims won and %d were refused, want 1 and %d", wonCount.Load(), refusedCount.Load(), claimCount-1)
	}
}
