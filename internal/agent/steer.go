package agent

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Steering: what the person writes while a turn of theirs is running goes
// into that turn, not after it.
//
// A second message used to wait for the whole of the first turn, which on
// a turn of many tool calls meant the agent went on with what the first
// message asked long after the person had said "not that one, the other",
// and only answered the correction once it had finished the wrong thing.
// Now the running turn takes the message in at the start of its next round,
// once the tool calls it is making have come back, and the model reads it
// before it decides what to do next. A turn that was about to end goes one
// round more instead, so nothing said while it answered is left unread.
//
// The message's own run stays, so whoever sent it -- the command line, a
// chat app -- goes on reading the answer through it: it relays the running
// turn's events until that turn ends. Should the running turn end before
// it could take the message in, the message's run answers it on its own,
// as a queued turn always did.

// isSteerable says whether a turn is one a person is typing into: not a
// run with nobody present, and not a turn a background command woke.
func isSteerable(settings *AskSettings) bool {
	return !settings.Headless && settings.Surface != backgroundSurface
}

// steer hands this turn a message the person wrote while it runs, and says
// whether it will take it in. It will not once it has decided to end.
func (self *AskRun) steer(message *AskRun) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.finished || self.isClosedToSteering {
		return false
	}
	self.steering = append(self.steering, message)
	return true
}

// closeToSteering is the turn deciding to end, unless a message is waiting
// to be taken in; it says whether one is, and if none is, nothing more is
// handed to it.
func (self *AskRun) closeToSteering() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if len(self.steering) > 0 {
		return true
	}
	self.isClosedToSteering = true
	return false
}

// takeSteering stores what the person wrote since the last round, in the
// order they wrote it, and returns it as the model reads it.
func (self *AskRun) takeSteering(ctx context.Context) ([]llm.ChatMessage, error) {
	self.mutex.Lock()
	taken := self.steering
	self.steering = nil
	self.mutex.Unlock()
	messages := make([]llm.ChatMessage, 0, len(taken))
	for _, message := range taken {
		// One the person stopped before it was read is left unsaid.
		if message.ctx.Err() != nil {
			continue
		}
		var saved *models.AgentMessage
		if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			if saved, err = keepPersonTurn(tx, message.settings); err != nil {
				return err
			}
			_, err = tx.UpdateAgentConversation(message.settings.Conversation.ID, func(conversation *models.AgentConversation) error {
				conversation.LastAt = time.Now()
				return nil
			})
			return err
		}); err != nil {
			return messages, err
		}
		settings := message.settings
		turn := userTurn(ctx, self.agent.settings.Storage, personText(settings), settings.Attachments, settings.Pictures, settings.References)
		turn.SourceID = saved.ID
		messages = append(messages, turn)
		message.markTakenIn()
	}
	return messages, nil
}

// markTakenIn records that the running turn read this message.
func (self *AskRun) markTakenIn() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.isTakenIn = true
}

func (self *AskRun) wasTakenIn() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.isTakenIn
}

// followSteered is the loop of a message handed to a running turn: it
// relays that turn's events to whoever follows this run, and says whether
// the turn took the message in before it ended.
func (self *AskRun) followSteered(into *AskRun) bool {
	events, unsubscribe := into.Subscribe()
	defer unsubscribe()
	for {
		select {
		case event, open := <-events:
			if !open {
				return self.wasTakenIn()
			}
			// Its own end is the running turn's; this run says its own.
			if event.Kind != EventDone && event.Kind != EventAsked {
				self.relay(event)
			}
		case <-self.ctx.Done():
			return self.wasTakenIn()
		}
	}
}

// relay passes another run's event on to this run's followers only. The
// conversation's feed has it already, from the run that emitted it.
func (self *AskRun) relay(event Event) {
	event.RunID = self.ID
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.finished {
		return
	}
	event.Sequence = self.nextSequence
	self.nextSequence++
	if len(self.events) < askEventBacklog {
		self.events = append(self.events, event)
	}
	for _, channel := range self.subscribers {
		select {
		case channel <- event:
		default:
		}
	}
}

// handBackSteering is a turn ending without reading what was handed to it:
// on an error, a stop, the most rounds it may take. Each message is then a
// turn of its own, one after another as they were written, and the last
// is what the next one waits for.
func (self *AskRun) handBackSteering() {
	self.mutex.Lock()
	pending := self.steering
	self.steering = nil
	self.isClosedToSteering = true
	self.mutex.Unlock()
	if len(pending) == 0 {
		return
	}
	// Set before this run's followers are told it ended, which is when the
	// messages' runs read it.
	for index := 1; index < len(pending); index++ {
		pending[index].previous = pending[index-1]
	}
	self.agent.runsMutex.Lock()
	if self.agent.latest[self.settings.Conversation.ID] == self {
		self.agent.latest[self.settings.Conversation.ID] = pending[len(pending)-1]
	}
	self.agent.runsMutex.Unlock()
}

// SteeredInto is the running turn this message was handed to, or nil when
// it is a turn of its own.
func (self *AskRun) SteeredInto() *AskRun {
	return self.steeredInto
}

// keepPersonTurn stores what the person said, with the files and threads
// that came with it.
func keepPersonTurn(tx db.Transaction, settings *AskSettings) (*models.AgentMessage, error) {
	// The references as they are kept and then told, in the transaction
	// that keeps them: a finance transaction that is not the agent's is
	// dropped, and one that is carries what the model is told about it.
	if len(settings.References) > 0 {
		references, err := resolveReferences(tx, settings.Agent.ID, settings.Conversation.ID, settings.References)
		if err != nil {
			return nil, err
		}
		settings.References = references
	}
	stored := &models.AgentMessage{ConversationID: settings.Conversation.ID, Role: string(llm.RoleUser), Content: settings.Message, References: settings.References}
	attachmentIds := make([]string, 0, len(settings.Attachments))
	for _, attachment := range settings.Attachments {
		stored.Attachments = append(stored.Attachments, *attachment)
		attachmentIds = append(attachmentIds, attachment.ID)
	}
	saved, err := tx.AppendAgentMessage(stored)
	if err != nil {
		return nil, err
	}
	if err := tx.ClaimAgentAttachments(attachmentIds, settings.Conversation.ID, saved.ID); err != nil {
		return nil, err
	}
	// The person writing is what lets ended background commands and
	// finished background work wake the conversation again. In the
	// transaction that keeps what they wrote, so that a wake on any
	// instance reads the two together.
	if isPersonWriting(settings) {
		if err := tx.ResetAgentConversationBackgroundWakes(settings.Conversation.ID); err != nil {
			return nil, err
		}
	}
	return saved, nil
}

// isPersonWriting says the message a turn keeps is the person's own: not
// a turn with nobody present, and not one an ended command or finished
// work woke.
func isPersonWriting(settings *AskSettings) bool {
	return !settings.Headless && settings.Surface != backgroundSurface
}
