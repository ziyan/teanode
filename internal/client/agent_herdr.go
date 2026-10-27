package client

import (
	"context"
	"time"
)

// The person's herdr sessions: the Claude Code and Codex sessions in
// herdr's panes on their attached computers.

// AgentHerdrSession is one coding session in one pane of one computer.
type AgentHerdrSession struct {
	Computer          string              `json:"computer"`
	PaneID            string              `json:"paneId"`
	PaneName          string              `json:"paneName"`
	WorkspaceID       string              `json:"workspaceId"`
	WorkspaceLabel    string              `json:"workspaceLabel"`
	WorkspaceNumber   int                 `json:"workspaceNumber"`
	TabID             string              `json:"tabId"`
	TabLabel          string              `json:"tabLabel"`
	TabNumber         int                 `json:"tabNumber"`
	IsTabFocused      bool                `json:"isTabFocused"`
	CodingAgentKind   string              `json:"codingAgentKind"`
	CodingSessionID   string              `json:"codingSessionId"`
	HerdrSessionState string              `json:"herdrSessionState"`
	HerdrAgentStatus  string              `json:"herdrAgentStatus"`
	PaneTitle         string              `json:"paneTitle"`
	WorkingDirectory  string              `json:"workingDirectory"`
	TranscriptPath    string              `json:"transcriptPath"`
	IsWatched         bool                `json:"isWatched"`
	Question          *AgentHerdrQuestion `json:"question"`
}

// AgentHerdrQuestion is the question a session waits on.
type AgentHerdrQuestion struct {
	QuestionFingerprint string                      `json:"questionFingerprint"`
	HerdrQuestionKind   string                      `json:"herdrQuestionKind"`
	QuestionText        string                      `json:"questionText"`
	IsMultipleChoice    bool                        `json:"isMultipleChoice"`
	IsFromTranscript    bool                        `json:"isFromTranscript"`
	Options             []*AgentHerdrQuestionOption `json:"options"`
}

// AgentHerdrQuestionOption is one option of a question.
type AgentHerdrQuestionOption struct {
	OptionNumber      int    `json:"optionNumber"`
	OptionLabel       string `json:"optionLabel"`
	OptionDescription string `json:"optionDescription"`
	HerdrOptionKind   string `json:"herdrOptionKind"`
}

// AgentHerdrList is every session, and the computers that were asked.
type AgentHerdrList struct {
	ComputerNames       []string             `json:"computerNames"`
	FailedComputerNames []string             `json:"failedComputerNames"`
	Sessions            []*AgentHerdrSession `json:"sessions"`
}

// AgentHerdrTurn is one thing said or done in a session.
type AgentHerdrTurn struct {
	HerdrTurnRole  string     `json:"herdrTurnRole"`
	TurnText       string     `json:"turnText"`
	TurnAt         *time.Time `json:"turnAt"`
	ToolInputText  string     `json:"toolInputText"`
	ToolResultText string     `json:"toolResultText"`
}

// AgentHerdrRead is a session's last turns.
type AgentHerdrRead struct {
	HerdrSession *AgentHerdrSession `json:"herdrSession"`
	Turns        []*AgentHerdrTurn  `json:"turns"`
	IsTruncated  bool               `json:"isTruncated"`
}

// AgentHerdrSubagent is one agent a session started, or one an agent
// started.
type AgentHerdrSubagent struct {
	SubagentID       string     `json:"subagentId"`
	ParentSubagentID string     `json:"parentSubagentId"`
	SubagentType     string     `json:"subagentType"`
	SubagentName     string     `json:"subagentName"`
	TaskText         string     `json:"taskText"`
	ModelName        string     `json:"modelName"`
	SubagentState    string     `json:"subagentState"`
	SpawnDepth       int        `json:"spawnDepth"`
	StartedAt        *time.Time `json:"startedAt"`
	EndedAt          *time.Time `json:"endedAt"`
	ToolNames        []string   `json:"toolNames"`
	ToolCallCount    int        `json:"toolCallCount"`
}

// AgentHerdrSubagents is the agents of a session's current request.
type AgentHerdrSubagents struct {
	HerdrSession     *AgentHerdrSession    `json:"herdrSession"`
	SessionModelName string                `json:"sessionModelName"`
	RequestStartedAt *time.Time            `json:"requestStartedAt"`
	Subagents        []*AgentHerdrSubagent `json:"subagents"`
	IsTruncated      bool                  `json:"isTruncated"`
}

// AgentHerdrScreen is a session's screen.
type AgentHerdrScreen struct {
	HerdrSession *AgentHerdrSession `json:"herdrSession"`
	ScreenText   string             `json:"screenText"`
}

// AgentHerdrWait is a session after a wait.
type AgentHerdrWait struct {
	HerdrSession *AgentHerdrSession `json:"herdrSession"`
	IsTimedOut   bool               `json:"isTimedOut"`
}

// AgentHerdrAnswer says whether the question took its answer.
type AgentHerdrAnswer struct {
	HerdrSession     *AgentHerdrSession `json:"herdrSession"`
	IsAnswerAccepted bool               `json:"isAnswerAccepted"`
	AnsweredWith     string             `json:"answeredWith"`
}

// AgentHerdrSetup says what setting the hooks up did.
type AgentHerdrSetup struct {
	Computer       string   `json:"computer"`
	IsInstalled    bool     `json:"isInstalled"`
	SettingsPath   string   `json:"settingsPath"`
	ScriptPath     string   `json:"scriptPath"`
	BackupPath     string   `json:"backupPath"`
	HookEventNames []string `json:"hookEventNames"`
}

// HerdrSessionFields are a session's fields, with its question, for every
// document that answers with one; the dashboard asks for the same.
const HerdrSessionFields = `computer paneId paneName workspaceId workspaceLabel workspaceNumber tabId tabLabel tabNumber isTabFocused codingAgentKind codingSessionId herdrSessionState herdrAgentStatus paneTitle workingDirectory transcriptPath isWatched
	question { questionFingerprint herdrQuestionKind questionText isMultipleChoice isFromTranscript options { optionNumber optionLabel optionDescription herdrOptionKind } }`

// The documents the command line sends, exported so a test can check them
// against the schema.
const (
	DocumentListAgentHerdrSessions = `query ($computer: String) {
		ListAgentHerdrSessions(computer: $computer) { computerNames failedComputerNames sessions { ` + HerdrSessionFields + ` } }
	}`
	DocumentReadAgentHerdrSession = `query ($computer: String, $paneId: String!, $turnCount: Int, $shouldIncludeToolDetail: Boolean) {
		ReadAgentHerdrSession(computer: $computer, paneId: $paneId, turnCount: $turnCount, shouldIncludeToolDetail: $shouldIncludeToolDetail) {
			herdrSession { ` + HerdrSessionFields + ` } turns { herdrTurnRole turnText turnAt toolInputText toolResultText } isTruncated
		}
	}`
	DocumentReadAgentHerdrSubagents = `query ($computer: String, $paneId: String!) {
		ReadAgentHerdrSubagents(computer: $computer, paneId: $paneId) {
			herdrSession { ` + HerdrSessionFields + ` } sessionModelName requestStartedAt isTruncated
			subagents { subagentId parentSubagentId subagentType subagentName taskText modelName subagentState spawnDepth startedAt endedAt toolNames toolCallCount }
		}
	}`
	DocumentReadAgentHerdrScreen = `query ($computer: String, $paneId: String!, $lineCount: Int, $shouldKeepColor: Boolean) {
		ReadAgentHerdrScreen(computer: $computer, paneId: $paneId, lineCount: $lineCount, shouldKeepColor: $shouldKeepColor) {
			herdrSession { ` + HerdrSessionFields + ` } screenText
		}
	}`
	DocumentSendAgentHerdrSession = `mutation ($computer: String, $paneId: String!, $text: String, $attachmentIds: [String!]) {
		SendAgentHerdrSession(computer: $computer, paneId: $paneId, text: $text, attachmentIds: $attachmentIds) { ` + HerdrSessionFields + ` }
	}`
	DocumentWaitAgentHerdrSession = `query ($computer: String, $paneId: String!, $waitSeconds: Int) {
		WaitAgentHerdrSession(computer: $computer, paneId: $paneId, waitSeconds: $waitSeconds) {
			herdrSession { ` + HerdrSessionFields + ` } isTimedOut
		}
	}`
	DocumentAnswerAgentHerdrQuestion = `mutation ($computer: String, $paneId: String!, $questionFingerprint: String!, $optionNumbers: [Int!], $optionLabels: [String!], $freeText: String) {
		AnswerAgentHerdrQuestion(computer: $computer, paneId: $paneId, questionFingerprint: $questionFingerprint, optionNumbers: $optionNumbers, optionLabels: $optionLabels, freeText: $freeText) {
			herdrSession { ` + HerdrSessionFields + ` } isAnswerAccepted answeredWith
		}
	}`
	DocumentWatchAgentHerdrSession = `mutation ($computer: String, $paneId: String!, $conversationId: String) {
		WatchAgentHerdrSession(computer: $computer, paneId: $paneId, conversationId: $conversationId) { ` + HerdrSessionFields + ` }
	}`
	DocumentOpenAgentHerdrSession = `mutation ($computer: String, $directory: String, $codingAgentKind: String, $agentName: String, $shouldSkipPermissions: Boolean, $paneId: String) {
		OpenAgentHerdrSession(computer: $computer, directory: $directory, codingAgentKind: $codingAgentKind, agentName: $agentName, shouldSkipPermissions: $shouldSkipPermissions, paneId: $paneId) { ` + HerdrSessionFields + ` }
	}`
	DocumentFocusAgentHerdrSession = `mutation ($computer: String, $paneId: String!) {
		FocusAgentHerdrSession(computer: $computer, paneId: $paneId) { ` + HerdrSessionFields + ` }
	}`
	DocumentCloseAgentHerdrSession = `mutation ($computer: String, $paneId: String!) {
		CloseAgentHerdrSession(computer: $computer, paneId: $paneId) { ` + HerdrSessionFields + ` }
	}`
	DocumentSetUpAgentHerdrHooks = `mutation ($computer: String, $isRemoval: Boolean) {
		SetUpAgentHerdrHooks(computer: $computer, isRemoval: $isRemoval) { computer isInstalled settingsPath scriptPath backupPath hookEventNames }
	}`
)

// herdrVariables are the variables every herdr document takes, leaving out
// a computer that was not named.
func herdrVariables(computer string, variables map[string]any) map[string]any {
	if computer != "" {
		variables["computer"] = computer
	}
	return variables
}

// ListAgentHerdrSessions is the coding sessions on the caller's computers,
// or on the one named.
func ListAgentHerdrSessions(ctx context.Context, connection *Client, computer string) (*AgentHerdrList, error) {
	var result struct {
		ListAgentHerdrSessions *AgentHerdrList `json:"ListAgentHerdrSessions"`
	}
	if err := connection.Execute(ctx, DocumentListAgentHerdrSessions, herdrVariables(computer, map[string]any{}), &result); err != nil {
		return nil, err
	}
	return result.ListAgentHerdrSessions, nil
}

// ReadAgentHerdrSession is a session's last turnCount turns; zero leaves
// the count to the server. shouldIncludeToolDetail gives each tool turn its
// whole input and what the tool answered.
func ReadAgentHerdrSession(ctx context.Context, connection *Client, computer, paneId string, turnCount int, shouldIncludeToolDetail bool) (*AgentHerdrRead, error) {
	var result struct {
		ReadAgentHerdrSession *AgentHerdrRead `json:"ReadAgentHerdrSession"`
	}
	variables := herdrVariables(computer, map[string]any{"paneId": paneId})
	if turnCount > 0 {
		variables["turnCount"] = turnCount
	}
	if shouldIncludeToolDetail {
		variables["shouldIncludeToolDetail"] = true
	}
	if err := connection.Execute(ctx, DocumentReadAgentHerdrSession, variables, &result); err != nil {
		return nil, err
	}
	return result.ReadAgentHerdrSession, nil
}

// ReadAgentHerdrSubagents is the agents a session started for its current
// request, each with the agent that started it named.
func ReadAgentHerdrSubagents(ctx context.Context, connection *Client, computer, paneId string) (*AgentHerdrSubagents, error) {
	var result struct {
		ReadAgentHerdrSubagents *AgentHerdrSubagents `json:"ReadAgentHerdrSubagents"`
	}
	if err := connection.Execute(ctx, DocumentReadAgentHerdrSubagents, herdrVariables(computer, map[string]any{"paneId": paneId}), &result); err != nil {
		return nil, err
	}
	return result.ReadAgentHerdrSubagents, nil
}

// ReadAgentHerdrScreen is a session's screen, or its last lineCount lines,
// with its colors when shouldKeepColor says so.
func ReadAgentHerdrScreen(ctx context.Context, connection *Client, computer, paneId string, lineCount int, shouldKeepColor bool) (*AgentHerdrScreen, error) {
	var result struct {
		ReadAgentHerdrScreen *AgentHerdrScreen `json:"ReadAgentHerdrScreen"`
	}
	variables := herdrVariables(computer, map[string]any{"paneId": paneId})
	if lineCount > 0 {
		variables["lineCount"] = lineCount
	}
	if shouldKeepColor {
		variables["shouldKeepColor"] = true
	}
	if err := connection.Execute(ctx, DocumentReadAgentHerdrScreen, variables, &result); err != nil {
		return nil, err
	}
	return result.ReadAgentHerdrScreen, nil
}

// SendAgentHerdrSession types text into a session and presses enter; a
// session at work takes it as a message in its turn. The agent's files
// named by attachmentIds are written on the computer and their paths typed
// before the text.
func SendAgentHerdrSession(ctx context.Context, connection *Client, computer, paneId, text string, attachmentIds []string) (*AgentHerdrSession, error) {
	var result struct {
		SendAgentHerdrSession *AgentHerdrSession `json:"SendAgentHerdrSession"`
	}
	variables := herdrVariables(computer, map[string]any{"paneId": paneId, "text": text})
	if len(attachmentIds) > 0 {
		variables["attachmentIds"] = attachmentIds
	}
	if err := connection.Execute(ctx, DocumentSendAgentHerdrSession, variables, &result); err != nil {
		return nil, err
	}
	return result.SendAgentHerdrSession, nil
}

// WaitAgentHerdrSession waits for a session to stop working.
func WaitAgentHerdrSession(ctx context.Context, connection *Client, computer, paneId string, waitSeconds int) (*AgentHerdrWait, error) {
	var result struct {
		WaitAgentHerdrSession *AgentHerdrWait `json:"WaitAgentHerdrSession"`
	}
	variables := herdrVariables(computer, map[string]any{"paneId": paneId})
	if waitSeconds > 0 {
		variables["waitSeconds"] = waitSeconds
	}
	if err := connection.Execute(ctx, DocumentWaitAgentHerdrSession, variables, &result); err != nil {
		return nil, err
	}
	return result.WaitAgentHerdrSession, nil
}

// AnswerAgentHerdrQuestion answers the question with this fingerprint;
// optionLabels, when given, must be the labels of the options chosen.
func AnswerAgentHerdrQuestion(ctx context.Context, connection *Client, computer, paneId, questionFingerprint string, optionNumbers []int, optionLabels []string, freeText string) (*AgentHerdrAnswer, error) {
	var result struct {
		AnswerAgentHerdrQuestion *AgentHerdrAnswer `json:"AnswerAgentHerdrQuestion"`
	}
	variables := herdrVariables(computer, map[string]any{"paneId": paneId, "questionFingerprint": questionFingerprint})
	if len(optionNumbers) > 0 {
		variables["optionNumbers"] = optionNumbers
	}
	if len(optionLabels) > 0 {
		variables["optionLabels"] = optionLabels
	}
	if freeText != "" {
		variables["freeText"] = freeText
	}
	if err := connection.Execute(ctx, DocumentAnswerAgentHerdrQuestion, variables, &result); err != nil {
		return nil, err
	}
	return result.AnswerAgentHerdrQuestion, nil
}

// WatchAgentHerdrSession has a conversation woken when a session next
// finishes: the one named, or the main conversation when none is.
func WatchAgentHerdrSession(ctx context.Context, connection *Client, computer, paneId, conversationId string) (*AgentHerdrSession, error) {
	var result struct {
		WatchAgentHerdrSession *AgentHerdrSession `json:"WatchAgentHerdrSession"`
	}
	variables := herdrVariables(computer, map[string]any{"paneId": paneId})
	if conversationId != "" {
		variables["conversationId"] = conversationId
	}
	if err := connection.Execute(ctx, DocumentWatchAgentHerdrSession, variables, &result); err != nil {
		return nil, err
	}
	return result.WatchAgentHerdrSession, nil
}

// OpenAgentHerdrSession starts a coding agent, "claude" or "codex", in a new
// herdr pane in a directory; agentName may be empty. shouldSkipPermissions
// starts it without asking before it acts. Given paneId, it opens beside
// that pane, and directory and codingAgentKind may be empty: they default
// to that pane's.
func OpenAgentHerdrSession(ctx context.Context, connection *Client, computer, directory, codingAgentKind, agentName string, shouldSkipPermissions bool, paneId string) (*AgentHerdrSession, error) {
	var result struct {
		OpenAgentHerdrSession *AgentHerdrSession `json:"OpenAgentHerdrSession"`
	}
	variables := herdrVariables(computer, map[string]any{"shouldSkipPermissions": shouldSkipPermissions})
	if directory != "" {
		variables["directory"] = directory
	}
	if codingAgentKind != "" {
		variables["codingAgentKind"] = codingAgentKind
	}
	if paneId != "" {
		variables["paneId"] = paneId
	}
	if agentName != "" {
		variables["agentName"] = agentName
	}
	if err := connection.Execute(ctx, DocumentOpenAgentHerdrSession, variables, &result); err != nil {
		return nil, err
	}
	return result.OpenAgentHerdrSession, nil
}

// FocusAgentHerdrSession brings a session's tab forward in herdr on its
// computer.
func FocusAgentHerdrSession(ctx context.Context, connection *Client, computer, paneId string) (*AgentHerdrSession, error) {
	var result struct {
		FocusAgentHerdrSession *AgentHerdrSession `json:"FocusAgentHerdrSession"`
	}
	if err := connection.Execute(ctx, DocumentFocusAgentHerdrSession, herdrVariables(computer, map[string]any{"paneId": paneId}), &result); err != nil {
		return nil, err
	}
	return result.FocusAgentHerdrSession, nil
}

// CloseAgentHerdrSession ends a session's coding agent and closes its pane.
func CloseAgentHerdrSession(ctx context.Context, connection *Client, computer, paneId string) (*AgentHerdrSession, error) {
	var result struct {
		CloseAgentHerdrSession *AgentHerdrSession `json:"CloseAgentHerdrSession"`
	}
	if err := connection.Execute(ctx, DocumentCloseAgentHerdrSession, herdrVariables(computer, map[string]any{"paneId": paneId}), &result); err != nil {
		return nil, err
	}
	return result.CloseAgentHerdrSession, nil
}

// SetUpAgentHerdrHooks puts TeaNode's reporting hooks in on a computer, or
// takes them out.
func SetUpAgentHerdrHooks(ctx context.Context, connection *Client, computer string, isRemoval bool) (*AgentHerdrSetup, error) {
	var result struct {
		SetUpAgentHerdrHooks *AgentHerdrSetup `json:"SetUpAgentHerdrHooks"`
	}
	if err := connection.Execute(ctx, DocumentSetUpAgentHerdrHooks, herdrVariables(computer, map[string]any{"isRemoval": isRemoval}), &result); err != nil {
		return nil, err
	}
	return result.SetUpAgentHerdrHooks, nil
}
