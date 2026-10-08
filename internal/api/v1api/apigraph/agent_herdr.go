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

	// One session's screen as it stands, or its last lineCount lines.
	// Needs agent:use.
	ReadAgentHerdrScreen(ctx context.Context, arguments ReadAgentHerdrScreenArguments) (*AgentHerdrScreenView, error)
}

// AgentHerdrMutation acts in them.
type AgentHerdrMutation interface {
	// Type text into a session and press enter, in view of whoever sits at
	// it. Refused while it asks a question, and while it works unless
	// shouldQueue. Needs agent:use.
	SendAgentHerdrSession(ctx context.Context, arguments SendAgentHerdrSessionArguments) (*AgentHerdrSessionView, error)

	// Wait for a session to stop working, waitSeconds at most, 30 by
	// default and 600 at most. Needs agent:use.
	WaitAgentHerdrSession(ctx context.Context, arguments WaitAgentHerdrSessionArguments) (*AgentHerdrWaitView, error)

	// Answer the question a session waits on with the options chosen, or
	// with text. Refused when the question is no longer the one with this
	// fingerprint: answered at the keyboard already, or changed. Needs
	// agent:use.
	AnswerAgentHerdrQuestion(ctx context.Context, arguments AnswerAgentHerdrQuestionArguments) (*AgentHerdrAnswerView, error)

	// Wake a conversation of the caller's when a session next finishes
	// its turn. Watches end when teanode computer restarts there. Needs
	// agent:use.
	WatchAgentHerdrSession(ctx context.Context, arguments WatchAgentHerdrSessionArguments) (*AgentHerdrSessionView, error)

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
}

// ReadAgentHerdrScreenArguments name a session, and how many lines.
type ReadAgentHerdrScreenArguments struct {
	Computer  string `json:"computer" graphapi:"nullable"`
	PaneID    string `json:"paneId"`
	LineCount int    `json:"lineCount" graphapi:"nullable"`
}

// SendAgentHerdrSessionArguments are the text and where it goes.
type SendAgentHerdrSessionArguments struct {
	Computer    string `json:"computer" graphapi:"nullable"`
	PaneID      string `json:"paneId"`
	Text        string `json:"text"`
	ShouldQueue bool   `json:"shouldQueue" graphapi:"nullable"`
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
	FreeText            string `json:"freeText" graphapi:"nullable"`
}

// WatchAgentHerdrSessionArguments name a session and the conversation to
// wake.
type WatchAgentHerdrSessionArguments struct {
	Computer       string `json:"computer" graphapi:"nullable"`
	PaneID         string `json:"paneId"`
	ConversationID string `json:"conversationId"`
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
	ComputerNames []string                 `json:"computerNames"`
	Sessions      []*AgentHerdrSessionView `json:"sessions"`
}

// AgentHerdrSessionView is one coding session in one pane.
type AgentHerdrSessionView struct {
	Computer        string `json:"computer"`
	PaneID          string `json:"paneId"`
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
}

// AgentHerdrReadView is a session's last turns.
type AgentHerdrReadView struct {
	HerdrSession *AgentHerdrSessionView `json:"herdrSession"`
	Turns        []*AgentHerdrTurnView  `json:"turns"`
	IsTruncated  bool                   `json:"isTruncated"`
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
	view := &AgentHerdrListView{ComputerNames: []string{}, Sessions: []*AgentHerdrSessionView{}}
	worker := self.agentWorker()
	if worker == nil {
		return view, nil
	}
	sessions, err := worker.HerdrSessions(ctx, found.ID, arguments.Computer)
	if err != nil {
		return nil, err
	}
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
		view.Sessions = append(view.Sessions, herdrSessionView(session.Computer, session.HerdrSession))
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
	read, err := worker.ReadHerdrSession(ctx, found.ID, arguments.Computer, arguments.PaneID, arguments.TurnCount)
	if err != nil {
		return nil, err
	}
	view := &AgentHerdrReadView{HerdrSession: herdrSessionView(read.Computer, read.HerdrSession), Turns: []*AgentHerdrTurnView{}, IsTruncated: read.IsTruncated}
	for _, turn := range read.Turns {
		view.Turns = append(view.Turns, &AgentHerdrTurnView{HerdrTurnRole: turn.HerdrTurnRole, TurnText: turn.TurnText, TurnAt: turn.TurnAt})
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
	screen, err := worker.ReadHerdrScreen(ctx, found.ID, arguments.Computer, arguments.PaneID, arguments.LineCount)
	if err != nil {
		return nil, err
	}
	return &AgentHerdrScreenView{HerdrSession: herdrSessionView(screen.Computer, screen.HerdrSession), ScreenText: screen.ScreenText}, nil
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
	session, err := worker.SendHerdrSession(ctx, found.ID, arguments.Computer, arguments.PaneID, arguments.Text, arguments.ShouldQueue)
	if err != nil {
		return nil, err
	}
	return herdrSessionView(session.Computer, session.HerdrSession), nil
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
	return &AgentHerdrWaitView{HerdrSession: herdrSessionView(waited.Computer, waited.HerdrSession), IsTimedOut: waited.IsTimedOut}, nil
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
	answered, err := worker.AnswerHerdrQuestion(ctx, found.ID, arguments.Computer, arguments.PaneID, arguments.QuestionFingerprint, arguments.OptionNumbers, arguments.FreeText)
	if err != nil {
		return nil, err
	}
	return &AgentHerdrAnswerView{
		HerdrSession: herdrSessionView(answered.Computer, answered.HerdrSession), IsAnswerAccepted: answered.IsAnswerAccepted, AnsweredWith: answered.AnsweredWith,
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
	session, err := worker.WatchHerdrSession(ctx, found.ID, arguments.Computer, arguments.PaneID,
		tools.BackgroundOrigin{AgentID: found.ID, ConversationID: strings.TrimSpace(arguments.ConversationID)})
	if err != nil {
		return nil, err
	}
	return herdrSessionView(session.Computer, session.HerdrSession), nil
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
		Computer: setup.Computer, IsInstalled: setup.IsInstalled, SettingsPath: setup.SettingsPath,
		ScriptPath: setup.ScriptPath, BackupPath: setup.BackupPath, HookEventNames: hookEventNames,
	}, nil
}

func herdrSessionView(computerName string, session *computer.HerdrSession) *AgentHerdrSessionView {
	if session == nil {
		return nil
	}
	view := &AgentHerdrSessionView{
		Computer: computerName, PaneID: session.PaneID, CodingAgentKind: session.CodingAgentKind, CodingSessionID: session.CodingSessionID,
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
