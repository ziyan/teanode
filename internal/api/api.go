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
	"context"
	"errors"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/aggregate"
	"github.com/ziyan/teanode/internal/util/ratelimit"

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

	// ErrConflict is a change refused because what it would change has
	// changed since the caller read it.
	ErrConflict = errors.New("api: conflict")
)

// TapRequiredPhrase is how the refusal of a request that only a person's tap
// may make begins: approving a call that spends the owner's money, or lending
// it. A command line token is refused these, because an agent can drive the
// person's command line, and says where to do it instead. The command line
// recognizes this phrase to explain the refusal in its own words.
const TapRequiredPhrase = "approve it on the dashboard or in the iPhone app"

// AgentCommandHeader is the request header the command line sends, with the
// value "1", when the computer program marked the command as one the agent ran
// for the person (the TEANODE_AGENT_COMMAND environment variable). The server
// then treats the request as the agent's, so what the agent may not do through
// its tools it cannot do by running the command line with the person's token
// either. It is a guard against mistakes and steered runs: anyone can leave the
// header off, and the person's own token is theirs to use.
const AgentCommandHeader = "X-Teanode-Agent-Command"

// Settings are the server-wide values the API needs.
type Settings struct {
	// RunningServices is the immutable set constructed by this process.
	RunningServices *config.Services

	// Secret key
	Secret []byte

	// Unique identifier for this backend instance
	BackendID string

	// Restarter ends this process so a supervisor starts a new one. Nil when
	// the server was started in a way that has no supervisor to do that, in
	// which case the API says restarting is unavailable rather than offering
	// a button that takes the server down and leaves it there.
	Restarter *Restarter

	// AuthLimiter bounds how often one address may present a credential,
	// shared with the submission listener so that a guess costs the same
	// whichever door it comes through. Nil disables the limit.
	AuthLimiter *ratelimit.Registry

	// Agent is the part of the personal agent the API calls in the request
	// rather than queues: work a person is waiting on. Nil when agents are
	// off, in which case the API says so.
	Agent AgentService

	// Chat is what carries a chat to the people in it: the feed of their
	// conversations, the other instances, their phones. Unlike the agent it
	// is there whatever the agent configuration is, nil only in a test with
	// no chats.
	Chat ChatService
}

// ChatService is what the API tells the chat hub in the request: who has the
// dashboard or the app in front of them, so that a phone is not told what a
// person is looking at. The rest of the hub, which speaks in the agent's
// events, is reached through the hub itself by the packages that know it.
type ChatService interface {
	// ReportPresence records one tab's or one phone's report for a person:
	// whether it is shown, how long since the person last used it, and which
	// conversation it has on screen, empty when none.
	ReportPresence(userId, reporterId string, isVisible bool, idleSeconds int, conversationId string, now time.Time)
}

// AgentService is what the API asks the agent to do at once.
type AgentService interface {
	// DraftReply writes a reply for the person to read, change and send.
	DraftReply(ctx context.Context, request *models.AgentDraftRequest) (*models.AgentDraft, error)
}

// Aggregations is the filter, sort and distinct pipeline a list query can be
// given, so that narrowing and ordering happen in the database rather than
// over whatever the browser happened to fetch.
//
// The shape mirrors the one used elsewhere in the fleet: a list of stages,
// each of which is exactly one of a match, a sort, or a distinct, applied in
// the order written.
type Aggregations = []*aggregate.Stage

// MaximumPageSize is the most rows one list query returns, however many
// were asked for. The dashboard pages at a fraction of it.
const MaximumPageSize = 1000

// Pagination parameters, used to filter returned results.
type Pagination struct {
	// Limit the returned results to first few
	First *uint64 `json:"first"`

	// Offset the returned results, skip amount specified by offset
	Offset *uint64 `json:"offset"`

	// Return results that after this cursor, usually ID can be used as cursor
	After *string `json:"after"`
}

// Limit returns a bounded page size. Omitted and zero sizes select the largest
// page; zero must not reach the database, where it means no limit.
func (self *Pagination) Limit() int {
	if self == nil || self.First == nil || *self.First == 0 || *self.First > MaximumPageSize {
		return MaximumPageSize
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
	options := &db.Options{Aggregations: aggregations, Columns: columns, Limit: uint64(self.Limit())}
	if self == nil {
		return options
	}
	if self.Offset != nil {
		options.Offset = *self.Offset
	}
	if self.After != nil {
		options.Cursor = *self.After
	}
	return options
}
