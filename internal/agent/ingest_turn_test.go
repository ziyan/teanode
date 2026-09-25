package agent

import (
	"testing"
	"time"
)

// The source that has waited longest for a computer has it next, rather
// than whichever asks first after it comes free; one that stops asking is
// not waited for.
func TestTheLongestWaitingSourceReadsNext(t *testing.T) {
	worker := &Agent{}
	start := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return start.Add(time.Duration(seconds) * time.Second) }

	if turn := worker.claimComputerAt("laptop", "chat", at(0)); !turn.isFree {
		t.Fatalf("a free computer is given out: %+v", turn)
	}
	// The drive starts a pass while the chat reads; it waits.
	if turn := worker.claimComputerAt("laptop", "drive", at(1)); turn.isFree || turn.other != "chat" || !turn.isReading {
		t.Fatalf("the drive waits for the chat: %+v", turn)
	}
	worker.releaseComputer("laptop", "chat")

	// The chat asks again first, but the drive has waited longer.
	if turn := worker.claimComputerAt("laptop", "chat", at(2)); turn.isFree || turn.other != "drive" || turn.isReading {
		t.Fatalf("the chat goes after the drive: %+v", turn)
	}
	if turn := worker.claimComputerAt("laptop", "drive", at(3)); !turn.isFree {
		t.Fatalf("the drive has its turn: %+v", turn)
	}
	worker.releaseComputer("laptop", "drive")

	// Now the chat is the one that has waited; it reads.
	if turn := worker.claimComputerAt("laptop", "chat", at(4)); !turn.isFree {
		t.Fatalf("the chat has its turn after the drive: %+v", turn)
	}
	worker.releaseComputer("laptop", "chat")

	// A source that asked a while ago keeps its place for as long as its
	// job takes to come round again, and no longer: the computer would
	// stand idle.
	if turn := worker.claimComputerAt("laptop", "github", at(5)); !turn.isFree {
		t.Fatalf("github reads: %+v", turn)
	}
	if turn := worker.claimComputerAt("laptop", "codex", at(6)); turn.isFree {
		t.Fatalf("codex waits for github: %+v", turn)
	}
	worker.releaseComputer("laptop", "github")
	// Codex asked four minutes ago and its job is on its way back: its
	// place is kept.
	if turn := worker.claimComputerAt("laptop", "chat", at(6+240)); turn.isFree || turn.other != "codex" || turn.isReading {
		t.Fatalf("a source that asked four minutes ago keeps its place: %+v", turn)
	}
	if turn := worker.claimComputerAt("laptop", "chat", at(6+360)); !turn.isFree {
		t.Fatalf("a source that stopped asking six minutes ago is not waited for: %+v", turn)
	}
}

// A source that could not reach its computer tries again soon; one whose
// computer is away tries again in a few minutes, or at its scheduled time
// if that comes first, rather than waiting for its hour.
func TestASourceThatCouldNotReachItsComputerTriesAgainSoon(t *testing.T) {
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	tomorrow, shortly := now.Add(20*time.Hour), now.Add(time.Minute)
	for _, each := range []struct {
		what       string
		waiting    waitingForDevice
		isAttached bool
		hasMore    bool
		scheduled  time.Time
		want       time.Time
	}{
		{"a laptop that is closed", waitingForDevice{name: "laptop"}, false, false, tomorrow, now.Add(ingestRetry)},
		{"a laptop that is closed, due sooner anyway", waitingForDevice{name: "laptop"}, false, false, shortly, shortly},
		{"a laptop that is closed, with no schedule", waitingForDevice{name: "laptop"}, false, false, time.Time{}, now.Add(ingestRetry)},
		{"a laptop that is closed partway through", waitingForDevice{name: "laptop"}, false, true, tomorrow, now.Add(ingestSoon)},
		{"an answer cut off by a restart", waitingForDevice{name: "laptop"}, true, false, tomorrow, now.Add(ingestSoon)},
		{"another source reading", waitingForDevice{name: "laptop", readingOther: "chat"}, true, false, tomorrow, now.Add(ingestSoon)},
		{"another source waiting longer", waitingForDevice{name: "laptop", behindOther: "drive"}, true, false, tomorrow, now.Add(ingestSoon)},
	} {
		if got := waitedUntil(&each.waiting, each.isAttached, each.hasMore, each.scheduled, now); !got.Equal(each.want) {
			t.Errorf("%s: tries again at %s, want %s", each.what, got, each.want)
		}
	}
}
