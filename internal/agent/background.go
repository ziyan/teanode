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
//
// Finished background work -- a survey or a subagent the agent did not
// wait for, background_work.go -- wakes the conversation through the same
// waker, under the same bounds and the same count.

const (
	// backgroundWakeGather is how long an ending waits for others before
	// the turn it wakes starts: three commands started together end
	// together, and they are one turn, not three.
	backgroundWakeGather = 2 * time.Second

	// A wake that failed is tried again after backgroundWakeRetry, and
	// given up after backgroundWakeAttempts in all.
	backgroundWakeRetry    = time.Minute
	backgroundWakeAttempts = 5

	// backgroundTailCharacters is how much of each stream a wake carries.
	// The notice brings a few thousand bytes of each; the model asks for
	// more with the shell tool's read.
	backgroundTailCharacters = 4000

	// backgroundSurface is the surface of a turn an ended command or
	// finished background work wakes.
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

// backgroundWake is what a conversation has waiting to wake it -- ended
// commands and finished background work -- and how many times waking it
// has failed.
type backgroundWake struct {
	agentId      string
	endings      []backgroundEnding
	works        []*models.AgentBackgroundWork
	isQueued     bool
	attemptCount int
}

// backgroundWorkInFlight is the key finished work is held under in the
// background in-flight set, apart from the ids of commands.
func backgroundWorkInFlight(workId string) string {
	return "work:" + workId
}

// ensureBackgroundLocked makes the maps the background waker keeps.
// Called with the background lock held.
func (self *Agent) ensureBackgroundLocked() {
	if self.backgroundInFlight == nil {
		self.backgroundInFlight = map[string]bool{}
		self.backgroundWakes = map[string]*backgroundWake{}
	}
}

// wakeForBackgroundWork has finished work wake the conversation that
// started it, with whatever else ends there meanwhile. Work started from
// the API, which has no conversation, wakes nothing.
//
// The row is claimed first, and only the claimer wakes: the instance that
// ran the work and another instance's sweep can both find it finished and
// unwoken, and without the claim both woke the conversation.
func (self *Agent) wakeForBackgroundWork(work *models.AgentBackgroundWork) {
	if work.ConversationID == "" {
		return
	}
	if !self.claimBackgroundWorkWake(work.ID) {
		return
	}
	self.backgroundMutex.Lock()
	defer self.backgroundMutex.Unlock()
	self.ensureBackgroundLocked()
	// Its turn is still to come here, and has run so long that the claim
	// expired and this instance took it again: the turn already has it.
	if self.backgroundInFlight[backgroundWorkInFlight(work.ID)] {
		return
	}
	self.backgroundInFlight[backgroundWorkInFlight(work.ID)] = true
	wake := self.backgroundWakes[work.ConversationID]
	if wake == nil {
		wake = &backgroundWake{agentId: work.AgentID}
		self.backgroundWakes[work.ConversationID] = wake
	}
	wake.works = append(wake.works, work)
	self.queueWakeLocked(work.ConversationID, wake, backgroundWakeGather)
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
	// Nothing to wake: started by a run with no conversation to wake, or
	// by somebody else's agent, or before there was an origin at all. It is acknowledged, so that it is
	// not said again, and its output can still be read.
	if origin.AgentID != agentId || origin.ConversationID == "" || origin.IsUnwakeable {
		go self.acknowledgeBackground(found, status.ID)
		return
	}

	self.backgroundMutex.Lock()
	defer self.backgroundMutex.Unlock()
	self.ensureBackgroundLocked()
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
// connects; finished work is left unwoken, and the sweep finds it again.
func (self *Agent) wakeForBackground(conversationId string) {
	self.backgroundMutex.Lock()
	wake := self.backgroundWakes[conversationId]
	delete(self.backgroundWakes, conversationId)
	self.backgroundMutex.Unlock()
	if wake == nil || (len(wake.endings) == 0 && len(wake.works) == 0) {
		return
	}
	err := self.tryWakeForBackground(conversationId, wake)

	self.backgroundMutex.Lock()
	defer self.backgroundMutex.Unlock()
	if err != nil && wake.attemptCount+1 < backgroundWakeAttempts && self.ctx.Err() == nil {
		log.Warningf("cannot wake conversation %q for background work or a command that ended, trying again in %s: %s", conversationId, backgroundWakeRetry, err)
		// What ended while this one failed joins it.
		retry := &backgroundWake{agentId: wake.agentId, endings: wake.endings, works: wake.works, attemptCount: wake.attemptCount + 1}
		if waiting := self.backgroundWakes[conversationId]; waiting != nil {
			retry.endings = append(retry.endings, waiting.endings...)
			retry.works = append(retry.works, waiting.works...)
			retry.isQueued = waiting.isQueued
		}
		self.backgroundWakes[conversationId] = retry
		self.queueWakeLocked(conversationId, retry, backgroundWakeRetry)
		return
	}
	if err != nil {
		log.Warningf("cannot wake conversation %q for background work or a command that ended; a command is said again when the computer next connects, and work is found again by the sweep: %s", conversationId, err)
		self.releaseBackgroundWorkWakes(wake.works)
	}
	for _, ending := range wake.endings {
		delete(self.backgroundInFlight, ending.status.ID)
	}
	for _, work := range wake.works {
		delete(self.backgroundInFlight, backgroundWorkInFlight(work.ID))
	}
}

// tryWakeForBackground starts the turn that tells the agent, then
// acknowledges the endings, and marks the work woken, once that turn is
// over. A server that stops before then leaves them unacknowledged, and
// the computer says them again, or the sweep finds the work again: a turn
// twice is better than none.
func (self *Agent) tryWakeForBackground(conversationId string, wake *backgroundWake) error {
	// Told to the computer as it is attached now: a turn can outlast the
	// connection the ending came in on, and an acknowledgement sent to
	// the one that went is lost, which has the ending said again at a
	// later reconnect and the agent woken a second time for it.
	acknowledge := func() {
		for _, ending := range wake.endings {
			self.acknowledgeBackground(self.currentComputer(wake.agentId, ending.computer), ending.status.ID)
		}
		if len(wake.works) == 0 {
			return
		}
		workIds := make([]string, 0, len(wake.works))
		for _, work := range wake.works {
			workIds = append(workIds, work.ID)
		}
		// Not the server's context, which a stop has already ended.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(self.ctx), jobCompletionTimeout)
		defer cancel()
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.MarkAgentBackgroundWorkWoken(workIds, time.Now())
		}); err != nil {
			log.Warningf("cannot mark background work %v as told: %s", workIds, err)
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
	// tell, and the output can still be read on the computer, the result
	// through the API.
	if conversation == nil || !FeatureAllowed(configuration, "ask") || self.operations == nil {
		acknowledge()
		return nil
	}

	// Out of turns or out of budget: the endings are written into the
	// transcript for the person and the next turn to read, and no model is
	// asked anything. Commands and work share the count: a survey whose
	// turn starts a subagent whose turn starts a command is one chain. The
	// count is the conversation's row, so the bound holds whichever
	// instance each wake of the chain lands on.
	backgroundWakesAlone := configuration.Agent.Limits.EffectiveBackgroundWakesAlone()
	if conversation.BackgroundWakeCount >= backgroundWakesAlone || deferral != nil {
		reason := fmt.Sprintf("%d turns since you last wrote were woken by background commands and work", backgroundWakesAlone)
		if deferral != nil {
			reason = deferral.Reason
		}
		note := fmt.Sprintf("%s; not woken again (%s).", backgroundEndedLine(wake.endings, wake.works), reason)
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
		Message: backgroundWakeMessage(wake.endings, wake.works), Surface: backgroundSurface,
		UsageKind: backgroundSurface,
	})
	if err != nil {
		return err
	}
	// Counted once the turn is started, so a wake that failed does not
	// spend one of the conversation's turns. The turn runs whether or not
	// the count is written; a count that was not costs the chain one more
	// turn before the bound, not the turn.
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.AddAgentConversationBackgroundWake(conversationId)
	}); err != nil {
		log.Warningf("cannot count the turn woken in conversation %q: %s", conversationId, err)
	}
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

// claimBackgroundWorkWake takes the waking of the conversation for a
// piece of finished work, and says whether this instance has it. A claim
// held by an instance that stopped before the turn was over expires after
// backgroundWorkWakeClaimExpiry, and the sweep takes it then.
func (self *Agent) claimBackgroundWorkWake(workId string) bool {
	ctx, cancel := context.WithTimeout(self.ctx, jobCompletionTimeout)
	defer cancel()
	now := time.Now()
	isClaimed := false
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		isClaimed, err = tx.ClaimAgentBackgroundWorkWake(workId, now, now.Add(-backgroundWorkWakeClaimExpiry))
		return err
	}); err != nil {
		log.Warningf("cannot claim the wake for background work %s; the sweep tries again: %s", workId, err)
		return false
	}
	return isClaimed
}

// releaseBackgroundWorkWakes lets go of the claims of a wake that was
// given up, for the sweep to take again at its next pass rather than once
// they expire.
func (self *Agent) releaseBackgroundWorkWakes(works []*models.AgentBackgroundWork) {
	if len(works) == 0 {
		return
	}
	workIds := make([]string, 0, len(works))
	for _, work := range works {
		workIds = append(workIds, work.ID)
	}
	// Not the server's context, which a stop has already ended.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(self.ctx), jobCompletionTimeout)
	defer cancel()
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.ReleaseAgentBackgroundWorkWakes(workIds)
	}); err != nil {
		log.Warningf("cannot let go of the wake for background work %v; it is taken again once it expires: %s", workIds, err)
	}
}

// backgroundWakeMessage is what the woken turn is given.
//
// It begins with a marker, so the transcript's readers know the person
// did not write it, and it carries the output and the results fenced,
// because what a command printed is data from the machine, and what a
// survey or a subagent answered was made from what it read, and neither
// is ever an instruction.
func backgroundWakeMessage(endings []backgroundEnding, works []*models.AgentBackgroundWork) string {
	var builder strings.Builder
	switch {
	case len(endings) == 1:
		builder.WriteString(models.BackgroundCommandMarker + " A command you left running in the background has ended. This is not the person speaking; they may not be watching.\n")
	case len(endings) > 1:
		fmt.Fprintf(&builder, "%s %d commands you left running in the background have ended. This is not the person speaking; they may not be watching.\n", models.BackgroundCommandMarker, len(endings))
	case len(works) == 1:
		builder.WriteString(models.BackgroundWorkMarker + " Work you started in the background has finished. This is not the person speaking; they may not be watching.\n")
	default:
		fmt.Fprintf(&builder, "%s %d pieces of work you started in the background have finished. This is not the person speaking; they may not be watching.\n", models.BackgroundWorkMarker, len(works))
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
	for _, work := range works {
		builder.WriteString("\n")
		// Each piece of work is marked too, so that one finishing beside
		// a command is not read as part of the command's output.
		fmt.Fprintf(&builder, "%s id: %s\nwhat: %s\noutcome: %s\n", models.BackgroundWorkMarker, work.ID, backgroundWorkWhat(work), backgroundWorkOutcome(work))
		if work.StartedAt != nil && work.FinishedAt != nil {
			fmt.Fprintf(&builder, "ran for: %s\n", work.FinishedAt.Sub(*work.StartedAt).Round(time.Second))
		}
		if work.WorkStatus == models.BackgroundWorkFailed {
			builder.WriteString("error:\n" + fenced(work.ErrorMessage) + "\n")
			continue
		}
		characterCount := len([]rune(work.ResultText))
		shown := cutRunes(work.ResultText, backgroundWorkResultCharacters)
		if characterCount > backgroundWorkResultCharacters {
			fmt.Fprintf(&builder, "result (%d characters in all, the first of it here):\n", characterCount)
		} else {
			builder.WriteString("result:\n")
		}
		builder.WriteString(fenced(shown) + "\n")
	}
	builder.WriteString("\nCarry on with what you started it for: act on how it ended, and tell the person what came of it.")
	if len(endings) > 0 {
		builder.WriteString(" shell with action read and the id gives more of the output.")
	}
	if len(works) > 0 {
		builder.WriteString(" background_work with action read and the id gives the whole of a result.")
	}
	return builder.String()
}

// backgroundOutcome is how one ended, in a line.
func backgroundOutcome(status *computer.BackgroundStatus) string {
	switch status.StopReason {
	// Stopped when asked is the person: the agent's own stops are
	// acknowledged as they are made, and never wake it.
	case computer.BackgroundStopReasonStopped:
		return "stopped by the person before it ended"
	case computer.BackgroundStopReasonLifetime:
		return "stopped after running 24 hours, as long as a background command may"
	case computer.BackgroundStopReasonShutdown:
		return "stopped because teanode computer was stopped on that machine"
	}
	return fmt.Sprintf("exit code: %d", status.ExitCode)
}

// backgroundEndedLine is the endings and the finished work in a line,
// for a note.
func backgroundEndedLine(endings []backgroundEnding, works []*models.AgentBackgroundWork) string {
	var said []string
	if len(endings) > 0 {
		described := make([]string, 0, len(endings))
		for _, ending := range endings {
			described = append(described, fmt.Sprintf("%q on %s, %s", tools.FirstWords(ending.status.Command, 8), ending.computer.name, backgroundOutcome(ending.status)))
		}
		said = append(said, "Background commands ended: "+strings.Join(described, "; "))
	}
	if len(works) > 0 {
		described := make([]string, 0, len(works))
		for _, work := range works {
			described = append(described, fmt.Sprintf("%s (%s), %s", backgroundWorkWhat(work), work.ID, backgroundWorkOutcome(work)))
		}
		said = append(said, "Background work finished: "+strings.Join(described, "; "))
	}
	return strings.Join(said, ". ")
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
