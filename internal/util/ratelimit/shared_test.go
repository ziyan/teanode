package ratelimit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/util/ratelimit"
)

// Two registries over one database, which is what two instances of the server
// are, spend one bucket between them.
func TestSharedRegistriesOverOneDatabaseSpendOneBucket(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	first := ratelimit.NewSharedRegistry("sign-in", 0.001, 4, ratelimit.Through(database.TransactionContext))
	second := ratelimit.NewSharedRegistry("sign-in", 0.001, 4, ratelimit.Through(database.TransactionContext))

	for attempt := 1; attempt <= 4; attempt++ {
		registry := first
		if attempt%2 == 0 {
			registry = second
		}
		if !registry.Allow("203.0.113.5") {
			t.Fatalf("attempt %d was refused, but the capacity is 4", attempt)
		}
	}
	if first.Allow("203.0.113.5") || second.Allow("203.0.113.5") {
		t.Error("an instance was allowed a fifth attempt after the two had spent four")
	}
	if first.For("203.0.113.5").Available() != 0 || second.For("203.0.113.5").Available() != 0 {
		t.Error("an instance counted tokens left that the other had spent")
	}
	if !first.Allow("203.0.113.6") {
		t.Error("another key was refused because the first was spent")
	}
}

// A limiter has its own buckets, even for the same key.
func TestSharedRegistriesWithDifferentNamesDoNotShare(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	signIn := ratelimit.NewSharedRegistry("sign-in", 0.001, 1, ratelimit.Through(database.TransactionContext))
	mail := ratelimit.NewSharedRegistry("mail", 0.001, 1, ratelimit.Through(database.TransactionContext))

	if !signIn.Allow("203.0.113.5") {
		t.Fatal("the first attempt was refused")
	}
	if !mail.Allow("203.0.113.5") {
		t.Error("the mail limiter was spent by the sign-in limiter")
	}
}

// Take spends up to what is asked and TakeAvailable says how many it got, as
// on a bucket kept in memory.
func TestSharedBucketTakesWhatIsThere(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	registry := ratelimit.NewSharedRegistry("mail", 0.001, 5, ratelimit.Through(database.TransactionContext))

	if got := registry.For("key").TakeAvailable(3); got != 3 {
		t.Errorf("took %d tokens, want 3", got)
	}
	registry.For("key").Take(10)
	if got := registry.For("key").Available(); got != 0 {
		t.Errorf("%d tokens left after taking more than there were, want 0", got)
	}
	if registry.For("key").Allow() {
		t.Error("a token was allowed from an empty bucket")
	}
}

// A key longer than the column holds is still one key.
func TestSharedBucketAcceptsAVeryLongKey(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	registry := ratelimit.NewSharedRegistry("password", 0.001, 1, ratelimit.Through(database.TransactionContext))
	key := strings.Repeat("a", 2000)

	if !registry.Allow(key) {
		t.Fatal("the first attempt was refused")
	}
	if registry.Allow(key) {
		t.Error("the second attempt was allowed, so the long key was not kept")
	}
}

// When the database cannot be reached the limit refuses, rather than letting
// everybody through.
func TestSharedRegistryRefusesWhenTheDatabaseFails(t *testing.T) {
	t.Parallel()
	registry := ratelimit.NewSharedRegistry("sign-in", 1, 10, func(context.Context, func(ratelimit.TokenStore) error) error {
		return errors.New("the database is down")
	})

	if registry.Allow("203.0.113.5") {
		t.Error("an attempt was allowed while the database was down")
	}
	if registry.For("203.0.113.5").Available() != 0 {
		t.Error("tokens were counted while the database was down")
	}
	if registry.For("203.0.113.5").TakeAvailable(1) != 0 {
		t.Error("a token was taken while the database was down")
	}
}
