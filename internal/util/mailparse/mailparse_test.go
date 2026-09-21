package mailparse_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

// A header of many short continuation lines used to be assembled by
// appending to a string, which copied the whole header for every line: four
// megabytes of them took minutes, and a message is split before anything
// about it is checked, so anyone who could open a connection could spend
// the server's time this way.
func TestSplitReadsALongFoldedHeaderInLinearTime(t *testing.T) {
	t.Parallel()

	var message strings.Builder
	message.WriteString("Subject: a\r\n")
	for message.Len() < 2*1024*1024 {
		message.WriteString(" x\r\n")
	}
	message.WriteString("\r\nbody\r\n")

	started := time.Now()
	_, _, err := mailparse.Split(strings.NewReader(message.String()))
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("splitting two megabytes of continuation lines took %s", elapsed)
	}
	// Bigger than any header has a reason to be, so refused rather than
	// carried around and rescanned.
	if err != mailparse.ErrTooManyHeaders {
		t.Errorf("got %v, want %v", err, mailparse.ErrTooManyHeaders)
	}

	// An ordinary folded header is still read whole.
	headers, body, err := mailparse.Split(strings.NewReader("Subject: one\r\n two\r\nFrom: a@example.com\r\n\r\nhello\r\n"))
	if err != nil {
		t.Fatalf("split failed: %s", err)
	}
	if len(headers) != 2 || headers[0] != "Subject: one\r\n two\r\n" || string(body) != "hello\r\n" {
		t.Errorf("got headers %q, body %q", headers, body)
	}

	// Too many headers is refused too.
	message.Reset()
	for index := 0; index <= mailparse.MaximumHeaders; index++ {
		message.WriteString("X-A: b\r\n")
	}
	message.WriteString("\r\n")
	if _, _, err := mailparse.Split(strings.NewReader(message.String())); err != mailparse.ErrTooManyHeaders {
		t.Errorf("got %v, want %v", err, mailparse.ErrTooManyHeaders)
	}
}

// How many headers carry a name, whatever their case: the check that
// refuses a message with two From lines rests on it.
func TestCountHeaders(t *testing.T) {
	t.Parallel()

	headers := []string{"From: a@example.com\r\n", "Subject: x\r\n", "FROM: b@example.com\r\n", "X-From: c\r\n"}
	if got := mailparse.CountHeaders(headers, "From"); got != 2 {
		t.Errorf("counted %d From headers, want 2", got)
	}
	if got := mailparse.CountHeaders(headers, "Reply-To"); got != 0 {
		t.Errorf("counted %d Reply-To headers, want 0", got)
	}
}
