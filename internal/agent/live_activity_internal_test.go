package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// A Live Activity is told the turn's state when it starts following it, and
// again only when the state changes; the end is told once.
func TestALiveActivityFollowsTheTurnsState(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	worker := New(&Settings{Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	relay := &fakeRelay{}
	worker.phones.override = relay
	run := &AskRun{ID: "a-run", agent: worker}

	if err := run.RegisterLiveActivity("sealed:an-activity"); err != nil {
		t.Fatalf("RegisterLiveActivity: %s", err)
	}
	for _, kind := range []EventKind{EventToolCall, EventText, EventConfirmation, EventQuestion, EventToolResult, EventTitled, EventDone, EventMessage} {
		run.followLiveActivities(kind)
	}
	run.followLiveActivities(EventDone)
	worker.waitGroup.Wait()

	told := relay.to("sealed:an-activity")
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
	// Sent from goroutines, so in any order; what matters is each once.
	want := map[string]bool{"working": true, "waiting": true, "done (ended)": true}
	counted := map[string]int{}
	for _, state := range states {
		counted[state]++
	}
	if len(states) != 4 || counted["working"] != 2 || counted["waiting"] != 1 || counted["done (ended)"] != 1 {
		t.Fatalf("told %v, want working on registering and again after waiting, waiting once and the end once (%v)", states, want)
	}
	if err := run.RegisterLiveActivity(""); err == nil {
		t.Error("an empty registration was taken")
	}
}
