package tools

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
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
	ReadOnly() bool

	// Offered is every tool this run may use; Loaded is which of the
	// deferred ones tool_search has loaded; Load marks one loaded.
	Offered() []*Tool
	Loaded() map[string]bool
	Load(name string)
}

type runKey struct{}

// WithRun puts the run into the context for the tools called in it.
func WithRun(ctx context.Context, run Run) context.Context {
	return context.WithValue(ctx, runKey{}, run)
}

// RunFrom is the run a tool was called in. A tool called with none is a
// programming error, said plainly rather than dereferenced.
func RunFrom(ctx context.Context) (Run, error) {
	run, ok := ctx.Value(runKey{}).(Run)
	if !ok || run == nil {
		return nil, fmt.Errorf("tools: no run in the context")
	}
	return run, nil
}
