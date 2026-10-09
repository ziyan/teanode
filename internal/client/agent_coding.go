package client

import (
	"context"
	"time"
)

// What a coding session (Claude Code, Codex) is shown of the person's
// memory, for the hooks `teanode hook` runs in those tools.

const (
	agentCodingContextSelection = `projectPath checkoutDirectory readFrom
			pages { path summary overview facts { number text } }
			lessons shownPaths text
			lastSession { title assistant lastActiveAt requests lastAnswer }`

	DocumentReadAgentCodingContext = `query ($directory: String!, $computerName: String, $homeDirectory: String, $sessionId: String, $remoteUrls: [String!], $head: String, $checkoutRoot: String) {
		ReadAgentCodingContext(directory: $directory, computerName: $computerName, homeDirectory: $homeDirectory, sessionId: $sessionId, remoteUrls: $remoteUrls, head: $head, checkoutRoot: $checkoutRoot) {
			` + agentCodingContextSelection + `
		}
	}`
	DocumentRecallAgentCodingMemory = `query ($prompt: String!, $directory: String!, $computerName: String, $homeDirectory: String, $shownPaths: [String!], $isEverywhere: Boolean, $remoteUrls: [String!], $head: String, $checkoutRoot: String) {
		RecallAgentCodingMemory(prompt: $prompt, directory: $directory, computerName: $computerName, homeDirectory: $homeDirectory, shownPaths: $shownPaths, isEverywhere: $isEverywhere, remoteUrls: $remoteUrls, head: $head, checkoutRoot: $checkoutRoot) {
			` + agentCodingContextSelection + `
		}
	}`
	DocumentCaptureAgentCodingSession = `mutation ($computerName: String!, $assistant: String!) {
		CaptureAgentCodingSession(computerName: $computerName, assistant: $assistant)
	}`
)

// AgentCodingPlace is where a coding session runs.
type AgentCodingPlace struct {
	Directory     string
	ComputerName  string
	HomeDirectory string
	SessionID     string

	// RemoteURLs, Head and CheckoutRoot are what git says about the
	// checkout the directory is in, where it is one.
	RemoteURLs   []string
	Head         string
	CheckoutRoot string
}

// AgentCodingContext is what a coding session is shown.
type AgentCodingContext struct {
	ProjectPath       string               `json:"projectPath"`
	CheckoutDirectory string               `json:"checkoutDirectory"`
	ReadFrom          string               `json:"readFrom"`
	Pages             []*AgentRecalledPage `json:"pages"`
	Lessons           []string             `json:"lessons"`
	ShownPaths        []string             `json:"shownPaths"`
	Text              string               `json:"text"`
	LastSession       *AgentCodingSession  `json:"lastSession"`
}

// AgentCodingSession is the previous session in a directory.
type AgentCodingSession struct {
	Title        string    `json:"title"`
	Assistant    string    `json:"assistant"`
	LastActiveAt time.Time `json:"lastActiveAt"`
	Requests     []string  `json:"requests"`
	LastAnswer   string    `json:"lastAnswer"`
}

func codingPlaceVariables(place *AgentCodingPlace) map[string]any {
	variables := map[string]any{"directory": place.Directory}
	if place.ComputerName != "" {
		variables["computerName"] = place.ComputerName
	}
	if place.HomeDirectory != "" {
		variables["homeDirectory"] = place.HomeDirectory
	}
	if len(place.RemoteURLs) > 0 {
		variables["remoteUrls"] = place.RemoteURLs
	}
	if place.Head != "" {
		variables["head"] = place.Head
	}
	if place.CheckoutRoot != "" {
		variables["checkoutRoot"] = place.CheckoutRoot
	}
	return variables
}

// ReadAgentCodingContext is what a session starting in a directory is
// shown. It changes nothing.
func ReadAgentCodingContext(ctx context.Context, connection *Client, place *AgentCodingPlace) (*AgentCodingContext, error) {
	variables := codingPlaceVariables(place)
	if place.SessionID != "" {
		variables["sessionId"] = place.SessionID
	}
	var result struct {
		ReadAgentCodingContext *AgentCodingContext `json:"ReadAgentCodingContext"`
	}
	if err := connection.Execute(ctx, DocumentReadAgentCodingContext, variables, &result); err != nil {
		return nil, err
	}
	return result.ReadAgentCodingContext, nil
}

// RecallAgentCodingMemory is what a prompt typed in a coding session
// recalls, less the pages in shownPaths. It changes nothing.
func RecallAgentCodingMemory(ctx context.Context, connection *Client, place *AgentCodingPlace, prompt string, shownPaths []string, isEverywhere bool) (*AgentCodingContext, error) {
	variables := codingPlaceVariables(place)
	variables["prompt"] = prompt
	if len(shownPaths) > 0 {
		variables["shownPaths"] = shownPaths
	}
	if isEverywhere {
		variables["isEverywhere"] = true
	}
	var result struct {
		RecallAgentCodingMemory *AgentCodingContext `json:"RecallAgentCodingMemory"`
	}
	if err := connection.Execute(ctx, DocumentRecallAgentCodingMemory, variables, &result); err != nil {
		return nil, err
	}
	return result.RecallAgentCodingMemory, nil
}

// CaptureAgentCodingSession asks the computer's sources of a coding
// tool's transcripts to read again now. It says whether one was asked.
func CaptureAgentCodingSession(ctx context.Context, connection *Client, computerName, assistant string) (bool, error) {
	var result struct {
		CaptureAgentCodingSession bool `json:"CaptureAgentCodingSession"`
	}
	err := connection.Execute(ctx, DocumentCaptureAgentCodingSession, map[string]any{"computerName": computerName, "assistant": assistant}, &result)
	return result.CaptureAgentCodingSession, err
}
