package storage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type countingObjectStore struct {
	Storage
	sweepCount atomic.Int32
}

func (self *countingObjectStore) Sweep(context.Context, time.Time, func(context.Context, string) (bool, error)) (int, error) {
	self.sweepCount.Add(1)
	return 0, nil
}

// Every instance lists the whole bucket when it sweeps, so the instances take
// turns: one sweeps the object store in each interval and the others skip it.
func TestTheObjectStoreIsSweptOnlyWhenThisInstanceHoldsTheTurn(test *testing.T) {
	for _, directory := range []string{"", test.TempDir()} {
		objectStore := &countingObjectStore{}
		isMyTurn := false
		var askedInterval time.Duration
		instance := &filesystem{
			settings: &Settings{
				Directory: directory,
				Retention: time.Hour,
				TakeMirrorSweep: func(_ context.Context, interval time.Duration) (bool, error) {
					askedInterval = interval
					return isMyTurn, nil
				},
			},
			mirror: objectStore,
		}

		if err := instance.sweepOnce(context.Background()); err != nil {
			test.Fatal(err)
		}
		if objectStore.sweepCount.Load() != 0 {
			test.Errorf("directory %q: the object store was swept without this instance's turn", directory)
		}

		isMyTurn = true
		if err := instance.sweepOnce(context.Background()); err != nil {
			test.Fatal(err)
		}
		if objectStore.sweepCount.Load() != 1 {
			test.Errorf("directory %q: the object store was swept %d times on this instance's turn, want 1", directory, objectStore.sweepCount.Load())
		}
		if askedInterval <= 0 || askedInterval >= sweepInterval {
			test.Errorf("the turn is asked for by an interval of %s, want a little under %s", askedInterval, sweepInterval)
		}
	}
}
