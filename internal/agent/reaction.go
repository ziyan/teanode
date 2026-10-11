package agent

import (
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// PublishReaction tells everybody watching a conversation, on this
// instance and the others, that a reaction on one of its messages
// changed: the emoji now there from that reactor, or none. Called once
// the change is committed.
//
// It belongs to no run, as a title does not, so it has no run id and is
// never replayed: a drawer that opens later reads it with the messages.
func (self *Agent) PublishReaction(conversationId, messageId, reactionEmoji string, reactorKind models.AgentReactorKind) {
	self.publish(Event{
		Kind: EventReacted, ConversationID: conversationId, MessageID: messageId,
		Emoji: reactionEmoji, ReactorKind: string(reactorKind), At: time.Now(),
	}, true)
}

// TellReaction is PublishReaction for a tool, which reaches the agent
// through the run it is called in (tools.ReactionTelling).
func (self *AskRun) TellReaction(conversationId, messageId, reactionEmoji string, reactorKind models.AgentReactorKind) {
	self.agent.PublishReaction(conversationId, messageId, reactionEmoji, reactorKind)
}

// TellReaction is PublishReaction for a tool called outside a
// conversation, over the Model Context Protocol.
func (self *directRun) TellReaction(conversationId, messageId, reactionEmoji string, reactorKind models.AgentReactorKind) {
	self.agent.PublishReaction(conversationId, messageId, reactionEmoji, reactorKind)
}
