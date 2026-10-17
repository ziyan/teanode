package chat

import (
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// Recipient is a member who may be told of a message on their phone.
type Recipient = agent.ChatRecipient

// notificationTitle is who spoke, and in which chat when it has several
// people in it. An agent is named with its owner, since most are called
// "Agent" and a chat can have several.
func notificationTitle(message *models.AgentMessage, chatTitle string, isGroup bool) string {
	authorName := strings.TrimSpace(message.AuthorName)
	if message.AuthorKind == models.ChatAuthorAgent {
		authorName = models.AgentSpeakerLabel(message.AuthorName, message.AuthorOwnerName)
	}
	if !isGroup || strings.TrimSpace(chatTitle) == "" {
		return authorName
	}
	return authorName + " in " + strings.TrimSpace(chatTitle)
}

// notificationBody is the first line of what was said, or what came with it
// when nothing was.
func notificationBody(message *models.AgentMessage) string {
	for _, line := range strings.Split(message.Content, "\n") {
		if strings.TrimSpace(line) != "" {
			return agent.PhoneBody(line)
		}
	}
	if len(message.Attachments) > 0 {
		return "Sent a file"
	}
	return ""
}

// NotifyChatMessage tells the phones of the members what somebody said: of
// each member but the author who has not silenced the chat and is not
// looking at a dashboard or the app. An agent's message is told to its owner
// too, who may be waiting for the answer away from the screen. The phones are
// told in the background.
// The title is the same in every language: the notification is plain text
// sent to a phone that decrypts it, and the phone has no catalog to word it
// from.
func (self *Hub) NotifyChatMessage(message *models.AgentMessage, chatTitle string, isGroup bool, recipients []Recipient) {
	now := time.Now()
	for _, recipient := range recipients {
		if recipient.AgentID == "" || (recipient.UserID == message.AuthorUserID && message.AuthorKind != models.ChatAuthorAgent) {
			continue
		}
		if recipient.MutedUntil != nil && recipient.MutedUntil.After(now) {
			continue
		}
		if self.settings.Presence.IsWatching(recipient.UserID, time.Time{}, now) {
			continue
		}
		self.phones.NotifyPhones(recipient.AgentID, models.NotificationChat, &push.Content{
			NotificationKind: models.NotificationChat,
			ConversationID:   recipient.ConversationID,
			Title:            notificationTitle(message, chatTitle, isGroup),
			Body:             notificationBody(message),
			ThreadID:         recipient.ConversationID,
			Buttons:          []push.Button{{Label: "Reply", ButtonAction: "reply"}, {Label: "Open", ButtonAction: "open"}},
		})
	}
}
