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
