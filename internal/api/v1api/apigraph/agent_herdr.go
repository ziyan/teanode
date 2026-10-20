package apigraph

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/computer"
)

// The person's herdr sessions: the Claude Code and Codex sessions in
// herdr's panes on their attached computers, which their agent works in
// beside them. The program on each computer looks, decides and presses;
// each of these asks it, as the agent's herdr tool does.

// AgentHerdrQuery reads them.
type AgentHerdrQuery interface {
	// The coding sessions in herdr on the caller's attached computers, or
	// on the one named, with the state each is in and the question each
	// waits on. Needs agent:use.
	ListAgentHerdrSessions(ctx context.Context, arguments ListAgentHerdrSessionsArguments) (*AgentHerdrListView, error)

	// The last turns of one session from its history file: turnCount of
	// them, 10 by default, 100 at most. Needs agent:use.
	ReadAgentHerdrSession(ctx context.Context, arguments ReadAgentHerdrSessionArguments) (*AgentHerdrReadView, error)

	// The agents one Claude Code session started for its current request, as
	// a tree: each with its kind, task, model, state, how long it ran and
	// the tools it used. An agent started by another names it in
	// parentSubagentId. A Codex session has none. Needs agent:use.
	ReadAgentHerdrSubagents(ctx context.Context, arguments ReadAgentHerdrSubagentsArguments) (*AgentHerdrSubagentsView, error)

	// One session's screen as it stands, or its last lineCount lines.
	// Needs agent:use.
	ReadAgentHerdrScreen(ctx context.Context, arguments ReadAgentHerdrScreenArguments) (*AgentHerdrScreenView, error)

	// Wait for a session to stop working, waitSeconds at most, 30 by
	// default and 600 at most. A query, though it waits: it changes
	// nothing, and a query holds no transaction open while it waits.
	// Needs agent:use.
	WaitAgentHerdrSession(ctx context.Context, arguments WaitAgentHerdrSessionArguments) (*AgentHerdrWaitView, error)
}

// AgentHerdrMutation acts in them.
type AgentHerdrMutation interface {
	// Type text into a session and press enter, in view of whoever sits at
	// it; a session at work takes it as a message in its turn. Refused while
	// it asks a question. Needs agent:use.
	SendAgentHerdrSession(ctx context.Context, arguments SendAgentHerdrSessionArguments) (*AgentHerdrSessionView, error)

	// Answer the question a session waits on with the options chosen, or
	// with text. Refused when the question is no longer the one with this
	// fingerprint: answered at the keyboard already, or changed; and, when
	// optionLabels are given, when the options are not labeled so. Needs
	// agent:use.
	AnswerAgentHerdrQuestion(ctx context.Context, arguments AnswerAgentHerdrQuestionArguments) (*AgentHerdrAnswerView, error)

	// Wake a conversation of the caller's when a session next finishes
	// its turn: the one named, or their main conversation. A watch outlives
	// a restart of teanode computer there. Needs agent:use.
	WatchAgentHerdrSession(ctx context.Context, arguments WatchAgentHerdrSessionArguments) (*AgentHerdrSessionView, error)

	// Start a coding agent, claude or codex, in a new herdr pane in a
	// directory on a computer: a new tab of the workspace named after the
	// directory, or a new workspace. Given paneId, it opens beside that
	// pane instead: a new tab in its workspace, in its directory and with
	// its coding agent unless directory and codingAgentKind say otherwise.
	// One that stops at a question as it starts is open and asking it.
	// Needs agent:use.
	OpenAgentHerdrSession(ctx context.Context, arguments OpenAgentHerdrSessionArguments) (*AgentHerdrSessionView, error)

	// Bring a session's tab forward in herdr on its computer, so whoever
	// sits there sees it. Needs agent:use.
	FocusAgentHerdrSession(ctx context.Context, arguments FocusAgentHerdrSessionArguments) (*AgentHerdrSessionView, error)

	// End a session's coding agent and close its pane. Refused while it
	// works. Needs agent:use.
	CloseAgentHerdrSession(ctx context.Context, arguments CloseAgentHerdrSessionArguments) (*AgentHerdrSessionView, error)

	// Put TeaNode's reporting hooks into Claude Code's settings on a
	// computer, beside what is there, or take them out with isRemoval. They
	// report what a session does as it does it, and decide nothing. Needs
	// agent:use.
	SetUpAgentHerdrHooks(ctx context.Context, arguments SetUpAgentHerdrHooksArguments) (*AgentHerdrSetupView, error)
}

// ListAgentHerdrSessionsArguments may name a computer.
type ListAgentHerdrSessionsArguments struct {
	Computer string `json:"computer" graphapi:"nullable"`
}

// ReadAgentHerdrSessionArguments name a session, and how many turns.
type ReadAgentHerdrSessionArguments struct {
	Computer  string `json:"computer" graphapi:"nullable"`
	PaneID    string `json:"paneId"`
	TurnCount int    `json:"turnCount" graphapi:"nullable"`
	// ShouldIncludeToolDetail gives each tool turn its whole input and what
	// the tool answered, in toolInputText and toolResultText.
	ShouldIncludeToolDetail bool `json:"shouldIncludeToolDetail" graphapi:"nullable"`
}

// ReadAgentHerdrSubagentsArguments name a session.
type ReadAgentHerdrSubagentsArguments struct {
	Computer string `json:"computer" graphapi:"nullable"`
	PaneID   string `json:"paneId"`
}

// ReadAgentHerdrScreenArguments name a session, and how many lines.
type ReadAgentHerdrScreenArguments struct {
	Computer  string `json:"computer" graphapi:"nullable"`
	PaneID    string `json:"paneId"`
	LineCount int    `json:"lineCount" graphapi:"nullable"`
	// ShouldKeepColor keeps the screen's colors in screenText, as the
	// terminal codes that select a color or a style and no others.
	ShouldKeepColor bool `json:"shouldKeepColor" graphapi:"nullable"`
}

// SendAgentHerdrSessionArguments are the text and where it goes, and the
// agent's files to send with it: written on the computer, their paths
// typed after the text.
type SendAgentHerdrSessionArguments struct {
	Computer      string   `json:"computer" graphapi:"nullable"`
	PaneID        string   `json:"paneId"`
	Text          string   `json:"text" graphapi:"nullable"`
	AttachmentIDs []string `json:"attachmentIds" graphapi:"nullable"`
}

// WaitAgentHerdrSessionArguments name a session, and how long to wait.
type WaitAgentHerdrSessionArguments struct {
	Computer    string `json:"computer" graphapi:"nullable"`
	PaneID      string `json:"paneId"`
	WaitSeconds int    `json:"waitSeconds" graphapi:"nullable"`
}

// AnswerAgentHerdrQuestionArguments are the question, by its fingerprint,
// and the answer: option numbers, text, or both when the option takes text.
type AnswerAgentHerdrQuestionArguments struct {
	Computer            string `json:"computer" graphapi:"nullable"`
	PaneID              string `json:"paneId"`
	QuestionFingerprint string `json:"questionFingerprint"`
	OptionNumbers       []int  `json:"optionNumbers" graphapi:"nullable"`
	// OptionLabels are the labels of the options chosen, as they were
	// shown, one for each number.
	OptionLabels []string `json:"optionLabels" graphapi:"nullable"`
	FreeText     string   `json:"freeText" graphapi:"nullable"`
}

// WatchAgentHerdrSessionArguments name a session and the conversation to
// wake.
type WatchAgentHerdrSessionArguments struct {
	Computer       string `json:"computer" graphapi:"nullable"`
	PaneID         string `json:"paneId"`
	ConversationID string `json:"conversationId" graphapi:"nullable"`
}

// OpenAgentHerdrSessionArguments say where, and which coding agent.
type OpenAgentHerdrSessionArguments struct {
	Computer        string `json:"computer" graphapi:"nullable"`
	Directory       string `json:"directory" graphapi:"nullable"`
	CodingAgentKind string `json:"codingAgentKind" graphapi:"nullable"`
	// AgentName is what the session is called in herdr; the directory's
	// name when left out.
	AgentName string `json:"agentName" graphapi:"nullable"`
	// ShouldSkipPermissions starts it without asking before it acts:
	// Claude Code with --dangerously-skip-permissions, Codex with --yolo.
	ShouldSkipPermissions bool `json:"shouldSkipPermissions" graphapi:"nullable"`
	// PaneID opens beside that pane: a new tab in its workspace, with its
	// directory and coding agent where directory and codingAgentKind are
	// left out.
	PaneID string `json:"paneId" graphapi:"nullable"`
}

// FocusAgentHerdrSessionArguments name a session.
type FocusAgentHerdrSessionArguments struct {
	Computer string `json:"computer" graphapi:"nullable"`
	PaneID   string `json:"paneId"`
}

// CloseAgentHerdrSessionArguments name a session.
type CloseAgentHerdrSessionArguments struct {
	Computer string `json:"computer" graphapi:"nullable"`
	PaneID   string `json:"paneId"`
}

// SetUpAgentHerdrHooksArguments name a computer.
type SetUpAgentHerdrHooksArguments struct {
	Computer  string `json:"computer" graphapi:"nullable"`
	IsRemoval bool   `json:"isRemoval" graphapi:"nullable"`
}

// AgentHerdrListView is every session, and the computers that were asked.
type AgentHerdrListView struct {
	// ComputerNames are the attached computers whose program watches
	// herdr; one that runs no herdr has no sessions.
	ComputerNames []string `json:"computerNames"`
	// FailedComputerNames are the ones that did not answer, whose sessions
	// are not in the list.
	FailedComputerNames []string                 `json:"failedComputerNames"`
	Sessions            []*AgentHerdrSessionView `json:"sessions"`
}

// AgentHerdrSessionView is one coding session in one pane.
type AgentHerdrSessionView struct {
	Computer string `json:"computer"`
	PaneID   string `json:"paneId"`
	// PaneName is the pane as the person finds it in herdr: workspace, tab
	// and agent; every argument that takes paneId takes it too.
	PaneName string `json:"paneName"`
	// The workspace and tab the pane is in, as herdr numbers and labels
	// them; empty when herdr's lists could not be read. IsTabFocused says
	// herdr shows that tab on the computer's screen now.
	WorkspaceID     string `json:"workspaceId"`
	WorkspaceLabel  string `json:"workspaceLabel"`
	WorkspaceNumber int    `json:"workspaceNumber"`
	TabID           string `json:"tabId"`
	TabLabel        string `json:"tabLabel"`
	TabNumber       int    `json:"tabNumber"`
	IsTabFocused    bool   `json:"isTabFocused"`
	CodingAgentKind string `json:"codingAgentKind"`
	CodingSessionID string `json:"codingSessionId"`
	// HerdrSessionState is idle, working, asking or unknown, as the
	// program decided it; HerdrAgentStatus is what herdr itself said.
	HerdrSessionState string                  `json:"herdrSessionState"`
	HerdrAgentStatus  string                  `json:"herdrAgentStatus"`
	PaneTitle         string                  `json:"paneTitle"`
	WorkingDirectory  string                  `json:"workingDirectory"`
	TranscriptPath    string                  `json:"transcriptPath"`
	IsWatched         bool                    `json:"isWatched"`
	Question          *AgentHerdrQuestionView `json:"question" graphapi:"nullable"`
}

// AgentHerdrQuestionView is a question a session waits on.
type AgentHerdrQuestionView struct {
	QuestionFingerprint string `json:"questionFingerprint"`
	// HerdrQuestionKind is question, toolApproval or planApproval.
	HerdrQuestionKind string                          `json:"herdrQuestionKind"`
	QuestionText      string                          `json:"questionText"`
	IsMultipleChoice  bool                            `json:"isMultipleChoice"`
	IsFromTranscript  bool                            `json:"isFromTranscript"`
	Options           []*AgentHerdrQuestionOptionView `json:"options"`
}

// AgentHerdrQuestionOptionView is one option of a question.
type AgentHerdrQuestionOptionView struct {
	OptionNumber      int    `json:"optionNumber"`
	OptionLabel       string `json:"optionLabel"`
	OptionDescription string `json:"optionDescription"`
	// HerdrOptionKind is choice, freeText (it takes typed text) or chat.
	HerdrOptionKind string `json:"herdrOptionKind"`
}

// AgentHerdrTurnView is one thing said or done in a session.
type AgentHerdrTurnView struct {
	// HerdrTurnRole is person, agent or tool.
	HerdrTurnRole string     `json:"herdrTurnRole"`
	TurnText      string     `json:"turnText"`
	TurnAt        *time.Time `json:"turnAt" graphapi:"nullable"`
	// ToolInputText and ToolResultText are a tool turn's whole input and
	// what the tool answered, when the read asked for them.
	ToolInputText  string `json:"toolInputText"`
	ToolResultText string `json:"toolResultText"`
	// HasToolDetail says a tool turn has an input or a result, which a read
	// asking for the detail brings.
	HasToolDetail bool `json:"hasToolDetail"`
}

// AgentHerdrReadView is a session's last turns.
type AgentHerdrReadView struct {
	HerdrSession *AgentHerdrSessionView `json:"herdrSession"`
	Turns        []*AgentHerdrTurnView  `json:"turns"`
	IsTruncated  bool                   `json:"isTruncated"`
}

// AgentHerdrSubagentView is one agent a session started, or one an agent
// started.
type AgentHerdrSubagentView struct {
	SubagentID string `json:"subagentId"`
	// ParentSubagentID is the agent that started this one; empty when the
	// session itself did.
	ParentSubagentID string `json:"parentSubagentId"`
	// SubagentType is the kind of agent, such as general-purpose or Explore.
	SubagentType string `json:"subagentType"`
	SubagentName string `json:"subagentName"`
	// TaskText is the one-line task its parent gave it.
	TaskText  string `json:"taskText"`
	ModelName string `json:"modelName"`
	// SubagentState is running, done or stopped.
	SubagentState string `json:"subagentState"`
	// SpawnDepth is 1 for an agent the session started, 2 for one that
	// agent started, and so on.
	SpawnDepth int        `json:"spawnDepth"`
	StartedAt  *time.Time `json:"startedAt" graphapi:"nullable"`
	// EndedAt is when its history was last written; null while it runs.
	EndedAt *time.Time `json:"endedAt" graphapi:"nullable"`
	// ToolNames are the tools it called, the most used first, at most 5.
	ToolNames     []string `json:"toolNames"`
	ToolCallCount int      `json:"toolCallCount"`
}

// AgentHerdrSubagentsView is the agents of a session's current request: those
// started at or after the person's last turn, and those still running from
// before it, the newest 100.
type AgentHerdrSubagentsView struct {
	HerdrSession     *AgentHerdrSessionView `json:"herdrSession"`
	SessionModelName string                 `json:"sessionModelName"`
	// RequestStartedAt is when the person last spoke to the session.
	RequestStartedAt *time.Time                `json:"requestStartedAt" graphapi:"nullable"`
	Subagents        []*AgentHerdrSubagentView `json:"subagents"`
	IsTruncated      bool                      `json:"isTruncated"`
}

// AgentHerdrScreenView is a session's screen.
type AgentHerdrScreenView struct {
	HerdrSession *AgentHerdrSessionView `json:"herdrSession"`
	ScreenText   string                 `json:"screenText"`
}

// AgentHerdrWaitView is a session after a wait.
type AgentHerdrWaitView struct {
	HerdrSession *AgentHerdrSessionView `json:"herdrSession"`
	IsTimedOut   bool                   `json:"isTimedOut"`
}

// AgentHerdrAnswerView says whether the question took its answer.
type AgentHerdrAnswerView struct {
	HerdrSession     *AgentHerdrSessionView `json:"herdrSession"`
	IsAnswerAccepted bool                   `json:"isAnswerAccepted"`
	AnsweredWith     string                 `json:"answeredWith"`
}

// AgentHerdrSetupView says what setting the hooks up did.
type AgentHerdrSetupView struct {
	Computer       string   `json:"computer"`
	IsInstalled    bool     `json:"isInstalled"`
	SettingsPath   string   `json:"settingsPath"`
	ScriptPath     string   `json:"scriptPath"`
	BackupPath     string   `json:"backupPath"`
	HookEventNames []string `json:"hookEventNames"`
}

func (self *graph) ListAgentHerdrSessions(ctx context.Context, arguments ListAgentHerdrSessionsArguments) (*AgentHerdrListView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	view := &AgentHerdrListView{ComputerNames: []string{}, FailedComputerNames: []string{}, Sessions: []*AgentHerdrSessionView{}}
	worker := self.agentWorker()
	if worker == nil {
		return view, nil
	}
	sessions, failedComputerNames, err := worker.HerdrSessions(ctx, found.ID, arguments.Computer)
	if err != nil {
		return nil, err
	}
	view.FailedComputerNames = failedComputerNames
	view.ComputerNames = worker.HerdrComputerNames(found.ID)
	if name := strings.TrimSpace(arguments.Computer); name != "" {
		view.ComputerNames = []string{}
		for _, each := range worker.HerdrComputerNames(found.ID) {
			if strings.EqualFold(each, name) {
				view.ComputerNames = append(view.ComputerNames, each)
			}
		}
	}
	for _, session := range sessions {
		view.Sessions = append(view.Sessions, herdrSessionView(session.ComputerName, session.HerdrSession))
	}
	return view, nil
}

func (self *graph) ReadAgentHerdrSession(ctx context.Context, arguments ReadAgentHerdrSessionArguments) (*AgentHerdrReadView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	read, err := worker.ReadHerdrSession(ctx, found.ID, arguments.Computer, arguments.PaneID, arguments.TurnCount, arguments.ShouldIncludeToolDetail)
	if err != nil {
		return nil, err
	}
	view := &AgentHerdrReadView{HerdrSession: herdrSessionView(read.ComputerName, read.HerdrSession), Turns: []*AgentHerdrTurnView{}, IsTruncated: read.IsTruncated}
	for _, turn := range read.Turns {
		view.Turns = append(view.Turns, &AgentHerdrTurnView{
			HerdrTurnRole: turn.HerdrTurnRole, TurnText: turn.TurnText, TurnAt: turn.TurnAt,
			ToolInputText: turn.ToolInputText, ToolResultText: turn.ToolResultText, HasToolDetail: turn.HasToolDetail,
		})
	}
	return view, nil
}

func (self *graph) ReadAgentHerdrSubagents(ctx context.Context, arguments ReadAgentHerdrSubagentsArguments) (*AgentHerdrSubagentsView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	read, err := worker.ReadHerdrSubagents(ctx, found.ID, arguments.Computer, arguments.PaneID)
	if err != nil {
		return nil, err
	}
	view := &AgentHerdrSubagentsView{
		HerdrSession: herdrSessionView(read.ComputerName, read.HerdrSession), SessionModelName: read.SessionModelName,
		RequestStartedAt: read.RequestStartedAt, Subagents: []*AgentHerdrSubagentView{}, IsTruncated: read.IsTruncated,
	}
	for _, subagent := range read.Subagents {
		view.Subagents = append(view.Subagents, &AgentHerdrSubagentView{
			SubagentID: subagent.SubagentID, ParentSubagentID: subagent.ParentSubagentID,
			SubagentType: subagent.SubagentType, SubagentName: subagent.SubagentName,
			TaskText: subagent.TaskText, ModelName: subagent.ModelName, SubagentState: subagent.SubagentState,
			SpawnDepth: subagent.SpawnDepth, StartedAt: subagent.StartedAt, EndedAt: subagent.EndedAt,
			ToolNames: subagent.ToolNames, ToolCallCount: subagent.ToolCallCount,
		})
	}
	return view, nil
}

func (self *graph) ReadAgentHerdrScreen(ctx context.Context, arguments ReadAgentHerdrScreenArguments) (*AgentHerdrScreenView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	screen, err := worker.ReadHerdrScreen(ctx, found.ID, arguments.Computer, arguments.PaneID, arguments.LineCount, arguments.ShouldKeepColor)
	if err != nil {
		return nil, err
	}
	return &AgentHerdrScreenView{HerdrSession: herdrSessionView(screen.ComputerName, screen.HerdrSession), ScreenText: screen.ScreenText}, nil
}

func (self *graph) SendAgentHerdrSession(ctx context.Context, arguments SendAgentHerdrSessionArguments) (*AgentHerdrSessionView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	sent, err := worker.SendHerdrSession(ctx, found.ID, arguments.Computer, arguments.PaneID, arguments.Text, arguments.AttachmentIDs)
	if err != nil {
		return nil, err
	}
	return herdrSessionView(sent.ComputerName, sent.HerdrSession.HerdrSession), nil
}

func (self *graph) WaitAgentHerdrSession(ctx context.Context, arguments WaitAgentHerdrSessionArguments) (*AgentHerdrWaitView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	waited, err := worker.WaitHerdrSession(ctx, found.ID, arguments.Computer, arguments.PaneID, arguments.WaitSeconds)
	if err != nil {
		return nil, err
	}
	return &AgentHerdrWaitView{HerdrSession: herdrSessionView(waited.ComputerName, waited.HerdrSession), IsTimedOut: waited.IsTimedOut}, nil
}

func (self *graph) AnswerAgentHerdrQuestion(ctx context.Context, arguments AnswerAgentHerdrQuestionArguments) (*AgentHerdrAnswerView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	answered, err := worker.AnswerHerdrQuestion(ctx, found.ID, arguments.Computer, arguments.PaneID, arguments.QuestionFingerprint, arguments.OptionNumbers, arguments.OptionLabels, arguments.FreeText)
	if err != nil {
		return nil, err
	}
	return &AgentHerdrAnswerView{
		HerdrSession: herdrSessionView(answered.ComputerName, answered.HerdrSession), IsAnswerAccepted: answered.IsAnswerAccepted, AnsweredWith: answered.AnsweredWith,
	}, nil
}

func (self *graph) WatchAgentHerdrSession(ctx context.Context, arguments WatchAgentHerdrSessionArguments) (*AgentHerdrSessionView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	conversationId, err := worker.HerdrWakeConversation(ctx, found.ID, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	session, err := worker.WatchHerdrSession(ctx, found.ID, arguments.Computer, arguments.PaneID,
		tools.BackgroundOrigin{AgentID: found.ID, ConversationID: conversationId})
	if err != nil {
		return nil, err
	}
	return herdrSessionView(session.ComputerName, session.HerdrSession), nil
}

func (self *graph) OpenAgentHerdrSession(ctx context.Context, arguments OpenAgentHerdrSessionArguments) (*AgentHerdrSessionView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	session, err := worker.OpenHerdrSession(ctx, found.ID, arguments.Computer, arguments.Directory, arguments.CodingAgentKind, arguments.AgentName, arguments.ShouldSkipPermissions, arguments.PaneID)
	if err != nil {
		return nil, err
	}
	return herdrSessionView(session.ComputerName, session.HerdrSession), nil
}

func (self *graph) FocusAgentHerdrSession(ctx context.Context, arguments FocusAgentHerdrSessionArguments) (*AgentHerdrSessionView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	focused, err := worker.FocusHerdrSession(ctx, found.ID, arguments.Computer, arguments.PaneID)
	if err != nil {
		return nil, err
	}
	return herdrSessionView(focused.ComputerName, focused.HerdrSession), nil
}

func (self *graph) CloseAgentHerdrSession(ctx context.Context, arguments CloseAgentHerdrSessionArguments) (*AgentHerdrSessionView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	closed, err := worker.CloseHerdrSession(ctx, found.ID, arguments.Computer, arguments.PaneID)
	if err != nil {
		return nil, err
	}
	return herdrSessionView(closed.ComputerName, closed.HerdrSession), nil
}

func (self *graph) SetUpAgentHerdrHooks(ctx context.Context, arguments SetUpAgentHerdrHooksArguments) (*AgentHerdrSetupView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	setup, err := worker.SetUpHerdrHooks(ctx, found.ID, arguments.Computer, arguments.IsRemoval)
	if err != nil {
		return nil, err
	}
	hookEventNames := setup.HookEventNames
	if hookEventNames == nil {
		hookEventNames = []string{}
	}
	return &AgentHerdrSetupView{
		Computer: setup.ComputerName, IsInstalled: setup.IsInstalled, SettingsPath: setup.SettingsPath,
		ScriptPath: setup.ScriptPath, BackupPath: setup.BackupPath, HookEventNames: hookEventNames,
	}, nil
}

func herdrSessionView(computerName string, session *computer.HerdrSession) *AgentHerdrSessionView {
	if session == nil {
		return nil
	}
	view := &AgentHerdrSessionView{
		Computer: computerName, PaneID: session.PaneID, PaneName: session.PaneName,
		WorkspaceID: session.WorkspaceID, WorkspaceLabel: session.WorkspaceLabel, WorkspaceNumber: session.WorkspaceNumber,
		TabID: session.TabID, TabLabel: session.TabLabel, TabNumber: session.TabNumber, IsTabFocused: session.IsTabFocused,
		CodingAgentKind: session.CodingAgentKind, CodingSessionID: session.CodingSessionID,
		HerdrSessionState: session.HerdrSessionState, HerdrAgentStatus: session.HerdrAgentStatus, PaneTitle: session.PaneTitle,
		WorkingDirectory: session.WorkingDirectory, TranscriptPath: session.TranscriptPath, IsWatched: session.IsWatched,
	}
	if question := session.Question; question != nil {
		view.Question = &AgentHerdrQuestionView{
			QuestionFingerprint: question.QuestionFingerprint, HerdrQuestionKind: question.HerdrQuestionKind, QuestionText: question.QuestionText,
			IsMultipleChoice: question.IsMultipleChoice, IsFromTranscript: question.IsFromTranscript, Options: []*AgentHerdrQuestionOptionView{},
		}
		for _, option := range question.Options {
			view.Question.Options = append(view.Question.Options, &AgentHerdrQuestionOptionView{
				OptionNumber: option.OptionNumber, OptionLabel: option.OptionLabel, OptionDescription: option.OptionDescription, HerdrOptionKind: option.HerdrOptionKind,
			})
		}
	}
	return view
}
