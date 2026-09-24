package client

import (
	"context"
	"time"
)

// The commands the agent's shell left running in the background on the
// person's attached computers, for the person to see and to stop.

// AgentBackgroundCommand is one background command, with its output when
// it was read.
type AgentBackgroundCommand struct {
	Computer       string     `json:"computer"`
	ID             string     `json:"id"`
	Command        string     `json:"command"`
	Directory      string     `json:"directory"`
	ConversationID string     `json:"conversationId"`
	StartedAt      time.Time  `json:"startedAt"`
	EndedAt        *time.Time `json:"endedAt"`
	IsRunning      bool       `json:"isRunning"`
	ExitCode       int        `json:"exitCode"`
	// StopReason is "stopped" or "lifetime" when it did not end by itself,
	// and empty otherwise.
	StopReason string `json:"stopReason"`

	Stdout            string `json:"stdout,omitempty"`
	Stderr            string `json:"stderr,omitempty"`
	IsStdoutTruncated bool   `json:"isStdoutTruncated,omitempty"`
	IsStderrTruncated bool   `json:"isStderrTruncated,omitempty"`
	StdoutByteCount   int    `json:"stdoutByteCount,omitempty"`
	StderrByteCount   int    `json:"stderrByteCount,omitempty"`
}

const backgroundCommandFields = `computer id command directory conversationId startedAt endedAt isRunning exitCode stopReason`

// The documents the command line sends, exported so a test can check them
// against the schema.
const (
	DocumentListAgentBackgroundCommands = `query ($conversationId: String) {
		ListAgentBackgroundCommands(conversationId: $conversationId) { ` + backgroundCommandFields + ` }
	}`
	DocumentReadAgentBackgroundCommand = `query ($computer: String!, $id: String!, $tailBytes: Int) {
		ReadAgentBackgroundCommand(computer: $computer, id: $id, tailBytes: $tailBytes) {
			` + backgroundCommandFields + `
			stdout stderr isStdoutTruncated isStderrTruncated stdoutByteCount stderrByteCount
		}
	}`
	DocumentStopAgentBackgroundCommand = `mutation ($computer: String!, $id: String!) {
		StopAgentBackgroundCommand(computer: $computer, id: $id) { computer id command isRunning stopReason exitCode endedAt }
	}`
)

// ListAgentBackgroundCommands is what runs, or ended lately, in the
// background on the caller's computers, newest first; only what one
// conversation started when conversationId is given.
func ListAgentBackgroundCommands(ctx context.Context, connection *Client, conversationId string) ([]*AgentBackgroundCommand, error) {
	var result struct {
		ListAgentBackgroundCommands []*AgentBackgroundCommand `json:"ListAgentBackgroundCommands"`
	}
	variables := map[string]any{}
	if conversationId != "" {
		variables["conversationId"] = conversationId
	}
	if err := connection.Execute(ctx, DocumentListAgentBackgroundCommands, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentBackgroundCommands, nil
}

// ReadAgentBackgroundCommand is one background command with the last
// tailBytes of each of its streams; zero leaves the amount to the server.
func ReadAgentBackgroundCommand(ctx context.Context, connection *Client, computer, id string, tailBytes int) (*AgentBackgroundCommand, error) {
	var result struct {
		ReadAgentBackgroundCommand *AgentBackgroundCommand `json:"ReadAgentBackgroundCommand"`
	}
	variables := map[string]any{"computer": computer, "id": id}
	if tailBytes > 0 {
		variables["tailBytes"] = tailBytes
	}
	if err := connection.Execute(ctx, DocumentReadAgentBackgroundCommand, variables, &result); err != nil {
		return nil, err
	}
	return result.ReadAgentBackgroundCommand, nil
}

// StopAgentBackgroundCommand stops one background command.
func StopAgentBackgroundCommand(ctx context.Context, connection *Client, computer, id string) (*AgentBackgroundCommand, error) {
	var result struct {
		StopAgentBackgroundCommand *AgentBackgroundCommand `json:"StopAgentBackgroundCommand"`
	}
	if err := connection.Execute(ctx, DocumentStopAgentBackgroundCommand, map[string]any{"computer": computer, "id": id}, &result); err != nil {
		return nil, err
	}
	return result.StopAgentBackgroundCommand, nil
}
