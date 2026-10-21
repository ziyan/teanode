package apigraph

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/api"
)

// passwordsListWriteTimeout is how long a Passwords list may take to be written
// to the reader. A list answers everything a person holds (up to 64 MiB), and a
// reader that takes the answer a few bytes at a time would otherwise keep the
// whole of it in memory, and the person's one place for reading, for as long as
// it liked. It is a variable so that a test can lower it.
var passwordsListWriteTimeout = time.Minute

// afterResponseKey is the context key of the functions a request runs once its
// answer has been written.
type afterResponseKey struct{}

// afterResponse holds functions that run when the answer of a GraphQL request
// has been written to the reader (or has failed to be), not when the resolver
// that wants them returns. A resolver builds the whole answer in memory and the
// server writes it afterwards, so what limits how many answers a person may
// have in memory at once has to be held until then.
type afterResponse struct {
	mutex     sync.Mutex
	functions []func()
}

func (self *afterResponse) add(function func()) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.functions = append(self.functions, function)
}

// run calls the functions once, last added first.
func (self *afterResponse) run() {
	self.mutex.Lock()
	functions := self.functions
	self.functions = nil
	self.mutex.Unlock()
	for index := len(functions) - 1; index >= 0; index-- {
		functions[index]()
	}
}

// contextWithAfterResponse is the context of a request that will run what is
// added to it when the answer has been written.
func contextWithAfterResponse(ctx context.Context) (context.Context, *afterResponse) {
	holder := &afterResponse{}
	return context.WithValue(ctx, afterResponseKey{}, holder), holder
}

// holdUntilResponseIsWritten arranges for release to run after the answer has
// been written, and says whether it did. When the request has no such place (a
// resolver called directly) the caller releases when it returns.
func holdUntilResponseIsWritten(ctx context.Context, release func()) bool {
	holder, isHeld := ctx.Value(afterResponseKey{}).(*afterResponse)
	if !isHeld {
		return false
	}
	holder.add(release)
	return true
}

// limitPasswordsListWrite gives the answer being built a deadline for being
// written to the reader. A response writer that cannot take a deadline is left
// as it is.
func limitPasswordsListWrite(ctx context.Context) {
	response := api.ContextResponse(ctx)
	if response == nil {
		return
	}
	if err := http.NewResponseController(response).SetWriteDeadline(time.Now().Add(passwordsListWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		log.Debugf("a write deadline could not be set on a Passwords list: %s", err)
	}
}
