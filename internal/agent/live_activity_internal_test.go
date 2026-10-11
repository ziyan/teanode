package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/push"
)

// newLiveActivityRun is a run of a worker that pushes through sender.
func newLiveActivityRun(sender push.Sender) (*AskRun, *Agent) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	worker := New(&Settings{Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	worker.phones.override = sender
	return &AskRun{ID: "a-run", agent: worker}, worker
}

// toldStates is what one activity was told, in order, with the end marked.
func toldStates(t *testing.T, told []*push.Delivery) []string {
	t.Helper()
	var states []string
	for _, delivery := range told {
		state := delivery.ActivityState
		if delivery.IsActivityEnded {
			state += " (ended)"
		}
		if delivery.Sealed != "" {
			t.Errorf("a Live Activity was sent content: %+v", delivery)
		}
		states = append(states, state)
	}
	return states
}

func sameStates(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// A Live Activity is told the turn's state when it starts following it, and
// again only when the state changes; the end is told once.
func TestALiveActivityFollowsTheTurnsState(t *testing.T) {
	relay := &fakeRelay{}
	run, worker := newLiveActivityRun(relay)

	if err := run.RegisterLiveActivity("sealed:an-activity"); err != nil {
		t.Fatalf("RegisterLiveActivity: %s", err)
	}
	worker.waitGroup.Wait()
	for _, kind := range []EventKind{EventToolCall, EventText, EventConfirmation, EventQuestion, EventToolResult, EventTitled, EventDone, EventMessage, EventDone} {
		run.followLiveActivities(kind, time.Now())
		// One state at a time, so that none is replaced before it is sent.
		worker.waitGroup.Wait()
	}

	got := toldStates(t, relay.to("sealed:an-activity"))
	want := []string{"working", "waiting", "working", "done (ended)"}
	if !sameStates(got, want) {
		t.Fatalf("told %v, want %v", got, want)
	}
	if err := run.RegisterLiveActivity(""); err == nil {
		t.Error("an empty registration was taken")
	}
}

// blockingRelay holds the first delivery until it is let go, the way a slow
// trip to the relay would, and records every delivery in the order sent.
type blockingRelay struct {
	fakeRelay
	isHolding bool
	held      chan struct{}
	release   chan struct{}
}

func (self *blockingRelay) Send(ctx context.Context, pushRegistration string, delivery *push.Delivery) error {
	if !self.isHolding {
		self.isHolding = true
		close(self.held)
		<-self.release
	}
	return self.fakeRelay.Send(ctx, pushRegistration, delivery)
}

// Each state used to go out from a goroutine of its own, so a "waiting"
// could reach the relay after the "working" that followed it and the lock
// screen would show the older one. One sender per run sends them in the
// order they happened, and a state replaced while the one before it was on
// its way is not sent at all.
func TestALiveActivityIsToldItsStatesInOrder(t *testing.T) {
	relay := &blockingRelay{held: make(chan struct{}), release: make(chan struct{})}
	run, worker := newLiveActivityRun(relay)

	if err := run.RegisterLiveActivity("sealed:an-activity"); err != nil {
		t.Fatalf("RegisterLiveActivity: %s", err)
	}
	<-relay.held
	started := time.Now()
	run.followLiveActivities(EventConfirmation, started.Add(time.Second))
	run.followLiveActivities(EventToolCall, started.Add(2*time.Second))
	run.followLiveActivities(EventQuestion, started.Add(3*time.Second))
	close(relay.release)
	worker.waitGroup.Wait()

	told := relay.to("sealed:an-activity")
	got := toldStates(t, told)
	if want := []string{"working", "waiting"}; !sameStates(got, want) {
		t.Fatalf("told %v, want %v: the newest state last, and none in between", got, want)
	}
	if !told[1].ActivityChangedAt.Equal(started.Add(3 * time.Second)) {
		t.Errorf("the last state carries %s, not when it changed", told[1].ActivityChangedAt)
	}
	if told[0].ActivityChangedAt.IsZero() || told[0].ActivityChangedAt.After(told[1].ActivityChangedAt) {
		t.Errorf("the states' times run %s then %s", told[0].ActivityChangedAt, told[1].ActivityChangedAt)
	}
}
