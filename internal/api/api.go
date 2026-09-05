// Package api holds what every version of the HTTP API shares: the error
// values, the request context, and the paths.
//
// The API itself lives in versioned subpackages. internal/api/v1api mounts
// version 1 under /api/v1, and its own subpackages implement the parts:
// apigraph the GraphQL endpoint, apisend the template send endpoint, apiauth
// the session endpoints a browser uses to log in.
//
// This package deliberately depends on almost nothing, so that the versioned
// packages and internal/web can all import it without a cycle.
package api

import (
	"errors"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/aggregate"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("api") //nolint:unused

var (
	ErrNotLoggedIn       = errors.New("api: not logged in")
	ErrAlreadyLoggedIn   = errors.New("api: already logged in")
	ErrNotFound          = errors.New("api: not found")
	ErrAlreadyExists     = errors.New("api: already exists")
	ErrPermissionDenied  = errors.New("api: permission denied")
	ErrInvalidArguments  = errors.New("api: invalid arguments")
	ErrInvalidDomain     = errors.New("api: invalid domain")
	ErrInvalidEmail      = errors.New("api: invalid email")
	ErrInvalidToken      = errors.New("api: invalid token")
	ErrInvalidCode       = errors.New("api: invalid code")
	ErrInvalidCredential = errors.New("api: invalid credential")
	ErrNotRetryable      = errors.New("api: not retryable")
	ErrTooManyRequests   = errors.New("api: too many attempts, try again later")
)

// Settings are the server-wide values the API needs.
type Settings struct {
	// Secret key
	Secret []byte

	// Unique identifier for this backend instance
	BackendID string

	// Restarter ends this process so a supervisor starts a new one. Nil when
	// the server was started in a way that has no supervisor to do that, in
	// which case the API says restarting is unavailable rather than offering
	// a button that takes the server down and leaves it there.
	Restarter *Restarter
}

// Aggregations is the filter, sort and distinct pipeline a list query can be
// given, so that narrowing and ordering happen in the database rather than
// over whatever the browser happened to fetch.
//
// The shape mirrors the one used elsewhere in the fleet: a list of stages,
// each of which is exactly one of a match, a sort, or a distinct, applied in
// the order written.
type Aggregations = []*aggregate.Stage

// Pagination parameters, used to filter returned results.
type Pagination struct {
	// Limit the returned results to first few
	First *uint64 `json:"first"`

	// Offset the returned results, skip amount specified by offset
	Offset *uint64 `json:"offset"`

	// Return results that after this cursor, usually ID can be used as cursor
	After *string `json:"after"`
}

// Limit returns how many results the caller asked for, or zero for no limit.
func (self *Pagination) Limit() int {
	if self == nil || self.First == nil {
		return 0
	}
	return int(*self.First)
}

// Options converts pagination parameters to options for a database query.
func (self *Pagination) Options() *db.Options {
	return self.OptionsWith(nil, nil)
}

// OptionsWith is Options plus the aggregation pipeline and the fields it is
// allowed to name.
func (self *Pagination) OptionsWith(aggregations Aggregations, columns aggregate.Columns) *db.Options {
	options := &db.Options{Aggregations: aggregations, Columns: columns}
	if self == nil {
		return options
	}
	if self.First != nil {
		options.Limit = *self.First
	}
	if self.Offset != nil {
		options.Offset = *self.Offset
	}
	if self.After != nil {
		options.Cursor = *self.After
	}
	return options
}
