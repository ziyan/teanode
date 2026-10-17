package apigraph

import (
	"context"
	"slices"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/chat"
	"github.com/ziyan/teanode/internal/db"
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
			IsArchived: member.IsConversationArchived,
		})
	}
	return recipients
}

// notifyChatMessage tells the members' phones what was said, through the hub.
func notifyChatMessage(hub *chat.Hub, message *models.AgentMessage, chatTitle string, isGroup bool, recipients []chat.Recipient) {
	hub.NotifyChatMessage(message, chatTitle, isGroup, recipients)
}

// publishMembersChanged tells the conversations a change to who is in a chat
// concerns, once the change has committed, that the people there changed: the
// screens that have the chat open read its members again, and a person taken
// out of it learns they were. Without it a person who was added, removed or
// who left was out of date until they reloaded.
func (self *graph) publishMembersChanged(ctx context.Context, conversationIds ...string) {
	hub := self.chatHub()
	if hub == nil {
		return
	}
	events := []agent.Event{}
	seen := map[string]bool{}
	for _, conversationId := range conversationIds {
		if conversationId == "" || seen[conversationId] {
			continue
		}
		seen[conversationId] = true
		events = append(events, agent.Event{Kind: agent.EventMembers, ConversationID: conversationId})
	}
	if len(events) == 0 {
		return
	}
	self.transaction(ctx).AfterCommit(func() { hub.PublishChatEvents(events) })
}

// activeConversationsOf are the conversations of the people in the chat now.
func activeConversationsOf(chat *models.Chat) []string {
	conversationIds := []string{}
	if chat == nil {
		return conversationIds
	}
	for _, member := range chat.ActiveMembers() {
		if member.ConversationID != "" {
			conversationIds = append(conversationIds, member.ConversationID)
		}
	}
	return conversationIds
}

// publishChatMembersChanged tells everybody in the chat now, and the people
// named, who are in it no longer.
func (self *graph) publishChatMembersChanged(ctx context.Context, chatId string, alsoUserIds ...string) error {
	chat, err := self.transaction(ctx).GetChat(chatId)
	if err != nil || chat == nil {
		return err
	}
	conversationIds := activeConversationsOf(chat)
	for _, member := range chat.Members {
		if member.IsRemoved && member.ConversationID != "" && slices.Contains(alsoUserIds, member.UserID) {
			conversationIds = append(conversationIds, member.ConversationID)
		}
	}
	self.publishMembersChanged(ctx, conversationIds...)
	return nil
}

// publishCircleChatMembersChanged tells everybody in the circle's chat now.
func (self *graph) publishCircleChatMembersChanged(ctx context.Context, circleId string) error {
	chat, err := self.transaction(ctx).GetCircleChat(circleId)
	if err != nil {
		return err
	}
	self.publishMembersChanged(ctx, activeConversationsOf(chat)...)
	return nil
}

// chatsBeforeLeaving is who was in each chat the people were in, taken before
// a change that may take them out of some: after it, a chat that is gone or
// that a person is out of is told to everybody who was in it, the person
// included, and the chats they are still in are left alone.
type chatsBeforeLeaving struct {
	userIds []string
	chats   map[string][]string
}

func (self *graph) snapshotChatsOf(tx db.Transaction, userIds ...string) (*chatsBeforeLeaving, error) {
	snapshot := &chatsBeforeLeaving{userIds: userIds, chats: map[string][]string{}}
	for _, userId := range userIds {
		chats, err := tx.ListChatsOf(userId)
		if err != nil {
			return nil, err
		}
		for _, chat := range chats {
			snapshot.chats[chat.ID] = activeConversationsOf(chat)
		}
	}
	return snapshot, nil
}

// publishMembersChangedSince tells the chats the snapshot's people were taken
// out of, or that went with them, who is in them now.
func (self *graph) publishMembersChangedSince(ctx context.Context, snapshot *chatsBeforeLeaving) error {
	tx := self.transaction(ctx)
	for chatId, before := range snapshot.chats {
		chat, err := tx.GetChat(chatId)
		if err != nil {
			return err
		}
		isChanged := chat == nil
		for _, userId := range snapshot.userIds {
			if chat != nil {
				if member := chat.Member(userId); member != nil && member.IsRemoved {
					isChanged = true
				}
			}
		}
		if !isChanged {
			continue
		}
		self.publishMembersChanged(ctx, append(slices.Clone(before), activeConversationsOf(chat)...)...)
	}
	return nil
}
