package bufferpool_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/bufferpool"
)

func TestAcquireBuffer(t *testing.T) {
	t.Parallel()
	buffer, releaseBuffer := bufferpool.AcquireBuffer()
	buffer.WriteString("hello world!")
	releaseBuffer()

	buffer2, releaseBuffer2 := bufferpool.AcquireBuffer()
	defer releaseBuffer2()
	if buffer2 != buffer {
		t.Fatalf("expecting to reuse buffer")
	}
	if buffer2.Len() != 0 {
		t.Fatalf("expecting acquired buffer to be empty: %d", buffer2.Len())
	}
}
