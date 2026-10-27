package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// The person's herdr sessions: the Claude Code and Codex sessions they keep
// in herdr's panes, on each computer they attached, which the agent works
// in beside them.
//
// The program on each computer does the looking, the deciding and the
// pressing (internal/computer/herdr.go), so the agent's tool, the dashboard
// and the command line, which all come through here or ask it directly,
// get the same answers and the same refusals. What this side adds is the
// person: a question a session asks is told to their phones, and opens the
// session on the iPhone's Coding screen, where it is answered (it is shown on
// the dashboard's Coding page too), and a session the agent watched wakes the
// conversation that watched it when it finishes.

const (
	// herdrQuestionNotification is the kind of a phone's notification that a
	// session asks: the app opens that session on its Coding screen.
	herdrQuestionNotification = "herdr_question"

	// herdrListWait is how long listing waits for each computer.
	herdrListWait = 15 * time.Second

	// herdrActionWait is how long an action that presses keys and looks
	// again is waited for.
	herdrActionWait = 30 * time.Second

	// herdrSettledTurnCount is how many of a finished session's last turns
	// its wake carries.
	herdrSettledTurnCount = 3
)

// HerdrSession is one coding session on one of a person's computers.
type HerdrSession struct {
	ComputerName string
	*computer.HerdrSession
}

// herdrSettling is a watched session that finished, waiting for its turn.
type herdrSettling struct {
	computer *attachedComputer
	event    *computer.HerdrEvent
	turns    []*computer.HerdrTurn
}

// herdrComputers are a person's computers whose program watches herdr.
func (self *Agent) herdrComputers(agentId string) []*attachedComputer {
	var found []*attachedComputer
	for _, attached := range self.computersFor(agentId) {
		if attached.HasFeature(computer.FeatureHerdr) {
			found = append(found, attached)
		}
	}
	return found
}

// herdrComputer is the computer an action means: the one named, or the only
// one that watches herdr.
func (self *Agent) herdrComputer(agentId, computerName string) (*attachedComputer, error) {
	computerName = strings.TrimSpace(computerName)
	attached := self.computersFor(agentId)
	if len(attached) == 0 {
		return nil, errors.New("no computer is attached; run `teanode computer start` on it")
	}
	if computerName != "" {
		for _, one := range attached {
			if strings.EqualFold(one.name, computerName) {
				if !one.HasFeature(computer.FeatureHerdr) {
					return nil, fmt.Errorf("the program on %s is too old for herdr; update teanode there and restart it", one.name)
				}
				return one, nil
			}
		}
		return nil, fmt.Errorf("no computer called %q is attached", computerName)
	}
	watching := self.herdrComputers(agentId)
	switch len(watching) {
	case 0:
		names := make([]string, 0, len(attached))
		for _, one := range attached {
			names = append(names, one.name)
		}
		return nil, fmt.Errorf("no attached computer watches herdr; the program on %s is too old for herdr, or herdr is not set up there: update teanode and restart it", strings.Join(names, ", "))
	case 1:
		return watching[0], nil
	}
	names := make([]string, 0, len(watching))
	for _, one := range watching {
		names = append(names, one.name)
	}
	return nil, fmt.Errorf("several computers run herdr (%s); say which", strings.Join(names, ", "))
}

// askHerdr asks a computer for a herdr action and decodes its answer.
func askHerdr[Result any](ctx context.Context, attached *attachedComputer, action string, arguments *computer.HerdrArguments, wait time.Duration) (*Result, error) {
	answer, err := attached.Ask(ctx, action, arguments, wait)
	if err != nil {
		return nil, err
	}
	var result Result
	if err := json.Unmarshal(answer, &result); err != nil {
		return nil, fmt.Errorf("%s answered something unreadable: %w", attached.what, err)
	}
	return &result, nil
}

// HerdrSessions are the coding sessions on a person's computers, every
// computer's or one's, by computer and pane, and the computers that did not
// answer: a computer left out of the list is not one whose questions went.
func (self *Agent) HerdrSessions(ctx context.Context, agentId, computerName string) ([]*HerdrSession, []string, error) {
	var asked []*attachedComputer
	if strings.TrimSpace(computerName) != "" {
		attached, err := self.herdrComputer(agentId, computerName)
		if err != nil {
			return nil, nil, err
		}
		asked = []*attachedComputer{attached}
	} else {
		asked = self.herdrComputers(agentId)
	}
	// Every computer is asked at once, and one that does not answer, or
	// runs no herdr, is left out rather than taking the others with it.
	answers := make([][]*HerdrSession, len(asked))
	hasFailedByIndex := make([]bool, len(asked))
	var waitGroup sync.WaitGroup
	for index, one := range asked {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			sessions, err := askHerdr[[]*computer.HerdrSession](ctx, one, "herdr_list", &computer.HerdrArguments{}, herdrListWait)
			if err != nil {
				log.Debugf("cannot list the herdr sessions on %q: %s", one.name, err)
				hasFailedByIndex[index] = true
				return
			}
			for _, session := range *sessions {
				answers[index] = append(answers[index], &HerdrSession{ComputerName: one.name, HerdrSession: session})
			}
		}()
	}
	waitGroup.Wait()
	listed, failedComputerNames := []*HerdrSession{}, []string{}
	for index, sessions := range answers {
		listed = append(listed, sessions...)
		if hasFailedByIndex[index] {
			failedComputerNames = append(failedComputerNames, asked[index].name)
		}
	}
	return listed, failedComputerNames, nil
}

// HerdrWakeConversation is the conversation a watch from the dashboard or
// the command line wakes: the one named, when it is the person's own and one
// a turn can be woken in, or their main conversation.
func (self *Agent) HerdrWakeConversation(ctx context.Context, agentId, conversationId string) (string, error) {
	var found *models.AgentConversation
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if conversationId = strings.TrimSpace(conversationId); conversationId != "" {
			found, err = tx.GetAgentConversation(conversationId)
			if err != nil {
				return err
			}
			if found == nil || found.AgentID != agentId || found.Kind == models.AgentConversationRun {
				return fmt.Errorf("there is no conversation %q of yours to wake", conversationId)
			}
			return nil
		}
		found, err = scheduleConversation(tx, agentId, "")
		return err
	})
	if err != nil {
		return "", err
	}
	return found.ID, nil
}

// HerdrComputerNames are the names of a person's computers whose program
// watches herdr, for the dashboard to say where it looked.
func (self *Agent) HerdrComputerNames(agentId string) []string {
	names := []string{}
	for _, one := range self.herdrComputers(agentId) {
		names = append(names, one.name)
	}
	return names
}

// HerdrRead is a session's last turns.
type HerdrRead struct {
	ComputerName string
	*computer.HerdrReadResult
}

// ReadHerdrSession is the last turns of a session.
func (self *Agent) ReadHerdrSession(ctx context.Context, agentId, computerName, paneId string, turnCount int, shouldIncludeToolDetail bool) (*HerdrRead, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	result, err := askHerdr[computer.HerdrReadResult](ctx, attached, "herdr_read", &computer.HerdrArguments{PaneID: paneId, TurnCount: turnCount, ShouldIncludeToolDetail: shouldIncludeToolDetail}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrRead{ComputerName: attached.name, HerdrReadResult: result}, nil
}

// HerdrSubagents is the agents a session started for its current request.
type HerdrSubagents struct {
	ComputerName string
	*computer.HerdrSubagentsResult
}

// ReadHerdrSubagents is the agents a session started, as the program on its
// computer reads them from the files Claude Code keeps beside the session's
// history.
func (self *Agent) ReadHerdrSubagents(ctx context.Context, agentId, computerName, paneId string) (*HerdrSubagents, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	if !attached.HasFeature(computer.FeatureHerdrSubagents) {
		return nil, fmt.Errorf("the program on %s is too old to show a session's agents; update teanode there and restart it", attached.name)
	}
	result, err := askHerdr[computer.HerdrSubagentsResult](ctx, attached, "herdr_subagents", &computer.HerdrArguments{PaneID: paneId}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrSubagents{ComputerName: attached.name, HerdrSubagentsResult: result}, nil
}

// HerdrScreen is a session's screen.
type HerdrScreen struct {
	ComputerName string
	*computer.HerdrScreenResult
}

// ReadHerdrScreen is a session's screen, or its last lineCount lines.
func (self *Agent) ReadHerdrScreen(ctx context.Context, agentId, computerName, paneId string, lineCount int, shouldKeepColor bool) (*HerdrScreen, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	result, err := askHerdr[computer.HerdrScreenResult](ctx, attached, "herdr_screen", &computer.HerdrArguments{PaneID: paneId, LineCount: lineCount, ShouldKeepColor: shouldKeepColor}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrScreen{ComputerName: attached.name, HerdrScreenResult: result}, nil
}

// SendHerdrSession types text into a session, followed by enter; a session
// at work takes it as a message in its turn.
func (self *Agent) SendHerdrSession(ctx context.Context, agentId, computerName, paneId, text string, attachmentIds []string) (*HerdrSent, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	asked := &computer.HerdrArguments{PaneID: paneId, Text: text}
	wait := herdrActionWait
	if len(attachmentIds) > 0 {
		if !attached.HasFeature(computer.FeatureHerdrFiles) {
			return nil, fmt.Errorf("the program on %s is too old to send files with; update teanode there and restart it", attached.name)
		}
		asked.HerdrFiles, err = tools.HerdrFilesOf(ctx, self.settings.Database, self.settings.Storage, agentId, "", attachmentIds)
		if err != nil {
			return nil, err
		}
		wait = 2 * time.Minute
	}
	result, err := askHerdr[computer.HerdrSendResult](ctx, attached, "herdr_send", asked, wait)
	if err != nil {
		return nil, err
	}
	return &HerdrSent{HerdrSession: HerdrSession{ComputerName: attached.name, HerdrSession: result.HerdrSession}, FilePaths: result.FilePaths}, nil
}

// HerdrSent is a session once something was sent to it, and where the
// files sent with it were written.
type HerdrSent struct {
	HerdrSession
	FilePaths []string
}

// HerdrWait is a session once it stopped working, or when the wait ran out.
type HerdrWait struct {
	ComputerName string
	*computer.HerdrWaitResult
}

// WaitHerdrSession waits for a session to stop working, waitSeconds at
// most.
func (self *Agent) WaitHerdrSession(ctx context.Context, agentId, computerName, paneId string, waitSeconds int) (*HerdrWait, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	result, err := askHerdr[computer.HerdrWaitResult](ctx, attached, "herdr_wait", &computer.HerdrArguments{PaneID: paneId, WaitSeconds: waitSeconds},
		time.Duration(max(waitSeconds, 30))*time.Second+herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrWait{ComputerName: attached.name, HerdrWaitResult: result}, nil
}

// HerdrAnswer says whether a question took its answer.
type HerdrAnswer struct {
	ComputerName string
	*computer.HerdrAnswerResult
}

// AnswerHerdrQuestion answers the question a session waits on, if it is
// still the one with this fingerprint.
func (self *Agent) AnswerHerdrQuestion(ctx context.Context, agentId, computerName, paneId, questionFingerprint string, optionNumbers []int, optionLabels []string, freeText string) (*HerdrAnswer, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	result, err := askHerdr[computer.HerdrAnswerResult](ctx, attached, "herdr_answer", &computer.HerdrArguments{
		PaneID: paneId, QuestionFingerprint: questionFingerprint, OptionNumbers: optionNumbers, OptionLabels: optionLabels, FreeText: freeText,
	}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrAnswer{ComputerName: attached.name, HerdrAnswerResult: result}, nil
}

// WatchHerdrSession has a conversation woken when a session next finishes.
func (self *Agent) WatchHerdrSession(ctx context.Context, agentId, computerName, paneId string, origin tools.BackgroundOrigin) (*HerdrSession, error) {
	if origin.ConversationID == "" || origin.IsUnwakeable {
		return nil, errors.New("there is no conversation to wake when it finishes")
	}
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(origin)
	if err != nil {
		return nil, err
	}
	session, err := askHerdr[computer.HerdrSession](ctx, attached, "herdr_watch", &computer.HerdrArguments{PaneID: paneId, Origin: encoded}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrSession{ComputerName: attached.name, HerdrSession: session}, nil
}

// OpenHerdrSession starts a coding agent, "claude" or "codex", in a new
// herdr pane in a directory of the person's on a computer.
//
// Given paneId, it opens beside that pane instead: a new tab in the pane's
// workspace, in its directory and with its coding agent unless directory
// and codingAgentKind say otherwise.
func (self *Agent) OpenHerdrSession(ctx context.Context, agentId, computerName, directory, codingAgentKind, agentName string, shouldSkipPermissions bool, paneId string) (*HerdrSession, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(paneId) != "" && !attached.HasFeature(computer.FeatureHerdrOpenBeside) {
		return nil, fmt.Errorf("the program on %s is too old to open a tab beside a session; update teanode there and restart it", attached.name)
	}
	// A coding agent takes a while to start: up to a minute is waited for.
	session, err := askHerdr[computer.HerdrSession](ctx, attached, "herdr_open", &computer.HerdrArguments{
		Directory: directory, CodingAgentKind: codingAgentKind, AgentName: agentName, ShouldSkipPermissions: shouldSkipPermissions, PaneID: paneId,
	}, 90*time.Second)
	if err != nil {
		return nil, err
	}
	return &HerdrSession{ComputerName: attached.name, HerdrSession: session}, nil
}

// FocusHerdrSession brings a session's tab forward in herdr on its
// computer, so whoever sits there sees it.
func (self *Agent) FocusHerdrSession(ctx context.Context, agentId, computerName, paneId string) (*HerdrSession, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	session, err := askHerdr[computer.HerdrSession](ctx, attached, "herdr_focus", &computer.HerdrArguments{PaneID: paneId}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrSession{ComputerName: attached.name, HerdrSession: session}, nil
}

// HerdrClose says a session's pane was closed.
type HerdrClose struct {
	ComputerName string
	*computer.HerdrCloseResult
}

// CloseHerdrSession ends a session's coding agent and its pane; refused
// while it works.
func (self *Agent) CloseHerdrSession(ctx context.Context, agentId, computerName, paneId string) (*HerdrClose, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	result, err := askHerdr[computer.HerdrCloseResult](ctx, attached, "herdr_close", &computer.HerdrArguments{PaneID: paneId}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrClose{ComputerName: attached.name, HerdrCloseResult: result}, nil
}

// HerdrSetup says what setting up the hooks did, on which computer.
type HerdrSetup struct {
	ComputerName string
	*computer.HerdrSetupResult
}

// SetUpHerdrHooks puts TeaNode's reporting hooks into Claude Code on a
// computer, or takes them out.
func (self *Agent) SetUpHerdrHooks(ctx context.Context, agentId, computerName string, isRemoval bool) (*HerdrSetup, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	result, err := askHerdr[computer.HerdrSetupResult](ctx, attached, "herdr_setup", &computer.HerdrArguments{IsRemoval: isRemoval}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrSetup{ComputerName: attached.name, HerdrSetupResult: result}, nil
}

// ComputerHerdrChanged is a computer saying, unasked, that a question came
// or went in one of its herdr panes, or that a watched session finished.
func (self *Agent) ComputerHerdrChanged(agentId string, connection DeviceConnection, data json.RawMessage) {
	var found *attachedComputer
	self.computersMutex.Lock()
	for _, attached := range self.computers[agentId] {
		if attached.connection == connection {
			found = attached
			break
		}
	}
	self.computersMutex.Unlock()
	if found == nil {
		return
	}
	var event computer.HerdrEvent
	if err := json.Unmarshal(data, &event); err != nil || event.HerdrEventID == "" || event.HerdrSession == nil {
		log.Warningf("the computer %q said something about herdr, unreadably: %v", found.name, err)
		return
	}
	switch event.HerdrEventKind {
	case computer.HerdrEventKindAsking:
		self.waitGroup.Add(1)
		go func() {
			defer self.waitGroup.Done()
			self.tellHerdrQuestion(agentId, found, &event)
		}()
	case computer.HerdrEventKindAnswered:
		// A question that went needs nothing more here: the Coding page and
		// screen read the session again, and nothing was written anywhere
		// that would need to say it went.
		self.waitGroup.Add(1)
		go func() {
			defer self.waitGroup.Done()
			self.acknowledgeHerdr(self.currentComputer(agentId, found), event.HerdrEventID)
		}()
	case computer.HerdrEventKindSettled:
		self.waitGroup.Add(1)
		go func() {
			defer self.waitGroup.Done()
			self.wakeForHerdrSettled(agentId, found, &event)
		}()
	default:
		// A question that went needs nothing here: its card asks the
		// computer whether it still waits.
		go self.acknowledgeHerdr(found, event.HerdrEventID)
	}
}

// acknowledgeHerdr tells the computer the server has heard an event, so it
// is not said again.
func (self *Agent) acknowledgeHerdr(attached *attachedComputer, id string) {
	ctx, cancel := context.WithTimeout(self.ctx, deviceAnswerWait)
	defer cancel()
	if _, err := attached.Ask(ctx, "herdr_acknowledge", &computer.HerdrArguments{HerdrEventIDs: []string{id}}, deviceAnswerWait); err != nil {
		log.Warningf("cannot acknowledge herdr event %s on %q: %s", id, attached.name, err)
	}
}

// herdrToldKey names a question once on one computer's pane.
func herdrToldKey(computerName string, event *computer.HerdrEvent) string {
	return computerName + "\x00" + event.HerdrSession.PaneID + "\x00" + event.HerdrSession.Question.QuestionFingerprint
}

// tellHerdrQuestion tells the person's phones that a session asks, and
// nothing more: the question is shown where the session is, on the Coding
// page and the iPhone's Coding screen, and the notification opens it there.
// Nothing is written into a conversation, so neither the main conversation
// nor the chat apps that follow it hear of it. A question said again after a
// reconnect is told once.
func (self *Agent) tellHerdrQuestion(agentId string, attached *attachedComputer, event *computer.HerdrEvent) {
	session := event.HerdrSession
	if session.Question != nil {
		key := herdrToldKey(attached.name, event)
		self.backgroundMutex.Lock()
		if self.herdrTold == nil {
			self.herdrTold = map[string]time.Time{}
		}
		for each, at := range self.herdrTold {
			if time.Since(at) > 24*time.Hour {
				delete(self.herdrTold, each)
			}
		}
		_, isKnown := self.herdrTold[key]
		if !isKnown {
			self.herdrTold[key] = time.Now()
		}
		self.backgroundMutex.Unlock()
		if !isKnown {
			self.notifyPhones(agentId, models.NotificationQuestion, herdrQuestionContent(attached.name, session))
		}
	}
	self.acknowledgeHerdr(self.currentComputer(agentId, attached), event.HerdrEventID)
}

// What a phone is told when a session asks: which session, on which
// computer, and what kind of question it is.
var (
	phraseSessionAsks         = push.Phrase{English: "%s on %s asks", Chinese: "%s（%s）在提问", Japanese: "%s（%s）が質問しています"}
	phraseSessionAsksApproval = push.Phrase{English: "%s on %s asks for your approval", Chinese: "%s（%s）请求你的批准", Japanese: "%s（%s）が承認を求めています"}
	phraseSessionAsksPlan     = push.Phrase{English: "%s on %s asks you to approve its plan", Chinese: "%s（%s）请你批准它的计划", Japanese: "%s（%s）が計画の承認を求めています"}
)

// herdrQuestionContent is the notification of a session's question: the
// session and its computer as the title, the question as the body (plain,
// as a lock screen shows it), and the computer, the pane and the question's
// fingerprint, by which the app opens the session. Its one button opens it:
// the question is answered on the Coding screen, where its options are.
func herdrQuestionContent(computerName string, session *computer.HerdrSession) *push.Content {
	question := session.Question
	phrase := phraseSessionAsks
	switch question.HerdrQuestionKind {
	case computer.HerdrQuestionKindToolApproval:
		phrase = phraseSessionAsksApproval
	case computer.HerdrQuestionKindPlanApproval:
		phrase = phraseSessionAsksPlan
	}
	paneName := paneNameOf(session)
	return &push.Content{
		NotificationKind:    herdrQuestionNotification,
		Title:               phrase.In(push.LanguageEnglish, paneName, computerName),
		Body:                PhoneBody(question.QuestionText),
		ThreadID:            herdrQuestionNotification + ":" + computerName + ":" + session.PaneID,
		Computer:            computerName,
		PaneID:              session.PaneID,
		QuestionFingerprint: question.QuestionFingerprint,
		Buttons:             []push.Button{{Label: "Open", ButtonAction: "open"}},
		Wording: func(content *push.Content, language string) {
			content.Title = phrase.In(language, paneName, computerName)
		},
	}
}

// paneNameOf is a session's pane as the person knows it in herdr, or its
// id where herdr gave it no name.
func paneNameOf(session *computer.HerdrSession) string {
	if name := strings.TrimSpace(session.PaneName); name != "" {
		return name
	}
	return "pane " + session.PaneID
}

// codingAgentName is what a coding agent is called to the person.
func codingAgentName(codingAgentKind string) string {
	switch codingAgentKind {
	case computer.CodingAgentKindClaude:
		return "Claude Code"
	case computer.CodingAgentKindCodex:
		return "Codex"
	}
	return codingAgentKind
}

// wakeForHerdrSettled has a watched session that finished wake the
// conversation that watched it, with its last turns.
func (self *Agent) wakeForHerdrSettled(agentId string, attached *attachedComputer, event *computer.HerdrEvent) {
	var origin tools.BackgroundOrigin
	_ = json.Unmarshal(event.Origin, &origin)
	if origin.AgentID != agentId || origin.ConversationID == "" || origin.IsUnwakeable {
		self.acknowledgeHerdr(attached, event.HerdrEventID)
		return
	}
	settling := &herdrSettling{computer: attached, event: event}
	ctx, cancel := context.WithTimeout(self.ctx, herdrActionWait)
	if read, err := askHerdr[computer.HerdrReadResult](ctx, attached, "herdr_read",
		&computer.HerdrArguments{PaneID: event.HerdrSession.PaneID, TurnCount: herdrSettledTurnCount}, herdrActionWait); err == nil {
		settling.turns = read.Turns
	}
	cancel()

	self.backgroundMutex.Lock()
	defer self.backgroundMutex.Unlock()
	self.ensureBackgroundLocked()
	// Said again after a reconnect while its turn is still to come.
	if self.backgroundInFlight[herdrInFlight(event.HerdrEventID)] {
		return
	}
	self.backgroundInFlight[herdrInFlight(event.HerdrEventID)] = true
	wake := self.backgroundWakes[origin.ConversationID]
	if wake == nil {
		wake = &backgroundWake{agentId: agentId}
		self.backgroundWakes[origin.ConversationID] = wake
	}
	wake.settlings = append(wake.settlings, settling)
	self.queueWakeLocked(origin.ConversationID, wake, backgroundWakeGather)
}

// herdrInFlight is the key a settled session is held under in the
// background in-flight set.
func herdrInFlight(eventId string) string {
	return "herdr:" + eventId
}

// herdrSettledMessage is what the agent is woken with for watched sessions
// that finished: the session's state, its question if it stopped on one,
// and its last turns, fenced, since they are what the session said.
func herdrSettledMessage(builder *strings.Builder, settlings []*herdrSettling) {
	for _, settling := range settlings {
		session := settling.event.HerdrSession
		builder.WriteString("\n")
		fmt.Fprintf(builder, "%s computer: %s\npane: %s (%s)\ncoding agent: %s\ndirectory: %s\nstate: %s\n", models.HerdrSessionMarker,
			settling.computer.name, paneNameOf(session), session.PaneID, codingAgentName(session.CodingAgentKind), session.WorkingDirectory, session.HerdrSessionState)
		var said strings.Builder
		if session.Question != nil {
			fmt.Fprintf(&said, "It is asking: %s\n", session.Question.QuestionText)
			for _, option := range session.Question.Options {
				fmt.Fprintf(&said, "%d. %s\n", option.OptionNumber, option.OptionLabel)
			}
		}
		for _, turn := range settling.turns {
			fmt.Fprintf(&said, "%s: %s\n", turn.HerdrTurnRole, turn.TurnText)
		}
		if said.Len() > 0 {
			builder.WriteString(fenced(strings.TrimSpace(said.String())) + "\n")
		}
	}
}

// herdrSettledLine is the finished sessions in a line, for a note.
func herdrSettledLine(settlings []*herdrSettling) string {
	described := make([]string, 0, len(settlings))
	for _, settling := range settlings {
		described = append(described, fmt.Sprintf("%s on %s, %s", paneNameOf(settling.event.HerdrSession), settling.computer.name, settling.event.HerdrSession.HerdrSessionState))
	}
	return "Herdr sessions finished: " + strings.Join(described, "; ")
}
