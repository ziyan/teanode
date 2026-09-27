package agent

import (
	"context"
	"testing"
)

// A turn takes what it is handed until it decides to end; handed something
// as it decides, it goes on instead; once it has ended, a message is a
// turn of its own.
func TestSteeringClosesOnlyWhenNothingWaits(t *testing.T) {
	running := &AskRun{}
	message := &AskRun{}
	message.ctx, message.cancel = context.WithCancel(context.Background())
	defer message.cancel()
	if !running.steer(message) {
		t.Fatal("a running turn takes a message")
	}
	if !running.closeToSteering() {
		t.Fatal("a turn with a message waiting goes one round more")
	}
	running.mutex.Lock()
	running.steering = nil
	running.mutex.Unlock()
	if running.closeToSteering() {
		t.Fatal("a turn with nothing waiting ends")
	}
	if running.steer(message) {
		t.Fatal("a turn that has decided to end takes nothing more")
	}
}
