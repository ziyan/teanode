package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// How long a call to a provider may take. Nothing bounds how long an answer
// takes in total: a long document read streams for minutes, and a cut at any
// fixed length made it fail however well the provider was doing.
//
// They are variables so that a test can shorten them.
var (
	// streamIdleTimeout is how long a streamed answer may go without a byte
	// from the provider, a comment line that keeps the connection warm
	// included. Every byte starts it again, so an answer that keeps arriving
	// is never cut, however long it runs. It also covers the wait for the
	// first byte, where the provider is thinking.
	streamIdleTimeout = 3 * time.Minute

	// plainRequestTimeout is the whole of a request that does not stream: the
	// wait for the answer and the reading of it. Generous, because a bound
	// that is never reached is only there so that a connection that died
	// without telling anyone still ends.
	plainRequestTimeout = 15 * time.Minute
)

// guardTimeout says which of the two clocks ran out. It is a deadline
// exceeded to errors.Is, as the timeouts it replaces were, so whoever treats
// a deadline as an ending and not a failure still does.
type guardTimeout struct {
	message string
}

func (self *guardTimeout) Error() string {
	return self.message
}

func (self *guardTimeout) Unwrap() error {
	return context.DeadlineExceeded
}

// IsProviderTimeout says whether err is one of the two clocks above running
// out: the provider went quiet or did not finish answering. It is a deadline
// exceeded to errors.Is, yet it is the provider failing, not the work being
// stopped, and a caller that requeues deadlines freely must still count it.
func IsProviderTimeout(err error) bool {
	var timeout *guardTimeout
	return errors.As(err, &timeout)
}

// NewProviderTimeout is the error a clock above reports, with the sentence
// that says which ran out.
func NewProviderTimeout(message string) error {
	return &guardTimeout{message: message}
}

func idleTimeoutOf(idle time.Duration) error {
	return NewProviderTimeout(fmt.Sprintf("nothing arrived from the provider for %s", idle))
}

func plainTimeoutOf(total time.Duration) error {
	return NewProviderTimeout(fmt.Sprintf("the provider did not finish answering within %s", total))
}

// reportTimeout turns the cancellation a clock caused into the reason it
// was caused, and leaves every other error as it is.
func reportTimeout(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var timeout *guardTimeout
	if cause := context.Cause(ctx); errors.As(cause, &timeout) {
		return timeout
	}
	return err
}

// guardedBody is a response body under the clocks of guardedDo.
type guardedBody struct {
	body       io.ReadCloser
	ctx        context.Context
	cancel     context.CancelCauseFunc
	idle       time.Duration
	idleTimer  *time.Timer
	totalTimer *time.Timer
}

func (self *guardedBody) Read(buffer []byte) (int, error) {
	if self.idleTimer != nil {
		// The wait for these bytes is what is timed, not the time the
		// caller spends with the last ones.
		self.idleTimer.Reset(self.idle)
	}
	count, err := self.body.Read(buffer)
	if self.idleTimer != nil && count > 0 {
		self.idleTimer.Reset(self.idle)
	}
	if err != nil && err != io.EOF {
		err = reportTimeout(self.ctx, err)
	}
	return count, err
}

func (self *guardedBody) Close() error {
	self.stop()
	err := self.body.Close()
	self.cancel(nil)
	return err
}

func (self *guardedBody) stop() {
	if self.idleTimer != nil {
		self.idleTimer.Stop()
	}
	if self.totalTimer != nil {
		self.totalTimer.Stop()
	}
}

// guardedDo sends a request under one of two clocks and returns a response
// whose body is under it too, until the caller closes it:
//
//   - idle above zero: the request is cut when nothing has arrived for that
//     long. Every read that brings bytes starts the clock again.
//   - total above zero: the request is cut at that long after it was sent,
//     whatever arrives.
//
// The client has no timeout of its own, since the whole of a response is
// what a client timeout bounds.
func guardedDo(client *http.Client, request *http.Request, idle, total time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(request.Context())
	guarded := &guardedBody{ctx: ctx, cancel: cancel, idle: idle}
	if idle > 0 {
		cause := idleTimeoutOf(idle)
		guarded.idleTimer = time.AfterFunc(idle, func() { cancel(cause) })
	}
	if total > 0 {
		cause := plainTimeoutOf(total)
		guarded.totalTimer = time.AfterFunc(total, func() { cancel(cause) })
	}
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		guarded.stop()
		err = reportTimeout(ctx, err)
		cancel(nil)
		return nil, err
	}
	guarded.body = response.Body
	response.Body = guarded
	return response, nil
}
