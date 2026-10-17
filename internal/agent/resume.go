package agent

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A restart can end a turn a person is waiting on: a deploy cancels the
// runs in flight (Stop), and the conversation is left ending on their
// message or on a tool's result, with no answer, until they type "try
// again". At the first look after a start, such a conversation is carried
// on by a turn the agent starts for itself.

const (
	// resumeWindow is how far back a turn may have been cut short and
	// still be carried on. Beyond it the person has long since asked
	// again or stopped waiting, and an answer out of the blue would be
	// noise. It is also how long after a start the look may still run.
	resumeWindow = 15 * time.Minute

	// resumeMostPerAgent is how many conversations one agent carries on
	// at a start, newest first: a restart cuts a few turns, never dozens.
	resumeMostPerAgent = 3

	// resumeConversations is how many of an agent's newest conversations
	// are looked at. The ones recent enough to have been cut are the
	// first few.
	resumeConversations = 50

	// resumeTailMessages is how many of a conversation's last messages
	// are read to find the message the turn opened with.
	resumeTailMessages = 200

	// resumeSurface is the surface of the turn that carries on: the
	// person's drawer, phone and chat app see it as any turn the agent
	// began, through the marker.
	resumeSurface = "resume"
)

// stopGrace is how long Stop lets the turns people are reading run before
// it cancels them. The server's shutdown timeout is 30 seconds but it is
// spent on the listeners before this, and a container is killed ten seconds
// after it is told to stop, so this stays well inside that. A variable so
// that a test need not wait it out.
var stopGrace = 5 * time.Second

// resumeMessage is what the turn that carries on is given. It begins with
// the marker, so that everything reading the transcript knows the person
// did not write it.
const resumeMessage = models.RestartResumeMarker + " The server restarted while you were answering the person's last message, and your turn was cut off. This is not the person speaking; they may not be watching.\n\n" +
	"Carry on from where you were, using what the transcript above already shows. Do not repeat a tool call whose result is already there. " +
	"A call you made with no result shown may or may not have run: look before you repeat anything that changes something. When you have what you need, answer the person's last message."

// resumeCutTurnsOnce looks, once per start, at the conversations the
// restart cut short. It waits for the way to act as a person, which the
// API sets after the worker starts, and gives up when the window since the
// start has passed.
func (self *Agent) resumeCutTurnsOnce(ctx context.Context) {
	if self.startedAt.IsZero() || self.hasResumedCutTurns.Load() || self.operations == nil {
		return
	}
	if !self.hasResumedCutTurns.CompareAndSwap(false, true) {
		return
	}
	if time.Since(self.startedAt) > resumeWindow {
		return
	}
	self.resumeCutTurns(ctx, self.startedAt)
}

// resumeCutTurns starts, for each agent, a turn in each of its few newest
// conversations that a start at startedAt found cut short, and returns once
// they are started rather than finished.
func (self *Agent) resumeCutTurns(ctx context.Context, startedAt time.Time) {
	configuration := self.settings.Configuration()
	if self.settings.Registry == nil || self.operations == nil || !FeatureAllowed(configuration, "ask") {
		return
	}
	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents whose turns a restart cut short: %s", err)
		return
	}
	for _, agent := range agents {
		if !agent.Active() {
			continue
		}
		self.resumeCutTurnsOf(ctx, agent, startedAt)
	}
}

func (self *Agent) resumeCutTurnsOf(ctx context.Context, agent *models.Agent, startedAt time.Time) {
	configuration := self.settings.Configuration()
	var owner *models.User
	var cut []*models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if owner, err = tx.GetUser(agent.UserID); err != nil || owner == nil {
			return err
		}
		// Out of budget, the turn would only fail and write a note about
		// it: nothing is better than that.
		if err := RequireBudget(tx, configuration, agent, owner, startedAt); err != nil {
			owner = nil
			return nil
		}
		conversations, err := tx.ListAgentConversations(agent.ID, []models.AgentConversationKind{models.AgentConversationMain, models.AgentConversationNamed}, &db.Options{Limit: resumeConversations})
		if err != nil {
			return err
		}
		for _, conversation := range conversations {
			// Newest first, so the first one too old is the end of them.
			if conversation.LastAt.Before(startedAt.Add(-resumeWindow)) {
				break
			}
			if conversation.ArchivedAt != nil || self.isTurnRunning(conversation.ID) {
				continue
			}
			isCut, err := isTurnCutShort(tx, conversation, startedAt)
			if err != nil {
				return err
			}
			if isCut {
				cut = append(cut, conversation)
			}
			if len(cut) == resumeMostPerAgent {
				break
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot look for the turns a restart cut short for agent %q: %s", agent.ID, err)
		return
	}
	if owner == nil || len(cut) == 0 {
		return
	}
	operations, err := self.operations(ctx, owner)
	if err != nil {
		log.Warningf("cannot act as %q to carry on a turn a restart cut short: %s", owner.Username, err)
		return
	}
	for _, conversation := range cut {
		if _, err := self.Ask(&AskSettings{
			Agent: agent, Owner: owner, Operations: operations, Conversation: conversation,
			Message: resumeMessage, Surface: resumeSurface,
		}); err != nil {
			log.Warningf("cannot carry on the turn a restart cut short in conversation %q: %s", conversation.ID, err)
			continue
		}
		log.Noticef("carrying on the turn a restart cut short in conversation %q", conversation.ID)
	}
}

// isTurnCutShort says whether a conversation ends in a turn that answers
// the person and was cut off before it answered: the last thing written is
// their message, or a tool's call or result in a turn that began with their
// message, written within resumeWindow before startedAt.
//
// Not an answer, not a note that ended the turn (stopped by the person,
// failed, out of rounds), not a card the person has yet to answer, and not
// a turn the agent began on its own: that includes the turn that carries on
// itself, so a restart that cuts the carried-on turn too is not answered
// with a third.
func isTurnCutShort(tx db.Transaction, conversation *models.AgentConversation, startedAt time.Time) (bool, error) {
	messages, err := tx.ListLatestAgentMessages(conversation.ID, resumeTailMessages)
	if err != nil {
		return false, err
	}
	// The notes a turn writes about itself while it works are not its end.
	for len(messages) > 0 && isTurnProgressNote(messages[len(messages)-1]) {
		messages = messages[:len(messages)-1]
	}
	if len(messages) == 0 {
		return false, nil
	}
	last := messages[len(messages)-1]
	if last.CreatedAt.Before(startedAt.Add(-resumeWindow)) || last.CreatedAt.After(startedAt) {
		return false, nil
	}
	switch last.Role {
	case "user", "tool":
	case "assistant":
		if len(last.ToolCalls) == 0 {
			return false, nil
		}
	default:
		return false, nil
	}
	var opening *models.AgentMessage
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			opening = messages[index]
			break
		}
	}
	if opening == nil || models.IsOwnTurn(opening.Content) || strings.TrimSpace(opening.Content) == "" {
		return false, nil
	}
	interactions, err := tx.ListOpenAgentInteractions(conversation.ID)
	if err != nil {
		return false, err
	}
	return len(interactions) == 0, nil
}

// isTurnProgressNote says a message is a note a turn writes while it
// works, which a turn cut short can end on.
func isTurnProgressNote(message *models.AgentMessage) bool {
	if message.Role != models.AgentMessageNote {
		return false
	}
	switch models.AgentNoteKind(message.Name) {
	case models.NoteDepth, models.NoteCompacting, models.NoteCompacted, models.NoteQueued:
		return true
	}
	return false
}

// waitForInteractiveTurns waits, for at most grace, until no turn a person
// is reading is in flight. A turn nobody is reading (a job's, a goal's) is
// not waited for: it is picked up again by the queue.
func (self *Agent) waitForInteractiveTurns(grace time.Duration) {
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !self.hasInteractiveTurn() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (self *Agent) hasInteractiveTurn() bool {
	self.runsMutex.Lock()
	defer self.runsMutex.Unlock()
	for _, run := range self.runs {
		if !run.settings.Headless && !run.settings.isUnattended && !run.isFinished() {
			return true
		}
	}
	return false
}
