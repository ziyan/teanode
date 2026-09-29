package models

import "time"

// Background work is a survey or a subagent the agent started and did not
// wait for. A queued job runs it; when it finishes, the conversation that
// started it is woken with what came of it, the way an ended background
// command wakes it. docs/planning/background-work-execplan.md is the
// design.

// AgentBackgroundWorkKind is what a piece of background work is.
type AgentBackgroundWorkKind string

const (
	// BackgroundWorkSurvey is a survey of an area of the graph.
	BackgroundWorkSurvey AgentBackgroundWorkKind = "survey"
	// BackgroundWorkSubagent is a piece of work handed to a run of its own.
	BackgroundWorkSubagent AgentBackgroundWorkKind = "subagent"
)

// AgentBackgroundWorkStatus is where a piece of background work is.
type AgentBackgroundWorkStatus string

const (
	BackgroundWorkQueued  AgentBackgroundWorkStatus = "queued"
	BackgroundWorkRunning AgentBackgroundWorkStatus = "running"
	BackgroundWorkDone    AgentBackgroundWorkStatus = "done"
	BackgroundWorkFailed  AgentBackgroundWorkStatus = "failed"
	// BackgroundWorkStopped was stopped by the person or the agent before
	// it finished. It wakes nothing: whoever stopped it knows.
	BackgroundWorkStopped AgentBackgroundWorkStatus = "stopped"
)

// IsFinished says whether the work will not run again.
func (self AgentBackgroundWorkStatus) IsFinished() bool {
	return self == BackgroundWorkDone || self == BackgroundWorkFailed || self == BackgroundWorkStopped
}

// AgentBackgroundWorkRequest is what was asked: a survey's question and
// scope, or a subagent's prompt and what it may use, which is fixed when
// it is started so that a run after a restart has the same tools.
type AgentBackgroundWorkRequest struct {
	Question  string `json:"question,omitempty"`
	ScopePath string `json:"scopePath,omitempty"`

	Prompt            string   `json:"prompt,omitempty"`
	AllowedToolNames  []string `json:"allowedToolNames,omitempty"`
	ReadOnlyToolNames []string `json:"readOnlyToolNames,omitempty"`
	IsReadOnly        bool     `json:"isReadOnly,omitempty"`
}

// AgentBackgroundWork is one piece of background work.
type AgentBackgroundWork struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`

	// ConversationID is the conversation to wake when it finishes; empty
	// when it was started from the API, which wakes nothing.
	ConversationID string                     `json:"conversationId"`
	WorkKind       AgentBackgroundWorkKind    `json:"workKind"`
	Title          string                     `json:"title"`
	WorkRequest    AgentBackgroundWorkRequest `json:"workRequest"`

	// IsPersonPresent says the turn that started it had the person
	// present; only such work wakes its conversation.
	IsPersonPresent bool                      `json:"isPersonPresent"`
	WorkStatus      AgentBackgroundWorkStatus `json:"workStatus"`

	// ResultText is the survey's report or the subagent's answer, RunIDs
	// every run it made, and ErrorMessage why it failed.
	ResultText   string   `json:"resultText"`
	RunIDs       []string `json:"runIds"`
	ErrorMessage string   `json:"errorMessage"`

	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	WokenAt    *time.Time `json:"wokenAt,omitempty"`
}
