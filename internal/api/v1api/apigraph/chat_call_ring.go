package apigraph

import (
	"context"
	"slices"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// ringChatCall rings the phones of the people a call rings, once it is
// committed. Their dashboards ring from ChatCallEvents.
func (self *graph) ringChatCall(ctx context.Context, session *chatSession, chatCall *models.ChatCall, rungUserIds []string) {
	hub := self.chatHub()
	if hub == nil || len(rungUserIds) == 0 {
		return
	}
	var rung []*models.ChatMember
	for _, member := range session.Chat.ActiveMembers() {
		if slices.Contains(rungUserIds, member.UserID) {
			rung = append(rung, member)
		}
	}
	recipients := recipientsOf(rung)
	callId, callerName, isGroup := chatCall.ID, session.Member.Name, session.Chat.ChatKind == models.ChatKindGroup
	// The group's own name, the same for everybody it rings; a direct chat
	// is named by the caller.
	chatTitle := db.ChatViewOf(session.Chat, session.User.ID).Title
	self.transaction(ctx).AfterCommit(func() {
		hub.RingChatCall(callId, callerName, chatTitle, isGroup, recipients)
	})
}

// unringChatCall tells the phones of the people given that the call no
// longer rings them, once the change is committed.
func (self *graph) unringChatCall(ctx context.Context, chatCall *models.ChatCall, userIds []string) {
	hub := self.chatHub()
	if hub == nil || len(userIds) == 0 {
		return
	}
	chat, err := self.transaction(ctx).GetChat(chatCall.ChatID)
	if err != nil || chat == nil {
		return
	}
	var told []*models.ChatMember
	for _, member := range chat.Members {
		if slices.Contains(userIds, member.UserID) {
			told = append(told, member)
		}
	}
	recipients, callId := recipientsOf(told), chatCall.ID
	self.transaction(ctx).AfterCommit(func() { hub.UnringChatCall(callId, recipients) })
}

// rungUserIds are the people a call rang, whose phones may still ring.
func rungUserIds(chatCall *models.ChatCall) []string {
	var userIds []string
	for _, member := range chatCall.Members {
		if member.RingState != models.ChatCallRingNone {
			userIds = append(userIds, member.UserID)
		}
	}
	return userIds
}
