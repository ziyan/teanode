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

	// A source that asked long ago and has stopped asking is not waited
	// for: the computer would stand idle.
	if turn := worker.claimComputerAt("laptop", "github", at(5)); !turn.isFree {
		t.Fatalf("github reads: %+v", turn)
	}
	if turn := worker.claimComputerAt("laptop", "codex", at(6)); turn.isFree {
		t.Fatalf("codex waits for github: %+v", turn)
	}
	worker.releaseComputer("laptop", "github")
	if turn := worker.claimComputerAt("laptop", "chat", at(6+90)); !turn.isFree {
		t.Fatalf("a source that stopped asking a minute and a half ago is not waited for: %+v", turn)
	}
}

// A source that could not reach its computer tries again at its next
// scheduled time only when the computer is away; cut off by a restart,
// waiting for another source, or with more to read, it tries again soon.
func TestASourceThatCouldNotReachItsComputerTriesAgainSoon(t *testing.T) {
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	scheduled := now.Add(20 * time.Hour)
	for _, each := range []struct {
		what       string
		waiting    waitingForDevice
		isAttached bool
		hasMore    bool
		want       time.Time
	}{
		{"a laptop that is closed", waitingForDevice{name: "laptop"}, false, false, scheduled},
		{"a laptop that is closed partway through", waitingForDevice{name: "laptop"}, false, true, now.Add(ingestSoon)},
		{"an answer cut off by a restart", waitingForDevice{name: "laptop"}, true, false, now.Add(ingestSoon)},
		{"another source reading", waitingForDevice{name: "laptop", readingOther: "chat"}, true, false, now.Add(ingestSoon)},
		{"another source waiting longer", waitingForDevice{name: "laptop", behindOther: "drive"}, true, false, now.Add(ingestSoon)},
	} {
		if got := waitedUntil(&each.waiting, each.isAttached, each.hasMore, scheduled, now); !got.Equal(each.want) {
			t.Errorf("%s: tries again at %s, want %s", each.what, got, each.want)
		}
	}
}
