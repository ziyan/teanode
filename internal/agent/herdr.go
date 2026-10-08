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
	"github.com/ziyan/teanode/internal/util/security"
)

// The person's herdr sessions: the Claude Code and Codex sessions they keep
// in herdr's panes, on each computer they attached, which the agent works
// in beside them.
//
// The program on each computer does the looking, the deciding and the
// pressing (internal/computer/herdr.go), so the agent's tool, the dashboard
// and the command line, which all come through here or ask it directly,
// get the same answers and the same refusals. What this side adds is the
// person: a question a session asks is written into their main
// conversation, which opens the drawer and is sent on to their chat apps,
// and a session the agent watched wakes the conversation that watched it
// when it finishes.

const (
	// herdrQuestionSurface is what a question is said on: like a goal's
	// call for the person, it opens the drawer.
	herdrQuestionSurface = "herdr_question"

	// herdrListWait is how long listing waits for each computer.
	herdrListWait = 15 * time.Second

	// herdrActionWait is how long an action that presses keys and looks
	// again is waited for.
	herdrActionWait = 30 * time.Second

	// herdrQuestionTries is how many times a question is tried while a
	// turn runs in the main conversation, herdrQuestionRetry apart.
	herdrQuestionTries = 20
	herdrQuestionRetry = 15 * time.Second

	// herdrSettledTurnCount is how many of a finished session's last turns
	// its wake carries.
	herdrSettledTurnCount = 3
)

// HerdrSession is one coding session on one of a person's computers.
type HerdrSession struct {
	Computer string
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
		return nil, fmt.Errorf("the program on %s is too old for herdr; update teanode there and restart it", attached[0].name)
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
// computer's or one's, by computer and pane.
func (self *Agent) HerdrSessions(ctx context.Context, agentId, computerName string) ([]*HerdrSession, error) {
	var asked []*attachedComputer
	if strings.TrimSpace(computerName) != "" {
		attached, err := self.herdrComputer(agentId, computerName)
		if err != nil {
			return nil, err
		}
		asked = []*attachedComputer{attached}
	} else {
		asked = self.herdrComputers(agentId)
	}
	// Every computer is asked at once, and one that does not answer, or
	// runs no herdr, is left out rather than taking the others with it.
	answers := make([][]*HerdrSession, len(asked))
	var waitGroup sync.WaitGroup
	for index, one := range asked {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			sessions, err := askHerdr[[]*computer.HerdrSession](ctx, one, "herdr_list", &computer.HerdrArguments{}, herdrListWait)
			if err != nil {
				log.Debugf("cannot list the herdr sessions on %q: %s", one.name, err)
				return
			}
			for _, session := range *sessions {
				answers[index] = append(answers[index], &HerdrSession{Computer: one.name, HerdrSession: session})
			}
		}()
	}
	waitGroup.Wait()
	listed := []*HerdrSession{}
	for _, sessions := range answers {
		listed = append(listed, sessions...)
	}
	return listed, nil
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
	Computer string
	*computer.HerdrReadResult
}

// ReadHerdrSession is the last turns of a session.
func (self *Agent) ReadHerdrSession(ctx context.Context, agentId, computerName, paneId string, turnCount int) (*HerdrRead, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	result, err := askHerdr[computer.HerdrReadResult](ctx, attached, "herdr_read", &computer.HerdrArguments{PaneID: paneId, TurnCount: turnCount}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrRead{Computer: attached.name, HerdrReadResult: result}, nil
}

// HerdrScreen is a session's screen.
type HerdrScreen struct {
	Computer string
	*computer.HerdrScreenResult
}

// ReadHerdrScreen is a session's screen, or its last lineCount lines.
func (self *Agent) ReadHerdrScreen(ctx context.Context, agentId, computerName, paneId string, lineCount int) (*HerdrScreen, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	result, err := askHerdr[computer.HerdrScreenResult](ctx, attached, "herdr_screen", &computer.HerdrArguments{PaneID: paneId, LineCount: lineCount}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrScreen{Computer: attached.name, HerdrScreenResult: result}, nil
}

// SendHerdrSession types text into a session, followed by enter.
func (self *Agent) SendHerdrSession(ctx context.Context, agentId, computerName, paneId, text string, shouldQueue bool) (*HerdrSession, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	result, err := askHerdr[computer.HerdrSendResult](ctx, attached, "herdr_send", &computer.HerdrArguments{PaneID: paneId, Text: text, ShouldQueue: shouldQueue}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrSession{Computer: attached.name, HerdrSession: result.HerdrSession}, nil
}

// HerdrWait is a session once it stopped working, or when the wait ran out.
type HerdrWait struct {
	Computer string
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
	return &HerdrWait{Computer: attached.name, HerdrWaitResult: result}, nil
}

// HerdrAnswer says whether a question took its answer.
type HerdrAnswer struct {
	Computer string
	*computer.HerdrAnswerResult
}

// AnswerHerdrQuestion answers the question a session waits on, if it is
// still the one with this fingerprint.
func (self *Agent) AnswerHerdrQuestion(ctx context.Context, agentId, computerName, paneId, questionFingerprint string, optionNumbers []int, freeText string) (*HerdrAnswer, error) {
	attached, err := self.herdrComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	result, err := askHerdr[computer.HerdrAnswerResult](ctx, attached, "herdr_answer", &computer.HerdrArguments{
		PaneID: paneId, QuestionFingerprint: questionFingerprint, OptionNumbers: optionNumbers, FreeText: freeText,
	}, herdrActionWait)
	if err != nil {
		return nil, err
	}
	return &HerdrAnswer{Computer: attached.name, HerdrAnswerResult: result}, nil
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
	return &HerdrSession{Computer: attached.name, HerdrSession: session}, nil
}

// HerdrSetup says what setting up the hooks did, on which computer.
type HerdrSetup struct {
	Computer string
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
	return &HerdrSetup{Computer: attached.name, HerdrSetupResult: result}, nil
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

// tellHerdrQuestion writes a session's question into the person's main
// conversation: a marker line for the transcript's readers and the next
// turn, then the question in the agent's words, which opens the drawer
// and goes on to their chat apps. A question said again after a reconnect
// is written once.
func (self *Agent) tellHerdrQuestion(agentId string, attached *attachedComputer, event *computer.HerdrEvent) {
	defer self.acknowledgeHerdr(self.currentComputer(agentId, attached), event.HerdrEventID)
	session := event.HerdrSession
	if session.Question == nil {
		return
	}
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
	_, isTold := self.herdrTold[key]
	self.herdrTold[key] = time.Now()
	self.backgroundMutex.Unlock()
	if isTold {
		return
	}
	checkIn, said := herdrQuestionCheckIn(attached.name, session), herdrQuestionSaid(attached.name, session)
	for try := 0; try < herdrQuestionTries; try++ {
		err := self.writeHerdrQuestion(agentId, checkIn, said)
		if err == nil {
			return
		}
		if !errors.Is(err, errTurnRunning) {
			log.Warningf("cannot tell the person of the herdr question in %s on %q: %s", session.PaneID, attached.name, err)
			return
		}
		select {
		case <-self.ctx.Done():
			return
		case <-time.After(herdrQuestionRetry):
		}
	}
}

// writeHerdrQuestion writes the two messages into the main conversation,
// when no turn runs there, and says so on the feed.
func (self *Agent) writeHerdrQuestion(agentId, checkIn, said string) error {
	ctx := self.ctx
	var main *models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		main, err = scheduleConversation(tx, agentId, "")
		return err
	}); err != nil {
		return err
	}
	isWritten, err := self.whileNoTurnRuns(main.ID, func() error {
		return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: main.ID, Role: "user", Content: checkIn}); err != nil {
				return err
			}
			_, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: main.ID, Role: "assistant", Content: said})
			return err
		})
	})
	if err != nil {
		return err
	}
	if !isWritten {
		return errTurnRunning
	}
	runId, at := security.NewULID(), time.Now()
	for sequence, event := range []Event{
		{Kind: EventAsked, Text: checkIn, Note: herdrQuestionSurface},
		{Kind: EventMessage, Text: said},
		{Kind: EventDone},
	} {
		event.RunID, event.ConversationID, event.Sequence, event.At = runId, main.ID, sequence, at
		self.publish(event, true)
	}
	return nil
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

// herdrQuestionCheckIn is the marker line a question is written under. The
// computer, the pane and the fingerprint follow the marker, for the drawer's
// card and for the turn that passes the person's answer on. What the
// session asked comes from its screen, so it is fenced as data.
func herdrQuestionCheckIn(computerName string, session *computer.HerdrSession) string {
	question := session.Question
	var asked strings.Builder
	asked.WriteString(question.QuestionText + "\n")
	for _, option := range question.Options {
		fmt.Fprintf(&asked, "%d. %s\n", option.OptionNumber, option.OptionLabel)
	}
	return fmt.Sprintf("%s %s %s %s\n", models.HerdrQuestionMarker, computerName, session.PaneID, question.QuestionFingerprint) +
		fmt.Sprintf("Nobody asked for this: the %s session in pane %s on %s (%s) stopped to ask the person something, and you showed it to them, unasked. What it asked:\n",
			codingAgentName(session.CodingAgentKind), session.PaneID, computerName, session.WorkingDirectory) +
		fenced(strings.TrimSpace(asked.String())) + "\n" +
		fmt.Sprintf("If they answer, pass their choice on with the herdr tool's answer (computer %q, pane %q, question_fingerprint %q) and the option numbers or the text they chose. Never choose for them.",
			computerName, session.PaneID, question.QuestionFingerprint)
}

// herdrQuestionSaid is the question as the person reads it, in the drawer
// and in their chat apps.
func herdrQuestionSaid(computerName string, session *computer.HerdrSession) string {
	question := session.Question
	var said strings.Builder
	what := "asks"
	switch question.HerdrQuestionKind {
	case computer.HerdrQuestionKindToolApproval:
		what = "asks for your approval"
	case computer.HerdrQuestionKindPlanApproval:
		what = "asks you to approve its plan"
	}
	where := session.WorkingDirectory
	if title := strings.TrimSpace(session.PaneTitle); title != "" {
		where = title + ", " + where
	}
	fmt.Fprintf(&said, "**%s** in pane %s on %s (%s) %s:\n\n", codingAgentName(session.CodingAgentKind), session.PaneID, computerName, where, what)
	for _, line := range strings.Split(question.QuestionText, "\n") {
		said.WriteString("> " + line + "\n")
	}
	said.WriteString("\n")
	for _, option := range question.Options {
		fmt.Fprintf(&said, "%d. %s", option.OptionNumber, option.OptionLabel)
		if option.OptionDescription != "" {
			said.WriteString(": " + option.OptionDescription)
		}
		said.WriteString("\n")
	}
	if question.IsMultipleChoice {
		said.WriteString("\nYou may choose several.")
	}
	said.WriteString("\nAnswer here, or in the pane.")
	return said.String()
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
		fmt.Fprintf(builder, "%s computer: %s\npane: %s\ncoding agent: %s\ndirectory: %s\nstate: %s\n", models.HerdrSessionMarker,
			settling.computer.name, session.PaneID, codingAgentName(session.CodingAgentKind), session.WorkingDirectory, session.HerdrSessionState)
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
		described = append(described, fmt.Sprintf("pane %s on %s, %s", settling.event.HerdrSession.PaneID, settling.computer.name, settling.event.HerdrSession.HerdrSessionState))
	}
	return "Herdr sessions finished: " + strings.Join(described, "; ")
}
