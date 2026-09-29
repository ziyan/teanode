package client

import (
	"context"
	"time"
)

// The surveys and subagents the agent started and did not wait for, and
// the surveys the command line starts: to start, read and stop.

// AgentBackgroundWork is one piece of background work, with its result
// when it was read one at a time.
type AgentBackgroundWork struct {
	ID string `json:"id"`
	// WorkKind is survey or subagent, and WorkStatus queued, running,
	// done, failed or stopped.
	WorkKind       string     `json:"workKind"`
	WorkStatus     string     `json:"workStatus"`
	Title          string     `json:"title"`
	ConversationID string     `json:"conversationId"`
	Question       string     `json:"question,omitempty"`
	ScopePath      string     `json:"scopePath,omitempty"`
	Prompt         string     `json:"prompt,omitempty"`
	ResultText     string     `json:"resultText,omitempty"`
	RunIDs         []string   `json:"runIds,omitempty"`
	ErrorMessage   string     `json:"errorMessage,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	StartedAt      *time.Time `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
	WokenAt        *time.Time `json:"wokenAt"`
}

// IsFinished says whether it will not run again.
func (self *AgentBackgroundWork) IsFinished() bool {
	switch self.WorkStatus {
	case "done", "failed", "stopped":
		return true
	}
	return false
}

const backgroundWorkFields = `id workKind workStatus title conversationId createdAt startedAt finishedAt wokenAt`

// The documents the command line sends, exported so a test can check them
// against the schema.
const (
	DocumentStartAgentSurvey = `mutation ($question: String!, $scopePath: String) {
		StartAgentSurvey(question: $question, scopePath: $scopePath) { ` + backgroundWorkFields + ` question scopePath }
	}`
	DocumentListAgentBackgroundWork = `query ($first: Int) {
		ListAgentBackgroundWork(first: $first) { ` + backgroundWorkFields + ` errorMessage }
	}`
	DocumentGetAgentBackgroundWork = `query ($id: String!) {
		GetAgentBackgroundWork(id: $id) {
			` + backgroundWorkFields + `
			question scopePath prompt resultText runIds errorMessage
		}
	}`
	DocumentStopAgentBackgroundWork = `mutation ($id: String!) {
		StopAgentBackgroundWork(id: $id) { ` + backgroundWorkFields + ` errorMessage }
	}`
)

// StartAgentSurvey starts a survey of a whole area of the graph, a page's
// path or everything when scopePath is empty, and returns at once; its
// report is read with GetAgentBackgroundWork once it is done.
func StartAgentSurvey(ctx context.Context, connection *Client, question, scopePath string) (*AgentBackgroundWork, error) {
	var result struct {
		StartAgentSurvey *AgentBackgroundWork `json:"StartAgentSurvey"`
	}
	variables := map[string]any{"question": question}
	if scopePath != "" {
		variables["scopePath"] = scopePath
	}
	if err := connection.Execute(ctx, DocumentStartAgentSurvey, variables, &result); err != nil {
		return nil, err
	}
	return result.StartAgentSurvey, nil
}

// ListAgentBackgroundWork is the agent's background work, newest first,
// without results; first zero leaves how many to the server.
func ListAgentBackgroundWork(ctx context.Context, connection *Client, first int) ([]*AgentBackgroundWork, error) {
	var result struct {
		ListAgentBackgroundWork []*AgentBackgroundWork `json:"ListAgentBackgroundWork"`
	}
	variables := map[string]any{}
	if first > 0 {
		variables["first"] = first
	}
	if err := connection.Execute(ctx, DocumentListAgentBackgroundWork, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentBackgroundWork, nil
}

// GetAgentBackgroundWork is one piece of background work with its result.
func GetAgentBackgroundWork(ctx context.Context, connection *Client, id string) (*AgentBackgroundWork, error) {
	var result struct {
		GetAgentBackgroundWork *AgentBackgroundWork `json:"GetAgentBackgroundWork"`
	}
	if err := connection.Execute(ctx, DocumentGetAgentBackgroundWork, map[string]any{"id": id}, &result); err != nil {
		return nil, err
	}
	return result.GetAgentBackgroundWork, nil
}

// StopAgentBackgroundWork stops one that is queued or running.
func StopAgentBackgroundWork(ctx context.Context, connection *Client, id string) (*AgentBackgroundWork, error) {
	var result struct {
		StopAgentBackgroundWork *AgentBackgroundWork `json:"StopAgentBackgroundWork"`
	}
	if err := connection.Execute(ctx, DocumentStopAgentBackgroundWork, map[string]any{"id": id}, &result); err != nil {
		return nil, err
	}
	return result.StopAgentBackgroundWork, nil
}
