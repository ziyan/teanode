package deferutil_test

import (
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/util/deferutil"
)

// TestRecoverContainsThePanic is the whole point: a panic in one goroutine
// must not reach the runtime and take the process with it. Without the guard,
// this test binary would crash rather than fail.
func TestRecoverContainsThePanic(t *testing.T) {
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)

	go func() {
		defer waitGroup.Done()
		defer deferutil.Recover()
		panic("a malformed message tripped something")
	}()

	waitGroup.Wait()
}

func TestRecoverIsHarmlessWithoutAPanic(t *testing.T) {
	var reached bool
	func() {
		defer deferutil.Recover()
		reached = true
	}()
	if !reached {
		t.Error("the guarded function did not run")
	}
}

func TestRecoverWith(t *testing.T) {
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)

	go func() {
		defer waitGroup.Done()
		defer deferutil.RecoverWith("delivering a message")
		var deliveries []string
		_ = deliveries[5] // out of range on purpose
	}()

	waitGroup.Wait()
}
