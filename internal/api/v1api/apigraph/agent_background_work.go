package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/models"
)

// Background work: the surveys and subagents the agent started and did
// not wait for, and the surveys a person starts from the command line,
// which run the same way. The rows are here, in the database, so every
// instance reads the same.

// AgentBackgroundWorkQuery reads it.
type AgentBackgroundWorkQuery interface {
	// The caller's agent's background work, newest first: what it is,
	// where it stands, and when; its result is read one at a time. Needs
	// agent:use.
	ListAgentBackgroundWork(ctx context.Context, arguments ListAgentBackgroundWorkArguments) ([]*AgentBackgroundWorkView, error)

	// One piece of it with its result: a survey's report or a subagent's
	// answer, and the runs it made. What a client asks every few seconds
	// to wait for a survey it started. Needs agent:use.
	GetAgentBackgroundWork(ctx context.Context, arguments GetAgentBackgroundWorkArguments) (*AgentBackgroundWorkView, error)
}

// AgentBackgroundWorkMutation starts and stops it.
type AgentBackgroundWorkMutation interface {
	// Start a survey of a whole area of the graph -- a theme, a page and
	// what is under it, or everything when scopePath is left out -- and
	// return at once with the work that runs it; its report is read with
	// GetAgentBackgroundWork once it is done. The same survey as
	// SurveyAgentMemory, without an HTTP request held open for the minutes
	// it takes. It wakes no conversation. Needs agent:use.
	StartAgentSurvey(ctx context.Context, arguments StartAgentSurveyArguments) (*AgentBackgroundWorkView, error)

	// Stop one that is queued or running. Stopped work wakes nothing.
	// Work that had already finished is returned as it is. Needs
	// agent:use.
	StopAgentBackgroundWork(ctx context.Context, arguments GetAgentBackgroundWorkArguments) (*AgentBackgroundWorkView, error)
}

type ListAgentBackgroundWorkArguments struct {
	First int `json:"first" graphapi:"nullable"`
}

// GetAgentBackgroundWorkArguments name one.
type GetAgentBackgroundWorkArguments struct {
	ID string `json:"id"`
}

// StartAgentSurveyArguments are the question and where, as a survey
// takes them.
type StartAgentSurveyArguments struct {
	Question  string `json:"question"`
	ScopePath string `json:"scopePath" graphapi:"nullable"`
}

// AgentBackgroundWorkView is one piece of background work.
type AgentBackgroundWorkView struct {
	ID string `json:"id"`

	// WorkKind is survey or subagent, and WorkStatus queued, running,
	// done, failed or stopped.
	WorkKind   string `json:"workKind"`
	WorkStatus string `json:"workStatus"`
	Title      string `json:"title"`

	// ConversationID is the conversation it wakes when it finishes; empty
	// for work started from the API.
	ConversationID string `json:"conversationId"`

	// What was asked: a survey's question and scope, a subagent's prompt.
	Question  string `json:"question"`
	ScopePath string `json:"scopePath"`
	Prompt    string `json:"prompt"`

	// ResultText is the report or the answer, RunIDs the runs it made,
	// each a transcript, and ErrorMessage why it failed.
	ResultText   string   `json:"resultText"`
	RunIDs       []string `json:"runIds"`
	ErrorMessage string   `json:"errorMessage"`

	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt" graphapi:"nullable"`
	FinishedAt *time.Time `json:"finishedAt" graphapi:"nullable"`
	WokenAt    *time.Time `json:"wokenAt" graphapi:"nullable"`
}

// backgroundWorkListMost is the most a list gives.
const backgroundWorkListMost = 100

func (self *graph) ListAgentBackgroundWork(ctx context.Context, arguments ListAgentBackgroundWorkArguments) ([]*AgentBackgroundWorkView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	first := arguments.First
	if first <= 0 || first > backgroundWorkListMost {
		first = backgroundWorkListMost
	}
	works, err := self.transaction(ctx).ListAgentBackgroundWork(found.ID, first)
	if err != nil {
		return nil, err
	}
	views := make([]*AgentBackgroundWorkView, 0, len(works))
	for _, work := range works {
		views = append(views, backgroundWorkView(work))
	}
	return views, nil
}

func (self *graph) GetAgentBackgroundWork(ctx context.Context, arguments GetAgentBackgroundWorkArguments) (*AgentBackgroundWorkView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	work, err := self.transaction(ctx).GetAgentBackgroundWork(found.ID, strings.TrimSpace(arguments.ID))
	if err != nil {
		return nil, err
	}
	if work == nil {
		return nil, fmt.Errorf("there is no background work %q", arguments.ID)
	}
	return backgroundWorkView(work), nil
}

func (self *graph) StartAgentSurvey(ctx context.Context, arguments StartAgentSurveyArguments) (*AgentBackgroundWorkView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil || !worker.CanSurvey() {
		return nil, agent.ErrUnavailable
	}
	if strings.TrimSpace(arguments.Question) == "" {
		return nil, fmt.Errorf("ask a question to survey")
	}
	work, err := worker.QueueBackgroundWork(self.transaction(ctx), agent.NewBackgroundSurvey(found.ID, arguments.Question, arguments.ScopePath))
	if err != nil {
		return nil, err
	}
	return backgroundWorkView(work), nil
}

func (self *graph) StopAgentBackgroundWork(ctx context.Context, arguments GetAgentBackgroundWorkArguments) (*AgentBackgroundWorkView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	work, err := worker.StopBackgroundWork(self.transaction(ctx), found.ID, strings.TrimSpace(arguments.ID))
	if err != nil {
		return nil, err
	}
	return backgroundWorkView(work), nil
}

func backgroundWorkView(work *models.AgentBackgroundWork) *AgentBackgroundWorkView {
	return &AgentBackgroundWorkView{
		ID: work.ID, WorkKind: string(work.WorkKind), WorkStatus: string(work.WorkStatus), Title: work.Title,
		ConversationID: work.ConversationID,
		Question:       work.WorkRequest.Question, ScopePath: work.WorkRequest.ScopePath, Prompt: work.WorkRequest.Prompt,
		ResultText: work.ResultText, RunIDs: nonNil(work.RunIDs), ErrorMessage: work.ErrorMessage,
		CreatedAt: work.CreatedAt, StartedAt: work.StartedAt, FinishedAt: work.FinishedAt, WokenAt: work.WokenAt,
	}
}
