package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

	// backgroundTailCharacters is how much of each stream a wake carries.
	// The notice brings a few thousand bytes of each; the model asks for
	// more with the shell tool's read.
	backgroundTailCharacters = 4000

	// backgroundSurface is the surface of a turn an ended command wakes.
	backgroundSurface = "background"
)

// backgroundEnding is one ended command waiting for its turn.
type backgroundEnding struct {
	computer *attachedComputer
	status   *computer.BackgroundStatus
}

// backgroundWake is what a conversation has waiting to wake it.
type backgroundWake struct {
	agentId  string
	endings  []backgroundEnding
	isQueued bool
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
	if wake.isQueued {
		return
	}
	wake.isQueued = true
	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		select {
		case <-time.After(backgroundWakeGather):
		case <-self.ctx.Done():
			return
		}
		self.wakeForBackground(origin.ConversationID)
	}()
}

// wakeForBackground takes what is waiting for a conversation and starts
// the turn that tells the agent, then acknowledges it once that turn is
// over. A server that stops before then leaves it unacknowledged, and the
// computer says it again: a turn twice is better than none.
func (self *Agent) wakeForBackground(conversationId string) {
	self.backgroundMutex.Lock()
	wake := self.backgroundWakes[conversationId]
	delete(self.backgroundWakes, conversationId)
	self.backgroundMutex.Unlock()
	if wake == nil || len(wake.endings) == 0 {
		return
	}
	defer func() {
		self.backgroundMutex.Lock()
		for _, ending := range wake.endings {
			delete(self.backgroundInFlight, ending.status.ID)
		}
		self.backgroundMutex.Unlock()
	}()
	acknowledge := func() {
		for _, ending := range wake.endings {
			self.acknowledgeBackground(ending.computer, ending.status.ID)
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
		log.Warningf("cannot wake conversation %q for a background command that ended: %s", conversationId, err)
		return
	}
	// The conversation is gone, or the agent is off: there is nobody to
	// tell, and the output can still be read on the computer.
	if conversation == nil || !FeatureAllowed(configuration, "ask") || self.operations == nil {
		acknowledge()
		return
	}

	self.backgroundMutex.Lock()
	woken := self.backgroundWakeCounts[conversationId]
	if woken < backgroundWakesAlone && deferral == nil {
		self.backgroundWakeCounts[conversationId] = woken + 1
	}
	self.backgroundMutex.Unlock()
	// Out of turns or out of budget: the endings are written into the
	// transcript for the person and the next turn to read, and no model is
	// asked anything.
	if woken >= backgroundWakesAlone || deferral != nil {
		reason := fmt.Sprintf("%d turns since you last wrote were woken by background commands", backgroundWakesAlone)
		if deferral != nil {
			reason = deferral.Reason
		}
		note := fmt.Sprintf("%s; not woken again (%s).", backgroundEndingsLine(wake.endings), reason)
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversationId, Role: models.AgentMessageNote, Content: note})
			return err
		}); err != nil {
			log.Warningf("cannot note background commands that ended on conversation %q: %s", conversationId, err)
			return
		}
		acknowledge()
		return
	}

	operations, err := self.operations(ctx, owner)
	if err != nil {
		log.Warningf("cannot act as %q to wake conversation %q: %s", owner.Username, conversationId, err)
		return
	}
	turn, err := self.Ask(&AskSettings{
		Agent: agent, Owner: owner, Operations: operations, Conversation: conversation,
		Message: backgroundWakeMessage(wake.endings), Surface: backgroundSurface,
		UsageKind: backgroundSurface,
	})
	if err != nil {
		log.Warningf("cannot wake conversation %q for a background command that ended: %s", conversationId, err)
		return
	}
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()
	for range events {
	}
	acknowledge()
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
	var listed []*BackgroundCommand
	for _, attached := range self.computersFor(agentId) {
		if !attached.HasBackground() {
			continue
		}
		answer, err := attached.Ask(ctx, "background_list", struct{}{}, deviceAnswerWait)
		if err != nil {
			return nil, err
		}
		var statuses []*computer.BackgroundStatus
		if err := json.Unmarshal(answer, &statuses); err != nil {
			return nil, fmt.Errorf("%s answered something unreadable: %w", attached.what, err)
		}
		for _, status := range statuses {
			listed = append(listed, backgroundCommandOf(attached, status))
		}
	}
	return listed, nil
}

// ReadBackgroundCommand is one, with the last tailBytes of each stream.
func (self *Agent) ReadBackgroundCommand(ctx context.Context, agentId, computerName, id string, tailBytes int) (*BackgroundCommand, error) {
	attached, err := self.backgroundComputer(agentId, computerName)
	if err != nil {
		return nil, err
	}
	answer, err := attached.Ask(ctx, "background_read", &computer.BackgroundReadArguments{ID: id, TailBytes: tailBytes}, deviceAnswerWait)
	if err != nil {
		return nil, err
	}
	var status computer.BackgroundStatus
	if err := json.Unmarshal(answer, &status); err != nil {
		return nil, fmt.Errorf("%s answered something unreadable: %w", attached.what, err)
	}
	return backgroundCommandOf(attached, &status), nil
}

// StopBackgroundCommand stops one for the person. The agent is told it
// ended, as it is of any ending it did not ask for.
func (self *Agent) StopBackgroundCommand(ctx context.Context, agentId, computerName, id string) (*BackgroundCommand, error) {
	attached, err := self.backgroundComputer(agentId, computerName)
	if err != nil {
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
