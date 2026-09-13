package security

import (
	"testing"
	"time"
)

// Identifiers are made often -- one per message, per item, per delivery, per
// audit row -- so the entropy behind them has to be cheap as well as
// unguessable. crypto/rand on every platform this runs on is a syscall-free
// read from a userspace pool, and the pool of monotonic readers keeps the
// allocation out of the hot path.
func TestIdentifiersAreCheapAndDistinct(t *testing.T) {
	seen := make(map[string]bool, 100000)
	started := time.Now()
	for index := 0; index < 100000; index++ {
		id := NewULID()
		if seen[id] {
			t.Fatalf("two identifiers the same after %d", index)
		}
		seen[id] = true
	}
	took := time.Since(started)
	// A fuse, not a benchmark: a hundred thousand in a second is far more
	// than any run of this server makes, and a regression that made these
	// expensive would show up here rather than in a mail queue.
	if took > 5*time.Second {
		t.Fatalf("a hundred thousand identifiers took %s", took)
	}
	t.Logf("a hundred thousand identifiers in %s", took)
}
