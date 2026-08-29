package ratelimit_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/util/ratelimit"
)

// A burst up to the capacity is allowed and the next attempt is not, which is
// the whole behaviour an authentication limit needs.
func TestABurstIsAllowedAndThenItIsNot(t *testing.T) {
	t.Parallel()
	registry := ratelimit.NewRegistry(1, 3, 1000, time.Minute)

	for attempt := 1; attempt <= 3; attempt++ {
		if !registry.Allow("10.0.0.1") {
			t.Fatalf("attempt %d was refused, but the capacity is 3", attempt)
		}
	}
	if registry.Allow("10.0.0.1") {
		t.Error("a fourth attempt was allowed, but the capacity is 3")
	}
}

// One caller exhausting their bucket must not affect anybody else, or the
// limit becomes a way to lock other people out.
func TestOneKeyDoesNotLimitAnother(t *testing.T) {
	t.Parallel()
	registry := ratelimit.NewRegistry(1, 2, 1000, time.Minute)

	for registry.Allow("10.0.0.1") { //nolint:revive // draining is the point
	}
	if registry.Allow("10.0.0.1") {
		t.Fatal("the drained key still allows attempts")
	}
	if !registry.Allow("10.0.0.2") {
		t.Error("a different key was refused because the first one was drained")
	}
}

// The registry is keyed by remote address, so it must not grow without bound.
func TestTheRegistryDoesNotGrowWithoutBound(t *testing.T) {
	t.Parallel()
	const limit = 100
	registry := ratelimit.NewRegistry(1, 5, limit, time.Minute)

	for index := 0; index < limit*20; index++ {
		registry.Allow(fmt.Sprintf("2001:db8::%x", index))
	}
	if held := registry.Len(); held > limit {
		t.Errorf("the registry holds %d keys, above its limit of %d", held, limit)
	}
}

// Past the limit a caller is still limited, because the bucket they are handed
// is fresh rather than absent. A fresh bucket allows a burst and no more.
func TestPastTheLimitCallersAreStillLimited(t *testing.T) {
	t.Parallel()
	registry := ratelimit.NewRegistry(1, 2, 1, time.Minute)

	// Fill the one slot with a key that keeps it.
	registry.Allow("10.0.0.1")

	// Everybody else gets an unheld bucket, which still refuses beyond its
	// capacity within the one call sequence it is used for.
	bucket := registry.For("10.0.0.2")
	// Each call spends a token, so these are three different questions even
	// though they read the same.
	for spent := 1; spent <= 2; spent++ {
		if !bucket.Allow() {
			t.Fatalf("the bucket handed out past the limit refused token %d of its capacity of 2", spent)
		}
	}
	if bucket.Allow() {
		t.Error("the bucket handed out past the limit allowed more than its capacity")
	}
}

// A bucket is only forgotten once it has refilled. Forgetting a drained one
// would clear the limit for whoever drained it, which is the failure mode the
// eviction rule exists to avoid.
func TestADrainedBucketIsNotForgotten(t *testing.T) {
	t.Parallel()
	// Idle of zero means every call sweeps, which is the harshest case.
	registry := ratelimit.NewRegistry(0.0001, 2, 1000, 0)

	for registry.Allow("10.0.0.1") { //nolint:revive // draining is the point
	}

	// Sweeping happens on the next call. Touch other keys so a sweep runs
	// several times over, then check the drained key is still refused.
	for index := 0; index < 10; index++ {
		registry.Allow(fmt.Sprintf("10.0.1.%d", index))
	}
	if registry.Allow("10.0.0.1") {
		t.Error("a drained bucket was forgotten, which clears the limit for whoever drained it")
	}
}

// A full bucket is indistinguishable from one that never existed, so dropping
// it costs nothing and is what keeps the map small.
func TestAFullBucketIsForgotten(t *testing.T) {
	t.Parallel()
	registry := ratelimit.NewRegistry(1000, 10, 1000, time.Millisecond)

	registry.Allow("10.0.0.1")
	if registry.Len() != 1 {
		t.Fatalf("expected the key to be held, got %d keys", registry.Len())
	}

	// Give it time to refill and to become idle, then provoke a sweep.
	time.Sleep(20 * time.Millisecond)
	registry.Allow("10.0.0.2")

	if registry.Len() > 1 {
		t.Errorf("the refilled key was kept; the registry holds %d keys", registry.Len())
	}
}

// The registry is reached from every connection handler at once.
func TestConcurrentUseIsSafe(t *testing.T) {
	t.Parallel()
	registry := ratelimit.NewRegistry(100, 10, 50, time.Millisecond)

	var waiter sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		waiter.Add(1)
		go func(worker int) {
			defer waiter.Done()
			for index := 0; index < 200; index++ {
				registry.Allow(fmt.Sprintf("10.0.%d.%d", worker, index%40))
			}
		}(worker)
	}
	waiter.Wait()
}
