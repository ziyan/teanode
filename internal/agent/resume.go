package agent

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// A restart can end a turn a person is waiting on: a deploy cancels the
// runs in flight (Stop), and the conversation is left ending on their
// message or on a tool's result, with no answer, until they type "try
// again". Such a conversation is carried on by a turn the agent starts for
// itself, on whichever instance claims the cut turn first: the one that
// restarted, or another that is still running.

const (
	// resumeWindow is how far back a turn may have been cut short and
	// still be carried on. Beyond it the person has long since asked
	// again or stopped waiting, and an answer out of the blue would be
	// noise.
	resumeWindow = 15 * time.Minute

	// resumeMostPerAgent is how many conversations one agent carries on
	// in one look: a restart cuts a few turns, never dozens.
	resumeMostPerAgent = 3

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

// resumeCutTurnsOnce carries on the turns that were cut short: those an
// instance stopped with, this one or another, and those whose instance went
// away without saying so (turn_record.go). Each cut turn is claimed by one
// instance, so a turn is carried on once however many instances look.
func (self *Agent) resumeCutTurnsOnce(ctx context.Context) {
	configuration := self.settings.Configuration()
	if self.settings.Database == nil || self.settings.Registry == nil || self.operations == nil || !FeatureAllowed(configuration, "ask") {
		return
	}
	now := time.Now()
	var claimed []*models.AgentTurn
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		claimed, err = tx.ClaimCutAgentTurns(now.Add(-turnStaleAfter), resumeClaimMost)
		return err
	}); err != nil {
		log.Warningf("cannot read the turns a restart cut short: %s", err)
		return
	}
	// A conversation once, as of the last of its turns that was cut, and
	// the newest first, for the few of each agent that are carried on.
	var cutTurns []*models.AgentTurn
	cutAtByConversation := map[string]time.Time{}
	for _, turn := range claimed {
		// Taken for lost because this instance was too slow to say it was
		// running (a stalled database), but it is: the heartbeat writes it
		// back, and carrying it on would answer twice.
		if turn.Instance == self.settings.Instance && self.isTurnRunningHere(turn.ConversationID) {
			continue
		}
		cutAt := turnCutMoment(turn, now)
		if now.Sub(cutAt) > resumeWindow {
			continue
		}
		previous, isKept := cutAtByConversation[turn.ConversationID]
		if !isKept {
			cutTurns = append(cutTurns, turn)
		}
		if !isKept || cutAt.After(previous) {
			cutAtByConversation[turn.ConversationID] = cutAt
		}
	}
	sort.SliceStable(cutTurns, func(first, second int) bool {
		firstCutAt := cutAtByConversation[cutTurns[first].ConversationID]
		secondCutAt := cutAtByConversation[cutTurns[second].ConversationID]
		if !firstCutAt.Equal(secondCutAt) {
			return firstCutAt.After(secondCutAt)
		}
		return cutTurns[first].StartedAt.After(cutTurns[second].StartedAt)
	})
	resumedCountByAgent := map[string]int{}
	for _, turn := range cutTurns {
		self.resumeCutTurn(ctx, turn.ConversationID, cutAtByConversation[turn.ConversationID], resumedCountByAgent)
	}
}

// resumeClaimMost is how many cut turns one look claims: a restart cuts a
// few turns, never dozens, and what is left is claimed by the next look.
const resumeClaimMost = 20

// turnCutMoment is when a turn was cut: when its instance said so, or, for
// one whose instance went away without saying so, the latest it can have
// been running, but not later than now.
func turnCutMoment(turn *models.AgentTurn, now time.Time) time.Time {
	if turn.CutAt != nil {
		return *turn.CutAt
	}
	cutAt := turn.HeartbeatAt.Add(turnStaleAfter)
	if cutAt.After(now) {
		return now
	}
	return cutAt
}

// resumeCutTurn starts the turn that carries on one conversation's cut turn,
// unless a turn already runs in it, or it does not end in a turn that was
// cut off before it answered. It returns once the turn is started rather
// than finished.
func (self *Agent) resumeCutTurn(ctx context.Context, conversationId string, cutAt time.Time, resumedCountByAgent map[string]int) {
	configuration := self.settings.Configuration()
	runId := security.NewULID()
	now := time.Now()
	var agent *models.Agent
	var owner *models.User
	var conversation *models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		// Two instances that claimed two cut turns of one conversation
		// take turns here, and the second finds the first one's turn.
		isLocked, err := tx.TryAdvisoryLock(db.LockKey("agent:resume:" + conversationId))
		if err != nil || !isLocked {
			return err
		}
		live, err := tx.ListLiveAgentTurns(conversationId, now.Add(-turnStaleAfter))
		if err != nil || len(live) > 0 {
			return err
		}
		if conversation, err = tx.GetAgentConversation(conversationId); err != nil || conversation == nil {
			return err
		}
		if conversation.ArchivedAt != nil || conversation.Kind != models.AgentConversationMain && conversation.Kind != models.AgentConversationNamed {
			return nil
		}
		if agent, err = tx.GetAgent(conversation.AgentID); err != nil || agent == nil || !agent.Active() {
			agent = nil
			return err
		}
		if resumedCountByAgent[agent.ID] >= resumeMostPerAgent {
			agent = nil
			return nil
		}
		if owner, err = tx.GetUser(agent.UserID); err != nil || owner == nil {
			return err
		}
		// Out of budget, the turn would only fail and write a note about
		// it: nothing is better than that.
		if err := RequireBudget(tx, configuration, agent, owner, now); err != nil {
			owner = nil
			return nil
		}
		isCut, err := isTurnCutShort(tx, conversation, cutAt)
		if err != nil || !isCut {
			owner = nil
			return err
		}
		// Recorded before it is started, in the same transaction, so that
		// the next instance to look finds it running.
		return tx.PutAgentTurn(&models.AgentTurn{
			RunID: runId, ConversationID: conversationId, Instance: self.settings.Instance,
			StartedAt: now, HeartbeatAt: now,
		})
	}); err != nil {
		log.Warningf("cannot look at the turn a restart cut short in conversation %q: %s", conversationId, err)
		return
	}
	if agent == nil || owner == nil {
		return
	}
	resumedCountByAgent[agent.ID]++
	operations, err := self.operations(ctx, owner)
	if err != nil {
		log.Warningf("cannot act as %q to carry on a turn a restart cut short: %s", owner.Username, err)
		self.forgetTurnRecord(runId)
		return
	}
	if _, err := self.Ask(&AskSettings{
		Agent: agent, Owner: owner, Operations: operations, Conversation: conversation,
		Message: resumeMessage, Surface: resumeSurface, RunID: runId,
	}); err != nil {
		log.Warningf("cannot carry on the turn a restart cut short in conversation %q: %s", conversationId, err)
		self.forgetTurnRecord(runId)
		return
	}
	log.Noticef("carrying on the turn a restart cut short in conversation %q", conversationId)
}

// forgetTurnRecord forgets a turn recorded for a run that was never started.
func (self *Agent) forgetTurnRecord(runId string) {
	if err := self.settings.Database.TransactionContext(context.WithoutCancel(self.ctx), func(tx db.Transaction) error {
		return tx.DeleteAgentTurn(runId)
	}); err != nil {
		log.Warningf("cannot forget the turn %q: %s", runId, err)
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
