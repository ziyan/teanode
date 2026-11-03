package chat

import (
	"strings"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

var (
	// What a phone shows of a call ringing it: who calls, and in a group
	// with a name, where.
	phraseCalling   = push.Phrase{English: "%s is calling", Chinese: "%s 来电", Japanese: "%sから着信"}
	phraseCallingIn = push.Phrase{English: "%s is calling in %s", Chinese: "%s 在%s中发起通话", Japanese: "%sが%sで通話を始めました"}
)

// RingChatCall rings the phones of the people a call rings, in the
// background: an incoming call on a phone that registered for ringing, an
// alert on one that did not. Each is worded in the phone's language. The
// content's CallID is the call's, which the app answers or declines.
func (self *Hub) RingChatCall(callId, callerName, chatTitle string, isGroup bool, recipients []Recipient) {
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		self.ringChatCall(callId, callerName, chatTitle, isGroup, recipients)
	}()
}

func (self *Hub) ringChatCall(callId, callerName, chatTitle string, isGroup bool, recipients []Recipient) {
	callerName, chatTitle = strings.TrimSpace(callerName), strings.TrimSpace(chatTitle)
	wording := func(language string) string {
		if isGroup && chatTitle != "" {
			return phraseCallingIn.In(language, callerName, chatTitle)
		}
		return phraseCalling.In(language, callerName)
	}
	for _, recipient := range recipients {
		if recipient.AgentID == "" {
			continue
		}
		self.phones.RingPhones(recipient.AgentID, &push.Content{
			NotificationKind: models.NotificationCall,
			ConversationID:   recipient.ConversationID,
			CallID:           callId,
			Title:            callerName,
			Body:             wording(push.LanguageEnglish),
			ThreadID:         recipient.ConversationID,
			Wording: func(content *push.Content, language string) {
				content.Body = wording(language)
			},
		})
	}
}

// UnringChatCall tells the phones of the people given that the call no
// longer rings them: they answered or declined it on another device, or it
// ended. The phone that answered or declined, when one did, is not told.
func (self *Hub) UnringChatCall(callId string, recipients []Recipient, exceptPhoneId string) {
	for _, recipient := range recipients {
		if recipient.AgentID == "" {
			continue
		}
		self.phones.UnringPhones(recipient.AgentID, &push.Content{
			NotificationKind: models.NotificationCallEnded,
			ConversationID:   recipient.ConversationID,
			CallID:           callId,
		}, exceptPhoneId)
	}
}
