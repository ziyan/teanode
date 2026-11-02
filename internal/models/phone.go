package models

import (
	"strings"
	"time"
)

// AgentPhone is a phone a person talks to their agent from, in the TeaNode
// app: one per install, tied to the OAuth client the app registered as.
type AgentPhone struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	AgentID  string `json:"agentId"`
	ClientID string `json:"clientId"`

	PhoneName  string `json:"phoneName"`
	PhoneModel string `json:"phoneModel"`

	// PushRegistration is the relay's sealed registration, empty once Apple
	// said the phone is gone. Never shown to anybody.
	PushRegistration string `json:"-"`

	// VoIPPushRegistration is the relay's sealed registration for the token
	// PushKit gave the app, which is how a phone is rung for a call. Empty
	// while the app has not registered one, or once Apple said it is gone.
	// Never shown to anybody.
	VoIPPushRegistration string `json:"-"`

	// PayloadKey is the phone's notification key, sealed. Never shown.
	PayloadKey string `json:"-"`

	// NotificationKinds are the kinds the phone wants; empty is all.
	NotificationKinds []string `json:"notificationKinds"`

	// PushedThrough is how far the agent's own turns have been sent.
	PushedThrough string `json:"-"`

	LastSeenAt        *time.Time `json:"lastSeenAt,omitempty"`
	LastDeliveredAt   *time.Time `json:"lastDeliveredAt,omitempty"`
	LastDeliveryError string     `json:"lastDeliveryError,omitempty"`

	// TouredAt is when the agent showed the person around the phone, once,
	// after it connected.
	TouredAt *time.Time `json:"touredAt,omitempty"`
}

// Kinds of notification a phone may receive.
const (
	// NotificationMessage is the agent saying something on its own: an
	// alert, a goal that needs the person, the agent starting a conversation.
	NotificationMessage = "message"
	// NotificationApproval is a card waiting for the person's approval.
	NotificationApproval = "approval"
	// NotificationQuestion is the agent asking the person something, and one
	// of their coding sessions asking (which opens the session on the
	// iPhone's Coding screen).
	NotificationQuestion = "question"
	// NotificationAnswer is an answer finished after the person left: to
	// something asked from the app, or asked in the main conversation on
	// the dashboard.
	NotificationAnswer = "answer"
	// NotificationCircle is an invitation to join a circle.
	NotificationCircle = "circle"
	// NotificationChat is a message from another person in a chat.
	NotificationChat = "chat"
	// NotificationSkillShareCall is a call of a shared skill waiting for its
	// owner's approval, and how it came out for the person who made it. It
	// opens the app and never answers from the notification: a lock screen
	// tap must not spend money.
	NotificationSkillShareCall = "skill_share_call"
	// NotificationPhoto is photos another person added to an album the
	// person reads.
	NotificationPhoto = "photo"

	// NotificationLocation is a person who shares their location arriving at
	// or leaving a place they named or labeled. It opens the app on Location
	// with that person.
	NotificationLocation = "location"

	// NotificationShopping is what the person's shopping lists need at a
	// store they just arrived at, and somebody starting to shop a list the
	// person shares. It opens the app on Shopping, at that store or list.
	NotificationShopping = "shopping"

	// NotificationCall is a call ringing: somebody started a call in a chat
	// the person is in. It is not one of NotificationKinds a phone chooses
	// among, and a phone's own choice never drops it, since a call that does
	// not ring is a missed call. Whether to ring a member at all is for
	// whoever starts the call to decide: a member who turned the chat's
	// notifications off is never rung.
	NotificationCall = "call"

	// NotificationCallEnded tells a phone still ringing for a call to stop:
	// the person answered or declined on another device, or the call ended.
	// It goes only to phones that can be rung, and is never shown.
	NotificationCallEnded = "call_ended"

	// NotificationJournalEveningQuestion is the journal's "How was today?",
	// with five mood buttons. It is not one of NotificationKinds a phone
	// chooses among: the person turns it on and off, and sets its time, in the
	// journal's settings, so a phone that chose only some kinds still gets it.
	NotificationJournalEveningQuestion = "journalEveningQuestion"
)

// NotificationKinds is every kind, in the order a person chooses among them.
var NotificationKinds = []string{NotificationMessage, NotificationApproval, NotificationQuestion, NotificationAnswer, NotificationCircle, NotificationChat, NotificationSkillShareCall, NotificationPhoto, NotificationLocation, NotificationShopping}

// Wants reports whether the phone takes a kind of notification.
func (self *AgentPhone) Wants(notificationKind string) bool {
	if notificationKind == NotificationJournalEveningQuestion || notificationKind == NotificationCall || notificationKind == NotificationCallEnded || len(self.NotificationKinds) == 0 {
		return true
	}
	for _, wanted := range self.NotificationKinds {
		if wanted == notificationKind {
			return true
		}
	}
	return false
}

// CanBeNotified reports whether the phone has a registration to send to.
func (self *AgentPhone) CanBeNotified() bool {
	return strings.TrimSpace(self.PushRegistration) != "" && self.PayloadKey != ""
}

// CanRing reports whether the phone can be rung for a call: it has a VoIP
// registration and a key to seal the call's content with.
func (self *AgentPhone) CanRing() bool {
	return strings.TrimSpace(self.VoIPPushRegistration) != "" && self.PayloadKey != ""
}
