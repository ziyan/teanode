package apigraph

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// What the person's coding sessions (Claude Code, Codex) are shown of
// their agent's memory, and the call that has a session read in when it
// answers. The hooks `teanode hook install` puts in those tools call
// these; the dashboard previews them. See
// docs/planning/coding-session-memory-execplan.md.

// AgentCodingQuery reads what a coding session is shown.
type AgentCodingQuery interface {
	// ReadAgentCodingContext is what a coding session starting in a
	// directory is shown: the checkout's project page and its liveliest
	// facts, the lessons that apply, and where the last session in the
	// directory stopped. Nothing is marked as used. Needs agent:use.
	ReadAgentCodingContext(ctx context.Context, arguments ReadAgentCodingContextArguments) (*AgentCodingContext, error)

	// RecallAgentCodingMemory is what a prompt typed in a coding session
	// recalls: recall kept to the checkout's project, the pages under it
	// and linked to it, and lessons, less the pages named in shownPaths.
	// Nothing is marked as used. Needs agent:use.
	RecallAgentCodingMemory(ctx context.Context, arguments RecallAgentCodingMemoryArguments) (*AgentCodingContext, error)
}

// AgentCodingMutation is what a coding session asks of the agent.
type AgentCodingMutation interface {
	// CaptureAgentCodingSession asks the sources reading a coding tool's
	// transcripts on a computer to read again now, so that a session's
	// latest answer is searchable within a minute. True when a source was
	// asked; false when none fits or one is already due. Needs agent:use.
	CaptureAgentCodingSession(ctx context.Context, arguments CaptureAgentCodingSessionArguments) (bool, error)
}

// ReadAgentCodingContextArguments is where a coding session runs.
type ReadAgentCodingContextArguments struct {
	// Directory is the session's working directory, absolute.
	Directory string `json:"directory"`
	// ComputerName is the computer it runs on, as attached; where the same
	// directory is a checkout on several, this one's is chosen.
	ComputerName string `json:"computerName" graphapi:"nullable"`
	// HomeDirectory is that computer's home directory, which a checkout
	// recorded as ~/... is under.
	HomeDirectory string `json:"homeDirectory" graphapi:"nullable"`
	// SessionID is the coding tool's id for the session, so that a
	// session being resumed is not reported as the one before it.
	SessionID string `json:"sessionId" graphapi:"nullable"`
}

// RecallAgentCodingMemoryArguments is a prompt and where it was typed.
type RecallAgentCodingMemoryArguments struct {
	Prompt        string `json:"prompt"`
	Directory     string `json:"directory"`
	ComputerName  string `json:"computerName" graphapi:"nullable"`
	HomeDirectory string `json:"homeDirectory" graphapi:"nullable"`
	// ShownPaths are pages the session was shown in its last few prompts,
	// which are not shown again.
	ShownPaths []string `json:"shownPaths" graphapi:"nullable"`
	// IsEverywhere recalls from the whole graph rather than the checkout's
	// project and what it links to.
	IsEverywhere *bool `json:"isEverywhere" graphapi:"nullable"`
}

// CaptureAgentCodingSessionArguments is which coding tool answered, and
// where.
type CaptureAgentCodingSessionArguments struct {
	ComputerName string `json:"computerName"`
	// Assistant is the source type of the tool's transcripts: claude-code
	// or codex.
	Assistant string `json:"assistant"`
}

// AgentCodingContext is what a coding session is shown.
type AgentCodingContext struct {
	// ProjectPath is the page of the checkout the directory is in, and
	// CheckoutDirectory where that checkout is; empty where memory knows
	// no checkout holding the directory.
	ProjectPath       string `json:"projectPath" graphapi:"nullable"`
	CheckoutDirectory string `json:"checkoutDirectory" graphapi:"nullable"`
	// Pages are the pages shown and the facts shown from each.
	Pages []*RecalledAgentPage `json:"pages"`
	// Lessons are the lessons shown, a line each.
	Lessons []string `json:"lessons"`
	// LastSession is the previous session in the directory; only when a
	// session starts.
	LastSession *AgentCodingSession `json:"lastSession" graphapi:"nullable"`
	// ShownPaths is every page shown, for the session to pass back so
	// they are not shown again for a while.
	ShownPaths []string `json:"shownPaths"`
	// Text is the whole of it as the session reads it; empty where there
	// is nothing to show.
	Text string `json:"text"`
}

// AgentCodingSession is a previous coding session in a directory.
type AgentCodingSession struct {
	Title        string    `json:"title"`
	Assistant    string    `json:"assistant"`
	LastActiveAt time.Time `json:"lastActiveAt"`
	// Requests are the person's last few requests in it, oldest first.
	Requests []string `json:"requests"`
	// LastAnswer is the start of the assistant's last answer.
	LastAnswer string `json:"lastAnswer" graphapi:"nullable"`
}

func (self *graph) ReadAgentCodingContext(ctx context.Context, arguments ReadAgentCodingContextArguments) (*AgentCodingContext, error) {
	principal, found, err := self.requireRecallPerson(ctx)
	if err != nil {
		return nil, err
	}
	request := &agent.CodingRequest{Directory: arguments.Directory, ComputerName: arguments.ComputerName, HomeDirectory: arguments.HomeDirectory, SessionID: arguments.SessionID}
	return self.codingContext(ctx, principal, found, request, false)
}

func (self *graph) RecallAgentCodingMemory(ctx context.Context, arguments RecallAgentCodingMemoryArguments) (*AgentCodingContext, error) {
	principal, found, err := self.requireRecallPerson(ctx)
	if err != nil {
		return nil, err
	}
	request := &agent.CodingRequest{
		Directory: arguments.Directory, ComputerName: arguments.ComputerName, HomeDirectory: arguments.HomeDirectory,
		Prompt: arguments.Prompt, ShownPaths: arguments.ShownPaths, IsEverywhere: arguments.IsEverywhere != nil && *arguments.IsEverywhere,
	}
	return self.codingContext(ctx, principal, found, request, true)
}

// codingContext answers both queries for a person already checked in a
// short read phase: it does the agent's work outside that phase (recall
// embeds the prompt), and checks again before releasing what it found, as
// RecallAgentMemory does.
func (self *graph) codingContext(ctx context.Context, principal *api.Principal, found *models.Agent, request *agent.CodingRequest, isPrompt bool) (*AgentCodingContext, error) {
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	if strings.TrimSpace(request.Directory) == "" {
		return nil, fmt.Errorf("%w: a directory is needed", api.ErrInvalidArguments)
	}
	var shown *agent.CodingContext
	var err error
	if isPrompt {
		shown, err = worker.CodingPromptRecall(ctx, found, principal.User, request)
	} else {
		shown, err = worker.CodingSessionStart(ctx, found, principal.User, request)
	}
	if err != nil {
		return nil, err
	}
	_, current, err := self.requireRecallPerson(ctx)
	if err != nil {
		return nil, err
	}
	if current.ID != found.ID {
		return nil, agent.ErrUnavailable
	}
	return codingContextOf(shown), nil
}

func codingContextOf(shown *agent.CodingContext) *AgentCodingContext {
	result := &AgentCodingContext{
		ProjectPath: shown.ProjectPath, CheckoutDirectory: shown.CheckoutDirectory,
		Pages: make([]*RecalledAgentPage, 0, len(shown.Pages)), Lessons: shown.Lessons, ShownPaths: shown.ShownPaths, Text: shown.Text,
	}
	for _, page := range shown.Pages {
		carried := &RecalledAgentPage{Path: page.Path, Summary: page.Summary, Overview: page.Overview, Facts: make([]*RecalledAgentFact, 0, len(page.Facts))}
		for _, fact := range page.Facts {
			carried.Facts = append(carried.Facts, &RecalledAgentFact{Number: fact.Number, Text: fact.Text})
		}
		result.Pages = append(result.Pages, carried)
	}
	if session := shown.LastSession; session != nil {
		result.LastSession = &AgentCodingSession{Title: session.Title, Assistant: session.Assistant, LastActiveAt: session.LastActiveAt, Requests: session.Requests, LastAnswer: session.LastAnswer}
		if result.LastSession.Requests == nil {
			result.LastSession.Requests = []string{}
		}
	}
	return result
}

func (self *graph) CaptureAgentCodingSession(ctx context.Context, arguments CaptureAgentCodingSessionArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	if !slices.Contains(agent.CodingAssistants, arguments.Assistant) {
		return false, fmt.Errorf("%w: the assistant is one of %s", api.ErrInvalidArguments, strings.Join(agent.CodingAssistants, ", "))
	}
	if strings.TrimSpace(arguments.ComputerName) == "" {
		return false, fmt.Errorf("%w: a computer name is needed", api.ErrInvalidArguments)
	}
	return agent.CaptureCodingSession(self.writing(ctx), found.ID, arguments.ComputerName, arguments.Assistant)
}
