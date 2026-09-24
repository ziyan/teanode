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
)

// Background commands: the ones the agent left running on the person's
// computer, and the turn that wakes it when one ends.
//
// The program on the computer holds them, and says when one ends. It says
// so with the origin the shell tool gave it when it started, which names the
// conversation to wake, and it says it again after every reconnect until it
// is acknowledged. So nothing about them is stored here: the program is the
// record, and what this side keeps is only what is in flight.
//
// The turn it wakes is the conversation's own, carried on from the one that
// started the command, the way a person's turn that runs on after they have
// looked away is still theirs: its cards are shown in the drawer and wait
// for them as any card does. What bounds it is that it only follows a turn
// the person took, never one of the agent's own with nobody present, and
// that a chain of commands that wake turns that start commands stops after
// backgroundWakesAlone, until the person writes again.

const (
	// backgroundWakeGather is how long an ending waits for others before
	// the turn it wakes starts: three commands started together end
	// together, and they are one turn, not three.
	backgroundWakeGather = 2 * time.Second

	// backgroundWakesAlone is how many turns ended commands wake in one
	// conversation before the person writes again.
	backgroundWakesAlone = 20

	// A wake that failed is tried again after backgroundWakeRetry, and
	// given up after backgroundWakeAttempts in all.
	backgroundWakeRetry    = time.Minute
	backgroundWakeAttempts = 5

	// backgroundTailCharacters is how much of each stream a wake carries.
	// The notice brings a few thousand bytes of each; the model asks for
	// more with the shell tool's read.
	backgroundTailCharacters = 4000

	// backgroundSurface is the surface of a turn an ended command wakes.
	backgroundSurface = "background"

	// backgroundListWait is how long listing waits for each computer. The
	// program answers from memory, so a computer slower than this is one
	// that is not answering.
	backgroundListWait = 10 * time.Second
)

// backgroundEnding is one ended command waiting for its turn.
type backgroundEnding struct {
	computer *attachedComputer
	status   *computer.BackgroundStatus
}

// backgroundWake is what a conversation has waiting to wake it, and how
// many times waking it has failed.
type backgroundWake struct {
	agentId      string
	endings      []backgroundEnding
	isQueued     bool
	attemptCount int
}

// ComputerBackgroundEnded is a computer saying a background command ended.
func (self *Agent) ComputerBackgroundEnded(agentId string, connection DeviceConnection, data json.RawMessage) {
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
	var status computer.BackgroundStatus
	if err := json.Unmarshal(data, &status); err != nil || status.ID == "" {
		log.Warningf("the computer %q said a background command ended, unreadably: %v", found.name, err)
		return
	}
	var origin tools.BackgroundOrigin
	_ = json.Unmarshal(status.Origin, &origin)
	// Nothing to wake: started by a turn with nobody present, which has
	// ended and which nobody is reading, or by somebody else's agent, or
	// before there was an origin at all. It is acknowledged, so that it is
	// not said again, and its output can still be read.
	if origin.AgentID != agentId || origin.ConversationID == "" || origin.IsHeadless {
		go self.acknowledgeBackground(found, status.ID)
		return
	}

	self.backgroundMutex.Lock()
	defer self.backgroundMutex.Unlock()
	if self.backgroundInFlight == nil {
		self.backgroundInFlight = map[string]bool{}
		self.backgroundWakes = map[string]*backgroundWake{}
		self.backgroundWakeCounts = map[string]int{}
	}
	// Said again after a reconnect while its turn is still to come: the
	// turn already has it.
	if self.backgroundInFlight[status.ID] {
		return
	}
	self.backgroundInFlight[status.ID] = true
	wake := self.backgroundWakes[origin.ConversationID]
	if wake == nil {
		wake = &backgroundWake{agentId: agentId}
		self.backgroundWakes[origin.ConversationID] = wake
	}
	wake.endings = append(wake.endings, backgroundEnding{computer: found, status: &status})
	self.queueWakeLocked(origin.ConversationID, wake, backgroundWakeGather)
}

// queueWakeLocked starts the wait before a conversation's wake, unless one
// is already waiting. Called with the background lock held.
func (self *Agent) queueWakeLocked(conversationId string, wake *backgroundWake, wait time.Duration) {
	if wake.isQueued {
		return
	}
	wake.isQueued = true
	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		select {
		case <-time.After(wait):
		case <-self.ctx.Done():
			return
		}
		self.wakeForBackground(conversationId)
	}()
}

// wakeForBackground takes what is waiting for a conversation and wakes it.
// A wake that fails for a passing reason -- the database, the model's
// provider -- is tried again a minute later, a few times, with whatever
// else ended meanwhile. After that the endings are let go of without
// being acknowledged, and the computer says them again when it next
// connects.
func (self *Agent) wakeForBackground(conversationId string) {
	self.backgroundMutex.Lock()
	wake := self.backgroundWakes[conversationId]
	delete(self.backgroundWakes, conversationId)
	self.backgroundMutex.Unlock()
	if wake == nil || len(wake.endings) == 0 {
		return
	}
	err := self.tryWakeForBackground(conversationId, wake)

	self.backgroundMutex.Lock()
	defer self.backgroundMutex.Unlock()
	if err != nil && wake.attemptCount+1 < backgroundWakeAttempts && self.ctx.Err() == nil {
		log.Warningf("cannot wake conversation %q for a background command that ended, trying again in %s: %s", conversationId, backgroundWakeRetry, err)
		// What ended while this one failed joins it.
		retry := &backgroundWake{agentId: wake.agentId, endings: wake.endings, attemptCount: wake.attemptCount + 1}
		if waiting := self.backgroundWakes[conversationId]; waiting != nil {
			retry.endings = append(retry.endings, waiting.endings...)
			retry.isQueued = waiting.isQueued
		}
		self.backgroundWakes[conversationId] = retry
		self.queueWakeLocked(conversationId, retry, backgroundWakeRetry)
		return
	}
	if err != nil {
		log.Warningf("cannot wake conversation %q for a background command that ended; it is said again when the computer next connects: %s", conversationId, err)
	}
	for _, ending := range wake.endings {
		delete(self.backgroundInFlight, ending.status.ID)
	}
}

// tryWakeForBackground starts the turn that tells the agent, then
// acknowledges the endings once that turn is over. A server that stops
// before then leaves them unacknowledged, and the computer says them
// again: a turn twice is better than none.
func (self *Agent) tryWakeForBackground(conversationId string, wake *backgroundWake) error {
	// Told to the computer as it is attached now: a turn can outlast the
	// connection the ending came in on, and an acknowledgement sent to
	// the one that went is lost, which has the ending said again at a
	// later reconnect and the agent woken a second time for it.
	acknowledge := func() {
		for _, ending := range wake.endings {
			self.acknowledgeBackground(self.currentComputer(wake.agentId, ending.computer), ending.status.ID)
		}
	}

	ctx := self.ctx
	configuration := self.settings.Configuration()
	var agent *models.Agent
	var owner *models.User
	var conversation *models.AgentConversation
	var deferral *Deferral
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if conversation, err = tx.GetAgentConversation(conversationId); err != nil || conversation == nil || conversation.AgentID != wake.agentId {
			conversation = nil
			return err
		}
		if agent, err = tx.GetAgent(wake.agentId); err != nil || agent == nil || !agent.Active() {
			conversation = nil
			return err
		}
		if owner, err = tx.GetUser(agent.UserID); err != nil || owner == nil {
			conversation = nil
			return err
		}
		if err := RequireBudget(tx, configuration, agent, owner, time.Now()); err != nil && !errors.As(err, &deferral) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	// The conversation is gone, or the agent is off: there is nobody to
	// tell, and the output can still be read on the computer.
	if conversation == nil || !FeatureAllowed(configuration, "ask") || self.operations == nil {
		acknowledge()
		return nil
	}

	self.backgroundMutex.Lock()
	wokenCount := self.backgroundWakeCounts[conversationId]
	self.backgroundMutex.Unlock()
	// Out of turns or out of budget: the endings are written into the
	// transcript for the person and the next turn to read, and no model is
	// asked anything.
	if wokenCount >= backgroundWakesAlone || deferral != nil {
		reason := fmt.Sprintf("%d turns since you last wrote were woken by background commands", backgroundWakesAlone)
		if deferral != nil {
			reason = deferral.Reason
		}
		note := fmt.Sprintf("%s; not woken again (%s).", backgroundEndingsLine(wake.endings), reason)
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversationId, Role: models.AgentMessageNote, Content: note})
			return err
		}); err != nil {
			return err
		}
		acknowledge()
		return nil
	}

	operations, err := self.operations(ctx, owner)
	if err != nil {
		return fmt.Errorf("cannot act as %q: %w", owner.Username, err)
	}
	turn, err := self.Ask(&AskSettings{
		Agent: agent, Owner: owner, Operations: operations, Conversation: conversation,
		Message: backgroundWakeMessage(wake.endings), Surface: backgroundSurface,
		UsageKind: backgroundSurface,
	})
	if err != nil {
		return err
	}
	// Counted once the turn is started, so a wake that failed does not
	// spend one of the conversation's turns.
	self.backgroundMutex.Lock()
	self.backgroundWakeCounts[conversationId]++
	self.backgroundMutex.Unlock()
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()
	for range events {
	}
	acknowledge()
	return nil
}

// acknowledgeBackground tells the computer the server has heard that one
// ended, so it is not said again.
func (self *Agent) acknowledgeBackground(attached *attachedComputer, id string) {
	ctx, cancel := context.WithTimeout(self.ctx, deviceAnswerWait)
	defer cancel()
	if _, err := attached.Ask(ctx, "background_acknowledge", &computer.BackgroundAcknowledgeArguments{IDs: []string{id}}, deviceAnswerWait); err != nil {
		log.Warningf("cannot acknowledge background command %s on %q: %s", id, attached.name, err)
	}
}

// currentComputer is the computer attached under the same name now, or
// the one given when that name has none.
func (self *Agent) currentComputer(agentId string, attached *attachedComputer) *attachedComputer {
	self.computersMutex.Lock()
	defer self.computersMutex.Unlock()
	if current := self.computers[agentId][attached.name]; current != nil {
		return current
	}
	return attached
}

// personTookTurn starts a conversation's count of woken turns again.
func (self *Agent) personTookTurn(conversationId string) {
	self.backgroundMutex.Lock()
	delete(self.backgroundWakeCounts, conversationId)
	self.backgroundMutex.Unlock()
}

// backgroundWakeMessage is what the woken turn is given.
//
// It begins with the marker, so the transcript's readers know the person
// did not write it, and it carries the output fenced, because what a
// command printed is data from the machine and never an instruction.
func backgroundWakeMessage(endings []backgroundEnding) string {
	var builder strings.Builder
	if len(endings) == 1 {
		builder.WriteString(models.BackgroundCommandMarker + " A command you left running in the background has ended. This is not the person speaking; they may not be watching.\n")
	} else {
		fmt.Fprintf(&builder, "%s %d commands you left running in the background have ended. This is not the person speaking; they may not be watching.\n", models.BackgroundCommandMarker, len(endings))
	}
	for _, ending := range endings {
		status := ending.status
		builder.WriteString("\n")
		fmt.Fprintf(&builder, "id: %s\ncomputer: %s\ncommand: %s\n", status.ID, ending.computer.name, status.Command)
		builder.WriteString(backgroundOutcome(status) + "\n")
		if status.EndedAt != nil {
			fmt.Fprintf(&builder, "ran for: %s\n", status.EndedAt.Sub(status.StartedAt).Round(time.Second))
		}
		var output strings.Builder
		fmt.Fprintf(&output, "stdout (%d bytes in all%s):\n%s\n", status.StdoutByteCount, lastOf(status.IsStdoutTruncated), lastCharacters(status.Stdout, backgroundTailCharacters))
		fmt.Fprintf(&output, "stderr (%d bytes in all%s):\n%s", status.StderrByteCount, lastOf(status.IsStderrTruncated), lastCharacters(status.Stderr, backgroundTailCharacters))
		builder.WriteString(fenced(output.String()) + "\n")
	}
	builder.WriteString("\nCarry on with what you started it for: act on how it ended, and tell the person what came of it. shell with action read and the id gives more of the output.")
	return builder.String()
}

// backgroundOutcome is how one ended, in a line.
func backgroundOutcome(status *computer.BackgroundStatus) string {
	switch status.StopReason {
	case computer.BackgroundStopReasonStopped:
		return "stopped before it ended: by the person, or with the computer's program"
	case computer.BackgroundStopReasonLifetime:
		return "stopped: it ran as long as a background command may"
	}
	return fmt.Sprintf("exit code: %d", status.ExitCode)
}

// backgroundEndingsLine is the endings in a line, for a note.
func backgroundEndingsLine(endings []backgroundEnding) string {
	described := make([]string, 0, len(endings))
	for _, ending := range endings {
		described = append(described, fmt.Sprintf("%q on %s, %s", tools.FirstWords(ending.status.Command, 8), ending.computer.name, backgroundOutcome(ending.status)))
	}
	return "Background commands ended: " + strings.Join(described, "; ")
}

func lastOf(isTruncated bool) string {
	if isTruncated {
		return ", the last of it here"
	}
	return ""
}

// lastCharacters is the end of a text, cut on a character.
func lastCharacters(text string, most int) string {
	characters := []rune(text)
	if len(characters) <= most {
		return text
	}
	return string(characters[len(characters)-most:])
}

// BackgroundCommand is one background command on one of a person's
// computers, for the API.
type BackgroundCommand struct {
	Computer string
	*computer.BackgroundStatus
	Origin tools.BackgroundOrigin
}

// BackgroundCommands are the background commands on a person's computers,
// newest first on each.
func (self *Agent) BackgroundCommands(ctx context.Context, agentId string) ([]*BackgroundCommand, error) {
	// Every computer is asked at once, so one that does not answer costs
	// the list its wait once rather than once per computer before it.
	attached := self.computersFor(agentId)
	answers := make([][]*BackgroundCommand, len(attached))
	var waitGroup sync.WaitGroup
	for index, one := range attached {
		if !one.HasBackground() {
			continue
		}
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			// A computer that does not answer is left out rather than
			// taking the others' list down with it; the dashboard asks
			// again soon.
			answer, err := one.Ask(ctx, "background_list", struct{}{}, backgroundListWait)
			if err != nil {
				log.Noticef("cannot list the background commands on %q: %s", one.name, err)
				return
			}
			var statuses []*computer.BackgroundStatus
			if err := json.Unmarshal(answer, &statuses); err != nil {
				log.Warningf("%s answered the list of background commands unreadably: %s", one.what, err)
				return
			}
			for _, status := range statuses {
				answers[index] = append(answers[index], backgroundCommandOf(one, status))
			}
		}()
	}
	waitGroup.Wait()
	listed := []*BackgroundCommand{}
	for _, commands := range answers {
		// Only this agent's: what another server's agent left on the same
		// machine is not this one's to show or to stop.
		for _, command := range commands {
			if command.Origin.AgentID == agentId {
				listed = append(listed, command)
			}
		}
	}
	return listed, nil
}

// ReadBackgroundCommand is one of this agent's, with the last tailBytes of
// each stream.
func (self *Agent) ReadBackgroundCommand(ctx context.Context, agentId, computerName, id string, tailBytes int) (*BackgroundCommand, error) {
	attached, err := self.backgroundComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	return readBackground(ctx, attached, agentId, id, tailBytes)
}

// StopBackgroundCommand stops one of this agent's for the person. The
// agent is told it ended, as it is of any ending it did not ask for.
func (self *Agent) StopBackgroundCommand(ctx context.Context, agentId, computerName, id string) (*BackgroundCommand, error) {
	attached, err := self.backgroundComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	// Read first, for whose it is: the list shows only this agent's, and
	// stopping must not reach further than seeing does.
	if _, err := readBackground(ctx, attached, agentId, id, 1); err != nil {
		return nil, err
	}
	answer, err := attached.Ask(ctx, "background_stop", &computer.BackgroundStopArguments{ID: id}, deviceAnswerWait)
	if err != nil {
		return nil, err
	}
	var status computer.BackgroundStatus
	if err := json.Unmarshal(answer, &status); err != nil {
		return nil, fmt.Errorf("%s answered something unreadable: %w", attached.what, err)
	}
	return backgroundCommandOf(attached, &status), nil
}

// readBackground reads one, and says there is no such command when it is
// not this agent's.
func readBackground(ctx context.Context, attached *attachedComputer, agentId, id string, tailBytes int) (*BackgroundCommand, error) {
	answer, err := attached.Ask(ctx, "background_read", &computer.BackgroundReadArguments{ID: id, TailBytes: tailBytes}, deviceAnswerWait)
	if err != nil {
		return nil, err
	}
	var status computer.BackgroundStatus
	if err := json.Unmarshal(answer, &status); err != nil {
		return nil, fmt.Errorf("%s answered something unreadable: %w", attached.what, err)
	}
	command := backgroundCommandOf(attached, &status)
	if command.Origin.AgentID != agentId {
		return nil, fmt.Errorf("there is no background command %q on %s", id, attached.name)
	}
	return command, nil
}

func (self *Agent) backgroundComputer(agentId, computerName string) (*attachedComputer, error) {
	for _, attached := range self.computersFor(agentId) {
		if strings.EqualFold(attached.name, strings.TrimSpace(computerName)) {
			if !attached.HasBackground() {
				return nil, fmt.Errorf("the program on %s does not keep background commands; update teanode there", attached.name)
			}
			return attached, nil
		}
	}
	return nil, fmt.Errorf("no computer called %q is attached", computerName)
}

func backgroundCommandOf(attached *attachedComputer, status *computer.BackgroundStatus) *BackgroundCommand {
	command := &BackgroundCommand{Computer: attached.name, BackgroundStatus: status}
	_ = json.Unmarshal(status.Origin, &command.Origin)
	return command
}
