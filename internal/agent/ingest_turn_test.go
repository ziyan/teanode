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

	if turn := worker.claimComputerAt("laptop", "chat", at(0), nil); !turn.isFree {
		t.Fatalf("a free computer is given out: %+v", turn)
	}
	// The drive starts a pass while the chat reads; it waits.
	if turn := worker.claimComputerAt("laptop", "drive", at(1), nil); turn.isFree || turn.other != "chat" || !turn.isReading {
		t.Fatalf("the drive waits for the chat: %+v", turn)
	}
	worker.releaseComputer("laptop", "chat")

	// The chat asks again first, but the drive has waited longer.
	if turn := worker.claimComputerAt("laptop", "chat", at(2), nil); turn.isFree || turn.other != "drive" || turn.isReading {
		t.Fatalf("the chat goes after the drive: %+v", turn)
	}
	if turn := worker.claimComputerAt("laptop", "drive", at(3), nil); !turn.isFree {
		t.Fatalf("the drive has its turn: %+v", turn)
	}
	worker.releaseComputer("laptop", "drive")

	// Now the chat is the one that has waited; it reads.
	if turn := worker.claimComputerAt("laptop", "chat", at(4), nil); !turn.isFree {
		t.Fatalf("the chat has its turn after the drive: %+v", turn)
	}
	worker.releaseComputer("laptop", "chat")

	// A source that asked a while ago keeps its place for as long as its
	// job takes to come round again, and no longer: the computer would
	// stand idle.
	if turn := worker.claimComputerAt("laptop", "github", at(5), nil); !turn.isFree {
		t.Fatalf("github reads: %+v", turn)
	}
	if turn := worker.claimComputerAt("laptop", "codex", at(6), nil); turn.isFree {
		t.Fatalf("codex waits for github: %+v", turn)
	}
	worker.releaseComputer("laptop", "github")
	// Codex asked four minutes ago and its job is on its way back: its
	// place is kept.
	if turn := worker.claimComputerAt("laptop", "chat", at(6+240), nil); turn.isFree || turn.other != "codex" || turn.isReading {
		t.Fatalf("a source that asked four minutes ago keeps its place: %+v", turn)
	}
	if turn := worker.claimComputerAt("laptop", "chat", at(6+360), nil); !turn.isFree {
		t.Fatalf("a source that stopped asking six minutes ago is not waited for: %+v", turn)
	}
}

// A source asked to read now (a coding session's transcripts, after an
// answer) goes ahead of one that has waited longer, though not ahead of
// the one reading now; two asked go in the order they were asked, and a
// scheduled pass of the same kind of source takes its turn.
func TestASourceAskedToReadNowGoesNext(t *testing.T) {
	worker := &Agent{}
	start := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return start.Add(time.Duration(seconds) * time.Second) }
	asked := func(seconds int) *time.Time { moment := at(seconds); return &moment }

	worker.claimComputerAt("laptop", "chat", at(0), nil)
	worker.claimComputerAt("laptop", "drive", at(1), nil)
	if turn := worker.claimComputerAt("laptop", "codex", at(3), asked(3)); turn.isFree || turn.other != "chat" || !turn.isReading {
		t.Fatalf("an asked source waits for the one reading now: %+v", turn)
	}
	if turn := worker.claimComputerAt("laptop", "claude-code", at(4), asked(2)); turn.isFree || turn.other != "chat" {
		t.Fatalf("and so does another: %+v", turn)
	}
	worker.releaseComputer("laptop", "chat")
	if turn := worker.claimComputerAt("laptop", "drive", at(5), nil); turn.isFree || turn.isReading {
		t.Fatalf("the drive, though it waited longer, goes after the sources asked to read now: %+v", turn)
	}
	if turn := worker.claimComputerAt("laptop", "codex", at(6), asked(3)); turn.isFree || turn.other != "claude-code" {
		t.Fatalf("of two asked, the one asked first goes first: %+v", turn)
	}
	if turn := worker.claimComputerAt("laptop", "claude-code", at(7), asked(2)); !turn.isFree {
		t.Fatalf("the source asked first reads next: %+v", turn)
	}
	worker.releaseComputer("laptop", "claude-code")
	if turn := worker.claimComputerAt("laptop", "codex", at(8), asked(3)); !turn.isFree {
		t.Fatalf("then the other: %+v", turn)
	}
	worker.releaseComputer("laptop", "codex")
	// A scheduled pass, nobody asking, waits its turn like any source.
	if turn := worker.claimComputerAt("laptop", "claude-code", at(9), nil); turn.isFree || turn.other != "drive" {
		t.Fatalf("a scheduled pass goes after the source that waited longer: %+v", turn)
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

// An evaluation answered by the agent itself names its effort after an @.
func TestAnAgentAnswerNamesItsEffort(t *testing.T) {
	for answerFrom, want := range map[string]struct {
		effort            string
		research, isAgent bool
	}{
		"agent": {"", false, true}, "agent@high": {"high", false, true}, "agent@low": {"low", false, true},
		"agent+research": {"", true, true}, "agent@medium+research": {"medium", true, true},
		"agent@loud": {"", false, false}, "memory": {"", false, false}, "both": {"", false, false},
	} {
		effort, research, isAgent := agentEffortOf(answerFrom)
		if effort != want.effort || research != want.research || isAgent != want.isAgent {
			t.Errorf("%s: %q %v %v", answerFrom, effort, research, isAgent)
		}
	}
}

// A request to read a source is answered by the pass that finishes after
// it; one with a page that ran out of time keeps it for the next pass,
// but not for ever.
func TestARequestOutlivesOneUnfinishedPassOnly(t *testing.T) {
	cursor := map[string]any{}
	first := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	third := second.Add(time.Minute)
	if answered := requestsAnsweredBefore(cursor, first, true); !answered.IsZero() {
		t.Fatalf("an unfinished pass answers nothing the first time: %v", answered)
	}
	if answered := requestsAnsweredBefore(cursor, second, true); !answered.Equal(first) {
		t.Fatalf("a second unfinished pass answers what was asked before the first: %v", answered)
	}
	if answered := requestsAnsweredBefore(cursor, third, false); !answered.Equal(third) {
		t.Fatalf("a finished pass answers what was asked before it began: %v", answered)
	}
	if _, isKept := cursor[cursorRequestKeptSince]; isKept {
		t.Fatalf("and forgets the unfinished passes")
	}
}
