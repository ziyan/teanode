package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// Operations is the API as the person, for a tool: every call runs in its
// own transaction with the person's permissions, and the audit trail
// names the person with the agent as the actor.
type Operations interface {
	// Execute runs one document of the API as the person and decodes the
	// data into result.
	Execute(ctx context.Context, document string, variables map[string]any, result any) error

	// Permissions is what the person may do.
	Permissions() *models.EffectivePermissions
}

// Run is the turn a tool is called in, as a tool sees it: who it is for,
// what it may reach, and what has been loaded so far. The loop puts its
// run into the context with WithRun; a tool takes it out with RunFrom.
// What a family of tools needs beyond this — a browser, an attached tab,
// a question put to the person — is an interface of its own that the run
// may also satisfy.
type Run interface {
	Owner() *models.User
	Agent() *models.Agent
	Conversation() *models.AgentConversation
	Operations() Operations
	Database() db.Database
	Configuration() *config.Configuration

	// Surface is where the person is: drawer, cli, api, mail, phone, or
	// empty for a run with nobody present.
	Surface() string
	Headless() bool

	// CanAsk says a question card may be put to the person and waited on:
	// they are there, whether or not they started the turn.
	CanAsk() bool
	ReadOnly() bool

	// Offered is every tool this run may use; Loaded is which of the
	// deferred ones tool_search has loaded; Load marks one loaded.
	Offered() []*Tool
	Loaded() map[string]bool
	Load(name string)

	// Storage is where a file the agent makes is kept.
	Storage() storage.Storage

	// Recall keeps a line a memory search found, for the <recalled>
	// overlay; Recalled is what has been kept this turn.
	Recall(line string)
	Recalled() []string

	// Ask puts a question to the person and waits for the answer.
	Ask(ctx context.Context, callId, question string, choices []string) (string, error)

	// Enqueue queues a job of the agent's, in the transaction given.
	Enqueue(tx db.Transaction, kind models.AgentJobKind, mailboxId, subjectId string) error

	// DraftReply writes a reply the way the draft pipeline does; DiscardDraft
	// removes a draft the agent holds; MeaningSearch finds messages that say
	// the same thing in other words, or nothing where search by meaning is
	// off.
	DraftReply(ctx context.Context, request *models.AgentDraftRequest) (*models.AgentDraft, error)
	DiscardDraft(ctx context.Context, tx db.Transaction, itemId string) error
	MeaningSearch(ctx context.Context, mailboxId, query string, limit int) ([]string, error)
}

type runKey struct{}

// WithRun puts the run into the context for the tools called in it.
func WithRun(ctx context.Context, run Run) context.Context {
	return context.WithValue(ctx, runKey{}, run)
}

// ErrLeftOpen is what Run.Ask answers when the person did not answer
// while the turn waited: the question card stays open for them, and a
// late answer starts a new turn with it.
var ErrLeftOpen = errors.New("the person has not answered yet; the card stays open")

// RunFrom is the run a tool was called in. A tool called with none is a
// programming error, said plainly rather than dereferenced.
func RunFrom(ctx context.Context) (Run, error) {
	run, ok := ctx.Value(runKey{}).(Run)
	if !ok || run == nil {
		return nil, fmt.Errorf("tools: no run in the context")
	}
	return run, nil
}

// MustRun is the run a tool was called in, for a tool the loop calls —
// the loop always puts one in. A test that calls a tool directly puts one
// in with WithRun; a call without one is a programming error and panics
// with RunFrom's message.
func MustRun(ctx context.Context) Run {
	run, err := RunFrom(ctx)
	if err != nil {
		panic(err)
	}
	return run
}

// ConversationIDOf is the conversation a run is part of, or empty when it is
// part of none: the night, and a call from a harness over the protocol.
//
// Every tool reads it through here rather than as Conversation().ID. That
// form dereferenced a nil conversation, and through the protocol it took
// down the request: the harness saw its connection dropped, with no answer
// and no error.
func ConversationIDOf(run Run) string {
	if conversation := run.Conversation(); conversation != nil {
		return conversation.ID
	}
	return ""
}
