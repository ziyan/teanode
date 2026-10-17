package apigraph

import (
	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/chat"
	"github.com/ziyan/teanode/internal/models"
)

// chatHub is what carries a chat to its members, whatever the agent
// configuration is, or nil in a test that has no chats. The hub is the one
// the server made at start and handed to the settings.
func (self *graph) chatHub() *chat.Hub {
	if self.settings == nil || self.settings.Chat == nil {
		return nil
	}
	hub, ok := self.settings.Chat.(*chat.Hub)
	if !ok {
		return nil
	}
	return hub
}

// phoneNotifier is what sends a notification to a phone, or nil when the graph
// has neither a hub nor a worker. Both hold the same one in a running server.
func (self *graph) phoneNotifier() *agent.PhoneNotifier {
	if hub := self.chatHub(); hub != nil {
		return hub.Phones()
	}
	if worker := self.agentWorker(); worker != nil {
		return worker.Phones()
	}
	return nil
}

// eventsForMembers is an event for each member who is in the chat, addressed
// to their own conversation; see chat.EventsForMembers.
func eventsForMembers(members []*models.ChatMember, build func(member *models.ChatMember) agent.Event) []agent.Event {
	return chat.EventsForMembers(members, build)
}

// recipientsOf are the members as the phone notifications know them: the ones
// in the chat who have a conversation to open it on.
func recipientsOf(members []*models.ChatMember) []chat.Recipient {
	recipients := []chat.Recipient{}
	for _, member := range members {
		if member.IsRemoved || member.ConversationID == "" {
			continue
		}
		recipients = append(recipients, chat.Recipient{
			UserID: member.UserID, AgentID: member.AgentID, ConversationID: member.ConversationID, MutedUntil: member.MutedUntil,
		})
	}
	return recipients
}

// notifyChatMessage tells the members' phones what was said, through the hub.
func notifyChatMessage(hub *chat.Hub, message *models.AgentMessage, chatTitle string, isGroup bool, recipients []chat.Recipient) {
	hub.NotifyChatMessage(message, chatTitle, isGroup, recipients)
}
