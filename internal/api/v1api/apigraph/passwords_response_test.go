package apigraph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
)

// deadlineRecorder is a response writer that keeps the write deadlines set on
// it.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	writeDeadlines []time.Time
}

func (self *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	self.writeDeadlines = append(self.writeDeadlines, deadline)
	return nil
}

// The write deadline of a Passwords list counts from when the answer begins to
// be written, not from when the resolver asked for it: building a large answer
// on a slow server took longer than the deadline, so the first reader lost its
// answer before it began, and a second list was let in and lost its own.
func TestTheListWriteDeadlineStartsWhenWritingBegins(test *testing.T) {
	test.Parallel()

	response := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	ctx := api.ContextWithResponse(context.Background(), response)
	ctx, holder := contextWithAfterResponse(ctx)

	limitPasswordsListWrite(ctx)
	if len(response.writeDeadlines) != 0 {
		test.Fatalf("the deadline was set while the answer was being built: %v", response.writeDeadlines)
	}

	writingBegins := time.Now()
	holder.startWriteDeadline(response)
	if len(response.writeDeadlines) != 1 {
		test.Fatalf("got %d deadlines, want one when the writing begins", len(response.writeDeadlines))
	}
	if earliest := writingBegins.Add(passwordsListWriteTimeout); response.writeDeadlines[0].Before(earliest) {
		test.Fatalf("the deadline %v is before %v, the write timeout after the writing began", response.writeDeadlines[0], earliest)
	}
}

// An answer that asked for no deadline (anything but a Passwords list) keeps
// the server's own.
func TestAnAnswerWithoutAListKeepsItsWriteDeadline(test *testing.T) {
	test.Parallel()

	response := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	_, holder := contextWithAfterResponse(context.Background())
	holder.startWriteDeadline(response)
	if len(response.writeDeadlines) != 0 {
		test.Fatalf("an answer that asked for none got a deadline: %v", response.writeDeadlines)
	}
	var _ http.ResponseWriter = response
}
