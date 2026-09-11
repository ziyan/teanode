package models

import "time"

// AgentChannelKind is a chat app a person may talk to their agent from.
type AgentChannelKind string

const (
	AgentChannelTelegram AgentChannelKind = "telegram"
	AgentChannelDiscord  AgentChannelKind = "discord"
)

// AgentChannelKinds are the apps there are.
var AgentChannelKinds = []AgentChannelKind{AgentChannelTelegram, AgentChannelDiscord}

// AgentChannel is a person's bot in a chat app, through which they talk
// to their agent's primary conversation: the bot's token, sealed, and the
// one chat linked to it with a code.
type AgentChannel struct {
	ID         string           `json:"id"`
	CreatedAt  time.Time        `json:"createdAt"`
	ModifiedAt time.Time        `json:"modifiedAt"`
	AgentID    string           `json:"agentId"`
	Kind       AgentChannelKind `json:"kind"`

	// Token is the bot's, sealed with the server secret; never shown.
	Token string `json:"-"`

	// BotName is what the app calls the bot, once the bot has been seen.
	BotName string `json:"botName,omitempty"`

	// LinkedID is the chat (Telegram) or the user (Discord) linked to the
	// bot; LinkedName what it was called when it linked. Empty until the
	// code has been sent from a chat.
	LinkedID   string `json:"linkedId,omitempty"`
	LinkedName string `json:"linkedName,omitempty"`

	// LinkCode is what a chat sends the bot to become the linked one; a
	// new one is drawn whenever the link is dropped.
	LinkCode string `json:"linkCode,omitempty"`

	Enabled    bool       `json:"enabled"`
	LastError  string     `json:"lastError,omitempty"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`

	// ClaimedBy is the server instance running the bot, and ClaimedUntil
	// when its claim lapses unless renewed: one instance runs a bot.
	ClaimedBy    string     `json:"claimedBy,omitempty"`
	ClaimedUntil *time.Time `json:"claimedUntil,omitempty"`
}

// Linked says whether a chat has linked itself to the bot.
func (self *AgentChannel) Linked() bool {
	return self != nil && self.LinkedID != ""
}
