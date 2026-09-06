package client

import (
	"errors"
	"fmt"
	"strings"
)

// The errors a caller can act on, as opposed to the ones it can only print.
//
// The server reports a refused token and a missing thing as GraphQL errors
// with HTTP 200, so the status code says nothing; the message does. The
// strings matched here are the ones internal/api declares. Matching text is
// the only choice a client has over the wire, and the values are pinned in a
// test so a rename on the server side is noticed.

var (
	// ErrUnauthorized means the server did not accept the token: HTTP 401,
	// or the API's own "not logged in".
	ErrUnauthorized = errors.New("client: the server refused the token")

	// ErrNotFound means the server has no such thing.
	ErrNotFound = errors.New("client: not found")
)

const (
	serverNotLoggedIn = "api: not logged in"
	serverNotFound    = "api: not found"
)

// ConnectionError means the server could not be reached at all, which is a
// different question from what it answered.
type ConnectionError struct {
	URL   string
	Cause error
}

func (self *ConnectionError) Error() string {
	return fmt.Sprintf("client: cannot reach %s: %s", self.URL, self.Cause)
}

func (self *ConnectionError) Unwrap() error {
	return self.Cause
}

// ReadOnlyError is a mutation refused by a read-only client before it was
// sent, so nothing on the server has changed.
type ReadOnlyError struct {
	URL string
}

func (self *ReadOnlyError) Error() string {
	return fmt.Sprintf("client: refusing to change anything on %s: this connection is read-only", self.URL)
}

// classify turns the errors a query returned into a typed one where the
// message is one the caller can act on, and leaves the rest as they are.
// Matched by prefix, because the server adds detail after the value ("api:
// not found: no such layout") and the detail is worth keeping.
func classify(errs Errors) error {
	for _, err := range errs {
		switch {
		case strings.HasPrefix(err.Message, serverNotLoggedIn):
			return fmt.Errorf("%w: %s", ErrUnauthorized, err.Message)
		case strings.HasPrefix(err.Message, serverNotFound):
			return fmt.Errorf("%w: %s", ErrNotFound, err.Message)
		}
	}
	return errs
}
