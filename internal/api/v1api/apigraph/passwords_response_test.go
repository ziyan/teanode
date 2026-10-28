package apigraph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/graphql-go/graphql"

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

// timedMarshaler is an answer that notes when its encoding ended.
type timedMarshaler struct {
	encodingEnded time.Time
}

func (self *timedMarshaler) MarshalJSON() ([]byte, error) {
	time.Sleep(20 * time.Millisecond)
	self.encodingEnded = time.Now()
	return []byte(`"sealed"`), nil
}

// The deadline also starts after the answer is encoded: encoding a large list
// on a slow server took most of the deadline, and the reader lost the answer
// moments after it began to arrive.
func TestTheListWriteDeadlineStartsAfterEncoding(test *testing.T) {
	test.Parallel()

	response := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	ctx := api.ContextWithResponse(context.Background(), response)
	ctx, holder := contextWithAfterResponse(ctx)
	limitPasswordsListWrite(ctx)

	answer := &timedMarshaler{}
	writeGraphResult(response, &graphql.Result{Data: answer}, holder)
	if len(response.writeDeadlines) != 1 {
		test.Fatalf("got %d deadlines, want one", len(response.writeDeadlines))
	}
	if earliest := answer.encodingEnded.Add(passwordsListWriteTimeout); response.writeDeadlines[0].Before(earliest) {
		test.Fatalf("the deadline %v is before %v, the write timeout after encoding ended", response.writeDeadlines[0], earliest)
	}
	if body := response.Body.String(); body != "{\"data\":\"sealed\"}\n" {
		test.Fatalf("the answer written: %q", body)
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
