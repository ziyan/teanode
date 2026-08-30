package bufferpool_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/bufferpool"
)

// What a caller is promised: the buffer it is handed is empty, whatever the
// last caller wrote into it.
//
// Not that it is the same buffer. sync.Pool makes no such promise — a garbage
// collection between the release and the next acquire empties the pool, and
// under -race another goroutine can take the one just released. This test used
// to assert the identity, and it failed in CI for exactly that reason: it was
// asserting a property of the runtime at that moment rather than a property of
// this code.
func TestAnAcquiredBufferIsEmpty(t *testing.T) {
	t.Parallel()

	buffer, releaseBuffer := bufferpool.AcquireBuffer()
	buffer.WriteString("hello world!")
	releaseBuffer()

	// Enough times that a reused buffer is overwhelmingly likely to come back
	// at least once; every one of them has to be empty either way.
	for range 100 {
		next, releaseNext := bufferpool.AcquireBuffer()
		if next.Len() != 0 {
			t.Fatalf("an acquired buffer holds %d bytes: %q", next.Len(), next.String())
		}
		next.WriteString("something else")
		releaseNext()
	}
}

// A released buffer is reset before the next caller sees it, which is the
// whole reason release exists rather than the pool being used directly.
func TestReleasingKeepsNothing(t *testing.T) {
	t.Parallel()

	first, release := bufferpool.AcquireBuffer()
	first.WriteString("a secret")
	release()

	second, releaseSecond := bufferpool.AcquireBuffer()
	defer releaseSecond()
	if second == first && second.Len() != 0 {
		t.Fatal("the same buffer came back with its contents still in it")
	}
}
