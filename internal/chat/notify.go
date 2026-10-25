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

// The fixed words of a chat's notification.
var (
	phraseSpokeIn  = push.Phrase{English: "%s in %s", Chinese: "%s（%s）", Japanese: "%s（%s）"}
	phraseSentFile = push.Phrase{English: "Sent a file", Chinese: "发送了文件", Japanese: "ファイルを送信しました"}

	// phraseOwnersAgent is somebody's agent, as the dashboard says it.
	phraseOwnersAgent = push.Phrase{English: "%s's agent", Chinese: "%s 的助手", Japanese: "%s のエージェント"}
)

// notificationTitle is who spoke, and in which chat when it has several
// people in it, in a language as push.PhoneLanguage names it. An agent is
// named with its owner, since most are called "Agent" and a chat can have
// several.
func notificationTitle(message *models.AgentMessage, chatTitle string, isGroup bool, language string) string {
	authorName := strings.TrimSpace(message.AuthorName)
	if message.AuthorKind == models.ChatAuthorAgent {
		authorName = agentSpeakerLabel(message.AuthorName, message.AuthorOwnerName, language)
	}
	if !isGroup || strings.TrimSpace(chatTitle) == "" {
		return authorName
	}
	return phraseSpokeIn.In(language, authorName, strings.TrimSpace(chatTitle))
}

// agentSpeakerLabel is models.AgentSpeakerLabel in a language: an agent
// named with whose it is, "<agent name> · <owner name>'s agent", or only
// whose while it keeps the default name.
func agentSpeakerLabel(agentName, ownerName, language string) string {
	if language == push.LanguageEnglish {
		return models.AgentSpeakerLabel(agentName, ownerName)
	}
	agentName, ownerName = strings.TrimSpace(agentName), strings.TrimSpace(ownerName)
	if ownerName == "" {
		return agentName
	}
	owned := phraseOwnersAgent.In(language, ownerName)
	if agentName == "" || agentName == models.AgentDefaultName {
		return owned
	}
	return agentName + " · " + owned
}

// notificationBody is the first line of what was said, or what came with it
// when nothing was, in a language as push.PhoneLanguage names it.
func notificationBody(message *models.AgentMessage, language string) string {
	for _, line := range strings.Split(message.Content, "\n") {
		if strings.TrimSpace(line) != "" {
			return agent.PhoneBody(line)
		}
	}
	if len(message.Attachments) > 0 {
		return phraseSentFile.In(language)
	}
	return ""
}

// NotifyChatMessage tells the phones of the members what somebody said: of
// each member but the author who has not silenced or archived the chat and is not
// looking at that chat in a dashboard or the app: being in the app on another
// screen, or in another conversation, does not hide a new message. An agent's message is told to its owner
// too, who may be waiting for the answer away from the screen. The phones are
// told in the background.
// Each member is told in their own language.
func (self *Hub) NotifyChatMessage(message *models.AgentMessage, chatTitle string, isGroup bool, recipients []Recipient) {
	now := time.Now()
	for _, recipient := range recipients {
		if recipient.AgentID == "" || (recipient.UserID == message.AuthorUserID && message.AuthorKind != models.ChatAuthorAgent) {
			continue
		}
		if recipient.IsConversationArchived || (recipient.MutedUntil != nil && recipient.MutedUntil.After(now)) {
			continue
		}
		if self.settings.Presence.IsWatchingConversation(recipient.UserID, recipient.ConversationID, now) {
			continue
		}
		self.phones.NotifyPhones(recipient.AgentID, models.NotificationChat, &push.Content{
			NotificationKind: models.NotificationChat,
			ConversationID:   recipient.ConversationID,
			Title:            notificationTitle(message, chatTitle, isGroup, push.LanguageEnglish),
			Body:             notificationBody(message, push.LanguageEnglish),
			ThreadID:         recipient.ConversationID,
			Buttons:          []push.Button{{Label: "Reply", ButtonAction: "reply"}, {Label: "Open", ButtonAction: "open"}},
			Wording: func(content *push.Content, language string) {
				content.Title = notificationTitle(message, chatTitle, isGroup, language)
				content.Body = notificationBody(message, language)
			},
		})
	}
}
