package ceremony

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Ceremonies are parked by anybody who asks to sign in, so the store stops
// taking them at a size, and takes them again once there is room.
func TestTheMemoryStoreRefusesPastItsLimit(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	ctx := context.Background()

	var first string
	for count := 0; count < maximumParked; count++ {
		ceremonyId, err := store.Park(ctx, &Ceremony{Username: "someone"})
		if err != nil {
			t.Fatalf("ceremony %d was refused, but the limit is %d: %s", count+1, maximumParked, err)
		}
		if count == 0 {
			first = ceremonyId
		}
	}
	if _, err := store.Park(ctx, &Ceremony{}); !errors.Is(err, ErrTooManyCeremonies) {
		t.Fatalf("a ceremony past the limit was parked: %v", err)
	}

	if _, err := store.Take(ctx, first); err != nil {
		t.Fatalf("a parked ceremony could not be taken: %s", err)
	}
	if _, err := store.Park(ctx, &Ceremony{}); err != nil {
		t.Errorf("a ceremony was refused after room was made: %s", err)
	}
}

// Expired ceremonies make room by themselves.
func TestExpiredCeremoniesDoNotCountTowardsTheLimit(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore().(*memoryStore)
	for count := 0; count < maximumParked; count++ {
		store.parked[string(rune(count))] = parkedCeremony{expires: time.Now().Add(-time.Second)}
	}
	if _, err := store.Park(context.Background(), &Ceremony{}); err != nil {
		t.Errorf("a ceremony was refused while every parked one had expired: %s", err)
	}
}
