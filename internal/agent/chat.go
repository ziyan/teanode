package agent

import (
	"github.com/ziyan/teanode/internal/models"
)

// What happens in a chat reaches its members through the feed of their own
// conversations. A chat has one row of what was said and a conversation for
// each member, so an event is published once for each member who is in the
// chat, to that member's conversation, with the message as that member reads
// it. A person taken out of the chat stops being published to at once, because
// the caller names only the members who are in it.
//
// The feed of a chat is the chat hub's (internal/chat), not the agent's: it
// has to exist on a server with no model, and so no agent. What an agent does
// in a chat, its answer posted there, the words of it as they are written and
// that it is thinking, goes out through the same hub, by PublishChatEvents
// below. The events of the agent's own turn in a chat conversation that are
// the owner's alone, its tool calls and results and its cards, stay on the
// agent's feed (chat_turn.go).
//
// docs/subsystems/streaming-and-instances.md lists the events.

// ChatPublisher tells everybody watching the conversations the events are for
// that something happened in their chat, on this instance and the others, and
// the phones of those who are not looking. The chat hub is one.
type ChatPublisher interface {
	PublishChatEvents(events []Event)

	// NotifyChatMessage tells the phones of the recipients what was said. A
	// person's own message does not notify them; an agent's message does,
	// whose answer its owner may be waiting for.
	NotifyChatMessage(message *models.AgentMessage, chatTitle string, isGroup bool, recipients []ChatRecipient)
}

// ChatRecipient is a member who may be told of a message on their phone.
type ChatRecipient struct {
	UserID         string
	AgentID        string
	ConversationID string

	// NotificationLevel is what the member asked to be told of in this chat:
	// every message, only one that mentions them, or nothing.
	NotificationLevel models.ChatNotificationLevel

	// IsConversationArchived says the member put the chat away: it is as
	// quiet for them as one turned off.
	IsConversationArchived bool
}

// PublishChatEvents hands events of a chat to the chat hub: what the agent
// posts in a chat, and that it is typing there. Each event is addressed to the
// conversation of one member, and names only members who are in the chat. None
// of them belongs to a run, so none is replayed.
func (self *Agent) PublishChatEvents(events []Event) {
	if self.settings.ChatEvents == nil {
		return
	}
	self.settings.ChatEvents.PublishChatEvents(events)
}

// FitEventForRelay cuts what is too long for a notification to the other
// instances, as the agent's own feed does for its events; see fitForRelay. The
// chat hub relays its events the same way.
func FitEventForRelay(event Event) Event {
	return fitForRelay(event)
}
