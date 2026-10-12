package models

import (
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/util/emoji"
)

// AgentReactorKind is who put a reaction on a message of a conversation.
type AgentReactorKind string

// The two who react: the person whose conversation it is, and their agent.
const (
	ReactorPerson AgentReactorKind = "person"
	ReactorAgent  AgentReactorKind = "agent"
)

// AgentMessageReaction is an emoji put on a message, as in a chat app: one
// per reactor per message, replaced by reacting again.
type AgentMessageReaction struct {
	Emoji       string           `json:"emoji"`
	ReactorKind AgentReactorKind `json:"reactorKind"`
	ReactedAt   time.Time        `json:"reactedAt"`
}

// CanBeReactedToBy says whether a reactor may put a reaction on the
// message: each side reacts to what the other said, as in a chat app, so
// the person to the agent's messages and the agent to the person's. Nobody
// reacts to their own words, nor to a tool's result or a note.
func (self *AgentMessage) CanBeReactedToBy(reactorKind AgentReactorKind) bool {
	switch reactorKind {
	case ReactorPerson:
		return self.Role == "assistant"
	case ReactorAgent:
		return self.Role == "user"
	}
	return false
}

// The bounds of a reaction's emoji. Two allows a pair a person means
// together, such as a heart and a pair of hands; more than that is a
// message, not a reaction.
const (
	ReactionEmojiMaximumCount = 2
	ReactionEmojiMaximumBytes = 32
)

// ValidateReactionEmoji says why an emoji cannot be a reaction, or nil
// when it can. An empty one is refused here: it means taking a reaction
// away, which the caller decides before asking.
func ValidateReactionEmoji(reactionEmoji string) error {
	if reactionEmoji == "" {
		return fmt.Errorf("a reaction needs an emoji")
	}
	if len(reactionEmoji) > ReactionEmojiMaximumBytes {
		return fmt.Errorf("a reaction is at most %d bytes of emoji", ReactionEmojiMaximumBytes)
	}
	found, err := emoji.Split(reactionEmoji)
	if err != nil {
		return fmt.Errorf("a reaction is emoji and nothing else, and %q is not", reactionEmoji)
	}
	if len(found) > ReactionEmojiMaximumCount {
		return fmt.Errorf("a reaction is at most %d emoji, and %q is %d", ReactionEmojiMaximumCount, reactionEmoji, len(found))
	}
	return nil
}
