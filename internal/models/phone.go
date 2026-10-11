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

	// PayloadKey is the phone's notification key, sealed. Never shown.
	PayloadKey string `json:"-"`

	// NotificationKinds are the kinds the phone wants; empty is all.
	NotificationKinds []string `json:"notificationKinds"`

	// PushedThrough is how far the agent's own turns have been sent.
	PushedThrough string `json:"-"`

	LastSeenAt        *time.Time `json:"lastSeenAt,omitempty"`
	LastDeliveredAt   *time.Time `json:"lastDeliveredAt,omitempty"`
	LastDeliveryError string     `json:"lastDeliveryError,omitempty"`
}

// Kinds of notification a phone may receive.
const (
	// NotificationMessage is the agent saying something on its own: an
	// alert, a coding session's question, a goal that needs the person, the
	// agent starting a conversation.
	NotificationMessage = "message"
	// NotificationApproval is a card waiting for the person's approval.
	NotificationApproval = "approval"
	// NotificationQuestion is the agent asking the person something.
	NotificationQuestion = "question"
	// NotificationAnswer is an answer finished after the person left: to
	// something asked from the app, or asked in the main conversation on
	// the dashboard, the command line or the API.
	NotificationAnswer = "answer"
)

// NotificationKinds is every kind, in the order a person chooses among them.
var NotificationKinds = []string{NotificationMessage, NotificationApproval, NotificationQuestion, NotificationAnswer}

// Wants reports whether the phone takes a kind of notification.
func (self *AgentPhone) Wants(notificationKind string) bool {
	if len(self.NotificationKinds) == 0 {
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
